package telegram

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gotd/td/tgerr"

	"github.com/demonidze1337/mia-lead-radar/internal/domain"
	"github.com/demonidze1337/mia-lead-radar/internal/storage"
)

var floodWaitPattern = regexp.MustCompile(`FLOOD(?:_PREMIUM)?_WAIT_(\d+)`)

// ClassifyTGError maps gotd/tgerr and string RPC errors to domain classes.
func ClassifyTGError(err error) (TGErrorClass, time.Duration) {
	if err == nil {
		return TGErrorNone, 0
	}
	if wait, ok := tgerr.AsFloodWait(err); ok {
		return TGErrorFloodWait, wait
	}
	message := strings.ToUpper(err.Error())
	if match := floodWaitPattern.FindStringSubmatch(message); len(match) == 2 {
		seconds, parseErr := strconv.Atoi(match[1])
		if parseErr == nil && seconds >= 0 {
			return TGErrorFloodWait, time.Duration(seconds) * time.Second
		}
	}
	if rpcErr, ok := tgerr.As(err); ok {
		switch {
		case rpcErr.IsOneOf("USER_ALREADY_PARTICIPANT"):
			return TGErrorAlreadyParticipant, 0
		case rpcErr.IsOneOf("INVITE_REQUEST_SENT"):
			return TGErrorInviteRequestSent, 0
		case rpcErr.IsOneOf("CHANNELS_TOO_MUCH"):
			return TGErrorChannelsTooMuch, 0
		case rpcErr.IsOneOf("USER_BANNED_IN_CHANNEL", "USER_RESTRICTED", "CHAT_WRITE_FORBIDDEN", "CHANNEL_BANNED"):
			return TGErrorBanned, 0
		case rpcErr.IsOneOf("CHANNEL_INVALID", "INVITE_HASH_EXPIRED", "USERNAME_NOT_OCCUPIED", "PEER_ID_INVALID", "CHANNEL_PRIVATE"):
			return TGErrorBadTarget, 0
		case rpcErr.Code == 400 || rpcErr.Code == 403 || rpcErr.Code == 404:
			return TGErrorBadTarget, 0
		default:
			return TGErrorTransient, 0
		}
	}
	return classifyStringError(message), 0
}

func classifyStringError(message string) TGErrorClass {
	switch {
	case strings.Contains(message, "FLOOD_WAIT"):
		return TGErrorFloodWait
	case strings.Contains(message, "USER_ALREADY_PARTICIPANT"):
		return TGErrorAlreadyParticipant
	case strings.Contains(message, "INVITE_REQUEST_SENT"):
		return TGErrorInviteRequestSent
	case strings.Contains(message, "CHANNELS_TOO_MUCH"):
		return TGErrorChannelsTooMuch
	case strings.Contains(message, "USER_BANNED_IN_CHANNEL") ||
		strings.Contains(message, "USER_RESTRICTED") ||
		strings.Contains(message, "CHAT_WRITE_FORBIDDEN") ||
		strings.Contains(message, "CHANNEL_BANNED"):
		return TGErrorBanned
	case strings.Contains(message, "CHANNEL_INVALID") ||
		strings.Contains(message, "INVITE_HASH_EXPIRED") ||
		strings.Contains(message, "USERNAME_NOT_OCCUPIED") ||
		strings.Contains(message, "PEER_ID_INVALID") ||
		strings.Contains(message, "CHANNEL_PRIVATE"):
		return TGErrorBadTarget
	default:
		return TGErrorTransient
	}
}

// Joiner executes persistent join-queue items through an account transport.
type Joiner struct {
	store     storage.Store
	gate      *FloodGate
	governor  *RateGovernor
	transport AccountTransport
}

// NewJoiner builds a Joiner bound to one account transport.
func NewJoiner(store storage.Store, gate *FloodGate, governor *RateGovernor, transport AccountTransport) *Joiner {
	return &Joiner{store: store, gate: gate, governor: governor, transport: transport}
}

// Join processes one queue item and records chat/account/queue state.
func (j *Joiner) Join(ctx context.Context, item domain.JoinQueueItem) (JoinOutcome, error) {
	if j == nil || j.store == nil || j.transport == nil {
		return JoinOutcome{}, fmt.Errorf("%w: join runtime is not configured", domain.ErrInvalidInput)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	accountID := j.transport.AccountID()
	if j.gate != nil {
		if blocked, until := j.gate.IsBlocked(accountID); blocked {
			outcome := JoinOutcome{
				Status:       domain.ChatStatusQueued,
				ErrorClass:   TGErrorFloodWait,
				FloodWait:    time.Until(until),
				ErrorMessage: "account is inside FloodWait cooldown",
			}
			return j.finish(ctx, item, outcome)
		}
	}
	if j.governor != nil {
		if err := j.governor.Wait(ctx, accountID, OpJoin); err != nil {
			outcome := JoinOutcome{
				Status:       domain.ChatStatusQueued,
				ErrorClass:   TGErrorTransient,
				ErrorMessage: err.Error(),
			}
			finished, finishErr := j.finish(ctx, item, outcome)
			if finishErr != nil {
				return JoinOutcome{}, finishErr
			}
			return finished, err
		}
	}
	parsed, err := ParseLink(firstNonEmpty(item.NormalizedLink, item.RawLink))
	if err != nil {
		return j.finish(ctx, item, JoinOutcome{
			Status:       domain.ChatStatusBad,
			ErrorClass:   TGErrorBadTarget,
			ErrorMessage: err.Error(),
		})
	}
	switch parsed.Kind {
	case LinkKindAddList:
		return j.joinAddList(ctx, item, parsed)
	case LinkKindInvite:
		return j.joinInvite(ctx, item, parsed)
	case LinkKindUsername, LinkKindMessage:
		if parsed.Username == "" {
			return j.finish(ctx, item, JoinOutcome{
				Status:       domain.ChatStatusBad,
				ErrorClass:   TGErrorBadTarget,
				ErrorMessage: "message link has no resolvable username",
			})
		}
		return j.joinUsername(ctx, item, parsed)
	default:
		return j.finish(ctx, item, JoinOutcome{Status: domain.ChatStatusBad, ErrorClass: TGErrorBadTarget})
	}
}

func (j *Joiner) joinAddList(ctx context.Context, item domain.JoinQueueItem, parsed ParsedLink) (JoinOutcome, error) {
	previews, err := j.transport.ImportChatList(ctx, parsed.AddListSlug)
	if err != nil {
		return j.fail(ctx, item, err)
	}
	chats := make([]domain.Chat, 0, len(previews))
	for _, preview := range previews {
		chat, err := j.upsertChat(ctx, preview, domain.ChatSourceAddList, domain.ChatStatusJoined)
		if err != nil {
			return JoinOutcome{}, err
		}
		if err := j.store.IncrementAccountJoins(ctx, j.transport.AccountID(), time.Now().UTC()); err != nil {
			return JoinOutcome{}, err
		}
		chats = append(chats, chat)
	}
	return j.finish(ctx, item, JoinOutcome{Status: domain.ChatStatusJoined, Chats: chats})
}

func (j *Joiner) joinInvite(ctx context.Context, item domain.JoinQueueItem, parsed ParsedLink) (JoinOutcome, error) {
	preview, err := j.transport.ImportInvite(ctx, parsed.InviteHash)
	if err != nil {
		return j.fail(ctx, item, err)
	}
	if preview.Username == "" {
		preview.Username = parsed.Username
	}
	chat, err := j.upsertChat(ctx, preview, domain.ChatSourceExtractedLink, domain.ChatStatusJoined)
	if err != nil {
		return JoinOutcome{}, err
	}
	if err := j.store.IncrementAccountJoins(ctx, j.transport.AccountID(), time.Now().UTC()); err != nil {
		return JoinOutcome{}, err
	}
	return j.finish(ctx, item, JoinOutcome{Status: domain.ChatStatusJoined, Chats: []domain.Chat{chat}})
}

func (j *Joiner) joinUsername(ctx context.Context, item domain.JoinQueueItem, parsed ParsedLink) (JoinOutcome, error) {
	preview, err := j.transport.ResolveUsername(ctx, parsed.Username)
	if err != nil {
		return j.fail(ctx, item, err)
	}
	if preview.Username != "" && !strings.EqualFold(preview.Username, parsed.Username) {
		// A username can point at a migrated channel; trust the resolved preview.
		parsed.Username = preview.Username
	}
	joined, joinErr := j.transport.JoinChannel(ctx, parsed.Username, preview.TGChatID, preview.AccessHash)
	if joinErr != nil {
		class, wait := ClassifyTGError(joinErr)
		switch class {
		case TGErrorAlreadyParticipant:
			chat, err := j.upsertChat(ctx, preview, domain.ChatSourceExtractedLink, domain.ChatStatusJoined)
			if err != nil {
				return JoinOutcome{}, err
			}
			return j.finish(ctx, item, JoinOutcome{Status: domain.ChatStatusJoined, Chats: []domain.Chat{chat}})
		case TGErrorInviteRequestSent:
			chat, err := j.upsertChat(ctx, preview, domain.ChatSourceExtractedLink, domain.ChatStatusRequiresApproval)
			if err != nil {
				return JoinOutcome{}, err
			}
			return j.finish(ctx, item, JoinOutcome{Status: domain.ChatStatusRequiresApproval, Chats: []domain.Chat{chat}, ErrorClass: class})
		default:
			return j.joinFailure(ctx, item, joinErr, class, wait)
		}
	}
	if joined.TGChatID == 0 {
		joined.TGChatID = preview.TGChatID
	}
	if joined.AccessHash == 0 {
		joined.AccessHash = preview.AccessHash
	}
	if joined.Username == "" {
		joined.Username = parsed.Username
	}
	if joined.Title == "" {
		joined.Title = preview.Title
	}
	chat, err := j.upsertChat(ctx, joined, domain.ChatSourceExtractedLink, domain.ChatStatusJoined)
	if err != nil {
		return JoinOutcome{}, err
	}
	if err := j.store.IncrementAccountJoins(ctx, j.transport.AccountID(), time.Now().UTC()); err != nil {
		return JoinOutcome{}, err
	}
	return j.finish(ctx, item, JoinOutcome{Status: domain.ChatStatusJoined, Chats: []domain.Chat{chat}})
}

func (j *Joiner) fail(ctx context.Context, item domain.JoinQueueItem, err error) (JoinOutcome, error) {
	class, wait := ClassifyTGError(err)
	return j.joinFailure(ctx, item, err, class, wait)
}

func (j *Joiner) joinFailure(ctx context.Context, item domain.JoinQueueItem, cause error, class TGErrorClass, wait time.Duration) (JoinOutcome, error) {
	outcome := JoinOutcome{ErrorClass: class, FloodWait: wait}
	reason := "join failed"
	if cause != nil {
		outcome.ErrorMessage = cause.Error()
		reason = cause.Error()
	}
	switch class {
	case TGErrorFloodWait:
		if j.gate != nil {
			until, err := j.gate.RegisterFlood(ctx, j.transport.AccountID(), wait, reason)
			if err != nil {
				return JoinOutcome{}, err
			}
			outcome.FloodWait = time.Until(until)
		}
		outcome.Status = domain.ChatStatusQueued
	case TGErrorChannelsTooMuch:
		outcome.Status = domain.ChatStatusSkipped
		if err := j.store.SetAccountStatus(ctx, j.transport.AccountID(), domain.AccountStatusChannelsFull, reason); err != nil {
			return JoinOutcome{}, err
		}
	case TGErrorBanned:
		outcome.Status = domain.ChatStatusBanned
		if err := j.store.SetAccountStatus(ctx, j.transport.AccountID(), domain.AccountStatusBanned, reason); err != nil {
			return JoinOutcome{}, err
		}
	case TGErrorBadTarget:
		outcome.Status = domain.ChatStatusBad
	case TGErrorTransient:
		outcome.Status = domain.ChatStatusQueued
	}
	return j.finish(ctx, item, outcome)
}

func (j *Joiner) finish(ctx context.Context, item domain.JoinQueueItem, outcome JoinOutcome) (JoinOutcome, error) {
	if item.ID <= 0 {
		return outcome, nil
	}
	completeCtx := context.WithoutCancel(ctx)
	status := domain.JoinJobDone
	var nextAttempt *time.Time
	switch {
	case outcome.ErrorClass == TGErrorFloodWait:
		status = domain.JoinJobFailed
		if outcome.FloodWait > 0 {
			at := time.Now().UTC().Add(outcome.FloodWait)
			nextAttempt = &at
		}
	case outcome.ErrorClass == TGErrorTransient:
		status = domain.JoinJobPending
		at := time.Now().UTC().Add(5 * time.Minute)
		nextAttempt = &at
	case outcome.Status == domain.ChatStatusBad || outcome.Status == domain.ChatStatusBanned || outcome.Status == domain.ChatStatusSkipped:
		status = domain.JoinJobDead
	}
	if err := j.store.CompleteJoinJob(completeCtx, item.ID, status, nextAttempt, outcome.ErrorMessage); err != nil {
		return JoinOutcome{}, err
	}
	return outcome, nil
}

func (j *Joiner) upsertChat(ctx context.Context, preview DialogPreview, source domain.ChatSource, status domain.ChatStatus) (domain.Chat, error) {
	chatType := preview.ChatType
	if chatType == "" {
		chatType = domain.ChatTypeGroup
	}
	return j.store.UpsertChat(ctx, domain.Chat{
		TGChatID:          preview.TGChatID,
		AccessHash:        preview.AccessHash,
		Username:          preview.Username,
		Title:             preview.Title,
		ChatType:          chatType,
		Source:            source,
		Status:            status,
		MemberCount:       preview.MemberCount,
		HasComments:       preview.HasComments,
		LinkedChatID:      preview.LinkedID,
		AssignedAccountID: j.transport.AccountID(),
	})
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
