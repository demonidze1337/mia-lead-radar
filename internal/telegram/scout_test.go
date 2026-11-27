package telegram

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/demonidze1337/mia-lead-radar/internal/domain"
)

func TestAssessChatQuality(t *testing.T) {
	now := time.Now().UTC()
	transport := &mockTransport{
		recent: func(context.Context, int64, int64, int, int) ([]IncomingMessage, error) {
			return []IncomingMessage{
				{Timestamp: now.Add(-10 * time.Minute)},
				{Timestamp: now.Add(-20 * time.Minute)},
				{Timestamp: now.Add(-2 * time.Hour)},
			}, nil
		},
	}
	preview := DialogPreview{TGChatID: 1, MemberCount: 100, ChatType: domain.ChatTypeGroup}
	report := AssessChatQuality(context.Background(), transport, preview, 50, 1)
	if !report.Passed || report.MsgVelocityPerHour != 2 {
		t.Fatalf("expected pass with velocity 2: %+v", report)
	}
	report = AssessChatQuality(context.Background(), transport, preview, 500, 1)
	if report.Passed || report.Reason == "" {
		t.Fatalf("member threshold not enforced: %+v", report)
	}
	report = AssessChatQuality(context.Background(), transport, preview, 50, 10)
	if report.Passed || report.Reason == "" {
		t.Fatalf("velocity threshold not enforced: %+v", report)
	}

	channel := DialogPreview{TGChatID: 2, MemberCount: 1000, ChatType: domain.ChatTypeChannel}
	report = AssessChatQuality(context.Background(), transport, channel, 50, 1)
	if report.Passed || report.Reason == "" {
		t.Fatalf("comment-less broadcast channel accepted: %+v", report)
	}
	channel.HasComments = true
	report = AssessChatQuality(context.Background(), transport, channel, 50, 1)
	if !report.Passed {
		t.Fatalf("channel with comments rejected: %+v", report)
	}

	failing := &mockTransport{recent: func(context.Context, int64, int64, int, int) ([]IncomingMessage, error) {
		return nil, errors.New("boom")
	}}
	report = AssessChatQuality(context.Background(), failing, preview, 50, 1)
	if report.Passed || report.Reason == "" {
		t.Fatalf("transport error not reported: %+v", report)
	}
}

func TestScoutImportMyChats(t *testing.T) {
	store := testStore(t)
	transport := &mockTransport{
		id: 5,
		dialogs: func(context.Context, int) ([]DialogPreview, error) {
			return []DialogPreview{
				{TGChatID: 11, Username: "first", Title: "First", ChatType: domain.ChatTypeGroup, MemberCount: 12, FolderNames: []string{"Work"}},
				{TGChatID: 12, Username: "second", Title: "Second", ChatType: domain.ChatTypeSupergroup, MemberCount: 50},
			}, nil
		},
	}
	scout := NewScout(store, testGovernor(), transport)
	chats, err := scout.ImportMyChats(context.Background())
	if err != nil {
		t.Fatalf("ImportMyChats: %v", err)
	}
	if len(chats) != 2 {
		t.Fatalf("chats = %d", len(chats))
	}
	first, err := store.GetChatByTGID(context.Background(), 11)
	if err != nil {
		t.Fatalf("GetChatByTGID: %v", err)
	}
	if first.Status != domain.ChatStatusJoined || first.Source != domain.ChatSourceUserImport || first.AssignedAccountID != 5 {
		t.Fatalf("imported chat = %+v", first)
	}
}

func TestScoutSearchAndQueue(t *testing.T) {
	store := testStore(t)
	transport := &mockTransport{
		id: 6,
		search: func(context.Context, string, int) ([]DialogPreview, error) {
			return []DialogPreview{
				{TGChatID: 21, Username: "PublicChat", Title: "Public"},
				{TGChatID: 22, Title: "Private No Username"},
			}, nil
		},
	}
	scout := NewScout(store, testGovernor(), transport)
	chats, err := scout.SearchAndQueue(context.Background(), "dev", 10)
	if err != nil {
		t.Fatalf("SearchAndQueue: %v", err)
	}
	if len(chats) != 2 {
		t.Fatalf("chats = %d", len(chats))
	}
	jobs, err := store.ListJoinQueue(context.Background(), domain.JoinJobPending, 10)
	if err != nil {
		t.Fatalf("ListJoinQueue: %v", err)
	}
	if len(jobs) != 1 || jobs[0].NormalizedLink != "https://t.me/publicchat" || jobs[0].Priority != 5 {
		t.Fatalf("jobs = %+v", jobs)
	}
	if _, err := scout.SearchAndQueue(context.Background(), " ", 10); !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatalf("empty query err = %v", err)
	}
}

func TestScoutExtractAndEnqueueLinks(t *testing.T) {
	store := testStore(t)
	scout := NewScout(store, nil, &mockTransport{id: 1})
	messages := []IncomingMessage{
		{ChatID: 100, Text: "join https://t.me/DevChat and https://t.me/+InviteHash"},
		{ChatID: 101, Text: "folder https://t.me/addlist/Tools and duplicate https://t.me/devchat"},
	}
	count, err := scout.ExtractAndEnqueueLinks(context.Background(), messages)
	if err != nil {
		t.Fatalf("ExtractAndEnqueueLinks: %v", err)
	}
	if count != 3 {
		t.Fatalf("inserted = %d, want 3", count)
	}
	jobs, err := store.ListJoinQueue(context.Background(), domain.JoinJobPending, 10)
	if err != nil {
		t.Fatalf("ListJoinQueue: %v", err)
	}
	if len(jobs) != 3 {
		t.Fatalf("jobs = %+v", jobs)
	}
	foundAddList := false
	for _, item := range jobs {
		if item.LinkType == domain.JoinLinkAddList {
			foundAddList = true
			if item.NormalizedLink != "https://t.me/addlist/Tools" || item.Priority != 10 {
				t.Errorf("addlist item = %+v", item)
			}
		}
	}
	if !foundAddList {
		t.Error("addlist link missing")
	}
}

func testGovernor() *RateGovernor {
	g := NewRateGovernor(map[OpKind]OpPolicy{
		OpJoin:          {Burst: 100, Interval: time.Millisecond},
		OpSearch:        {Burst: 100, Interval: time.Millisecond},
		OpReadHistory:   {Burst: 100, Interval: time.Millisecond},
		OpFetchMessages: {Burst: 100, Interval: time.Millisecond},
	}, nil)
	g.jitter = func(time.Duration) time.Duration { return 0 }
	return g
}
