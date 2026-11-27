package server

import (
	"fmt"
	"github.com/demonidze1337/mia-lead-radar/internal/domain"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func (s *Server) handleAnalyticsFunnel(w http.ResponseWriter, r *http.Request) {
	if s.deps.Insights == nil {
		writeError(w, fmt.Errorf("%w: insights service is not configured", domain.ErrInvalidInput))
		return
	}
	radarID, _ := strconv.ParseInt(r.URL.Query().Get("radar_id"), 10, 64)
	days := intQuery(r.URL.Query().Get("days"), 7)
	summary, err := s.deps.Insights.GetSummary(r.Context(), radarID, days)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, summary)
}

func (s *Server) handleAnalyticsDaily(w http.ResponseWriter, r *http.Request) {
	radarID, _ := strconv.ParseInt(r.URL.Query().Get("radar_id"), 10, 64)
	days := intQuery(r.URL.Query().Get("days"), 30)
	metrics, err := s.deps.Store.ListDailyMetrics(r.Context(), radarID, days)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, metrics)
}

func (s *Server) handleAnalyticsTrash(w http.ResponseWriter, r *http.Request) {
	radarID, _ := strconv.ParseInt(r.URL.Query().Get("radar_id"), 10, 64)
	samples, err := s.deps.Store.ListTrashSamples(r.Context(), radarID, intQuery(r.URL.Query().Get("limit"), 25))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, samples)
}

func (s *Server) handleAnalyticsTags(w http.ResponseWriter, r *http.Request) {
	radarID, _ := strconv.ParseInt(r.URL.Query().Get("radar_id"), 10, 64)
	stats, err := s.deps.Store.ListTagStats(r.Context(), radarID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, stats)
}

func (s *Server) handleNoiseGuardCheck(w http.ResponseWriter, r *http.Request) {
	if s.deps.Insights == nil {
		writeError(w, fmt.Errorf("%w: insights service is not configured", domain.ErrInvalidInput))
		return
	}
	var body struct {
		RadarID int64 `json:"radar_id"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	radarID := body.RadarID
	if radarID <= 0 {
		radar, err := s.deps.Store.GetActiveRadar(r.Context())
		if err != nil {
			writeError(w, err)
			return
		}
		radarID = radar.ID
	}
	alert, err := s.deps.Insights.EvaluateNoiseGuard(r.Context(), radarID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"alert": alert, "triggered": alert != nil})
}

func (s *Server) handleTagThrottle(w http.ResponseWriter, r *http.Request) {
	var body struct {
		RadarID int64  `json:"radar_id"`
		Tag     string `json:"tag"`
		Until   string `json:"until,omitempty"`
		Clear   bool   `json:"clear,omitempty"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	radarID := body.RadarID
	if radarID <= 0 {
		radar, err := s.deps.Store.GetActiveRadar(r.Context())
		if err != nil {
			writeError(w, err)
			return
		}
		radarID = radar.ID
	}
	if strings.TrimSpace(body.Tag) == "" {
		writeError(w, fmt.Errorf("%w: tag is required", domain.ErrInvalidInput))
		return
	}
	var until *time.Time
	if !body.Clear {
		value := time.Now().UTC().Add(6 * time.Hour)
		if body.Until != "" {
			parsed, err := time.Parse(time.RFC3339, body.Until)
			if err != nil {
				writeError(w, fmt.Errorf("%w: until must be RFC3339", domain.ErrInvalidInput))
				return
			}
			value = parsed.UTC()
		}
		until = &value
	}
	if err := s.deps.Store.SetTagThrottle(r.Context(), radarID, body.Tag, until); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"radar_id": radarID, "tag": body.Tag, "until": until})
}
