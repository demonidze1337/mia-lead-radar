package server

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/demonidze1337/mia-lead-radar/internal/domain"
	"github.com/demonidze1337/mia-lead-radar/internal/llm"
	"github.com/demonidze1337/mia-lead-radar/internal/telegram"
)

func (s *Server) handleSetupStatus(w http.ResponseWriter, r *http.Request) {
	settings, err := s.deps.Store.GetSettings(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	accounts, err := s.deps.Store.ListAccounts(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	_, radarErr := s.deps.Store.GetActiveRadar(r.Context())
	writeJSON(w, http.StatusOK, map[string]any{
		"hardware":         s.deps.Hardware,
		"presets":          llm.RecommendPresets(s.deps.Hardware),
		"settings":         maskSettings(settings),
		"accounts":         accounts,
		"has_active_radar": radarErr == nil,
		"setup_completed":  settings.SetupCompleted,
	})
}

func (s *Server) handleLLMTest(w http.ResponseWriter, r *http.Request) {
	var cfg domain.LLMConfig
	if !decodeJSON(w, r, &cfg) {
		return
	}
	if settings, err := s.deps.Store.GetSettings(r.Context()); err == nil {
		cfg.APIKey = resolveLLMAPIKey(cfg, settings.LLM)
	}
	client, err := llm.NewClient(cfg)
	if err != nil {
		writeError(w, err)
		return
	}
	response := map[string]any{"ok": true}
	if err := client.Ping(r.Context()); err != nil {
		response["ok"] = false
		response["error"] = err.Error()
	} else if models, err := client.ListModels(r.Context()); err == nil {
		response["models"] = models
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleLLMSave(w http.ResponseWriter, r *http.Request) {
	var cfg domain.LLMConfig
	if !decodeJSON(w, r, &cfg) {
		return
	}
	settings, err := s.deps.Store.GetSettings(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	cfg.APIKey = resolveLLMAPIKey(cfg, settings.LLM)
	settings.LLM = cfg
	if err := s.deps.Store.SaveSettings(r.Context(), settings); err != nil {
		writeError(w, err)
		return
	}
	if client, err := llm.NewClient(cfg); err == nil {
		s.setLLM(client)
	}
	writeJSON(w, http.StatusOK, map[string]any{"saved": true})
}

func (s *Server) handleTelegramQR(w http.ResponseWriter, r *http.Request) {
	if s.deps.Telegram == nil {
		writeError(w, fmt.Errorf("%w: MTProto client is not configured", domain.ErrInvalidInput))
		return
	}
	if err := s.deps.Telegram.Auth().StartQR(context.WithoutCancel(r.Context())); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.deps.Telegram.State())
}

func (s *Server) handleTelegramPhone(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Phone string `json:"phone"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if s.deps.Telegram == nil {
		writeError(w, fmt.Errorf("%w: MTProto client is not configured", domain.ErrInvalidInput))
		return
	}
	if err := s.deps.Telegram.Auth().SubmitPhone(r.Context(), body.Phone); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.deps.Telegram.State())
}

func (s *Server) handleTelegramCode(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Code string `json:"code"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if s.deps.Telegram == nil {
		writeError(w, fmt.Errorf("%w: MTProto client is not configured", domain.ErrInvalidInput))
		return
	}
	if err := s.deps.Telegram.Auth().SubmitCode(r.Context(), body.Code); err != nil {
		writeError(w, err)
		return
	}
	state := s.deps.Telegram.State()
	if state.Step == telegram.AuthStepAuthorized {
		if err := s.saveTelegramAccount(r, state); err != nil {
			writeError(w, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, state)
}

func (s *Server) handleTelegramPassword(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Password string `json:"password"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if s.deps.Telegram == nil {
		writeError(w, fmt.Errorf("%w: MTProto client is not configured", domain.ErrInvalidInput))
		return
	}
	if err := s.deps.Telegram.Auth().SubmitPassword(r.Context(), body.Password); err != nil {
		writeError(w, err)
		return
	}
	state := s.deps.Telegram.State()
	if state.Step == telegram.AuthStepAuthorized {
		if err := s.saveTelegramAccount(r, state); err != nil {
			writeError(w, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, state)
}

func (s *Server) handleTelegramStatus(w http.ResponseWriter, r *http.Request) {
	if s.deps.Telegram == nil {
		writeError(w, fmt.Errorf("%w: MTProto client is not configured", domain.ErrInvalidInput))
		return
	}
	state := s.deps.Telegram.State()
	if state.Step == telegram.AuthStepAuthorized {
		if err := s.saveTelegramAccount(r, state); err != nil {
			writeError(w, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, state)
}

func (s *Server) saveTelegramAccount(r *http.Request, state telegram.AuthSessionState) error {
	if state.TGUserID <= 0 {
		return nil
	}
	now := time.Now().UTC()
	_, err := s.deps.Store.UpsertAccount(r.Context(), domain.TelegramAccount{
		Label:        firstString(state.Username, state.FirstName, "Telegram"),
		PhoneMasked:  state.PhoneMasked,
		TGUserID:     state.TGUserID,
		Username:     state.Username,
		FirstName:    state.FirstName,
		SessionPath:  s.deps.Telegram.SessionPath(),
		Status:       domain.AccountStatusActive,
		LastActiveAt: &now,
	})
	return err
}

func (s *Server) handleSetupBot(w http.ResponseWriter, r *http.Request) {
	var body struct {
		BotToken        string `json:"bot_token"`
		OwnerTelegramID int64  `json:"owner_telegram_id"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	settings, err := s.deps.Store.GetSettings(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	settings.OwnerBotToken = keepExistingSecret(body.BotToken, settings.OwnerBotToken)
	if body.OwnerTelegramID > 0 {
		settings.OwnerTelegramID = body.OwnerTelegramID
	}
	if err := s.deps.Store.SaveSettings(r.Context(), settings); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"owner_telegram_id": settings.OwnerTelegramID, "configured": settings.OwnerBotToken != ""})
}

func (s *Server) handleSetupComplete(w http.ResponseWriter, r *http.Request) {
	settings, err := s.deps.Store.GetSettings(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	settings.SetupCompleted = true
	if err := s.deps.Store.SaveSettings(r.Context(), settings); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"setup_completed": true})
}

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	settings, err := s.deps.Store.GetSettings(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, maskSettings(settings))
}

func (s *Server) handlePutSettings(w http.ResponseWriter, r *http.Request) {
	var incoming domain.RuntimeSettings
	if !decodeJSON(w, r, &incoming) {
		return
	}
	existing, err := s.deps.Store.GetSettings(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	incoming.LLM.APIKey = resolveLLMAPIKey(incoming.LLM, existing.LLM)
	incoming.OwnerBotToken = keepExistingSecret(incoming.OwnerBotToken, existing.OwnerBotToken)
	if err := s.deps.Store.SaveSettings(r.Context(), incoming); err != nil {
		writeError(w, err)
		return
	}
	if client, err := llm.NewClient(incoming.LLM); err == nil {
		s.setLLM(client)
	}
	writeJSON(w, http.StatusOK, maskSettings(incoming))
}

func maskSettings(settings domain.RuntimeSettings) domain.RuntimeSettings {
	settings.LLM.APIKey = maskSecret(settings.LLM.APIKey)
	settings.OwnerBotToken = maskSecret(settings.OwnerBotToken)
	if settings.TargetLanguages == nil {
		settings.TargetLanguages = []string{}
	}
	return settings
}

func firstString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
