package server

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/demonidze1337/mia-lead-radar/internal/domain"
	"github.com/demonidze1337/mia-lead-radar/internal/storage"
	"github.com/demonidze1337/mia-lead-radar/internal/telegram"
)

func (s *Server) handleListChats(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	filter := storage.ChatFilter{
		Query:  strings.TrimSpace(query.Get("query")),
		Limit:  intQuery(query.Get("limit"), 100),
		Offset: intQuery(query.Get("offset"), 0),
	}
	for _, part := range strings.Split(query.Get("status"), ",") {
		if value := strings.TrimSpace(part); value != "" {
			filter.Statuses = append(filter.Statuses, domain.ChatStatus(value))
		}
	}
	if raw := query.Get("account_id"); raw != "" {
		filter.AssignedAccountID, _ = strconv.ParseInt(raw, 10, 64)
	}
	chats, err := s.deps.Store.ListChats(r.Context(), filter)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, chats)
}

func (s *Server) handleImportDialogs(w http.ResponseWriter, r *http.Request) {
	if s.deps.Scout == nil {
		writeError(w, fmt.Errorf("%w: scout is not configured", domain.ErrInvalidInput))
		return
	}
	chats, err := s.deps.Scout.ImportMyChats(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"imported": len(chats), "chats": chats})
}

func (s *Server) handleEnqueueLinks(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Links []string `json:"links"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	now := time.Now().UTC()
	inserted := 0
	items := make([]domain.JoinQueueItem, 0, len(body.Links))
	seen := make(map[string]struct{})
	for _, raw := range body.Links {
		parsedLinks := make([]telegram.ParsedLink, 0, 1)
		if parsed, err := telegram.ParseLink(raw); err == nil {
			parsedLinks = append(parsedLinks, parsed)
		} else {
			parsedLinks = append(parsedLinks, telegram.ExtractLinks(raw)...)
		}
		for _, parsed := range parsedLinks {
			if _, exists := seen[parsed.Normalized]; exists {
				continue
			}
			seen[parsed.Normalized] = struct{}{}
			item, wasInserted, err := s.deps.Store.EnqueueJoinJob(r.Context(), domain.JoinQueueItem{
				NormalizedLink: parsed.Normalized,
				LinkType:       joinLinkType(parsed.Kind),
				RawLink:        raw,
				NextAttemptAt:  now,
				Priority:       5,
			})
			if err != nil {
				writeError(w, err)
				return
			}
			if wasInserted {
				inserted++
			}
			items = append(items, item)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"inserted": inserted, "items": items})
}

func joinLinkType(kind telegram.LinkKind) domain.JoinLinkType {
	switch kind {
	case telegram.LinkKindInvite:
		return domain.JoinLinkInvite
	case telegram.LinkKindAddList:
		return domain.JoinLinkAddList
	case telegram.LinkKindMessage:
		return domain.JoinLinkMessage
	default:
		return domain.JoinLinkUsername
	}
}

func (s *Server) handleListAccounts(w http.ResponseWriter, r *http.Request) {
	accounts, err := s.deps.Store.ListAccounts(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, accounts)
}

func (s *Server) handleClearFlood(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	now := time.Now().UTC()
	if err := s.deps.Store.SetAccountFloodUntil(r.Context(), id, now.Add(-time.Second), "manual clear"); err != nil {
		writeError(w, err)
		return
	}
	if _, err := s.deps.Store.ClearExpiredFloods(r.Context(), now); err != nil {
		writeError(w, err)
		return
	}
	if err := s.deps.Store.SetAccountStatus(r.Context(), id, domain.AccountStatusActive, ""); err != nil {
		writeError(w, err)
		return
	}
	account, err := s.deps.Store.GetAccount(r.Context(), id)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, account)
}

func (s *Server) handleDeleteAccount(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	if err := s.deps.Store.DeleteAccount(r.Context(), id); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": true})
}

func (s *Server) handlePipelineStats(w http.ResponseWriter, r *http.Request) {
	if s.deps.Pipeline == nil {
		writeJSON(w, http.StatusOK, telegram.PipelineStats{})
		return
	}
	writeJSON(w, http.StatusOK, s.deps.Pipeline.Stats())
}

func (s *Server) handlePipelinePause(w http.ResponseWriter, r *http.Request) {
	if s.deps.Pipeline == nil {
		writeError(w, fmt.Errorf("%w: pipeline is not configured", domain.ErrInvalidInput))
		return
	}
	s.deps.Pipeline.Pause()
	writeJSON(w, http.StatusOK, map[string]any{"paused": true})
}

func (s *Server) handlePipelineResume(w http.ResponseWriter, r *http.Request) {
	if s.deps.Pipeline == nil {
		writeError(w, fmt.Errorf("%w: pipeline is not configured", domain.ErrInvalidInput))
		return
	}
	s.deps.Pipeline.Resume()
	writeJSON(w, http.StatusOK, map[string]any{"paused": false})
}
