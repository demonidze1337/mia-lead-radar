package server

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/demonidze1337/mia-lead-radar/internal/domain"
	tier0filter "github.com/demonidze1337/mia-lead-radar/internal/filter"
	"github.com/demonidze1337/mia-lead-radar/internal/llm"
	"github.com/demonidze1337/mia-lead-radar/internal/prompts"
)

func (s *Server) handleListRadars(w http.ResponseWriter, r *http.Request) {
	radars, err := s.deps.Store.ListRadars(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, radars)
}

func (s *Server) handleCreateRadar(w http.ResponseWriter, r *http.Request) {
	var radar domain.RadarProfile
	if !decodeJSON(w, r, &radar) {
		return
	}
	created, err := s.deps.Store.CreateRadar(r.Context(), radar, domain.RadarChangeWizard, "created via HTTP API")
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

func (s *Server) handleUpdateRadar(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	var radar domain.RadarProfile
	if !decodeJSON(w, r, &radar) {
		return
	}
	radar.ID = id
	updated, err := s.deps.Store.UpdateRadar(r.Context(), radar, domain.RadarChangeManual, "updated via HTTP API")
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) handleRadarVersions(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	limit := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil {
			limit = parsed
		}
	}
	versions, err := s.deps.Store.ListRadarVersions(r.Context(), id, limit)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, versions)
}

func (s *Server) handleRadarSuggest(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Description     string   `json:"description"`
		TargetLanguages []string `json:"target_languages"`
		UILanguage      string   `json:"ui_language"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	provider := s.getLLM()
	if provider == nil {
		writeError(w, fmt.Errorf("%w: LLM provider is not configured", domain.ErrInvalidInput))
		return
	}
	if strings.TrimSpace(body.Description) == "" {
		writeError(w, fmt.Errorf("%w: description is required", domain.ErrInvalidInput))
		return
	}
	var result prompts.PhraseSuggestResult
	if _, err := provider.CompleteJSON(r.Context(), llm.ChatRequest{
		Tier:        llm.TierSmart,
		Messages:    prompts.BuildPhraseSuggestMessages(body.Description, body.TargetLanguages, body.UILanguage),
		Temperature: 0.3,
		Schema:      prompts.PhraseSuggestSchema(),
	}, &result); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleRadarSimulate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		RadarID    int64  `json:"radar_id"`
		Message    string `json:"message"`
		ChatID     int64  `json:"chat_id"`
		MsgID      int    `json:"msg_id"`
		AuthorID   int64  `json:"author_id"`
		AuthorName string `json:"author_name"`
		UseLLM     bool   `json:"use_llm"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if strings.TrimSpace(body.Message) == "" {
		writeError(w, fmt.Errorf("%w: message is required", domain.ErrInvalidInput))
		return
	}
	radar, err := s.resolveRadar(r, body.RadarID)
	if err != nil {
		writeError(w, err)
		return
	}
	engine := s.deps.Filter
	if engine == nil {
		writeError(w, fmt.Errorf("%w: Tier-0 engine is not configured", domain.ErrInvalidInput))
		return
	}
	throttles := make(map[string]time.Time)
	if stats, err := s.deps.Store.ListTagStats(r.Context(), radar.ID); err == nil {
		now := time.Now().UTC()
		for _, stat := range stats {
			if stat.ThrottledUntil != nil && stat.ThrottledUntil.After(now) {
				throttles[stat.Tag] = *stat.ThrottledUntil
			}
		}
	}
	result := engine.Evaluate(tier0filter.CandidateInput{
		ChatID:     body.ChatID,
		MsgID:      body.MsgID,
		AuthorID:   body.AuthorID,
		AuthorName: body.AuthorName,
		Text:       body.Message,
		DryRun:     true,
	}, radar, throttles)
	response := map[string]any{"eval": result}
	if body.UseLLM && result.Passed {
		provider := s.getLLM()
		if provider == nil {
			response["llm_error"] = "LLM provider is not configured"
		} else {
			settings, _ := s.deps.Store.GetSettings(r.Context())
			var qualified prompts.QualifyResult
			if _, err := provider.CompleteJSON(r.Context(), llm.ChatRequest{
				Tier:        llm.TierFast,
				Messages:    prompts.BuildQualifierMessages(radar, settings.UILanguage, "Simulator", result.DetectedLang, body.Message, result.MatchedTags),
				Temperature: 0.1,
				Schema:      prompts.QualifySchema(),
			}, &qualified); err != nil {
				response["llm_error"] = err.Error()
			} else {
				response["qualify"] = qualified
			}
		}
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) resolveRadar(r *http.Request, radarID int64) (domain.RadarProfile, error) {
	if radarID > 0 {
		return s.deps.Store.GetRadarByID(r.Context(), radarID)
	}
	radar, err := s.deps.Store.GetActiveRadar(r.Context())
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return domain.RadarProfile{}, fmt.Errorf("%w: no active radar", domain.ErrNotFound)
		}
		return domain.RadarProfile{}, err
	}
	return radar, nil
}
