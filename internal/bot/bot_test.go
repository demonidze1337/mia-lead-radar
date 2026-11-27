package bot

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/demonidze1337/mia-lead-radar/internal/domain"
	"github.com/demonidze1337/mia-lead-radar/internal/telegram"
)

type fakeController struct {
	paused      atomic.Bool
	pauseCalls  atomic.Int64
	resumeCalls atomic.Int64
}

func (f *fakeController) Pause() {
	f.pauseCalls.Add(1)
	f.paused.Store(true)
}

func (f *fakeController) Resume() {
	f.resumeCalls.Add(1)
	f.paused.Store(false)
}

func (f *fakeController) Stats() telegram.PipelineStats {
	return telegram.PipelineStats{
		Running:        true,
		Paused:         f.paused.Load(),
		ActiveAccounts: 2,
		FloodedAccount: 1,
		ProcessedMsgs:  17,
		QualifiedLeads: 3,
	}
}

type capturedMessage map[string]any

type botServer struct {
	mu       sync.Mutex
	batches  [][]botUpdate
	updates  int
	messages []capturedMessage
	block    bool
}

func (s *botServer) nextBatch() []botUpdate {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.batches) == 0 {
		return nil
	}
	batch := s.batches[0]
	s.batches = s.batches[1:]
	return batch
}

func (s *botServer) sent() []capturedMessage {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]capturedMessage{}, s.messages...)
}

func newBotServer(t *testing.T, batches [][]botUpdate, block bool) (*httptest.Server, *botServer) {
	t.Helper()
	state := &botServer{batches: batches, block: block}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/getUpdates"):
			state.mu.Lock()
			state.updates++
			state.mu.Unlock()
			if state.block {
				<-r.Context().Done()
				return
			}
			time.Sleep(2 * time.Millisecond)
			batch := state.nextBatch()
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": batch})
		case strings.HasSuffix(r.URL.Path, "/sendMessage"):
			var payload map[string]any
			_ = json.NewDecoder(r.Body).Decode(&payload)
			state.mu.Lock()
			state.messages = append(state.messages, payload)
			state.mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": map[string]any{}})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)
	return server, state
}

func ownerUpdate(id int64, sender int64, text string) botUpdate {
	update := botUpdate{UpdateID: id}
	update.Message = &struct {
		MessageID int `json:"message_id"`
		From      *struct {
			ID int64 `json:"id"`
		} `json:"from"`
		Chat struct {
			ID int64 `json:"id"`
		} `json:"chat"`
		Text string `json:"text"`
	}{MessageID: 1, Text: text}
	update.Message.From = &struct {
		ID int64 `json:"id"`
	}{ID: sender}
	update.Message.Chat.ID = sender
	return update
}

func TestOwnerBotOnlyRespondsToOwner(t *testing.T) {
	controller := &fakeController{}
	batches := [][]botUpdate{{
		ownerUpdate(1, 999, "/pause"),
		ownerUpdate(2, 42, "/pause"),
	}}
	server, state := newBotServer(t, batches, false)
	manager := New(Config{Token: "TEST", OwnerTelegramID: 42, APIBase: server.URL}, controller)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- manager.Start(ctx) }()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && (controller.pauseCalls.Load() < 1 || len(state.sent()) < 1) {
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Start: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Start did not stop")
	}
	if controller.pauseCalls.Load() != 1 {
		t.Fatalf("pause calls = %d, want 1 (stranger ignored)", controller.pauseCalls.Load())
	}
	if messages := state.sent(); len(messages) != 1 {
		t.Fatalf("sent = %d, want 1", len(messages))
	}
}

func TestOwnerBotCommands(t *testing.T) {
	controller := &fakeController{}
	summary := func(context.Context) (domain.FunnelSummary, error) {
		return domain.FunnelSummary{MessagesSeen: 100, TotalLeads: 5, TrashCount: 40, TrashRatioPct: 44.4}, nil
	}
	batches := [][]botUpdate{{
		ownerUpdate(1, 42, "/start"),
		ownerUpdate(2, 42, "/help"),
		ownerUpdate(3, 42, "/status"),
		ownerUpdate(4, 42, "/pause"),
		ownerUpdate(5, 42, "/resume"),
		ownerUpdate(6, 42, "/stats"),
	}}
	server, state := newBotServer(t, batches, false)
	manager := New(Config{Token: "TEST", OwnerTelegramID: 42, APIBase: server.URL, Summary: summary}, controller)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- manager.Start(ctx) }()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && len(state.sent()) < 6 {
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	<-done
	messages := state.sent()
	if len(messages) < 6 {
		t.Fatalf("sent = %d, want >= 6", len(messages))
	}
	joined := make([]string, 0, len(messages))
	for _, message := range messages {
		if text, ok := message["text"].(string); ok {
			joined = append(joined, text)
		}
	}
	all := strings.Join(joined, "\n---\n")
	for _, want := range []string{"Owner commands", "pipeline and account status", "Pipeline paused", "Pipeline resumed", "Funnel summary"} {
		if !strings.Contains(all, want) {
			t.Errorf("missing command response %q in %s", want, all)
		}
	}
	if controller.pauseCalls.Load() != 1 || controller.resumeCalls.Load() != 1 {
		t.Fatalf("pause/resume = %d/%d", controller.pauseCalls.Load(), controller.resumeCalls.Load())
	}
}

func TestOwnerBotLeadAlertEscapesHTML(t *testing.T) {
	server, state := newBotServer(t, nil, false)
	manager := New(Config{Token: "TEST", OwnerTelegramID: 42, APIBase: server.URL, WebAppBaseURL: "https://radar.example.com"}, &fakeController{})
	lead := domain.Lead{
		ChatTitle:  `Dev <b>Chat</b>`,
		AuthorName: `Eve & "Co"`,
		Score:      88,
		Category:   domain.LeadCategoryClear,
		Text:       `<script>alert("x")</script> & more`,
		Reason:     `needs <urgent> "help"`,
		MsgLink:    "https://t.me/devchat/1",
	}
	if err := manager.NotifyLead(context.Background(), lead, "Radar <1>"); err != nil {
		t.Fatalf("NotifyLead: %v", err)
	}
	messages := state.sent()
	if len(messages) != 1 {
		t.Fatalf("sent = %d", len(messages))
	}
	text, _ := messages[0]["text"].(string)
	if strings.Contains(text, "<script>") || !strings.Contains(text, "&lt;script&gt;") {
		t.Fatalf("HTML not escaped: %s", text)
	}
	if !strings.Contains(text, "&amp;") || strings.Contains(text, `Eve & "Co"`) {
		t.Fatalf("ampersand/quote escaping missing: %s", text)
	}
	if messages[0]["parse_mode"] != "HTML" {
		t.Errorf("parse_mode = %v", messages[0]["parse_mode"])
	}
	markup, ok := messages[0]["reply_markup"].(map[string]any)
	if !ok {
		t.Fatalf("missing reply markup: %#v", messages[0])
	}
	raw, _ := json.Marshal(markup)
	if !strings.Contains(string(raw), "https://t.me/devchat/1") || !strings.Contains(string(raw), "https://radar.example.com") {
		t.Fatalf("keyboard URLs missing: %s", raw)
	}
}

func TestOwnerBotGracefulStopAndErrors(t *testing.T) {
	server, _ := newBotServer(t, nil, false)
	manager := New(Config{Token: "TEST", OwnerTelegramID: 42, APIBase: server.URL, PollTimeout: time.Second}, &fakeController{})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- manager.Start(ctx) }()
	time.Sleep(30 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Start returned %v on cancellation", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("graceful stop timed out")
	}

	bad := New(Config{Token: "", OwnerTelegramID: 42}, &fakeController{})
	if err := bad.Start(context.Background()); err == nil {
		t.Fatal("expected missing token error")
	}
	noController := New(Config{Token: "TEST", OwnerTelegramID: 42}, nil)
	if err := noController.Start(context.Background()); err == nil {
		t.Fatal("expected missing controller error")
	}
	disabled := New(Config{},
		&fakeController{})
	if err := disabled.NotifyLead(context.Background(), domain.Lead{}, ""); err != nil {
		t.Fatalf("disabled bot should no-op: %v", err)
	}
}

func TestOwnerBotAccountAlert(t *testing.T) {
	server, state := newBotServer(t, nil, false)
	manager := New(Config{Token: "TEST", OwnerTelegramID: 42, APIBase: server.URL}, &fakeController{})
	acc := domain.TelegramAccount{Label: "main <x>", Status: domain.AccountStatusFloodWait}
	if err := manager.NotifyAccountAlert(context.Background(), acc, "FLOOD_WAIT_60"); err != nil {
		t.Fatalf("NotifyAccountAlert: %v", err)
	}
	messages := state.sent()
	if len(messages) != 1 {
		t.Fatalf("sent = %d", len(messages))
	}
	text := messages[0]["text"].(string)
	if !strings.Contains(text, "&lt;x&gt;") || !strings.Contains(text, "FLOOD_WAIT_60") {
		t.Fatalf("alert text = %q", text)
	}
}
