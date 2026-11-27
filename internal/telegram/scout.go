package telegram

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/demonidze1337/mia-lead-radar/internal/domain"
	"github.com/demonidze1337/mia-lead-radar/internal/storage"
)

const recentMessageWindow = time.Hour

// AssessChatQuality evaluates member count, recent message velocity, and comment
// availability for broadcast channels before joining or scanning a chat.
func AssessChatQuality(ctx context.Context, transport AccountTransport, preview DialogPreview, minMembers int, minVelocityPerHour float64) ChatQualityReport {
	report := ChatQualityReport{
		MemberCount:  preview.MemberCount,
		HasComments:  preview.HasComments,
		LinkedChatID: preview.LinkedID,
	}
	if minMembers > 0 && preview.MemberCount < minMembers {
		report.Reason = fmt.Sprintf("member count %d is below minimum %d", preview.MemberCount, minMembers)
		return report
	}
	if preview.ChatType == domain.ChatTypeChannel && !preview.HasComments && preview.LinkedID == 0 {
		report.Reason = "broadcast channel has no comments or linked discussion chat"
		return report
	}
	if transport == nil {
		report.Reason = "no account transport available"
		return report
	}
	messages, err := transport.GetRecentMessages(ctx, preview.TGChatID, preview.AccessHash, 0, 50)
	if err != nil {
		report.Reason = "recent messages unavailable: " + err.Error()
		return report
	}
	cutoff := time.Now().UTC().Add(-recentMessageWindow)
	recent := 0
	for _, msg := range messages {
		if !msg.Timestamp.IsZero() && msg.Timestamp.After(cutoff) {
			recent++
		}
	}
	report.MsgVelocityPerHour = float64(recent)
	if minVelocityPerHour > 0 && report.MsgVelocityPerHour < minVelocityPerHour {
		report.Reason = fmt.Sprintf("message velocity %.1f/h is below minimum %.1f/h", report.MsgVelocityPerHour, minVelocityPerHour)
		return report
	}
	report.Passed = true
	return report
}

// Scout imports the account's dialogs/folders, discovers chats via global search,
// and feeds extracted Telegram links into the persistent join queue.
type Scout struct {
	store     storage.Store
	governor  *RateGovernor
	transport AccountTransport
}

// NewScout builds a Scout bound to one account transport.
func NewScout(store storage.Store, governor *RateGovernor, transport AccountTransport) *Scout {
	return &Scout{store: store, governor: governor, transport: transport}
}

// ImportMyChats imports all dialogs (including folder membership metadata) as joined chats.
func (s *Scout) ImportMyChats(ctx context.Context) ([]domain.Chat, error) {
	if s == nil || s.store == nil || s.transport == nil {
		return nil, fmt.Errorf("%w: scout runtime is not configured", domain.ErrInvalidInput)
	}
	if s.governor != nil {
		if err := s.governor.Wait(ctx, s.transport.AccountID(), OpFetchMessages); err != nil {
			return nil, err
		}
	}
	previews, err := s.transport.ListDialogs(ctx, 500)
	if err != nil {
		return nil, err
	}
	chats := make([]domain.Chat, 0, len(previews))
	for _, preview := range previews {
		chat, err := s.store.UpsertChat(ctx, domain.Chat{
			TGChatID:          preview.TGChatID,
			AccessHash:        preview.AccessHash,
			Username:          preview.Username,
			Title:             preview.Title,
			ChatType:          defaultChatType(preview.ChatType),
			Source:            domain.ChatSourceUserImport,
			Status:            domain.ChatStatusJoined,
			MemberCount:       preview.MemberCount,
			HasComments:       preview.HasComments,
			LinkedChatID:      preview.LinkedID,
			AssignedAccountID: s.transport.AccountID(),
		})
		if err != nil {
			return nil, err
		}
		chats = append(chats, chat)
	}
	return chats, nil
}

// SearchAndQueue searches globally, upserts discovered chats, and queues joinable usernames.
func (s *Scout) SearchAndQueue(ctx context.Context, query string, limit int) ([]domain.Chat, error) {
	if s == nil || s.store == nil || s.transport == nil {
		return nil, fmt.Errorf("%w: scout runtime is not configured", domain.ErrInvalidInput)
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("%w: empty search query", domain.ErrInvalidInput)
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}
	if s.governor != nil {
		if err := s.governor.Wait(ctx, s.transport.AccountID(), OpSearch); err != nil {
			return nil, err
		}
	}
	previews, err := s.transport.SearchGlobal(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	chats := make([]domain.Chat, 0, len(previews))
	now := time.Now().UTC()
	for _, preview := range previews {
		chat, err := s.store.UpsertChat(ctx, domain.Chat{
			TGChatID:          preview.TGChatID,
			AccessHash:        preview.AccessHash,
			Username:          preview.Username,
			Title:             preview.Title,
			ChatType:          defaultChatType(preview.ChatType),
			Source:            domain.ChatSourceSearchGlobal,
			Status:            domain.ChatStatusQueued,
			MemberCount:       preview.MemberCount,
			HasComments:       preview.HasComments,
			LinkedChatID:      preview.LinkedID,
			AssignedAccountID: s.transport.AccountID(),
		})
		if err != nil {
			return nil, err
		}
		chats = append(chats, chat)
		username := strings.TrimSpace(strings.TrimPrefix(preview.Username, "@"))
		if username == "" {
			continue
		}
		link := "https://t.me/" + strings.ToLower(username)
		if _, _, err := s.store.EnqueueJoinJob(ctx, domain.JoinQueueItem{
			NormalizedLink: link,
			LinkType:       domain.JoinLinkUsername,
			RawLink:        link,
			Priority:       5,
			NextAttemptAt:  now.Add(-time.Second),
		}); err != nil {
			return nil, err
		}
	}
	return chats, nil
}

// ExtractAndEnqueueLinks scans messages and queues every deduplicated link.
func (s *Scout) ExtractAndEnqueueLinks(ctx context.Context, messages []IncomingMessage) (int, error) {
	if s == nil || s.store == nil {
		return 0, fmt.Errorf("%w: scout runtime is not configured", domain.ErrInvalidInput)
	}
	now := time.Now().UTC()
	inserted := 0
	for _, message := range messages {
		for _, link := range ExtractLinks(message.Text) {
			item := domain.JoinQueueItem{
				NormalizedLink: link.Normalized,
				LinkType:       joinLinkType(link.Kind),
				RawLink:        link.Raw,
				SourceChatID:   message.ChatID,
				NextAttemptAt:  now.Add(-time.Second),
			}
			if link.Kind == LinkKindAddList {
				item.Priority = 10
			}
			_, wasInserted, err := s.store.EnqueueJoinJob(ctx, item)
			if err != nil {
				return inserted, err
			}
			if wasInserted {
				inserted++
			}
		}
	}
	return inserted, nil
}

func defaultChatType(chatType domain.ChatType) domain.ChatType {
	if chatType == "" {
		return domain.ChatTypeGroup
	}
	return chatType
}

func joinLinkType(kind LinkKind) domain.JoinLinkType {
	switch kind {
	case LinkKindInvite:
		return domain.JoinLinkInvite
	case LinkKindAddList:
		return domain.JoinLinkAddList
	case LinkKindMessage:
		return domain.JoinLinkMessage
	default:
		return domain.JoinLinkUsername
	}
}
