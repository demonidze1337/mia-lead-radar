package sqlite

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/demonidze1337/mia-lead-radar/internal/domain"
	"github.com/demonidze1337/mia-lead-radar/internal/storage"
)

func TestAccountLifecycleFloodAndDailyJoins(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	acc, err := store.UpsertAccount(ctx, domain.TelegramAccount{
		Label:       "main",
		PhoneMasked: "+1***01",
		TGUserID:    555,
		Username:    "main_user",
		FirstName:   "Main",
		SessionPath: "/tmp/session",
	})
	if err != nil {
		t.Fatalf("UpsertAccount insert: %v", err)
	}
	if acc.ID <= 0 || acc.Status != domain.AccountStatusPendingAuth {
		t.Fatalf("unexpected account: %+v", acc)
	}

	again, err := store.UpsertAccount(ctx, domain.TelegramAccount{
		TGUserID: 555,
		Username: "renamed",
		Label:    "primary",
	})
	if err != nil {
		t.Fatalf("UpsertAccount update: %v", err)
	}
	if again.ID != acc.ID || again.Username != "renamed" || again.PhoneMasked != "+1***01" || again.SessionPath != "/tmp/session" {
		t.Fatalf("unexpected merged account: %+v", again)
	}
	accounts, err := store.ListAccounts(ctx)
	if err != nil {
		t.Fatalf("ListAccounts: %v", err)
	}
	if len(accounts) != 1 {
		t.Fatalf("accounts = %d, want 1", len(accounts))
	}

	floodUntil := time.Now().UTC().Add(30 * time.Minute).Truncate(time.Microsecond)
	if err := store.SetAccountFloodUntil(ctx, acc.ID, floodUntil, "FLOOD_WAIT_1800"); err != nil {
		t.Fatalf("SetAccountFloodUntil: %v", err)
	}
	flooded, err := store.GetAccount(ctx, acc.ID)
	if err != nil {
		t.Fatalf("GetAccount: %v", err)
	}
	if flooded.Status != domain.AccountStatusFloodWait || flooded.FloodUntil == nil || !flooded.FloodUntil.Equal(floodUntil) {
		t.Fatalf("unexpected flood state: %+v", flooded)
	}
	if flooded.LastError != "FLOOD_WAIT_1800" {
		t.Errorf("last error = %q", flooded.LastError)
	}
	cleared, err := store.ClearExpiredFloods(ctx, time.Now().UTC())
	if err != nil {
		t.Fatalf("ClearExpiredFloods too early: %v", err)
	}
	if cleared != 0 {
		t.Errorf("cleared = %d, want 0 before expiry", cleared)
	}
	cleared, err = store.ClearExpiredFloods(ctx, floodUntil.Add(time.Second))
	if err != nil {
		t.Fatalf("ClearExpiredFloods: %v", err)
	}
	if cleared != 1 {
		t.Errorf("cleared = %d, want 1", cleared)
	}
	recovered, _ := store.GetAccount(ctx, acc.ID)
	if recovered.Status != domain.AccountStatusActive || recovered.FloodUntil != nil || recovered.LastError != "" {
		t.Fatalf("unexpected recovered state: %+v", recovered)
	}

	day1 := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
	if err := store.IncrementAccountJoins(ctx, acc.ID, day1); err != nil {
		t.Fatalf("IncrementAccountJoins 1: %v", err)
	}
	if err := store.IncrementAccountJoins(ctx, acc.ID, day1.Add(time.Hour)); err != nil {
		t.Fatalf("IncrementAccountJoins 2: %v", err)
	}
	joined, _ := store.GetAccount(ctx, acc.ID)
	if joined.DailyJoinsCount != 2 || joined.JoinedChatsCount != 2 {
		t.Fatalf("day1 counters: %+v", joined)
	}
	day2 := day1.Add(24 * time.Hour)
	if err := store.IncrementAccountJoins(ctx, acc.ID, day2); err != nil {
		t.Fatalf("IncrementAccountJoins day2: %v", err)
	}
	joined, _ = store.GetAccount(ctx, acc.ID)
	if joined.DailyJoinsCount != 1 || joined.JoinedChatsCount != 3 {
		t.Fatalf("day2 counters: %+v", joined)
	}
	if joined.DailyJoinsResetAt == nil || joined.DailyJoinsResetAt.Format("2006-01-02") != "2026-03-11" {
		t.Fatalf("reset date = %v", joined.DailyJoinsResetAt)
	}

	refreshed, err := store.UpsertAccount(ctx, domain.TelegramAccount{TGUserID: 555, Label: "refreshed"})
	if err != nil {
		t.Fatalf("UpsertAccount refresh: %v", err)
	}
	if refreshed.DailyJoinsCount != 1 || refreshed.JoinedChatsCount != 3 {
		t.Fatalf("profile upsert reset join counters: %+v", refreshed)
	}

	if err := store.SetAccountStatus(ctx, acc.ID, domain.AccountStatusBanned, "spam"); err != nil {
		t.Fatalf("SetAccountStatus: %v", err)
	}
	banned, _ := store.GetAccount(ctx, acc.ID)
	if banned.Status != domain.AccountStatusBanned || banned.LastError != "spam" {
		t.Fatalf("unexpected banned account: %+v", banned)
	}
	if err := store.SetAccountStatus(ctx, 9999, domain.AccountStatusActive, ""); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("missing account status err = %v", err)
	}

	if err := store.DeleteAccount(ctx, acc.ID); err != nil {
		t.Fatalf("DeleteAccount: %v", err)
	}
	if _, err := store.GetAccount(ctx, acc.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("deleted account err = %v", err)
	}
}

func TestUpsertAccountRejectsMissingID(t *testing.T) {
	store := newTestStore(t)
	_, err := store.UpsertAccount(context.Background(), domain.TelegramAccount{ID: 9999, TGUserID: 1})
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestChatsUpsertFiltersAndProgress(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	chat, err := store.UpsertChat(ctx, domain.Chat{
		TGChatID:    123,
		Username:    "DevChat",
		Title:       "Developer Chat",
		ChatType:    domain.ChatTypeSupergroup,
		Source:      domain.ChatSourceSearchGlobal,
		MemberCount: 1200,
	})
	if err != nil {
		t.Fatalf("UpsertChat insert: %v", err)
	}
	if chat.ID <= 0 || chat.Status != domain.ChatStatusQueued {
		t.Fatalf("unexpected chat: %+v", chat)
	}

	byTG, err := store.UpsertChat(ctx, domain.Chat{TGChatID: 123, Title: "Renamed Chat", Status: domain.ChatStatusJoined})
	if err != nil {
		t.Fatalf("UpsertChat by tg: %v", err)
	}
	if byTG.ID != chat.ID || byTG.Title != "Renamed Chat" || byTG.Status != domain.ChatStatusJoined || byTG.MemberCount != 1200 {
		t.Fatalf("unexpected merged chat: %+v", byTG)
	}

	byUsername, err := store.UpsertChat(ctx, domain.Chat{Username: "devchat", Title: "Third"})
	if err != nil {
		t.Fatalf("UpsertChat by username: %v", err)
	}
	if byUsername.ID != chat.ID {
		t.Fatalf("username upsert created a duplicate: %d != %d", byUsername.ID, chat.ID)
	}

	other, err := store.UpsertChat(ctx, domain.Chat{
		TGChatID: 456,
		Title:    "Marketing Chat",
		Status:   domain.ChatStatusJoined,
	})
	if err != nil {
		t.Fatalf("UpsertChat other: %v", err)
	}

	gotByID, err := store.GetChatByID(ctx, chat.ID)
	if err != nil || gotByID.ID != chat.ID {
		t.Fatalf("GetChatByID: %+v %v", gotByID, err)
	}
	gotByTGID, err := store.GetChatByTGID(ctx, 123)
	if err != nil || gotByTGID.ID != chat.ID {
		t.Fatalf("GetChatByTGID: %+v %v", gotByTGID, err)
	}

	joinedChats, err := store.ListChats(ctx, storage.ChatFilter{Statuses: []domain.ChatStatus{domain.ChatStatusJoined}})
	if err != nil {
		t.Fatalf("ListChats status: %v", err)
	}
	if len(joinedChats) != 2 {
		t.Fatalf("joined chats = %d, want 2", len(joinedChats))
	}
	found, err := store.ListChats(ctx, storage.ChatFilter{Query: "market"})
	if err != nil {
		t.Fatalf("ListChats query: %v", err)
	}
	if len(found) != 1 || found[0].ID != other.ID {
		t.Fatalf("query result = %+v", found)
	}
	found, err = store.ListChats(ctx, storage.ChatFilter{Query: "100%_off"})
	if err != nil || len(found) != 0 {
		t.Fatalf("LIKE escaping query = %+v err=%v", found, err)
	}

	readAt := time.Now().UTC().Truncate(time.Microsecond)
	scannedAt := readAt.Add(time.Minute)
	if err := store.UpdateChatProgress(ctx, chat.ID, 77, &readAt, &scannedAt); err != nil {
		t.Fatalf("UpdateChatProgress: %v", err)
	}
	progressed, _ := store.GetChatByID(ctx, chat.ID)
	if progressed.LastMessageID != 77 || progressed.LastReadAt == nil || !progressed.LastReadAt.Equal(readAt) ||
		progressed.LastScannedAt == nil || !progressed.LastScannedAt.Equal(scannedAt) {
		t.Fatalf("unexpected progress: %+v", progressed)
	}
	if err := store.UpdateChatProgress(ctx, chat.ID, 80, nil, nil); err != nil {
		t.Fatalf("UpdateChatProgress nil times: %v", err)
	}
	progressed, _ = store.GetChatByID(ctx, chat.ID)
	if progressed.LastMessageID != 80 || progressed.LastReadAt == nil || !progressed.LastReadAt.Equal(readAt) {
		t.Fatalf("nil timestamps should preserve values: %+v", progressed)
	}
	if err := store.UpdateChatProgress(ctx, 9999, 1, nil, nil); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("missing chat progress err = %v", err)
	}

	if _, err := store.GetChatByID(ctx, 9999); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("missing chat err = %v", err)
	}
	if _, err := store.GetChatByTGID(ctx, 999999); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("missing chat tg err = %v", err)
	}
}

func TestJoinQueueEnqueueClaimComplete(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC()

	low, inserted, err := store.EnqueueJoinJob(ctx, domain.JoinQueueItem{
		NormalizedLink: "https://t.me/low",
		RawLink:        "t.me/low",
		Priority:       1,
		NextAttemptAt:  now.Add(-time.Minute),
	})
	if err != nil || !inserted {
		t.Fatalf("EnqueueJoinJob low: %+v inserted=%v err=%v", low, inserted, err)
	}
	duplicate, inserted, err := store.EnqueueJoinJob(ctx, domain.JoinQueueItem{
		NormalizedLink: "https://t.me/low",
		Priority:       99,
	})
	if err != nil {
		t.Fatalf("EnqueueJoinJob duplicate: %v", err)
	}
	if inserted || duplicate.ID != low.ID || duplicate.Priority != 1 {
		t.Fatalf("duplicate handling: %+v inserted=%v", duplicate, inserted)
	}
	high, _, err := store.EnqueueJoinJob(ctx, domain.JoinQueueItem{
		NormalizedLink: "https://t.me/high",
		LinkType:       domain.JoinLinkUsername,
		Priority:       10,
		NextAttemptAt:  now.Add(-time.Minute),
	})
	if err != nil {
		t.Fatalf("EnqueueJoinJob high: %v", err)
	}
	future, _, err := store.EnqueueJoinJob(ctx, domain.JoinQueueItem{
		NormalizedLink: "https://t.me/future",
		Priority:       50,
		NextAttemptAt:  now.Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("EnqueueJoinJob future: %v", err)
	}

	claimed, err := store.ClaimNextJoinJob(ctx, now)
	if err != nil {
		t.Fatalf("ClaimNextJoinJob: %v", err)
	}
	if claimed == nil || claimed.ID != high.ID || claimed.Status != domain.JoinJobInProgress || claimed.Attempts != 1 {
		t.Fatalf("unexpected first claim: %+v", claimed)
	}
	claimed, err = store.ClaimNextJoinJob(ctx, now)
	if err != nil {
		t.Fatalf("ClaimNextJoinJob second: %v", err)
	}
	if claimed == nil || claimed.ID != low.ID || claimed.Attempts != 1 {
		t.Fatalf("unexpected second claim: %+v", claimed)
	}
	claimed, err = store.ClaimNextJoinJob(ctx, now)
	if err != nil || claimed != nil {
		t.Fatalf("expected empty due queue, got %+v err=%v", claimed, err)
	}
	claimed, err = store.ClaimNextJoinJob(ctx, now.Add(2*time.Hour))
	if err != nil || claimed == nil || claimed.ID != future.ID {
		t.Fatalf("future job not claimable later: %+v err=%v", claimed, err)
	}

	retryAt := now.Add(15 * time.Minute).Truncate(time.Microsecond)
	if err := store.CompleteJoinJob(ctx, high.ID, domain.JoinJobFailed, &retryAt, "FLOOD_WAIT"); err != nil {
		t.Fatalf("CompleteJoinJob: %v", err)
	}
	failed, err := store.ListJoinQueue(ctx, domain.JoinJobFailed, 10)
	if err != nil {
		t.Fatalf("ListJoinQueue: %v", err)
	}
	if len(failed) != 1 || failed[0].LastError != "FLOOD_WAIT" || !failed[0].NextAttemptAt.Equal(retryAt) {
		t.Fatalf("unexpected failed job: %+v", failed)
	}
	if err := store.CompleteJoinJob(ctx, 9999, domain.JoinJobDone, nil, ""); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("missing join job err = %v", err)
	}
	if err := store.CompleteJoinJob(ctx, high.ID, domain.JoinJobStatus("bogus"), nil, ""); !errors.Is(err, domain.ErrInvalidInput) {
		t.Errorf("invalid status err = %v", err)
	}
	if _, _, err := store.EnqueueJoinJob(ctx, domain.JoinQueueItem{}); !errors.Is(err, domain.ErrInvalidInput) {
		t.Errorf("empty link err = %v", err)
	}
}

func TestJoinQueueInferLinkType(t *testing.T) {
	tests := map[string]domain.JoinLinkType{
		"https://t.me/addlist/abc": domain.JoinLinkAddList,
		"https://t.me/+abcdef":     domain.JoinLinkInvite,
		"https://t.me/chat/123":    domain.JoinLinkMessage,
		"https://t.me/username":    domain.JoinLinkMessage,
		"username":                 domain.JoinLinkUsername,
	}
	for link, want := range tests {
		if got := inferLinkType(link); got != want {
			t.Errorf("inferLinkType(%q) = %q, want %q", link, got, want)
		}
	}
}
