package telegram

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gotd/td/tgerr"

	"github.com/demonidze1337/mia-lead-radar/internal/domain"
	"github.com/demonidze1337/mia-lead-radar/internal/events"
	tier0filter "github.com/demonidze1337/mia-lead-radar/internal/filter"
	"github.com/demonidze1337/mia-lead-radar/internal/insights"
	"github.com/demonidze1337/mia-lead-radar/internal/llm"
	"github.com/demonidze1337/mia-lead-radar/internal/prompts"
	"github.com/demonidze1337/mia-lead-radar/internal/storage"
)

type fakeQualifier struct {
	calls atomic.Int64
	score int
}

func (f *fakeQualifier) Complete(context.Context, llm.ChatRequest) (llm.ChatResponse, error) {
	return llm.ChatResponse{Content: "{}", Model: "fake-model"}, nil
}

func (f *fakeQualifier) CompleteJSON(_ context.Context, _ llm.ChatRequest, target any) (llm.ChatResponse, error) {
	f.calls.Add(1)
	result, ok := target.(*prompts.QualifyResult)
	if !ok {
		return llm.ChatResponse{}, errors.New("unexpected qualification target")
	}
	result.Score = f.score
	result.IsLead = f.score >= 31
	result.Category = string(domain.CategoryFromScore(f.score))
	result.Lang = "en"
	result.Reason = "clear buyer request"
	result.MatchedConcepts = []string{"website"}
	return llm.ChatResponse{Content: "{}", Model: "fake-model"}, nil
}

func (f *fakeQualifier) Ping(context.Context) error { return nil }
func (f *fakeQualifier) ListModels(context.Context) ([]string, error) {
	return []string{"fake-model"}, nil
}

type fakeNotifier struct {
	calls atomic.Int64
}

func (f *fakeNotifier) NotifyLead(context.Context, domain.Lead, string) error {
	f.calls.Add(1)
	return nil
}

func setupPipeline(t *testing.T, transports ...AccountTransport) (*Pipeline, events.Bus, *fakeQualifier, *fakeNotifier, domain.TelegramAccount, domain.Chat) {
	t.Helper()
	store := testStore(t)
	ctx := context.Background()
	_, err := store.CreateRadar(ctx, domain.RadarProfile{
		Name:            "pipeline",
		Description:     "Website development",
		TargetLanguages: []string{"en"},
		Tags: []domain.RadarTag{
			{Phrase: "website", Kind: domain.RadarTagRoot, Weight: 1, Enabled: true},
			{Phrase: "developer", Kind: domain.RadarTagRoot, Weight: 1, Enabled: true},
		},
		IsActive: true,
	}, domain.RadarChangeManual, "test")
	if err != nil {
		t.Fatalf("CreateRadar: %v", err)
	}
	settings := domain.DefaultRuntimeSettings()
	settings.UILanguage = "en"
	settings.MinLeadScore = 31
	settings.AutoReadEnabled = true
	if err := store.SaveSettings(ctx, settings); err != nil {
		t.Fatalf("SaveSettings: %v", err)
	}
	acc, err := store.UpsertAccount(ctx, domain.TelegramAccount{Label: "main", TGUserID: 100, Status: domain.AccountStatusActive})
	if err != nil {
		t.Fatalf("UpsertAccount: %v", err)
	}
	for i, transport := range transports {
		if i == 0 {
			if mock, ok := transport.(*mockTransport); ok {
				mock.id = acc.ID
			}
			continue
		}
		extra, err := store.UpsertAccount(ctx, domain.TelegramAccount{Label: "extra", TGUserID: int64(100 + i), Status: domain.AccountStatusActive})
		if err != nil {
			t.Fatalf("UpsertAccount extra: %v", err)
		}
		if mock, ok := transport.(*mockTransport); ok {
			mock.id = extra.ID
		}
	}
	chat, err := store.UpsertChat(ctx, domain.Chat{
		TGChatID:          500,
		AccessHash:        44,
		Username:          "buyerchat",
		Title:             "Buyer Chat",
		ChatType:          domain.ChatTypeSupergroup,
		Status:            domain.ChatStatusJoined,
		AssignedAccountID: acc.ID,
	})
	if err != nil {
		t.Fatalf("UpsertChat: %v", err)
	}
	for _, transport := range transports {
		if mock, ok := transport.(*mockTransport); ok && mock.id == 0 {
			mock.id = acc.ID
		}
	}
	bus := events.NewBus(256)
	t.Cleanup(bus.Close)
	qualifier := &fakeQualifier{score: 88}
	notifier := &fakeNotifier{}
	insightService := insights.NewService(store, bus)
	governor := testGovernor()
	gate := NewFloodGate(store, 0, nil)
	pipeline := NewPipeline(PipelineDeps{
		Store:        store,
		Bus:          bus,
		Filter:       tier0filter.NewEngine(),
		LLM:          qualifier,
		Insights:     insightService,
		Governor:     governor,
		FloodGate:    gate,
		Notifier:     notifier,
		Transports:   transports,
		ScanInterval: 10 * time.Millisecond,
		JoinInterval: 15 * time.Millisecond,
		ReadInterval: 20 * time.Millisecond,
	})
	return pipeline, bus, qualifier, notifier, acc, chat
}

func waitUntil(t *testing.T, timeout time.Duration, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if check() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition not reached before timeout")
}

func TestPipelineEndToEndAndJoinQueue(t *testing.T) {
	var readCount atomic.Int64
	transport := &mockTransport{
		recent: func(_ context.Context, tgChatID, _ int64, _ int, _ int) ([]IncomingMessage, error) {
			if tgChatID != 500 {
				return nil, nil
			}
			now := time.Now().UTC()
			return []IncomingMessage{
				{MsgID: 1, AuthorID: 42, AuthorName: "Buyer", Text: "I need a website developer for my project with budget 5000", Timestamp: now},
				{MsgID: 2, AuthorID: 43, AuthorName: "Seller", Text: "I offer website development services, dm me for price list", Timestamp: now.Add(time.Second)},
				{MsgID: 3, AuthorID: 44, AuthorName: "Noise", Text: "hi", Timestamp: now.Add(2 * time.Second)},
			}, nil
		},
		readHistory: func(context.Context, int64, int64, int) error {
			readCount.Add(1)
			return nil
		},
		resolve: func(context.Context, string) (DialogPreview, error) {
			return DialogPreview{TGChatID: 900, AccessHash: 55, Username: "joindev", Title: "Join Dev"}, nil
		},
		joinChannel: func(context.Context, string, int64, int64) (DialogPreview, error) {
			return DialogPreview{TGChatID: 900, AccessHash: 55, Username: "joindev", Title: "Join Dev"}, nil
		},
	}
	pipeline, bus, qualifier, notifier, _, chat := setupPipeline(t, transport)
	store := pipeline.deps.Store
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- pipeline.Run(ctx) }()

	if _, _, err := store.EnqueueJoinJob(ctx, domain.JoinQueueItem{
		NormalizedLink: "https://t.me/joindev", LinkType: domain.JoinLinkUsername, NextAttemptAt: time.Now().Add(-time.Second),
	}); err != nil {
		t.Fatalf("EnqueueJoinJob: %v", err)
	}

	waitUntil(t, 3*time.Second, func() bool {
		leads, _, _ := store.ListLeads(ctx, pipelineLeadFilter())
		return len(leads) >= 1
	})
	waitUntil(t, 3*time.Second, func() bool { return readCount.Load() > 0 })
	waitUntil(t, 3*time.Second, func() bool {
		jobs, _ := store.ListJoinQueue(ctx, domain.JoinJobDone, 10)
		return len(jobs) >= 1
	})

	leads, _, err := store.ListLeads(ctx, pipelineLeadFilter())
	if err != nil || len(leads) != 1 {
		t.Fatalf("leads = %+v err=%v", leads, err)
	}
	lead := leads[0]
	if lead.Score != 88 || lead.Category != domain.LeadCategoryClear || lead.MsgID != 1 || lead.LLMModel != "fake-model" {
		t.Fatalf("lead = %+v", lead)
	}
	if lead.MsgLink != "https://t.me/buyerchat/1" {
		t.Errorf("msg link = %q", lead.MsgLink)
	}
	if qualifier.calls.Load() == 0 {
		t.Error("LLM qualifier was not called")
	}
	if notifier.calls.Load() == 0 {
		t.Error("Owner bot notifier was not called")
	}
	trash, err := store.ListTrashSamples(ctx, lead.RadarID, 50)
	if err != nil || len(trash) == 0 {
		t.Fatalf("trash samples = %+v err=%v", trash, err)
	}
	progressed, _ := store.GetChatByTGID(ctx, chat.TGChatID)
	if progressed.LastMessageID != 3 {
		t.Errorf("last message = %d, want 3", progressed.LastMessageID)
	}
	stats := pipeline.Stats()
	if stats.ProcessedMsgs == 0 || stats.QualifiedLeads == 0 || !stats.Running {
		t.Errorf("stats = %+v", stats)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("pipeline did not stop")
	}

	types := make(map[domain.EventType]bool)
	for _, ev := range bus.Recent(0) {
		types[ev.Type] = true
	}
	for _, want := range []domain.EventType{domain.EventTier0Rejected, domain.EventTier0Passed, domain.EventLLMQualified, domain.EventLeadNew} {
		if !types[want] {
			t.Errorf("missing event %q", want)
		}
	}
}

func pipelineLeadFilter() storage.LeadFilter {
	return storage.LeadFilter{}
}

func TestPipelinePauseResume(t *testing.T) {
	transport := &mockTransport{
		recent: func(context.Context, int64, int64, int, int) ([]IncomingMessage, error) {
			return []IncomingMessage{{MsgID: 1, Text: "I need a website developer with budget 3000", AuthorID: 1, Timestamp: time.Now().UTC()}}, nil
		},
	}
	pipeline, _, _, _, _, _ := setupPipeline(t, transport)
	store := pipeline.deps.Store
	pipeline.Pause()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- pipeline.Run(ctx) }()
	time.Sleep(80 * time.Millisecond)
	if leads, _, _ := store.ListLeads(ctx, pipelineLeadFilter()); len(leads) != 0 {
		t.Fatalf("paused pipeline processed leads: %+v", leads)
	}
	if !pipeline.Stats().Paused {
		t.Error("Stats().Paused is false")
	}
	pipeline.Resume()
	waitUntil(t, 3*time.Second, func() bool {
		leads, _, _ := store.ListLeads(ctx, pipelineLeadFilter())
		return len(leads) > 0
	})
	cancel()
	<-done
}

func TestPipelineFloodWaitSwitchesAccount(t *testing.T) {
	transport1 := &mockTransport{
		recent: func(context.Context, int64, int64, int, int) ([]IncomingMessage, error) {
			return nil, tgerr.New(420, "FLOOD_WAIT_60")
		},
	}
	transport2 := &mockTransport{
		recent: func(context.Context, int64, int64, int, int) ([]IncomingMessage, error) {
			return []IncomingMessage{{MsgID: 1, Text: "I need a website developer with budget 7000", AuthorID: 2, Timestamp: time.Now().UTC()}}, nil
		},
	}
	pipeline, bus, _, _, _, _ := setupPipeline(t, transport1, transport2)
	store := pipeline.deps.Store
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = pipeline.Run(ctx) }()
	waitUntil(t, 4*time.Second, func() bool {
		leads, _, _ := store.ListLeads(ctx, pipelineLeadFilter())
		return len(leads) > 0
	})
	accounts, _ := store.ListAccounts(ctx)
	if len(accounts) < 2 {
		t.Fatalf("accounts = %+v", accounts)
	}
	blocked := 0
	for _, acc := range accounts {
		if isBlocked, _ := pipeline.deps.FloodGate.IsBlocked(acc.ID); isBlocked {
			blocked++
		}
	}
	if blocked == 0 {
		t.Fatal("first account was not FloodGate-blocked")
	}
	foundFloodEvent := false
	for _, ev := range bus.Recent(0) {
		if ev.Type == domain.EventMTProtoFlood {
			foundFloodEvent = true
		}
	}
	if !foundFloodEvent {
		t.Error("MTProto flood event missing")
	}
	if pipeline.Stats().FloodedAccount == 0 {
		t.Error("flooded account stat is zero")
	}
}
