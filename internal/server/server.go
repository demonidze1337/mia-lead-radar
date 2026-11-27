package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/demonidze1337/mia-lead-radar/internal/config"
	"github.com/demonidze1337/mia-lead-radar/internal/domain"
	"github.com/demonidze1337/mia-lead-radar/internal/events"
	tier0filter "github.com/demonidze1337/mia-lead-radar/internal/filter"
	"github.com/demonidze1337/mia-lead-radar/internal/insights"
	"github.com/demonidze1337/mia-lead-radar/internal/llm"
	"github.com/demonidze1337/mia-lead-radar/internal/storage"
	"github.com/demonidze1337/mia-lead-radar/internal/telegram"
)

// PipelineController exposes pause/resume/stats for HTTP and Owner Bot.
type PipelineController interface {
	Pause()
	Resume()
	Stats() telegram.PipelineStats
}

// Deps wires the HTTP server to application services.
type Deps struct {
	Store     storage.Store
	Bus       events.Bus
	Auth      *Authenticator
	Filter    tier0filter.Engine
	Insights  insights.Service
	LLM       llm.Provider
	Pipeline  PipelineController
	Telegram  *telegram.MTProtoClient
	Scout     *telegram.Scout
	Hardware  domain.HardwareProfile
	Debug     bool
	Static    http.Handler
	Logger    *slog.Logger
	WebAppURL string
}

// Server hosts REST, WebSocket, auth, and embedded SPA routes.
type Server struct {
	deps Deps
	auth *Authenticator
	ws   *WSHub
	mux  http.Handler

	llmMu sync.RWMutex
	llm   llm.Provider
}

// New creates the HTTP server and its routes.
func New(deps Deps) (*Server, error) {
	if deps.Store == nil {
		return nil, fmt.Errorf("%w: server store is required", domain.ErrInvalidInput)
	}
	if deps.Auth == nil {
		deps.Auth = &Authenticator{Store: deps.Store}
	}
	if deps.Logger == nil {
		deps.Logger = slog.Default()
	}
	if deps.Hardware.RAMBytes == 0 && deps.Hardware.CPUCores == 0 {
		deps.Hardware = config.DetectHardware()
	}
	s := &Server{deps: deps, auth: deps.Auth, llm: deps.LLM}
	s.ws = NewWSHub(deps.Bus, deps.Auth)
	s.mux = s.routes()
	return s, nil
}

// Handler returns the root HTTP handler.
func (s *Server) Handler() http.Handler { return s.mux }

// Close drains the WebSocket hub.
func (s *Server) Close() { s.ws.Close() }

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", s.handleHealth)
	mux.HandleFunc("GET /api/auth/status", s.handleAuthStatus)
	mux.HandleFunc("POST /api/auth/telegram", s.handleAuthTelegram)
	mux.HandleFunc("POST /api/auth/local", s.handleAuthLocal)
	mux.HandleFunc("POST /api/auth/logout", s.handleAuthLogout)

	protected := func(pattern string, handler http.HandlerFunc) {
		mux.Handle(pattern, s.auth.RequireAuth(handler))
	}

	protected("GET /api/ws/terminal", s.ws.HandleWS)
	mux.HandleFunc("GET /api/setup/status", s.authWrap(s.handleSetupStatus))
	mux.HandleFunc("POST /api/setup/llm/test", s.authWrap(s.handleLLMTest))
	mux.HandleFunc("POST /api/setup/llm/save", s.authWrap(s.handleLLMSave))
	mux.HandleFunc("POST /api/setup/telegram/qr", s.authWrap(s.handleTelegramQR))
	mux.HandleFunc("POST /api/setup/telegram/phone", s.authWrap(s.handleTelegramPhone))
	mux.HandleFunc("POST /api/setup/telegram/code", s.authWrap(s.handleTelegramCode))
	mux.HandleFunc("POST /api/setup/telegram/password", s.authWrap(s.handleTelegramPassword))
	mux.HandleFunc("GET /api/setup/telegram/status", s.authWrap(s.handleTelegramStatus))
	mux.HandleFunc("POST /api/setup/bot", s.authWrap(s.handleSetupBot))
	mux.HandleFunc("POST /api/setup/complete", s.authWrap(s.handleSetupComplete))
	mux.HandleFunc("GET /api/settings", s.authWrap(s.handleGetSettings))
	mux.HandleFunc("PUT /api/settings", s.authWrap(s.handlePutSettings))

	mux.HandleFunc("GET /api/radars", s.authWrap(s.handleListRadars))
	mux.HandleFunc("POST /api/radars", s.authWrap(s.handleCreateRadar))
	mux.HandleFunc("PUT /api/radars/{id}", s.authWrap(s.handleUpdateRadar))
	mux.HandleFunc("GET /api/radars/{id}/versions", s.authWrap(s.handleRadarVersions))
	mux.HandleFunc("POST /api/radars/suggest", s.authWrap(s.handleRadarSuggest))
	mux.HandleFunc("POST /api/radars/simulate", s.authWrap(s.handleRadarSimulate))

	mux.HandleFunc("GET /api/leads", s.authWrap(s.handleListLeads))
	mux.HandleFunc("GET /api/leads/export", s.authWrap(s.handleExportLeads))
	mux.HandleFunc("PATCH /api/leads/{id}/status", s.authWrap(s.handleLeadStatus))
	mux.HandleFunc("POST /api/leads/{id}/view", s.authWrap(s.handleLeadViewed))
	mux.HandleFunc("POST /api/leads/{id}/profile-opened", s.authWrap(s.handleLeadProfileOpened))
	mux.HandleFunc("POST /api/leads/{id}/false-positive", s.authWrap(s.handleLeadFalsePositive))
	mux.HandleFunc("POST /api/leads/{id}/icebreaker", s.authWrap(s.handleLeadIcebreaker))

	mux.HandleFunc("GET /api/chats", s.authWrap(s.handleListChats))
	mux.HandleFunc("POST /api/chats/import-dialogs", s.authWrap(s.handleImportDialogs))
	mux.HandleFunc("POST /api/chats/enqueue", s.authWrap(s.handleEnqueueLinks))
	mux.HandleFunc("GET /api/accounts", s.authWrap(s.handleListAccounts))
	mux.HandleFunc("POST /api/accounts/{id}/clear-flood", s.authWrap(s.handleClearFlood))
	mux.HandleFunc("DELETE /api/accounts/{id}", s.authWrap(s.handleDeleteAccount))
	mux.HandleFunc("GET /api/pipeline/stats", s.authWrap(s.handlePipelineStats))
	mux.HandleFunc("POST /api/pipeline/pause", s.authWrap(s.handlePipelinePause))
	mux.HandleFunc("POST /api/pipeline/resume", s.authWrap(s.handlePipelineResume))

	mux.HandleFunc("POST /api/consultant/audit", s.authWrap(s.handleConsultantAudit))
	mux.HandleFunc("POST /api/consultant/apply-patch", s.authWrap(s.handleConsultantApplyPatch))
	mux.HandleFunc("POST /api/consultant/roleplay", s.authWrap(s.handleConsultantRoleplay))
	mux.HandleFunc("POST /api/consultant/roleplay/review", s.authWrap(s.handleConsultantReview))
	mux.HandleFunc("GET /api/consultant/history", s.authWrap(s.handleConsultantHistory))

	mux.HandleFunc("GET /api/analytics/funnel", s.authWrap(s.handleAnalyticsFunnel))
	mux.HandleFunc("GET /api/analytics/daily", s.authWrap(s.handleAnalyticsDaily))
	mux.HandleFunc("GET /api/analytics/trash-samples", s.authWrap(s.handleAnalyticsTrash))
	mux.HandleFunc("GET /api/analytics/tags", s.authWrap(s.handleAnalyticsTags))
	mux.HandleFunc("POST /api/analytics/noise-guard/check", s.authWrap(s.handleNoiseGuardCheck))
	mux.HandleFunc("POST /api/analytics/tags/throttle", s.authWrap(s.handleTagThrottle))

	static := s.deps.Static
	if static == nil {
		static = StaticHandler()
	}
	mux.Handle("GET /", static)
	return s.securityHeaders(s.limitRequestBody(mux))
}

func (s *Server) authWrap(handler http.HandlerFunc) http.HandlerFunc {
	protected := s.auth.RequireAuth(handler)
	return func(w http.ResponseWriter, r *http.Request) { protected.ServeHTTP(w, r) }
}

func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("X-Frame-Options", "SAMEORIGIN")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		next.ServeHTTP(w, r)
	})
}

func (s *Server) limitRequestBody(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body != nil && strings.HasPrefix(r.URL.Path, "/api/") {
			r.Body = http.MaxBytesReader(w, r.Body, 2<<20)
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) getLLM() llm.Provider {
	s.llmMu.RLock()
	defer s.llmMu.RUnlock()
	return s.llm
}

func (s *Server) setLLM(provider llm.Provider) {
	s.llmMu.Lock()
	s.llm = provider
	s.llmMu.Unlock()
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "version": "dev"})
}

func (s *Server) handleAuthStatus(w http.ResponseWriter, r *http.Request) {
	settings, err := s.deps.Store.GetSettings(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	response := map[string]any{
		"authenticated":   false,
		"setup_completed": settings.SetupCompleted,
	}
	if ac, err := s.auth.AuthenticateRequest(r); err == nil {
		response["authenticated"] = true
		response["auth"] = ac
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleAuthTelegram(w http.ResponseWriter, r *http.Request) {
	var body struct {
		InitData string `json:"init_data"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	settings, err := s.deps.Store.GetSettings(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	if settings.OwnerTelegramID <= 0 {
		writeError(w, fmt.Errorf("%w: owner Telegram id is not configured", domain.ErrUnauthorized))
		return
	}
	user, err := VerifyTelegramInitData(body.InitData, settings.OwnerBotToken, settings.OwnerTelegramID, time.Now().UTC(), MaxInitDataAge)
	if err != nil {
		writeError(w, err)
		return
	}
	token, sess, err := s.auth.IssueBrowserSession(r.Context(), "telegram", user.ID)
	if err != nil {
		writeError(w, err)
		return
	}
	setSessionCookie(w, r, token)
	writeJSON(w, http.StatusOK, map[string]any{"token": token, "session": sess})
}

func (s *Server) handleAuthLocal(w http.ResponseWriter, r *http.Request) {
	var body struct {
		AccessCode string `json:"access_code"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if !s.auth.CanIssueLocalSession(r, body.AccessCode) {
		writeError(w, domain.ErrUnauthorized)
		return
	}
	token, sess, err := s.auth.IssueBrowserSession(r.Context(), "local", 0)
	if err != nil {
		writeError(w, err)
		return
	}
	setSessionCookie(w, r, token)
	writeJSON(w, http.StatusOK, map[string]any{"token": token, "session": sess})
}

func (s *Server) handleAuthLogout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
	writeJSON(w, http.StatusOK, map[string]any{"status": "logged_out"})
}

func setSessionCookie(w http.ResponseWriter, r *http.Request, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   int(DefaultSessionTTL.Seconds()),
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteLaxMode,
	})
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeError(w, fmt.Errorf("%w: invalid JSON body: %v", domain.ErrInvalidInput, err))
		return false
	}
	return true
}

func decodeOptionalJSON(r *http.Request, target any) error {
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	err := decoder.Decode(target)
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("%w: invalid JSON body: %v", domain.ErrInvalidInput, err)
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func mustJSON(value any) string {
	raw, err := json.Marshal(value)
	if err != nil {
		return "{}"
	}
	return string(raw)
}

func writeError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	switch {
	case errors.Is(err, domain.ErrNotFound):
		status = http.StatusNotFound
	case errors.Is(err, domain.ErrInvalidInput):
		status = http.StatusBadRequest
	case errors.Is(err, domain.ErrUnauthorized):
		status = http.StatusUnauthorized
	case errors.Is(err, domain.ErrConflict):
		status = http.StatusConflict
	}
	writeJSON(w, status, map[string]any{"error": err.Error()})
}

func maskSecret(value string) string {
	if strings.TrimSpace(value) == "" {
		return ""
	}
	return "***"
}

func keepExistingSecret(incoming, existing string) string {
	if strings.TrimSpace(incoming) == "" || strings.TrimSpace(incoming) == "***" {
		return existing
	}
	return incoming
}

func parseID(raw string) (int64, error) {
	id, err := strconvParseInt(raw)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("%w: invalid id", domain.ErrInvalidInput)
	}
	return id, nil
}

// strconvParseInt avoids importing strconv in every handler file.
func strconvParseInt(value string) (int64, error) {
	return strconv.ParseInt(strings.TrimSpace(value), 10, 64)
}
