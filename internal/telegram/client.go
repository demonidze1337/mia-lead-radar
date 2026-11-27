package telegram

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gotd/td/session"
	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/auth"
	"github.com/gotd/td/telegram/auth/qrlogin"
	"github.com/gotd/td/tg"

	"github.com/demonidze1337/mia-lead-radar/internal/domain"
)

// MTProtoConfig configures one persistent gotd/td account session.
type MTProtoConfig struct {
	AccountID   int64
	AppID       int
	AppHash     string
	SessionsDir string
}

// MTProtoClient owns a gotd/td client, its session file, and interactive auth state.
type MTProtoClient struct {
	cfg        MTProtoConfig
	client     *telegram.Client
	dispatcher tg.UpdateDispatcher
	ready      chan struct{}
	runDone    chan error
	session    string

	mu       sync.RWMutex
	started  bool
	state    AuthSessionState
	phone    string
	codeHash string
}

// NewMTProtoClient creates a persistent gotd/td client without connecting.
func NewMTProtoClient(cfg MTProtoConfig) (*MTProtoClient, error) {
	if cfg.AccountID <= 0 {
		return nil, fmt.Errorf("%w: account id is required", domain.ErrInvalidInput)
	}
	if cfg.AppID <= 0 || strings.TrimSpace(cfg.AppHash) == "" {
		return nil, fmt.Errorf("%w: Telegram app id and hash are required", domain.ErrInvalidInput)
	}
	if strings.TrimSpace(cfg.SessionsDir) == "" {
		return nil, fmt.Errorf("%w: sessions directory is required", domain.ErrInvalidInput)
	}
	if err := os.MkdirAll(cfg.SessionsDir, 0o700); err != nil {
		return nil, fmt.Errorf("telegram: create sessions dir: %w", err)
	}
	sessionPath := filepath.Join(cfg.SessionsDir, fmt.Sprintf("account_%d.session", cfg.AccountID))
	dispatcher := tg.NewUpdateDispatcher()
	client := telegram.NewClient(cfg.AppID, cfg.AppHash, telegram.Options{
		SessionStorage: &session.FileStorage{Path: sessionPath},
		UpdateHandler:  dispatcher,
		Device:         telegram.DeviceTDesktopWindows(),
	})
	c := &MTProtoClient{
		cfg:        cfg,
		client:     client,
		dispatcher: dispatcher,
		ready:      make(chan struct{}),
		runDone:    make(chan error, 1),
		session:    sessionPath,
		state: AuthSessionState{
			SessionKey: fmt.Sprintf("account-%d", cfg.AccountID),
			AccountID:  cfg.AccountID,
			Step:       AuthStepIdle,
			UpdatedAt:  time.Now().UTC(),
		},
	}
	return c, nil
}

// SessionPath returns the persisted session file path (never logged).
func (c *MTProtoClient) SessionPath() string { return c.session }

// Start launches the MTProto connection loop and waits until it is ready.
func (c *MTProtoClient) Start(ctx context.Context) error {
	if c == nil || c.client == nil {
		return fmt.Errorf("%w: MTProto client is not configured", domain.ErrInvalidInput)
	}
	c.mu.Lock()
	if c.started {
		c.mu.Unlock()
		select {
		case <-c.ready:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	c.started = true
	c.mu.Unlock()

	go func() {
		err := c.client.Run(ctx, func(runCtx context.Context) error {
			close(c.ready)
			<-runCtx.Done()
			return runCtx.Err()
		})
		c.runDone <- err
	}()

	select {
	case <-c.ready:
		if status, err := c.client.Auth().Status(ctx); err == nil && status.Authorized {
			c.setAuthorizedUser(status.User)
		}
		return nil
	case err := <-c.runDone:
		if errors.Is(err, context.Canceled) {
			return ctx.Err()
		}
		return fmt.Errorf("telegram: start: %w", err)
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Close terminates the underlying MTProto client and its session.
func (c *MTProtoClient) Close() error {
	if c == nil || c.client == nil {
		return nil
	}
	return c.client.Close()
}

// Transport returns the gotd-backed AccountTransport implementation.
func (c *MTProtoClient) Transport() AccountTransport {
	return &gotdTransport{accountID: c.cfg.AccountID, api: c.client.API(), client: c.client}
}

// State returns a copy of the current safe auth snapshot.
func (c *MTProtoClient) State() AuthSessionState {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.state
}

// Auth returns the interactive authentication coordinator.
func (c *MTProtoClient) Auth() *AuthManager {
	return &AuthManager{client: c}
}

// AuthManager drives QR / phone / code / 2FA login flows for the Setup Wizard.
type AuthManager struct {
	client *MTProtoClient
}

// StartQR begins QR login and publishes fresh QR URLs via AuthSessionState.
func (a *AuthManager) StartQR(ctx context.Context) error {
	c := a.client
	if c == nil || !c.isStarted() {
		return fmt.Errorf("%w: MTProto client is not started", domain.ErrInvalidInput)
	}
	c.setState(func(s *AuthSessionState) {
		s.Step = AuthStepQRPending
		s.Error = ""
	})
	loggedIn := qrlogin.OnLoginToken(c.dispatcher)
	go func() {
		authorization, err := c.client.QR().Auth(ctx, loggedIn, func(_ context.Context, token qrlogin.Token) error {
			c.setState(func(s *AuthSessionState) {
				s.Step = AuthStepQRPending
				s.QRURL = token.URL()
				s.Error = ""
			})
			return nil
		})
		if err != nil {
			if ctx.Err() != nil {
				c.setState(func(s *AuthSessionState) { s.Step = AuthStepCancelled })
				return
			}
			c.setState(func(s *AuthSessionState) {
				s.Step = AuthStepFailed
				s.Error = err.Error()
			})
			return
		}
		c.setAuthorized(authorization)
	}()
	return nil
}

// SubmitPhone requests a login code for the given phone number.
func (a *AuthManager) SubmitPhone(ctx context.Context, phone string) error {
	c := a.client
	if c == nil || !c.isStarted() {
		return fmt.Errorf("%w: MTProto client is not started", domain.ErrInvalidInput)
	}
	phone = strings.TrimSpace(phone)
	if phone == "" {
		return fmt.Errorf("%w: phone is required", domain.ErrInvalidInput)
	}
	sent, err := c.client.Auth().SendCode(ctx, phone, auth.SendCodeOptions{})
	if err != nil {
		c.failState(err)
		return fmt.Errorf("telegram: send code: %w", err)
	}
	sentCode, ok := sent.(*tg.AuthSentCode)
	if !ok {
		err = fmt.Errorf("telegram: unexpected sent code type %T", sent)
		c.failState(err)
		return err
	}
	c.mu.Lock()
	c.phone = phone
	c.codeHash = sentCode.PhoneCodeHash
	c.mu.Unlock()
	c.setState(func(s *AuthSessionState) {
		s.Step = AuthStepPhoneCode
		s.PhoneMasked = maskPhone(phone)
		s.Error = ""
	})
	return nil
}

// SubmitCode completes phone login or transitions to 2FA when required.
func (a *AuthManager) SubmitCode(ctx context.Context, code string) error {
	c := a.client
	if c == nil || !c.isStarted() {
		return fmt.Errorf("%w: MTProto client is not started", domain.ErrInvalidInput)
	}
	c.mu.RLock()
	phone, hash := c.phone, c.codeHash
	c.mu.RUnlock()
	if phone == "" || hash == "" {
		return fmt.Errorf("%w: phone login has not been started", domain.ErrInvalidInput)
	}
	authorization, err := c.client.Auth().SignIn(ctx, phone, strings.TrimSpace(code), hash)
	if err != nil {
		if errors.Is(err, auth.ErrPasswordAuthNeeded) {
			c.setState(func(s *AuthSessionState) {
				s.Step = AuthStep2FA
				s.Error = ""
			})
			return nil
		}
		c.failState(err)
		return fmt.Errorf("telegram: sign in: %w", err)
	}
	c.setAuthorized(authorization)
	return nil
}

// SubmitPassword completes the 2FA step.
func (a *AuthManager) SubmitPassword(ctx context.Context, password string) error {
	c := a.client
	if c == nil || !c.isStarted() {
		return fmt.Errorf("%w: MTProto client is not started", domain.ErrInvalidInput)
	}
	authorization, err := c.client.Auth().Password(ctx, password)
	if err != nil {
		c.failState(err)
		return fmt.Errorf("telegram: 2FA: %w", err)
	}
	c.setAuthorized(authorization)
	return nil
}

func (c *MTProtoClient) isStarted() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.started
}

func (c *MTProtoClient) setState(update func(*AuthSessionState)) {
	c.mu.Lock()
	update(&c.state)
	c.state.UpdatedAt = time.Now().UTC()
	c.mu.Unlock()
}

func (c *MTProtoClient) failState(err error) {
	c.setState(func(s *AuthSessionState) {
		s.Step = AuthStepFailed
		if err != nil {
			s.Error = err.Error()
		}
	})
}

func (c *MTProtoClient) setAuthorized(authorization *tg.AuthAuthorization) {
	if authorization == nil {
		c.failState(errors.New("empty authorization"))
		return
	}
	user, _ := authorization.User.(*tg.User)
	c.setAuthorizedUser(user)
}

func (c *MTProtoClient) setAuthorizedUser(user *tg.User) {
	c.setState(func(s *AuthSessionState) {
		s.Step = AuthStepAuthorized
		s.Error = ""
		s.QRURL = ""
		if user != nil {
			s.TGUserID = user.ID
			if username, ok := user.GetUsername(); ok {
				s.Username = username
			}
			s.FirstName = user.FirstName
		}
	})
}

func maskPhone(phone string) string {
	trimmed := strings.TrimSpace(phone)
	if len(trimmed) <= 4 {
		return "***"
	}
	return "***" + trimmed[len(trimmed)-2:]
}

// gotdTransport adapts gotd/td RPCs to AccountTransport.
type gotdTransport struct {
	accountID int64
	api       *tg.Client
	client    *telegram.Client
}

var _ AccountTransport = (*gotdTransport)(nil)

func (t *gotdTransport) AccountID() int64 { return t.accountID }

func (t *gotdTransport) ListDialogs(ctx context.Context, limit int) ([]DialogPreview, error) {
	if limit <= 0 {
		limit = 100
	}
	result, err := t.api.MessagesGetDialogs(ctx, &tg.MessagesGetDialogsRequest{
		Limit:      limit,
		OffsetPeer: &tg.InputPeerEmpty{},
	})
	if err != nil {
		return nil, err
	}
	var dialogs []tg.DialogClass
	var chats []tg.ChatClass
	switch value := result.(type) {
	case *tg.MessagesDialogs:
		dialogs, chats = value.Dialogs, value.Chats
	case *tg.MessagesDialogsSlice:
		dialogs, chats = value.Dialogs, value.Chats
	}
	folderNames := make(map[int]string)
	if filters, err := t.api.MessagesGetDialogFilters(ctx); err == nil {
		for _, filter := range filters.Filters {
			switch value := filter.(type) {
			case *tg.DialogFilter:
				folderNames[value.ID] = value.Title.Text
			case *tg.DialogFilterChatlist:
				folderNames[value.ID] = value.Title.Text
			}
		}
	}
	if len(dialogs) == 0 {
		return previewsFromChats(chats), nil
	}
	previews := make([]DialogPreview, 0, len(dialogs))
	for _, dialog := range dialogs {
		dialogValue, ok := dialog.(*tg.Dialog)
		if !ok {
			continue
		}
		peer := dialogValue.Peer
		preview, ok := previewFromPeer(peer, chats)
		if !ok {
			continue
		}
		if folderID, ok := dialogValue.GetFolderID(); ok {
			if name := folderNames[folderID]; name != "" {
				preview.FolderNames = []string{name}
			}
		}
		previews = append(previews, preview)
	}
	return previews, nil
}

func (t *gotdTransport) SearchGlobal(ctx context.Context, query string, limit int) ([]DialogPreview, error) {
	if limit <= 0 {
		limit = 50
	}
	result, err := t.api.MessagesSearchGlobal(ctx, &tg.MessagesSearchGlobalRequest{
		Q:          query,
		Filter:     &tg.InputMessagesFilterEmpty{},
		Limit:      limit,
		OffsetPeer: &tg.InputPeerEmpty{},
	})
	if err != nil {
		return nil, err
	}
	var chats []tg.ChatClass
	switch value := result.(type) {
	case *tg.MessagesMessages:
		chats = value.Chats
	case *tg.MessagesMessagesSlice:
		chats = value.Chats
	case *tg.MessagesChannelMessages:
		chats = value.Chats
	}
	return previewsFromChats(chats), nil
}

func (t *gotdTransport) ResolveUsername(ctx context.Context, username string) (DialogPreview, error) {
	result, err := t.api.ContactsResolveUsername(ctx, &tg.ContactsResolveUsernameRequest{Username: strings.TrimPrefix(username, "@")})
	if err != nil {
		return DialogPreview{}, err
	}
	if preview, ok := previewFromPeer(result.Peer, result.Chats); ok {
		return preview, nil
	}
	if len(result.Chats) > 0 {
		if preview, ok := previewFromChatClass(result.Chats[0]); ok {
			return preview, nil
		}
	}
	return DialogPreview{}, fmt.Errorf("telegram: resolved peer has no group/channel")
}

func (t *gotdTransport) CheckChatInvite(ctx context.Context, hash string) (DialogPreview, bool, error) {
	result, err := t.api.MessagesCheckChatInvite(ctx, hash)
	if err != nil {
		return DialogPreview{}, false, err
	}
	switch value := result.(type) {
	case *tg.ChatInviteAlready:
		preview, _ := previewFromChatClass(value.Chat)
		return preview, true, nil
	case *tg.ChatInvitePeek:
		preview, _ := previewFromChatClass(value.Chat)
		return preview, false, nil
	case *tg.ChatInvite:
		preview := DialogPreview{
			Title:       value.Title,
			MemberCount: value.ParticipantsCount,
			ChatType:    domain.ChatTypeGroup,
			HasComments: false,
		}
		if value.Broadcast {
			preview.ChatType = domain.ChatTypeChannel
		} else if value.Megagroup {
			preview.ChatType = domain.ChatTypeSupergroup
		}
		return preview, false, nil
	default:
		return DialogPreview{}, false, fmt.Errorf("telegram: unexpected invite type %T", result)
	}
}

func (t *gotdTransport) JoinChannel(ctx context.Context, username string, tgChatID, accessHash int64) (DialogPreview, error) {
	if tgChatID == 0 || accessHash == 0 {
		resolved, err := t.ResolveUsername(ctx, username)
		if err != nil {
			return DialogPreview{}, err
		}
		tgChatID, accessHash = resolved.TGChatID, resolved.AccessHash
	}
	tgChatID = normalizeTGID(tgChatID)
	if _, err := t.api.ChannelsJoinChannel(ctx, &tg.InputChannel{ChannelID: tgChatID, AccessHash: accessHash}); err != nil {
		return DialogPreview{}, err
	}
	preview := DialogPreview{TGChatID: tgChatID, AccessHash: accessHash, Username: strings.TrimPrefix(username, "@"), Title: username}
	if resolved, err := t.ResolveUsername(ctx, username); err == nil {
		preview = resolved
	}
	return preview, nil
}

func (t *gotdTransport) ImportInvite(ctx context.Context, hash string) (DialogPreview, error) {
	preview, already, err := t.CheckChatInvite(ctx, hash)
	if err != nil {
		return DialogPreview{}, err
	}
	if already {
		return preview, nil
	}
	if _, err := t.api.MessagesImportChatInvite(ctx, hash); err != nil {
		return DialogPreview{}, err
	}
	if after, joined, err := t.CheckChatInvite(ctx, hash); err == nil && joined {
		return after, nil
	}
	return preview, nil
}

func (t *gotdTransport) ImportChatList(ctx context.Context, slug string) ([]DialogPreview, error) {
	result, err := t.api.ChatlistsCheckChatlistInvite(ctx, slug)
	if err != nil {
		return nil, err
	}
	switch value := result.(type) {
	case *tg.ChatlistsChatlistInviteAlready:
		return previewsFromChats(value.Chats), nil
	case *tg.ChatlistsChatlistInvite:
		peers := make([]tg.InputPeerClass, 0, len(value.Peers))
		for _, peer := range value.Peers {
			if input := inputPeerForPeer(peer, value.Chats); input != nil {
				peers = append(peers, input)
			}
		}
		if _, err := t.api.ChatlistsJoinChatlistInvite(ctx, &tg.ChatlistsJoinChatlistInviteRequest{
			Slug:  slug,
			Peers: peers,
		}); err != nil {
			return nil, err
		}
		return previewsFromChats(value.Chats), nil
	default:
		return nil, fmt.Errorf("telegram: unexpected chatlist invite type %T", result)
	}
}

func (t *gotdTransport) GetRecentMessages(ctx context.Context, tgChatID, accessHash int64, minID, limit int) ([]IncomingMessage, error) {
	if limit <= 0 {
		limit = 50
	}
	peer := inputPeerFor(tgChatID, accessHash)
	result, err := t.api.MessagesGetHistory(ctx, &tg.MessagesGetHistoryRequest{
		Peer:  peer,
		MinID: minID,
		Limit: limit,
	})
	if err != nil {
		return nil, err
	}
	var messages []tg.MessageClass
	var chats []tg.ChatClass
	var users []tg.UserClass
	switch value := result.(type) {
	case *tg.MessagesMessages:
		messages, chats, users = value.Messages, value.Chats, value.Users
	case *tg.MessagesMessagesSlice:
		messages, chats, users = value.Messages, value.Chats, value.Users
	case *tg.MessagesChannelMessages:
		messages, chats, users = value.Messages, value.Chats, value.Users
	}
	userMap := make(map[int64]*tg.User, len(users))
	for _, user := range users {
		if u, ok := user.(*tg.User); ok {
			userMap[u.ID] = u
		}
	}
	chatMeta := make(map[int64]DialogPreview, len(chats))
	for _, chat := range chats {
		if preview, ok := previewFromChatClass(chat); ok {
			chatMeta[preview.TGChatID] = preview
		}
	}
	preview := chatMeta[normalizeTGID(tgChatID)]
	out := make([]IncomingMessage, 0, len(messages))
	for _, raw := range messages {
		message, ok := raw.(*tg.Message)
		if !ok {
			continue
		}
		incoming := IncomingMessage{
			TGChatID:     tgChatID,
			AccessHash:   accessHash,
			ChatTitle:    preview.Title,
			ChatUsername: preview.Username,
			ChatType:     preview.ChatType,
			MsgID:        message.ID,
			Text:         message.Message,
			Timestamp:    time.Unix(int64(message.Date), 0).UTC(),
			Buttons:      buttonsFromMarkup(message.ReplyMarkup),
			HasPhoto:     isPhotoMessage(message.Media),
		}
		if from, ok := message.FromID.(*tg.PeerUser); ok {
			incoming.AuthorID = from.UserID
			if user := userMap[from.UserID]; user != nil {
				incoming.AuthorName = strings.TrimSpace(user.FirstName + " " + user.LastName)
				incoming.AuthorUsername, _ = user.GetUsername()
				incoming.IsBot = user.GetBot()
			}
		}
		out = append(out, incoming)
	}
	return out, nil
}

func (t *gotdTransport) ReadHistory(ctx context.Context, tgChatID, accessHash int64, maxID int) error {
	_, err := t.api.MessagesReadHistory(ctx, &tg.MessagesReadHistoryRequest{
		Peer:  inputPeerFor(tgChatID, accessHash),
		MaxID: maxID,
	})
	return err
}

func (t *gotdTransport) PressButton(ctx context.Context, tgChatID, accessHash int64, msgID int, callbackData []byte) error {
	_, err := t.api.MessagesGetBotCallbackAnswer(ctx, &tg.MessagesGetBotCallbackAnswerRequest{
		Peer:  inputPeerFor(tgChatID, accessHash),
		MsgID: msgID,
		Data:  callbackData,
	})
	return err
}

func (t *gotdTransport) SendReply(ctx context.Context, tgChatID, accessHash int64, replyToMsgID int, text string) error {
	request := &tg.MessagesSendMessageRequest{
		Peer:     inputPeerFor(tgChatID, accessHash),
		Message:  text,
		RandomID: time.Now().UnixNano(),
	}
	request.SetReplyTo(&tg.InputReplyToMessage{ReplyToMsgID: replyToMsgID})
	_, err := t.api.MessagesSendMessage(ctx, request)
	return err
}

func (t *gotdTransport) DownloadMessageImage(ctx context.Context, tgChatID, accessHash int64, msgID int) (string, error) {
	if t.client == nil {
		return "", errors.New("telegram: downloader unavailable")
	}
	result, err := t.api.MessagesGetMessages(ctx, []tg.InputMessageClass{&tg.InputMessageID{ID: msgID}})
	if err != nil {
		return "", err
	}
	messages := messagesFromResult(result)
	var photo *tg.Photo
	for _, raw := range messages {
		message, ok := raw.(*tg.Message)
		if !ok {
			continue
		}
		media, ok := message.Media.(*tg.MessageMediaPhoto)
		if !ok {
			continue
		}
		if p, ok := media.Photo.(*tg.Photo); ok {
			photo = p
			break
		}
	}
	if photo == nil {
		return "", errors.New("telegram: message has no photo")
	}
	location := &tg.InputPhotoFileLocation{
		ID:            photo.ID,
		AccessHash:    photo.AccessHash,
		FileReference: photo.FileReference,
		ThumbSize:     photoThumbType(photo),
	}
	var buffer bytes.Buffer
	if _, err := t.client.Download(location).Stream(ctx, &buffer); err != nil {
		return "", err
	}
	data := buffer.Bytes()
	contentType := http.DetectContentType(data)
	return "data:" + contentType + ";base64," + base64.StdEncoding.EncodeToString(data), nil
}

func previewsFromChats(chats []tg.ChatClass) []DialogPreview {
	out := make([]DialogPreview, 0, len(chats))
	for _, chat := range chats {
		if preview, ok := previewFromChatClass(chat); ok {
			out = append(out, preview)
		}
	}
	return out
}

func previewFromChatClass(chat tg.ChatClass) (DialogPreview, bool) {
	switch value := chat.(type) {
	case *tg.Chat:
		return DialogPreview{
			TGChatID:    value.ID,
			Title:       value.Title,
			ChatType:    domain.ChatTypeGroup,
			MemberCount: value.ParticipantsCount,
		}, true
	case *tg.Channel:
		username, _ := value.GetUsername()
		members, _ := value.GetParticipantsCount()
		chatType := domain.ChatTypeSupergroup
		if value.Broadcast {
			chatType = domain.ChatTypeChannel
		}
		return DialogPreview{
			TGChatID:    value.ID,
			AccessHash:  value.AccessHash,
			Username:    username,
			Title:       value.Title,
			ChatType:    chatType,
			MemberCount: members,
			HasComments: value.HasLink,
		}, true
	default:
		return DialogPreview{}, false
	}
}

func previewFromPeer(peer tg.PeerClass, chats []tg.ChatClass) (DialogPreview, bool) {
	id, ok := peerID(peer)
	if !ok {
		return DialogPreview{}, false
	}
	for _, chat := range chats {
		if preview, ok := previewFromChatClass(chat); ok && preview.TGChatID == id {
			return preview, true
		}
	}
	return DialogPreview{}, false
}

func peerID(peer tg.PeerClass) (int64, bool) {
	switch value := peer.(type) {
	case *tg.PeerChannel:
		return value.ChannelID, true
	case *tg.PeerChat:
		return value.ChatID, true
	default:
		return 0, false
	}
}

func inputPeerForPeer(peer tg.PeerClass, chats []tg.ChatClass) tg.InputPeerClass {
	switch value := peer.(type) {
	case *tg.PeerChannel:
		for _, chat := range chats {
			if channel, ok := chat.(*tg.Channel); ok && channel.ID == value.ChannelID {
				return &tg.InputPeerChannel{ChannelID: channel.ID, AccessHash: channel.AccessHash}
			}
		}
	case *tg.PeerChat:
		return &tg.InputPeerChat{ChatID: value.ChatID}
	}
	return nil
}

func inputPeerFor(tgChatID, accessHash int64) tg.InputPeerClass {
	id := normalizeTGID(tgChatID)
	if accessHash != 0 {
		return &tg.InputPeerChannel{ChannelID: id, AccessHash: accessHash}
	}
	return &tg.InputPeerChat{ChatID: id}
}

func normalizeTGID(id int64) int64 {
	if id < 0 {
		id = -id
	}
	raw := fmt.Sprintf("%d", id)
	if strings.HasPrefix(raw, "100") && len(raw) > 3 {
		var trimmed int64
		if _, err := fmt.Sscanf(raw[3:], "%d", &trimmed); err == nil {
			return trimmed
		}
	}
	return id
}

func buttonsFromMarkup(markup tg.ReplyMarkupClass) []CaptchaButton {
	inline, ok := markup.(*tg.ReplyInlineMarkup)
	if !ok {
		return nil
	}
	buttons := make([]CaptchaButton, 0, 4)
	for row, line := range inline.Rows {
		for col, button := range line.Buttons {
			item := CaptchaButton{Text: button.Text, Row: row, Col: col}
			if callback, ok := button.Type.(*tg.InlineButtonTypeCallback); ok {
				item.CallbackData = callback.Data
			}
			buttons = append(buttons, item)
		}
	}
	return buttons
}

func isPhotoMessage(media tg.MessageMediaClass) bool {
	_, ok := media.(*tg.MessageMediaPhoto)
	return ok
}

func messagesFromResult(result tg.MessagesMessagesClass) []tg.MessageClass {
	switch value := result.(type) {
	case *tg.MessagesMessages:
		return value.Messages
	case *tg.MessagesMessagesSlice:
		return value.Messages
	case *tg.MessagesChannelMessages:
		return value.Messages
	default:
		return nil
	}
}

func photoThumbType(photo *tg.Photo) string {
	for i := len(photo.Sizes) - 1; i >= 0; i-- {
		switch size := photo.Sizes[i].(type) {
		case *tg.PhotoSize:
			if size.Type != "" {
				return size.Type
			}
		case *tg.PhotoSizeProgressive:
			if size.Type != "" {
				return size.Type
			}
		case *tg.PhotoCachedSize:
			if size.Type != "" {
				return size.Type
			}
		}
	}
	return "x"
}
