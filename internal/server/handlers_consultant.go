package server

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/demonidze1337/mia-lead-radar/internal/domain"
	"github.com/demonidze1337/mia-lead-radar/internal/llm"
	"github.com/demonidze1337/mia-lead-radar/internal/prompts"
)

func (s *Server) handleConsultantAudit(w http.ResponseWriter, r *http.Request) {
	var body struct {
		RadarID  int64  `json:"radar_id"`
		Question string `json:"question"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	radar, err := s.resolveRadar(r, body.RadarID)
	if err != nil {
		writeError(w, err)
		return
	}
	provider := s.getLLM()
	if provider == nil {
		writeError(w, fmt.Errorf("%w: LLM provider is not configured", domain.ErrInvalidInput))
		return
	}
	versions, _ := s.deps.Store.ListRadarVersions(r.Context(), radar.ID, 20)
	trash, _ := s.deps.Store.ListTrashSamples(r.Context(), radar.ID, 25)
	settings, _ := s.deps.Store.GetSettings(r.Context())
	var funnel domain.FunnelSummary
	if s.deps.Insights != nil {
		funnel, _ = s.deps.Insights.GetSummary(r.Context(), radar.ID, 7)
	}
	var result prompts.AuditResult
	if _, err := provider.CompleteJSON(r.Context(), llm.ChatRequest{
		Tier:        llm.TierSmart,
		Messages:    prompts.BuildRadarAuditMessages(radar, versions, funnel, trash, body.Question, settings.UILanguage),
		Temperature: 0.3,
		Schema:      prompts.AuditSchema(),
	}, &result); err != nil {
		writeError(w, err)
		return
	}
	sessionKey := fmt.Sprintf("audit-%d", radar.ID)
	_, _ = s.deps.Store.AppendConsultantMessage(r.Context(), domain.ConsultantMessage{
		SessionKey: sessionKey, Role: "user", Mode: "audit", Content: firstString(body.Question, "Full radar audit"),
	})
	_, _ = s.deps.Store.AppendConsultantMessage(r.Context(), domain.ConsultantMessage{
		SessionKey: sessionKey, Role: "assistant", Mode: "audit", Content: result.Reply,
		MetadataJSON: mustJSON(result),
	})
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleConsultantApplyPatch(w http.ResponseWriter, r *http.Request) {
	var body struct {
		RadarID int64              `json:"radar_id"`
		Patch   prompts.RadarPatch `json:"patch"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	radar, err := s.resolveRadar(r, body.RadarID)
	if err != nil {
		writeError(w, err)
		return
	}
	if body.Patch.Description != "" {
		radar.Description = body.Patch.Description
	}
	if len(body.Patch.Tags) > 0 {
		radar.Tags = body.Patch.Tags
	}
	if len(body.Patch.StopPhrases) > 0 {
		radar.StopPhrases = body.Patch.StopPhrases
	}
	summary := body.Patch.Summary
	if summary == "" {
		summary = "AI Audit radar patch"
	}
	updated, err := s.deps.Store.UpdateRadar(r.Context(), radar, domain.RadarChangeAIAudit, summary)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) handleConsultantRoleplay(w http.ResponseWriter, r *http.Request) {
	var body struct {
		RadarID     int64  `json:"radar_id"`
		LeadID      int64  `json:"lead_id"`
		SellerReply string `json:"seller_reply"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	lead, err := s.deps.Store.GetLeadByID(r.Context(), body.LeadID)
	if err != nil {
		writeError(w, err)
		return
	}
	radarID := body.RadarID
	if radarID <= 0 {
		radarID = lead.RadarID
	}
	radar, err := s.deps.Store.GetRadarByID(r.Context(), radarID)
	if err != nil {
		writeError(w, err)
		return
	}
	provider := s.getLLM()
	if provider == nil {
		writeError(w, fmt.Errorf("%w: LLM provider is not configured", domain.ErrInvalidInput))
		return
	}
	sessionKey := fmt.Sprintf("roleplay-%d", lead.ID)
	history, _ := s.deps.Store.ListConsultantMessages(r.Context(), sessionKey, 30)
	var reply prompts.RoleplayReply
	if _, err := provider.CompleteJSON(r.Context(), llm.ChatRequest{
		Tier:        llm.TierSmart,
		Messages:    prompts.BuildRoleplayMessages(radar, lead, history, body.SellerReply),
		Temperature: 0.8,
		Schema:      prompts.RoleplayReplySchema(),
	}, &reply); err != nil {
		writeError(w, err)
		return
	}
	_, _ = s.deps.Store.AppendConsultantMessage(r.Context(), domain.ConsultantMessage{
		SessionKey: sessionKey, Role: "user", Mode: "roleplay", LeadID: lead.ID, Content: body.SellerReply,
	})
	_, _ = s.deps.Store.AppendConsultantMessage(r.Context(), domain.ConsultantMessage{
		SessionKey: sessionKey, Role: "assistant", Mode: "roleplay", LeadID: lead.ID, Content: reply.Message,
		MetadataJSON: mustJSON(reply),
	})
	writeJSON(w, http.StatusOK, reply)
}

func (s *Server) handleConsultantReview(w http.ResponseWriter, r *http.Request) {
	var body struct {
		LeadID     int64                      `json:"lead_id"`
		Transcript []domain.ConsultantMessage `json:"transcript"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	lead, err := s.deps.Store.GetLeadByID(r.Context(), body.LeadID)
	if err != nil {
		writeError(w, err)
		return
	}
	provider := s.getLLM()
	if provider == nil {
		writeError(w, fmt.Errorf("%w: LLM provider is not configured", domain.ErrInvalidInput))
		return
	}
	transcript := body.Transcript
	if len(transcript) == 0 {
		transcript, _ = s.deps.Store.ListConsultantMessages(r.Context(), fmt.Sprintf("roleplay-%d", lead.ID), 30)
	}
	settings, _ := s.deps.Store.GetSettings(r.Context())
	var review prompts.RoleplayReview
	if _, err := provider.CompleteJSON(r.Context(), llm.ChatRequest{
		Tier:        llm.TierSmart,
		Messages:    prompts.BuildRoleplayReviewMessages(lead, transcript, settings.UILanguage),
		Temperature: 0.3,
		Schema:      prompts.RoleplayReviewSchema(),
	}, &review); err != nil {
		writeError(w, err)
		return
	}
	_, _ = s.deps.Store.AppendConsultantMessage(r.Context(), domain.ConsultantMessage{
		SessionKey: fmt.Sprintf("review-%d", lead.ID), Role: "assistant", Mode: "roleplay",
		LeadID: lead.ID, Content: review.ImprovedText, MetadataJSON: mustJSON(review),
	})
	writeJSON(w, http.StatusOK, review)
}

func (s *Server) handleConsultantHistory(w http.ResponseWriter, r *http.Request) {
	sessionKey := strings.TrimSpace(r.URL.Query().Get("session_key"))
	if sessionKey == "" {
		writeError(w, fmt.Errorf("%w: session_key is required", domain.ErrInvalidInput))
		return
	}
	messages, err := s.deps.Store.ListConsultantMessages(r.Context(), sessionKey, intQuery(r.URL.Query().Get("limit"), 50))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, messages)
}
