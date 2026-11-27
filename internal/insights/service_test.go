package insights

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/demonidze1337/mia-lead-radar/internal/domain"
	"github.com/demonidze1337/mia-lead-radar/internal/events"
	"github.com/demonidze1337/mia-lead-radar/internal/storage"
	"github.com/demonidze1337/mia-lead-radar/internal/storage/sqlite"
)

func newTestManager(t *testing.T, now time.Time) (*Manager, *sqlite.SQLiteStore, events.Bus) {
	t.Helper()
	store, err := sqlite.New(filepath.Join(t.TempDir(), "insights.db"))
	if err != nil {
		t.Fatalf("sqlite.New: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	bus := events.NewBus(64)
	t.Cleanup(bus.Close)
	manager := NewService(store, bus, WithClock(func() time.Time { return now }))
	return manager, store, bus
}

func createRadar(t *testing.T, store storage.Store, name string) domain.RadarProfile {
	t.Helper()
	radar, err := store.CreateRadar(context.Background(), domain.RadarProfile{
		Name:            name,
		Description:     "Website development for small businesses",
		TargetLanguages: []string{"en"},
		Tags: []domain.RadarTag{
			{Phrase: "website", Kind: domain.RadarTagRoot, Weight: 1, Enabled: true},
			{Phrase: "developer", Kind: domain.RadarTagRoot, Weight: 1, Enabled: true},
		},
		IsActive: true,
	}, domain.RadarChangeManual, "test setup")
	if err != nil {
		t.Fatalf("CreateRadar: %v", err)
	}
	return radar
}

func createLead(t *testing.T, store storage.Store, radarID int64, msgID int, text string) domain.Lead {
	t.Helper()
	lead, _, err := store.SaveLead(context.Background(), domain.Lead{
		RadarID:     radarID,
		TGChatID:    900,
		MsgID:       msgID,
		ChatTitle:   "Buyer Chat",
		Text:        text,
		Lang:        "en",
		Score:       70,
		Category:    domain.LeadCategoryPotential,
		Status:      domain.LeadStatusNew,
		MatchedTags: []string{"website"},
	})
	if err != nil {
		t.Fatalf("SaveLead: %v", err)
	}
	return lead
}

func TestRecordTier0Outcome(t *testing.T) {
	now := time.Now().UTC()
	manager, store, _ := newTestManager(t, now)
	radar := createRadar(t, store, "tier0")
	ctx := context.Background()

	if err := manager.RecordTier0Outcome(ctx, radar.ID, false, []string{"website", "developer", "website", ""}); err != nil {
		t.Fatalf("RecordTier0Outcome rejected: %v", err)
	}
	if err := manager.RecordTier0Outcome(ctx, radar.ID, true, []string{"website"}); err != nil {
		t.Fatalf("RecordTier0Outcome passed: %v", err)
	}
	summary, err := store.GetFunnelSummary(ctx, radar.ID, 1, 75)
	if err != nil {
		t.Fatalf("GetFunnelSummary: %v", err)
	}
	if summary.MessagesSeen != 2 || summary.Tier0Rejected != 1 || summary.Tier0Passed != 1 {
		t.Fatalf("unexpected summary: %+v", summary)
	}
	stats, err := store.ListTagStats(ctx, radar.ID)
	if err != nil {
		t.Fatalf("ListTagStats: %v", err)
	}
	if len(stats) != 2 {
		t.Fatalf("stats = %d, want 2 (duplicates deduped)", len(stats))
	}
	for _, stat := range stats {
		if stat.TotalHits != 1 || stat.TrashHits != 1 || stat.LeadHits != 0 {
			t.Errorf("unexpected tag stat: %+v", stat)
		}
	}
	if err := manager.RecordTier0Outcome(ctx, 0, false, nil); !errors.Is(err, domain.ErrInvalidInput) {
		t.Errorf("invalid radar err = %v", err)
	}
}

func TestRecordLLMOutcome(t *testing.T) {
	now := time.Now().UTC()
	manager, store, _ := newTestManager(t, now)
	radar := createRadar(t, store, "llm")
	ctx := context.Background()

	if err := manager.RecordLLMOutcome(ctx, radar.ID, domain.LeadCategoryTrash, []string{"website"}); err != nil {
		t.Fatalf("RecordLLMOutcome trash: %v", err)
	}
	if err := manager.RecordLLMOutcome(ctx, radar.ID, domain.LeadCategoryClear, []string{"website", "developer"}); err != nil {
		t.Fatalf("RecordLLMOutcome clear: %v", err)
	}
	summary, err := store.GetFunnelSummary(ctx, radar.ID, 1, 75)
	if err != nil {
		t.Fatalf("GetFunnelSummary: %v", err)
	}
	if summary.LLMCalled != 2 || summary.TrashCount != 1 || summary.ClearCount != 1 {
		t.Fatalf("unexpected summary: %+v", summary)
	}
	if summary.SavedTimeSeconds != rejectedNoiseSavedSeconds+qualifiedLeadSavedSeconds {
		t.Fatalf("saved seconds = %d", summary.SavedTimeSeconds)
	}
	stats, _ := store.ListTagStats(ctx, radar.ID)
	if len(stats) != 2 {
		t.Fatalf("stats = %+v", stats)
	}
	for _, stat := range stats {
		switch stat.Tag {
		case "website":
			if stat.TotalHits != 2 || stat.TrashHits != 1 || stat.LeadHits != 1 {
				t.Errorf("unexpected website stat: %+v", stat)
			}
		case "developer":
			if stat.TotalHits != 1 || stat.TrashHits != 0 || stat.LeadHits != 1 {
				t.Errorf("unexpected developer stat: %+v", stat)
			}
		}
	}
	if err := manager.RecordLLMOutcome(ctx, radar.ID, domain.LeadCategory("bogus"), nil); !errors.Is(err, domain.ErrInvalidInput) {
		t.Errorf("invalid category err = %v", err)
	}
}

func TestFunnelActionsIdempotent(t *testing.T) {
	now := time.Now().UTC()
	manager, store, _ := newTestManager(t, now)
	radar := createRadar(t, store, "funnel")
	lead := createLead(t, store, radar.ID, 1, "I need a website developer with budget.")
	ctx := context.Background()

	for i := 0; i < 2; i++ {
		if err := manager.RecordFunnelAction(ctx, radar.ID, lead.ID, FunnelViewed); err != nil {
			t.Fatalf("FunnelViewed: %v", err)
		}
		if err := manager.RecordFunnelAction(ctx, radar.ID, lead.ID, FunnelProfileOpened); err != nil {
			t.Fatalf("FunnelProfileOpened: %v", err)
		}
	}
	summary, _ := store.GetFunnelSummary(ctx, radar.ID, 1, 75)
	if summary.ViewedCount != 1 || summary.ProfileOpenedCount != 1 {
		t.Fatalf("idempotency failed: %+v", summary)
	}
	reloaded, _ := store.GetLeadByID(ctx, lead.ID)
	if reloaded.ViewedAt == nil || reloaded.ProfileOpenedAt == nil {
		t.Fatalf("lead timestamps missing: %+v", reloaded)
	}

	for i := 0; i < 2; i++ {
		if err := manager.RecordFunnelAction(ctx, radar.ID, lead.ID, FunnelIcebreaker); err != nil {
			t.Fatalf("FunnelIcebreaker: %v", err)
		}
	}
	summary, _ = store.GetFunnelSummary(ctx, radar.ID, 1, 75)
	if summary.IcebreakerCount != 2 || summary.SavedTimeSeconds != 2*icebreakerSavedSeconds {
		t.Fatalf("icebreaker telemetry: %+v", summary)
	}
	if err := manager.RecordIcebreaker(ctx, radar.ID, lead.ID, "Hi, saw your post", 40); err != nil {
		t.Fatalf("RecordIcebreaker: %v", err)
	}
	reloaded, _ = store.GetLeadByID(ctx, lead.ID)
	if reloaded.IcebreakerCount != 1 || len(reloaded.Icebreakers) != 1 {
		t.Fatalf("draft not persisted: %+v", reloaded)
	}
	summary, _ = store.GetFunnelSummary(ctx, radar.ID, 1, 75)
	if summary.IcebreakerCount != 3 || summary.SavedTimeSeconds != 2*icebreakerSavedSeconds+40 {
		t.Fatalf("extended icebreaker telemetry: %+v", summary)
	}
	if err := manager.RecordFunnelAction(ctx, radar.ID, lead.ID, FunnelAction("bogus")); !errors.Is(err, domain.ErrInvalidInput) {
		t.Errorf("invalid action err = %v", err)
	}
}

func TestFunnelViewedConcurrentIdempotent(t *testing.T) {
	now := time.Now().UTC()
	manager, store, _ := newTestManager(t, now)
	radar := createRadar(t, store, "concurrent")
	lead := createLead(t, store, radar.ID, 1, "Concurrent view lead with website developer budget.")
	ctx := context.Background()

	var wg sync.WaitGroup
	errs := make(chan error, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := manager.RecordFunnelAction(ctx, radar.ID, lead.ID, FunnelViewed); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("concurrent viewed: %v", err)
	}
	summary, _ := store.GetFunnelSummary(ctx, radar.ID, 1, 75)
	if summary.ViewedCount != 1 {
		t.Fatalf("ViewedCount = %d, want 1", summary.ViewedCount)
	}
}

func TestMarkFalsePositiveLearnsStopWords(t *testing.T) {
	now := time.Now().UTC()
	manager, store, _ := newTestManager(t, now)
	radar := createRadar(t, store, "learn")
	text := "I am looking for a cheap website designer in London for a crypto giveaway project with budget."
	lead := createLead(t, store, radar.ID, 1, text)
	ctx := context.Background()

	updated, err := manager.MarkFalsePositive(ctx, radar.ID, lead.ID, true)
	if err != nil {
		t.Fatalf("MarkFalsePositive: %v", err)
	}
	if updated.Version != radar.Version+1 {
		t.Fatalf("version = %d, want %d", updated.Version, radar.Version+1)
	}
	joined := stringsJoin(updated.LearnedStopWords)
	for _, want := range []string{"giveaway", "crypto", "london"} {
		if !contains(updated.LearnedStopWords, want) {
			t.Errorf("learned stop words %v missing %q (%s)", updated.LearnedStopWords, want, joined)
		}
	}
	for _, banned := range []string{"website", "developer", "looking"} {
		if contains(updated.LearnedStopWords, banned) {
			t.Errorf("learned excluded/common word %q: %v", banned, updated.LearnedStopWords)
		}
	}
	reloaded, _ := store.GetLeadByID(ctx, lead.ID)
	if reloaded.Status != domain.LeadStatusFalsePositive {
		t.Fatalf("status = %q", reloaded.Status)
	}
	summary, _ := store.GetFunnelSummary(ctx, radar.ID, 1, 75)
	if summary.FalsePositiveCount != 1 {
		t.Fatalf("false positive count = %d", summary.FalsePositiveCount)
	}
	stats, _ := store.ListTagStats(ctx, radar.ID)
	if len(stats) != 1 || stats[0].FalsePositiveHits != 1 || stats[0].TotalHits != 1 {
		t.Fatalf("tag stats = %+v", stats)
	}

	again, err := manager.MarkFalsePositive(ctx, radar.ID, lead.ID, true)
	if err != nil {
		t.Fatalf("second MarkFalsePositive: %v", err)
	}
	if again.Version != updated.Version {
		t.Fatalf("re-marking bumped version: %d -> %d", updated.Version, again.Version)
	}
	summary, _ = store.GetFunnelSummary(ctx, radar.ID, 1, 75)
	if summary.FalsePositiveCount != 1 {
		t.Fatalf("re-marking double-counted: %d", summary.FalsePositiveCount)
	}
}

func TestEvaluateNoiseGuard(t *testing.T) {
	now := time.Now().UTC()
	manager, store, bus := newTestManager(t, now)
	radar := createRadar(t, store, "noise")
	ctx := context.Background()
	eventsCh, unsubscribe := bus.Subscribe(4)
	defer unsubscribe()

	for i := 0; i < 8; i++ {
		if err := store.RecordTagHit(ctx, radar.ID, "broad", false, false, now); err != nil {
			t.Fatalf("RecordTagHit broad: %v", err)
		}
		if err := store.RecordTagHit(ctx, radar.ID, "clean", true, false, now); err != nil {
			t.Fatalf("RecordTagHit clean: %v", err)
		}
	}
	alert, err := manager.EvaluateNoiseGuard(ctx, radar.ID)
	if err != nil {
		t.Fatalf("EvaluateNoiseGuard: %v", err)
	}
	if alert == nil {
		t.Fatal("expected noise alert")
	}
	if len(alert.ThrottledTags) != 1 || alert.ThrottledTags[0] != "broad" {
		t.Fatalf("throttled = %#v", alert.ThrottledTags)
	}
	if len(alert.NoisyTags) != 1 || alert.NoisyTags[0].Tag != "broad" {
		t.Fatalf("noisy = %+v", alert.NoisyTags)
	}
	if alert.ThresholdPct != 75 || alert.TriggeredAt.IsZero() {
		t.Fatalf("alert fields = %+v", alert)
	}
	select {
	case ev := <-eventsCh:
		if ev.Type != domain.EventNoiseGuard {
			t.Fatalf("event type = %q", ev.Type)
		}
		if ev.Data["radar_id"] != radar.ID {
			t.Fatalf("event data = %#v", ev.Data)
		}
	case <-time.After(time.Second):
		t.Fatal("no NoiseGuard event published")
	}
	stats, _ := store.ListTagStats(ctx, radar.ID)
	for _, stat := range stats {
		if stat.Tag == "broad" {
			if stat.ThrottledUntil == nil || !stat.ThrottledUntil.After(now) {
				t.Fatalf("broad tag not throttled: %+v", stat)
			}
		}
	}

	second, err := manager.EvaluateNoiseGuard(ctx, radar.ID)
	if err != nil {
		t.Fatalf("second EvaluateNoiseGuard: %v", err)
	}
	if second == nil || len(second.ThrottledTags) != 0 {
		t.Fatalf("already throttled alert = %+v", second)
	}
	select {
	case ev := <-eventsCh:
		t.Fatalf("unexpected duplicate event: %+v", ev)
	default:
	}

	settings, _ := store.GetSettings(ctx)
	settings.NoiseGuardEnabled = false
	if err := store.SaveSettings(ctx, settings); err != nil {
		t.Fatalf("SaveSettings: %v", err)
	}
	if alert, err := manager.EvaluateNoiseGuard(ctx, radar.ID); err != nil || alert != nil {
		t.Fatalf("disabled guard = %+v, %v", alert, err)
	}

	cleanRadar := createRadar(t, store, "clean-radar")
	for i := 0; i < 8; i++ {
		if err := store.RecordTagHit(ctx, cleanRadar.ID, "clean", true, false, now); err != nil {
			t.Fatalf("clean radar hit: %v", err)
		}
	}
	settings.NoiseGuardEnabled = true
	_ = store.SaveSettings(ctx, settings)
	if alert, err := manager.EvaluateNoiseGuard(ctx, cleanRadar.ID); err != nil || alert != nil {
		t.Fatalf("clean radar = %+v, %v", alert, err)
	}
}

func TestExportLeadsCSVJSONAndFormulaSafety(t *testing.T) {
	now := time.Now().UTC()
	manager, store, _ := newTestManager(t, now)
	radar := createRadar(t, store, "export")
	ctx := context.Background()

	createLead(t, store, radar.ID, 1, `=HYPERLINK("http://evil","click")`)
	lead2 := createLead(t, store, radar.ID, 2, "I need a website developer, budget 1000.")
	lead2.MatchedTags = []string{"website", "developer"}
	if _, _, err := store.SaveLead(ctx, lead2); err != nil {
		t.Fatalf("save lead2: %v", err)
	}
	if err := store.UpdateLeadStatus(ctx, lead2.ID, domain.LeadStatusHidden); err != nil {
		t.Fatalf("hide lead2: %v", err)
	}
	createLead(t, store, radar.ID, 3, "Third lead with +plus and @mention")

	var csvBuffer bytes.Buffer
	if err := manager.ExportLeads(ctx, radar.ID, ExportCSV, &csvBuffer); err != nil {
		t.Fatalf("ExportCSV: %v", err)
	}
	rows, err := csv.NewReader(bytes.NewReader(csvBuffer.Bytes())).ReadAll()
	if err != nil {
		t.Fatalf("parse CSV: %v", err)
	}
	if len(rows) != 4 {
		t.Fatalf("CSV rows = %d, want 4", len(rows))
	}
	var formulaRow, hiddenRow []string
	for _, row := range rows[1:] {
		switch row[0] {
		case "1":
			formulaRow = row
		case "2":
			hiddenRow = row
		}
	}
	if formulaRow == nil || !stringsHasPrefix(formulaRow[11], "'=") {
		t.Fatalf("formula cell not neutralized: %#v", formulaRow)
	}
	if hiddenRow == nil || hiddenRow[12] != "en" || hiddenRow[15] != "hidden" {
		t.Fatalf("unexpected hidden CSV row: %#v", hiddenRow)
	}

	var jsonBuffer bytes.Buffer
	if err := manager.ExportLeads(ctx, radar.ID, ExportJSON, &jsonBuffer); err != nil {
		t.Fatalf("ExportJSON: %v", err)
	}
	var decoded []domain.Lead
	if err := json.Unmarshal(jsonBuffer.Bytes(), &decoded); err != nil {
		t.Fatalf("decode JSON export: %v", err)
	}
	if len(decoded) != 3 {
		t.Fatalf("JSON leads = %d, want 3 (hidden included)", len(decoded))
	}
	if err := manager.ExportLeads(ctx, radar.ID, ExportFormat("xml"), io.Discard); !errors.Is(err, domain.ErrInvalidInput) {
		t.Errorf("unsupported export err = %v", err)
	}
	if err := manager.ExportLeads(ctx, radar.ID, ExportCSV, nil); !errors.Is(err, domain.ErrInvalidInput) {
		t.Errorf("nil writer err = %v", err)
	}
}

func TestExportLeadsPagination(t *testing.T) {
	now := time.Now().UTC()
	manager, store, _ := newTestManager(t, now)
	radar := createRadar(t, store, "pagination")
	ctx := context.Background()

	err := store.WithTx(ctx, func(tx storage.Store) error {
		for i := 0; i < 501; i++ {
			if _, _, err := tx.SaveLead(ctx, domain.Lead{
				RadarID:   radar.ID,
				TGChatID:  777,
				MsgID:     i + 1,
				ChatTitle: "Bulk",
				Text:      fmt.Sprintf("Lead number %d needs a website developer", i),
				Lang:      "en",
				Score:     50,
				Category:  domain.LeadCategoryDoubtful,
				Status:    domain.LeadStatusNew,
			}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("bulk save: %v", err)
	}
	var buffer bytes.Buffer
	if err := manager.ExportLeads(ctx, radar.ID, ExportJSON, &buffer); err != nil {
		t.Fatalf("ExportLeads: %v", err)
	}
	var decoded []domain.Lead
	if err := json.Unmarshal(buffer.Bytes(), &decoded); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(decoded) != 501 {
		t.Fatalf("exported %d leads, want 501", len(decoded))
	}
}

func TestNeutralizeCSV(t *testing.T) {
	tests := map[string]string{
		"=1+1":      "'=1+1",
		"+phone":    "'+phone",
		"-cmd":      "'-cmd",
		"@user":     "'@user",
		"\tcell":    "'\tcell",
		"\rcell":    "'\rcell",
		"safe text": "safe text",
		"":          "",
	}
	for input, want := range tests {
		if got := neutralizeCSV(input); got != want {
			t.Errorf("neutralizeCSV(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestDispatchLeadWebhookHMAC(t *testing.T) {
	now := time.Now().UTC()
	manager, store, _ := newTestManager(t, now)
	radar := createRadar(t, store, "webhook")
	lead := createLead(t, store, radar.ID, 1, "Webhook lead text")

	var received WebhookPayload
	var rawBody []byte
	var signature, contentType string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rawBody, _ = io.ReadAll(r.Body)
		signature = r.Header.Get("X-MIA-Signature")
		contentType = r.Header.Get("Content-Type")
		_ = json.Unmarshal(rawBody, &received)
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	if err := manager.DispatchLeadWebhook(context.Background(), server.URL, "topsecret", radar.Name, lead); err != nil {
		t.Fatalf("DispatchLeadWebhook: %v", err)
	}
	if contentType != "application/json" {
		t.Errorf("content type = %q", contentType)
	}
	if received.Event != string(domain.EventLeadNew) || received.Lead.ID != lead.ID || received.RadarName != radar.Name {
		t.Fatalf("payload = %+v", received)
	}
	mac := hmac.New(sha256.New, []byte("topsecret"))
	_, _ = mac.Write(rawBody)
	wantSignature := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	if signature != wantSignature {
		t.Fatalf("signature = %q, want %q", signature, wantSignature)
	}

	for _, badURL := range []string{"", "ftp://example.com/hook", "file:///tmp/x"} {
		if err := manager.DispatchLeadWebhook(context.Background(), badURL, "", "", lead); !errors.Is(err, domain.ErrInvalidInput) {
			t.Errorf("URL %q err = %v", badURL, err)
		}
	}
	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("boom"))
	}))
	defer failing.Close()
	if err := manager.DispatchLeadWebhook(context.Background(), failing.URL, "", "", lead); err == nil {
		t.Fatal("expected webhook status error")
	}
}

func TestDispatchLeadWebhookTimeout(t *testing.T) {
	now := time.Now().UTC()
	manager, store, _ := newTestManager(t, now)
	manager.webhookTimeout = 30 * time.Millisecond
	radar := createRadar(t, store, "timeout")
	lead := createLead(t, store, radar.ID, 1, "Timeout lead")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	err := manager.DispatchLeadWebhook(context.Background(), server.URL, "", "", lead)
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

func TestGetSummary(t *testing.T) {
	now := time.Now().UTC()
	manager, store, _ := newTestManager(t, now)
	radar := createRadar(t, store, "summary")
	ctx := context.Background()
	if err := manager.RecordTier0Outcome(ctx, radar.ID, false, []string{"website"}); err != nil {
		t.Fatalf("RecordTier0Outcome: %v", err)
	}
	summary, err := manager.GetSummary(ctx, radar.ID, 7)
	if err != nil {
		t.Fatalf("GetSummary: %v", err)
	}
	if summary.MessagesSeen != 1 || summary.Tier0Rejected != 1 {
		t.Fatalf("summary = %+v", summary)
	}
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func stringsJoin(values []string) string {
	result := ""
	for i, value := range values {
		if i > 0 {
			result += ","
		}
		result += value
	}
	return result
}

func stringsHasPrefix(value, prefix string) bool {
	return len(value) >= len(prefix) && value[:len(prefix)] == prefix
}
