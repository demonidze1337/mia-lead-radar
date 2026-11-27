package telegram

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/gotd/td/tgerr"

	"github.com/demonidze1337/mia-lead-radar/internal/domain"
	"github.com/demonidze1337/mia-lead-radar/internal/storage/sqlite"
)

func TestClassifyTGError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		kind TGErrorClass
		wait time.Duration
	}{
		{"nil", nil, TGErrorNone, 0},
		{"flood tgerr", tgerr.New(420, "FLOOD_WAIT_42"), TGErrorFloodWait, 42 * time.Second},
		{"flood premium", tgerr.New(420, "FLOOD_PREMIUM_WAIT_7"), TGErrorFloodWait, 7 * time.Second},
		{"flood string", errors.New("rpc error: code = 420 desc = FLOOD_WAIT_17"), TGErrorFloodWait, 17 * time.Second},
		{"already", tgerr.New(400, "USER_ALREADY_PARTICIPANT"), TGErrorAlreadyParticipant, 0},
		{"invite sent", tgerr.New(400, "INVITE_REQUEST_SENT"), TGErrorInviteRequestSent, 0},
		{"bad invite", errors.New("rpc error: code = 400 desc = INVITE_HASH_EXPIRED"), TGErrorBadTarget, 0},
		{"bad username", tgerr.New(400, "USERNAME_NOT_OCCUPIED"), TGErrorBadTarget, 0},
		{"too much", tgerr.New(400, "CHANNELS_TOO_MUCH"), TGErrorChannelsTooMuch, 0},
		{"banned", tgerr.New(403, "USER_BANNED_IN_CHANNEL"), TGErrorBanned, 0},
		{"restricted", errors.New("rpc error: code = 400 desc = USER_RESTRICTED"), TGErrorBanned, 0},
		{"write forbidden", tgerr.New(403, "CHAT_WRITE_FORBIDDEN"), TGErrorBanned, 0},
		{"transient", errors.New("connection reset by peer"), TGErrorTransient, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			kind, wait := ClassifyTGError(tc.err)
			if kind != tc.kind || wait != tc.wait {
				t.Fatalf("ClassifyTGError = %q/%v, want %q/%v", kind, wait, tc.kind, tc.wait)
			}
		})
	}
}

func newJoinTest(t *testing.T, transport AccountTransport) (*Joiner, *FloodGate, domain.TelegramAccount, *sqlite.SQLiteStore) {
	t.Helper()
	store := testStore(t)
	ctx := context.Background()
	acc, err := store.UpsertAccount(ctx, domain.TelegramAccount{
		Label: "joiner", TGUserID: 99, Status: domain.AccountStatusActive,
	})
	if err != nil {
		t.Fatalf("UpsertAccount: %v", err)
	}
	if transport == nil {
		transport = &mockTransport{id: acc.ID}
	}
	gate := NewFloodGate(store, 0, nil)
	governor := NewRateGovernor(map[OpKind]OpPolicy{
		OpJoin: {Burst: 10, Interval: time.Millisecond},
	}, nil)
	governor.jitter = func(time.Duration) time.Duration { return 0 }
	return NewJoiner(store, gate, governor, transport), gate, acc, store
}

func enqueueJoin(t *testing.T, store *sqlite.SQLiteStore, link string, linkType domain.JoinLinkType) domain.JoinQueueItem {
	t.Helper()
	item, _, err := store.EnqueueJoinJob(context.Background(), domain.JoinQueueItem{
		NormalizedLink: link,
		LinkType:       linkType,
		NextAttemptAt:  time.Now().Add(-time.Second),
	})
	if err != nil {
		t.Fatalf("EnqueueJoinJob: %v", err)
	}
	return item
}

func TestJoinerUsernameSuccess(t *testing.T) {
	preview := DialogPreview{TGChatID: 555, AccessHash: 77, Username: "devchat", Title: "Dev Chat", ChatType: domain.ChatTypeSupergroup, MemberCount: 500}
	transport := &mockTransport{}
	joiner, _, acc, store := newJoinTest(t, transport)
	transport.id = acc.ID
	transport.resolve = func(context.Context, string) (DialogPreview, error) { return preview, nil }
	transport.joinChannel = func(context.Context, string, int64, int64) (DialogPreview, error) { return preview, nil }
	item := enqueueJoin(t, store, "https://t.me/devchat", domain.JoinLinkUsername)

	outcome, err := joiner.Join(context.Background(), item)
	if err != nil {
		t.Fatalf("Join: %v", err)
	}
	if outcome.Status != domain.ChatStatusJoined || len(outcome.Chats) != 1 {
		t.Fatalf("outcome = %+v", outcome)
	}
	chat, err := store.GetChatByTGID(context.Background(), 555)
	if err != nil || chat.Status != domain.ChatStatusJoined || chat.AssignedAccountID != acc.ID {
		t.Fatalf("chat = %+v err=%v", chat, err)
	}
	reloaded, _ := store.GetAccount(context.Background(), acc.ID)
	if reloaded.JoinedChatsCount != 1 || reloaded.DailyJoinsCount != 1 {
		t.Fatalf("account counters = %+v", reloaded)
	}
	jobs, _ := store.ListJoinQueue(context.Background(), domain.JoinJobDone, 10)
	if len(jobs) != 1 {
		t.Fatalf("jobs = %+v", jobs)
	}
}

func TestJoinerAddListAndInvite(t *testing.T) {
	transport := &mockTransport{}
	joiner, _, acc, store := newJoinTest(t, transport)
	transport.id = acc.ID
	transport.importList = func(context.Context, string) ([]DialogPreview, error) {
		return []DialogPreview{{TGChatID: 1, Title: "A"}, {TGChatID: 2, Title: "B"}}, nil
	}
	item := enqueueJoin(t, store, "https://t.me/addlist/folder", domain.JoinLinkAddList)
	outcome, err := joiner.Join(context.Background(), item)
	if err != nil || outcome.Status != domain.ChatStatusJoined || len(outcome.Chats) != 2 {
		t.Fatalf("addlist outcome=%+v err=%v", outcome, err)
	}
	reloaded, _ := store.GetAccount(context.Background(), acc.ID)
	if reloaded.JoinedChatsCount != 2 {
		t.Fatalf("joined count = %d", reloaded.JoinedChatsCount)
	}

	transport.importInvite = func(context.Context, string) (DialogPreview, error) {
		return DialogPreview{TGChatID: 3, Title: "Invite"}, nil
	}
	item = enqueueJoin(t, store, "https://t.me/+secret", domain.JoinLinkInvite)
	outcome, err = joiner.Join(context.Background(), item)
	if err != nil || outcome.Status != domain.ChatStatusJoined || len(outcome.Chats) != 1 {
		t.Fatalf("invite outcome=%+v err=%v", outcome, err)
	}
}

func TestJoinerFloodWaitAndAlreadyParticipant(t *testing.T) {
	transport := &mockTransport{}
	joiner, gate, acc, store := newJoinTest(t, transport)
	transport.id = acc.ID
	preview := DialogPreview{TGChatID: 777, Username: "floodchat", Title: "Flood"}
	transport.resolve = func(context.Context, string) (DialogPreview, error) { return preview, nil }
	transport.joinChannel = func(context.Context, string, int64, int64) (DialogPreview, error) {
		return DialogPreview{}, tgerr.New(420, "FLOOD_WAIT_3")
	}
	item := enqueueJoin(t, store, "https://t.me/floodchat", domain.JoinLinkUsername)
	outcome, err := joiner.Join(context.Background(), item)
	if err != nil {
		t.Fatalf("Join flood: %v", err)
	}
	if outcome.ErrorClass != TGErrorFloodWait || outcome.FloodWait < 2*time.Second {
		t.Fatalf("flood outcome = %+v", outcome)
	}
	if blocked, _ := gate.IsBlocked(acc.ID); !blocked {
		t.Fatal("FloodGate not registered")
	}
	account, _ := store.GetAccount(context.Background(), acc.ID)
	if account.Status != domain.AccountStatusFloodWait {
		t.Fatalf("account status = %q", account.Status)
	}
	jobs, _ := store.ListJoinQueue(context.Background(), domain.JoinJobFailed, 10)
	if len(jobs) != 1 || jobs[0].NextAttemptAt.Before(time.Now()) {
		t.Fatalf("failed jobs = %+v", jobs)
	}

	transport.joinChannel = func(context.Context, string, int64, int64) (DialogPreview, error) {
		return DialogPreview{}, tgerr.New(400, "USER_ALREADY_PARTICIPANT")
	}
	transport2 := &mockTransport{}
	joiner2, _, acc2, store2 := newJoinTest(t, transport2)
	transport2.id = acc2.ID
	transport2.resolve = func(context.Context, string) (DialogPreview, error) {
		return DialogPreview{TGChatID: 778, Username: "alreadychat", Title: "Already"}, nil
	}
	transport2.joinChannel = func(context.Context, string, int64, int64) (DialogPreview, error) {
		return DialogPreview{}, tgerr.New(400, "USER_ALREADY_PARTICIPANT")
	}
	item = enqueueJoin(t, store2, "https://t.me/alreadychat", domain.JoinLinkUsername)
	outcome, err = joiner2.Join(context.Background(), item)
	if err != nil || outcome.Status != domain.ChatStatusJoined {
		t.Fatalf("already outcome=%+v err=%v", outcome, err)
	}
}

func TestJoinerPermanentFailureAndChannelsFull(t *testing.T) {
	transport := &mockTransport{}
	joiner, _, acc, store := newJoinTest(t, transport)
	transport.id = acc.ID
	transport.resolve = func(context.Context, string) (DialogPreview, error) {
		return DialogPreview{}, tgerr.New(400, "USERNAME_NOT_OCCUPIED")
	}
	item := enqueueJoin(t, store, "https://t.me/missing", domain.JoinLinkUsername)
	outcome, err := joiner.Join(context.Background(), item)
	if err != nil || outcome.ErrorClass != TGErrorBadTarget {
		t.Fatalf("bad target outcome=%+v err=%v", outcome, err)
	}
	jobs, _ := store.ListJoinQueue(context.Background(), domain.JoinJobDead, 10)
	if len(jobs) != 1 {
		t.Fatalf("bad target job = %+v", jobs)
	}

	transport.resolve = func(context.Context, string) (DialogPreview, error) {
		return DialogPreview{TGChatID: 123, Username: "bigchat"}, nil
	}
	transport.joinChannel = func(context.Context, string, int64, int64) (DialogPreview, error) {
		return DialogPreview{}, tgerr.New(400, "CHANNELS_TOO_MUCH")
	}
	item = enqueueJoin(t, store, "https://t.me/bigchat", domain.JoinLinkUsername)
	outcome, err = joiner.Join(context.Background(), item)
	if err != nil || outcome.ErrorClass != TGErrorChannelsTooMuch {
		t.Fatalf("too much outcome=%+v err=%v", outcome, err)
	}
	account, _ := store.GetAccount(context.Background(), acc.ID)
	if account.Status != domain.AccountStatusChannelsFull {
		t.Fatalf("account status = %q", account.Status)
	}
}

func TestJoinerContextCancellation(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	acc, err := store.UpsertAccount(ctx, domain.TelegramAccount{TGUserID: 44, Status: domain.AccountStatusActive})
	if err != nil {
		t.Fatalf("UpsertAccount: %v", err)
	}
	transport := &mockTransport{id: acc.ID}
	governor := NewRateGovernor(map[OpKind]OpPolicy{
		OpJoin: {Burst: 1, Interval: time.Hour},
	}, nil)
	governor.jitter = func(time.Duration) time.Duration { return 0 }
	if err := governor.Wait(ctx, acc.ID, OpJoin); err != nil {
		t.Fatalf("prime: %v", err)
	}
	joiner := NewJoiner(store, NewFloodGate(store, 0, nil), governor, transport)
	item := enqueueJoin(t, store, "https://t.me/devchat", domain.JoinLinkUsername)
	waitCtx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()
	if _, err := joiner.Join(waitCtx, item); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want deadline exceeded", err)
	}
}
