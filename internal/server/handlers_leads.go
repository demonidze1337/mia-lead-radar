package server

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/demonidze1337/mia-lead-radar/internal/domain"
	"github.com/demonidze1337/mia-lead-radar/internal/insights"
	"github.com/demonidze1337/mia-lead-radar/internal/llm"
	"github.com/demonidze1337/mia-lead-radar/internal/prompts"
	"github.com/demonidze1337/mia-lead-radar/internal/storage"
)

func (s *Server) handleListLeads(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	filter := storage.LeadFilter{
		Query:         strings.TrimSpace(query.Get("query")),
		IncludeHidden: query.Get("include_hidden") == "true",
		Limit:         intQuery(query.Get("limit"), 50),
		Offset:        intQuery(query.Get("offset"), 0),
		MinScore:      intQuery(query.Get("min_score"), 0),
		MaxScore:      intQuery(query.Get("max_score"), 0),
	}
	if raw := query.Get("radar_id"); raw != "" {
		filter.RadarID, _ = strconv.ParseInt(raw, 10, 64)
	}
	filter.Statuses = parseLeadStatuses(query.Get("status"))
	filter.Categories = parseLeadCategories(query.Get("category"))
	leads, total, err := s.deps.Store.ListLeads(r.Context(), filter)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"leads": leads, "total": total, "limit": filter.Limit, "offset": filter.Offset})
}

func (s *Server) handleLeadStatus(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	var body struct {
		Status domain.LeadStatus `json:"status"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if body.Status == "" {
		writeError(w, fmt.Errorf("%w: status is required", domain.ErrInvalidInput))
		return
	}
	if err := s.deps.Store.UpdateLeadStatus(r.Context(), id, body.Status); err != nil {
		writeError(w, err)
		return
	}
	lead, err := s.deps.Store.GetLeadByID(r.Context(), id)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, lead)
}

func (s *Server) handleLeadViewed(w http.ResponseWriter, r *http.Request) {
	s.funnelAction(w, r, insights.FunnelViewed)
}

func (s *Server) handleLeadProfileOpened(w http.ResponseWriter, r *http.Request) {
	s.funnelAction(w, r, insights.FunnelProfileOpened)
}

func (s *Server) funnelAction(w http.ResponseWriter, r *http.Request, action insights.FunnelAction) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	if s.deps.Insights == nil {
		writeError(w, fmt.Errorf("%w: insights service is not configured", domain.ErrInvalidInput))
		return
	}
	lead, err := s.deps.Store.GetLeadByID(r.Context(), id)
	if err != nil {
		writeError(w, err)
		return
	}
	if err := s.deps.Insights.RecordFunnelAction(r.Context(), lead.RadarID, id, action); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

func (s *Server) handleLeadFalsePositive(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	var body struct {
		LearnStopWords bool `json:"learn_stop_words"`
	}
	if err := decodeOptionalJSON(r, &body); err != nil {
		writeError(w, err)
		return
	}
	if s.deps.Insights == nil {
		writeError(w, fmt.Errorf("%w: insights service is not configured", domain.ErrInvalidInput))
		return
	}
	lead, err := s.deps.Store.GetLeadByID(r.Context(), id)
	if err != nil {
		writeError(w, err)
		return
	}
	radar, err := s.deps.Insights.MarkFalsePositive(r.Context(), lead.RadarID, id, body.LearnStopWords)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "radar": radar})
}

func (s *Server) handleLeadIcebreaker(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	var body struct {
		Style string `json:"style"`
	}
	if err := decodeOptionalJSON(r, &body); err != nil {
		writeError(w, err)
		return
	}
	lead, err := s.deps.Store.GetLeadByID(r.Context(), id)
	if err != nil {
		writeError(w, err)
		return
	}
	radar, err := s.deps.Store.GetRadarByID(r.Context(), lead.RadarID)
	if err != nil {
		writeError(w, err)
		return
	}
	provider := s.getLLM()
	if provider == nil {
		writeError(w, fmt.Errorf("%w: LLM provider is not configured", domain.ErrInvalidInput))
		return
	}
	var result prompts.IcebreakerResult
	if _, err := provider.CompleteJSON(r.Context(), llm.ChatRequest{
		Tier:        llm.TierSmart,
		Messages:    prompts.BuildIcebreakerMessages(radar, lead, body.Style, lead.Icebreakers),
		Temperature: 0.7,
		Schema:      prompts.IcebreakerSchema(),
	}, &result); err != nil {
		writeError(w, err)
		return
	}
	if len(result.Drafts) > 0 {
		if recorder, ok := s.deps.Insights.(interface {
			RecordIcebreaker(ctx context.Context, radarID, leadID int64, text string, savedSeconds int) error
		}); ok {
			_ = recorder.RecordIcebreaker(r.Context(), lead.RadarID, id, result.Drafts[0], 30)
		} else if s.deps.Insights != nil {
			_ = s.deps.Store.AppendLeadIcebreaker(r.Context(), id, result.Drafts[0], 30)
			_ = s.deps.Insights.RecordFunnelAction(r.Context(), lead.RadarID, id, insights.FunnelIcebreaker)
		}
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleExportLeads(w http.ResponseWriter, r *http.Request) {
	if s.deps.Insights == nil {
		writeError(w, fmt.Errorf("%w: insights service is not configured", domain.ErrInvalidInput))
		return
	}
	format := insights.ExportFormat(strings.ToLower(strings.TrimSpace(r.URL.Query().Get("format"))))
	if format == "" {
		format = insights.ExportCSV
	}
	if format != insights.ExportCSV && format != insights.ExportJSON {
		writeError(w, fmt.Errorf("%w: unsupported export format", domain.ErrInvalidInput))
		return
	}
	var buffer bytes.Buffer
	var radarID int64
	if raw := r.URL.Query().Get("radar_id"); raw != "" {
		radarID, _ = strconv.ParseInt(raw, 10, 64)
	}
	if err := s.deps.Insights.ExportLeads(r.Context(), radarID, format, &buffer); err != nil {
		writeError(w, err)
		return
	}
	contentType := "text/csv; charset=utf-8"
	filename := "leads.csv"
	if format == insights.ExportJSON {
		contentType = "application/json; charset=utf-8"
		filename = "leads.json"
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", "attachment; filename="+filename)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(buffer.Bytes())
}

func parseLeadStatuses(raw string) []domain.LeadStatus {
	out := make([]domain.LeadStatus, 0, 2)
	for _, part := range strings.Split(raw, ",") {
		if value := strings.TrimSpace(part); value != "" {
			out = append(out, domain.LeadStatus(value))
		}
	}
	return out
}

func parseLeadCategories(raw string) []domain.LeadCategory {
	out := make([]domain.LeadCategory, 0, 2)
	for _, part := range strings.Split(raw, ",") {
		if value := strings.TrimSpace(part); value != "" {
			out = append(out, domain.LeadCategory(value))
		}
	}
	return out
}

func intQuery(raw string, fallback int) int {
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return value
}
