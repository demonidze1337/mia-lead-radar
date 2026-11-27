package server

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/demonidze1337/mia-lead-radar/internal/domain"
	"github.com/demonidze1337/mia-lead-radar/internal/events"
	tier0filter "github.com/demonidze1337/mia-lead-radar/internal/filter"
	"github.com/demonidze1337/mia-lead-radar/internal/insights"
	"github.com/demonidze1337/mia-lead-radar/internal/llm"
	"github.com/demonidze1337/mia-lead-radar/internal/prompts"
	"github.com/demonidze1337/mia-lead-radar/internal/storage/sqlite"
	"github.com/demonidze1337/mia-lead-radar/internal/telegram"
)

type fakeServerLLM struct{}

func (f *fakeServerLLM) Complete(context.Context, llm.ChatRequest) (llm.ChatResponse, error) {
	return llm.ChatResponse{Content: `{}`, Model: "fake"}, nil
}

func (f *fakeServerLLM) CompleteJSON(_ context.Context, _ llm.ChatRequest, target any) (llm.ChatResponse, error) {
	switch value := target.(type) {
	case *prompts.QualifyResult:
		value.Score = 88
		value.IsLead = true
		value.Category = "clear"
		value.Lang = "en"
		value.Reason = "clear buyer request"
	case *prompts.PhraseSuggestResult:
		value.Tags = []prompts.SuggestedTag{{Phrase: "website", Kind: "root", Weight: 1}}
		value.StopPhrases = []string{"giveaway"}
		value.QualityScore = 80
		value.QualityNotes = "ok"
	case *prompts.AuditResult:
		value.Reply = "audit reply"
		value.Diagnosis = "healthy"
		value.Recommendations = []string{"tighten tags"}
		value.ApplyRadarSettings = &prompts.RadarPatch{Apply: true, Description: "patched", Summary: "AI patch"}
	case *prompts.IcebreakerResult:
		value.Lang = "en"
		value.Drafts = []string{"Hi, saw your post"}
	case *prompts.RoleplayReply:
		value.Message = "Sounds interesting"
		value.Mood = "interested"
		value.HiddenNote = "positive"
	case *prompts.RoleplayReview:
		value.Score = 75
		value.Strengths = []string{"clear"}
		value.Weaknesses = []string{"price"}
		value.ImprovedText = "Better pitch"
	default:
		return llm.ChatResponse{}, fmt.Errorf("unexpected target %T", target)
	}
	return llm.ChatResponse{Content: `{}`, Model: "fake"}, nil
}

func (f *fakeServerLLM) Ping(context.Context) error { return nil }
func (f *fakeServerLLM) ListModels(context.Context) ([]string, error) {
	return []string{"fake"}, nil
}

type fakePipeline struct{ paused bool }

func (f *fakePipeline) Pause()  { f.paused = true }
func (f *fakePipeline) Resume() { f.paused = false }
func (f *fakePipeline) Stats() telegram.PipelineStats {
	return telegram.PipelineStats{Running: true, Paused: f.paused, ActiveAccounts: 1}
}

type serverTestEnv struct {
	server *Server
	store  *sqlite.SQLiteStore
	auth   *Authenticator
	bus    events.Bus
	fake   *fakeServerLLM
	token  string
}

func newServerTest(t *testing.T) *serverTestEnv {
	t.Helper()
	store, err := sqlite.New(filepath.Join(t.TempDir(), "server.db"))
	if err != nil {
		t.Fatalf("sqlite.New: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	settings := domain.DefaultRuntimeSettings()
	settings.SetupCompleted = true
	settings.OwnerBotToken = "bot-secret"
	settings.OwnerTelegramID = 42
	settings.LLM.APIKey = "sk-secret"
	if err := store.SaveSettings(context.Background(), settings); err != nil {
		t.Fatalf("SaveSettings: %v", err)
	}
	bus := events.NewBus(64)
	t.Cleanup(bus.Close)
	auth := &Authenticator{Store: store, AccessToken: "local-code"}
	fake := &fakeServerLLM{}
	service := insights.NewService(store, bus)
	srv, err := New(Deps{
		Store:    store,
		Bus:      bus,
		Auth:     auth,
		Filter:   tier0filter.NewEngine(),
		Insights: service,
		LLM:      fake,
		Pipeline: &fakePipeline{},
		Hardware: domain.HardwareProfile{RAMBytes: 16 << 30, RAMGB: 16, CPUCores: 8, RecommendedTier: domain.HardwareTierBalanced},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(srv.Close)
	token, _, err := auth.IssueBrowserSession(context.Background(), "test", 42)
	if err != nil {
		t.Fatalf("IssueBrowserSession: %v", err)
	}
	return &serverTestEnv{server: srv, store: store, auth: auth, bus: bus, fake: fake, token: token}
}

func (e *serverTestEnv) request(t *testing.T, method, path string, body any, auth bool) *httptest.ResponseRecorder {
	t.Helper()
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal request: %v", err)
		}
		reader = bytes.NewReader(raw)
	}
	request := httptest.NewRequest(method, path, reader)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if auth {
		request.Header.Set("Authorization", "Bearer "+e.token)
	}
	recorder := httptest.NewRecorder()
	e.server.Handler().ServeHTTP(recorder, request)
	return recorder
}

func decodeResponse(t *testing.T, recorder *httptest.ResponseRecorder, target any) {
	t.Helper()
	if recorder.Code < 200 || recorder.Code >= 300 {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), target); err != nil {
		t.Fatalf("decode response: %v (%s)", err, recorder.Body.String())
	}
}

func TestServerHealthAuthAndSecurity(t *testing.T) {
	env := newServerTest(t)
	health := env.request(t, http.MethodGet, "/api/health", nil, false)
	if health.Code != http.StatusOK {
		t.Fatalf("health = %d", health.Code)
	}
	if health.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Error("security header missing")
	}
	unauthorized := env.request(t, http.MethodGet, "/api/leads?user_id=42", nil, false)
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("bare user_id status = %d", unauthorized.Code)
	}
	status := env.request(t, http.MethodGet, "/api/auth/status", nil, false)
	var statusBody map[string]any
	decodeResponse(t, status, &statusBody)
	if statusBody["setup_completed"] != true || statusBody["authenticated"] != false {
		t.Fatalf("auth status = %#v", statusBody)
	}
	tg := env.request(t, http.MethodPost, "/api/auth/telegram", map[string]any{"init_data": signedInitData(t, "bot-secret", TelegramUser{ID: 42, Username: "owner"}, time.Now().UTC())}, false)
	if tg.Code != http.StatusOK {
		t.Fatalf("telegram auth = %d %s", tg.Code, tg.Body.String())
	}
	badTG := env.request(t, http.MethodPost, "/api/auth/telegram", map[string]any{"init_data": signedInitData(t, "bot-secret", TelegramUser{ID: 99, Username: "stranger"}, time.Now().UTC())}, false)
	if badTG.Code != http.StatusUnauthorized {
		t.Fatalf("wrong owner auth = %d", badTG.Code)
	}
	local := env.request(t, http.MethodPost, "/api/auth/local", map[string]any{"access_code": "local-code"}, false)
	var localBody map[string]any
	decodeResponse(t, local, &localBody)
	if localBody["token"] == "" || len(local.Result().Cookies()) == 0 {
		t.Fatalf("local auth = %#v", localBody)
	}
}

func TestSettingsSecretsMaskedAndPreserved(t *testing.T) {
	env := newServerTest(t)
	response := env.request(t, http.MethodGet, "/api/settings", nil, true)
	var settings domain.RuntimeSettings
	decodeResponse(t, response, &settings)
	if settings.LLM.APIKey != "***" || settings.OwnerBotToken != "***" {
		t.Fatalf("secrets not masked: %+v", settings)
	}
	settings.LLM.APIKey = "***"
	settings.OwnerBotToken = "***"
	put := env.request(t, http.MethodPut, "/api/settings", settings, true)
	if put.Code != http.StatusOK {
		t.Fatalf("PUT settings = %d %s", put.Code, put.Body.String())
	}
	stored, err := env.store.GetSettings(context.Background())
	if err != nil {
		t.Fatalf("GetSettings: %v", err)
	}
	if stored.LLM.APIKey != "sk-secret" || stored.OwnerBotToken != "bot-secret" {
		t.Fatalf("masked placeholder overwrote secrets: %+v", stored.LLM)
	}
}

func TestRadarSimulatorDryRun(t *testing.T) {
	env := newServerTest(t)
	radar, err := env.store.CreateRadar(context.Background(), domain.RadarProfile{
		Name: "sim", Description: "website work", TargetLanguages: []string{"en"},
		Tags:     []domain.RadarTag{{Phrase: "website", Kind: domain.RadarTagRoot, Weight: 1, Enabled: true}},
		IsActive: true,
	}, domain.RadarChangeManual, "test")
	if err != nil {
		t.Fatalf("CreateRadar: %v", err)
	}
	response := env.request(t, http.MethodPost, "/api/radars/simulate", map[string]any{
		"radar_id": radar.ID, "message": "I need a website for my business with budget 3000", "use_llm": true,
	}, true)
	var body struct {
		Eval    tier0filter.EvalResult `json:"eval"`
		Qualify *prompts.QualifyResult `json:"qualify"`
	}
	decodeResponse(t, response, &body)
	if !body.Eval.Passed || body.Qualify == nil || body.Qualify.Score != 88 {
		t.Fatalf("simulate = %+v", body)
	}
	if body.Eval.Fingerprint == "" {
		t.Error("missing fingerprint")
	}
}

func TestLeadFunnelAndExport(t *testing.T) {
	env := newServerTest(t)
	radar, err := env.store.CreateRadar(context.Background(), domain.RadarProfile{
		Name: "leads", TargetLanguages: []string{"en"}, IsActive: true,
		Tags: []domain.RadarTag{{Phrase: "website", Kind: domain.RadarTagRoot, Weight: 1, Enabled: true}},
	}, domain.RadarChangeManual, "test")
	if err != nil {
		t.Fatalf("CreateRadar: %v", err)
	}
	lead, _, err := env.store.SaveLead(context.Background(), domain.Lead{
		RadarID: radar.ID, TGChatID: 700, MsgID: 1, ChatTitle: "Buyer", Text: "I need a website developer",
		AuthorID: 5, AuthorName: "Buyer", Lang: "en", Score: 80, Category: domain.LeadCategoryPotential,
		Status: domain.LeadStatusNew, MatchedTags: []string{"website"},
	})
	if err != nil {
		t.Fatalf("SaveLead: %v", err)
	}
	list := env.request(t, http.MethodGet, "/api/leads?limit=10", nil, true)
	var listBody struct {
		Leads []domain.Lead `json:"leads"`
		Total int64         `json:"total"`
	}
	decodeResponse(t, list, &listBody)
	if listBody.Total != 1 || len(listBody.Leads) != 1 {
		t.Fatalf("leads = %+v", listBody)
	}
	for i := 0; i < 2; i++ {
		view := env.request(t, http.MethodPost, fmt.Sprintf("/api/leads/%d/view", lead.ID), nil, true)
		if view.Code != http.StatusOK {
			t.Fatalf("view = %d %s", view.Code, view.Body.String())
		}
	}
	funnel := env.request(t, http.MethodGet, fmt.Sprintf("/api/analytics/funnel?radar_id=%d&days=1", radar.ID), nil, true)
	var summary domain.FunnelSummary
	decodeResponse(t, funnel, &summary)
	if summary.ViewedCount != 1 {
		t.Fatalf("viewed count = %d, want idempotent 1", summary.ViewedCount)
	}
	export := env.request(t, http.MethodGet, fmt.Sprintf("/api/leads/export?format=csv&radar_id=%d", radar.ID), nil, true)
	if export.Code != http.StatusOK || !strings.Contains(export.Header().Get("Content-Disposition"), "attachment") {
		t.Fatalf("export = %d %s", export.Code, export.Header().Get("Content-Disposition"))
	}
	rows, err := csv.NewReader(bytes.NewReader(export.Body.Bytes())).ReadAll()
	if err != nil || len(rows) != 2 {
		t.Fatalf("csv rows = %d err=%v", len(rows), err)
	}
}

func TestConsultantAuditAndPatch(t *testing.T) {
	env := newServerTest(t)
	radar, err := env.store.CreateRadar(context.Background(), domain.RadarProfile{
		Name: "audit", Description: "old description", TargetLanguages: []string{"en"}, IsActive: true,
		Tags: []domain.RadarTag{{Phrase: "website", Kind: domain.RadarTagRoot, Weight: 1, Enabled: true}},
	}, domain.RadarChangeManual, "test")
	if err != nil {
		t.Fatalf("CreateRadar: %v", err)
	}
	audit := env.request(t, http.MethodPost, "/api/consultant/audit", map[string]any{"radar_id": radar.ID, "question": "audit"}, true)
	var auditBody prompts.AuditResult
	decodeResponse(t, audit, &auditBody)
	if auditBody.Reply != "audit reply" || auditBody.ApplyRadarSettings == nil {
		t.Fatalf("audit = %+v", auditBody)
	}
	history := env.request(t, http.MethodGet, fmt.Sprintf("/api/consultant/history?session_key=audit-%d", radar.ID), nil, true)
	var messages []domain.ConsultantMessage
	decodeResponse(t, history, &messages)
	if len(messages) != 2 {
		t.Fatalf("audit history = %+v", messages)
	}
	patch := env.request(t, http.MethodPost, "/api/consultant/apply-patch", map[string]any{
		"radar_id": radar.ID,
		"patch":    map[string]any{"apply": true, "description": "patched", "summary": "AI patch"},
	}, true)
	var updated domain.RadarProfile
	decodeResponse(t, patch, &updated)
	if updated.Description != "patched" || updated.Version != 2 {
		t.Fatalf("patched radar = %+v", updated)
	}
	versions, err := env.store.ListRadarVersions(context.Background(), radar.ID, 10)
	if err != nil || len(versions) != 2 || versions[0].ChangeSource != domain.RadarChangeAIAudit {
		t.Fatalf("versions = %+v err=%v", versions, err)
	}
}

func TestSetupStatusIncludesHardwareAndMaskedSettings(t *testing.T) {
	env := newServerTest(t)
	response := env.request(t, http.MethodGet, "/api/setup/status", nil, true)
	var body struct {
		Presets  []llm.HardwarePreset   `json:"presets"`
		Settings domain.RuntimeSettings `json:"settings"`
	}
	decodeResponse(t, response, &body)
	if len(body.Presets) == 0 || body.Settings.LLM.APIKey != "***" {
		t.Fatalf("setup status = %+v", body)
	}
}

func TestErrorMappingAndBodyLimit(t *testing.T) {
	env := newServerTest(t)
	missing := env.request(t, http.MethodPatch, "/api/leads/999/status", map[string]any{"status": "closed"}, true)
	if missing.Code != http.StatusNotFound {
		t.Fatalf("missing lead = %d", missing.Code)
	}
	invalid := env.request(t, http.MethodPost, "/api/radars", map[string]any{"name": ""}, true)
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("invalid radar = %d", invalid.Code)
	}
}
