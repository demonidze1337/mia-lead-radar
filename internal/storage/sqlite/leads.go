package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/demonidze1337/mia-lead-radar/internal/domain"
	"github.com/demonidze1337/mia-lead-radar/internal/storage"
)

const leadColumns = `id, radar_id, chat_id, tg_chat_id, chat_title, chat_username, msg_id, msg_link,
	author_id, author_username, author_name, text, lang, score, category, status, reason,
	matched_tags_json, tier0_score, llm_model, icebreakers_json, viewed_at, profile_opened_at,
	icebreaker_count, saved_time_seconds, created_at, updated_at`

// SaveLead inserts a lead idempotently. A duplicate (tg_chat_id, msg_id) returns
// the existing row with inserted=false instead of failing.
func (s *SQLiteStore) SaveLead(ctx context.Context, lead domain.Lead) (domain.Lead, bool, error) {
	var out domain.Lead
	var inserted bool
	err := s.WithTx(ctx, func(tx storage.Store) error {
		bound := tx.(*SQLiteStore)
		now := nowUTC()
		lead.ID = 0
		lead.CreatedAt = normalizeTime(lead.CreatedAt)
		lead.UpdatedAt = now
		if lead.Status == "" {
			lead.Status = domain.LeadStatusNew
		}
		if lead.Category == "" {
			lead.Category = domain.CategoryFromScore(lead.Score)
		}
		if lead.Lang == "" {
			lead.Lang = "und"
		}
		matchedTags, err := marshalJSON(nonNilStrings(lead.MatchedTags))
		if err != nil {
			return err
		}
		icebreakers, err := marshalJSON(nonNilStrings(lead.Icebreakers))
		if err != nil {
			return err
		}
		res, err := bound.exec.ExecContext(ctx, `INSERT INTO leads(
			radar_id, chat_id, tg_chat_id, chat_title, chat_username, msg_id, msg_link,
			author_id, author_username, author_name, text, lang, score, category, status, reason,
			matched_tags_json, tier0_score, llm_model, icebreakers_json, viewed_at, profile_opened_at,
			icebreaker_count, saved_time_seconds, created_at, updated_at)
			VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(tg_chat_id, msg_id) DO NOTHING`,
			lead.RadarID, lead.ChatID, lead.TGChatID, lead.ChatTitle, lead.ChatUsername, lead.MsgID,
			lead.MsgLink, lead.AuthorID, lead.AuthorUsername, lead.AuthorName, lead.Text, lead.Lang,
			lead.Score, string(lead.Category), string(lead.Status), lead.Reason, matchedTags,
			lead.Tier0Score, lead.LLMModel, icebreakers, formatTimePtr(lead.ViewedAt),
			formatTimePtr(lead.ProfileOpenedAt), lead.IcebreakerCount, lead.SavedTimeSeconds,
			formatTime(lead.CreatedAt), formatTime(lead.UpdatedAt))
		if err != nil {
			return fmt.Errorf("sqlite: save lead: %w", err)
		}
		affected, err := res.RowsAffected()
		if err != nil {
			return fmt.Errorf("sqlite: save lead rows: %w", err)
		}
		if affected == 0 {
			out, err = bound.getLeadByChatMsg(ctx, lead.TGChatID, lead.MsgID)
			return err
		}
		inserted = true
		id, err := res.LastInsertId()
		if err != nil {
			return fmt.Errorf("sqlite: lead id: %w", err)
		}
		out, err = bound.getLeadByID(ctx, id)
		return err
	})
	return out, inserted, err
}

func (s *SQLiteStore) GetLeadByID(ctx context.Context, id int64) (domain.Lead, error) {
	return s.getLeadByID(ctx, id)
}

func (s *SQLiteStore) getLeadByID(ctx context.Context, id int64) (domain.Lead, error) {
	row := s.exec.QueryRowContext(ctx, "SELECT "+leadColumns+" FROM leads WHERE id = ?", id)
	lead, err := scanLead(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.Lead{}, notFound("lead", id)
		}
		return domain.Lead{}, fmt.Errorf("sqlite: get lead: %w", err)
	}
	return lead, nil
}

func (s *SQLiteStore) getLeadByChatMsg(ctx context.Context, tgChatID int64, msgID int) (domain.Lead, error) {
	row := s.exec.QueryRowContext(ctx, "SELECT "+leadColumns+" FROM leads WHERE tg_chat_id = ? AND msg_id = ?", tgChatID, msgID)
	lead, err := scanLead(row)
	if err != nil {
		return domain.Lead{}, fmt.Errorf("sqlite: get existing lead: %w", err)
	}
	return lead, nil
}

func (s *SQLiteStore) ListLeads(ctx context.Context, filter storage.LeadFilter) ([]domain.Lead, int64, error) {
	where, args := buildLeadWhere(filter)
	var total int64
	if err := s.exec.QueryRowContext(ctx, "SELECT COUNT(*) FROM leads"+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("sqlite: count leads: %w", err)
	}
	query := "SELECT " + leadColumns + " FROM leads" + where + " ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?"
	queryArgs := append(append([]any{}, args...), normalizeLimit(filter.Limit, 50), normalizeOffset(filter.Offset))
	rows, err := s.exec.QueryContext(ctx, query, queryArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("sqlite: list leads: %w", err)
	}
	defer rows.Close()
	leads := make([]domain.Lead, 0)
	for rows.Next() {
		lead, err := scanLead(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("sqlite: scan lead: %w", err)
		}
		leads = append(leads, lead)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("sqlite: iterate leads: %w", err)
	}
	return leads, total, nil
}

func buildLeadWhere(filter storage.LeadFilter) (string, []any) {
	clauses := make([]string, 0, 6)
	args := make([]any, 0, 10)
	if filter.RadarID != 0 {
		clauses = append(clauses, "radar_id = ?")
		args = append(args, filter.RadarID)
	}
	if len(filter.Statuses) > 0 {
		placeholders := make([]string, len(filter.Statuses))
		for i, status := range filter.Statuses {
			placeholders[i] = "?"
			args = append(args, string(status))
		}
		clauses = append(clauses, "status IN ("+strings.Join(placeholders, ",")+")")
	} else if !filter.IncludeHidden {
		clauses = append(clauses, "status <> ?")
		args = append(args, string(domain.LeadStatusHidden))
	}
	if len(filter.Categories) > 0 {
		placeholders := make([]string, len(filter.Categories))
		for i, category := range filter.Categories {
			placeholders[i] = "?"
			args = append(args, string(category))
		}
		clauses = append(clauses, "category IN ("+strings.Join(placeholders, ",")+")")
	}
	if filter.Query != "" {
		pattern := "%" + escapeLike(filter.Query) + "%"
		clauses = append(clauses, `(text LIKE ? ESCAPE '\' OR author_name LIKE ? ESCAPE '\' OR author_username LIKE ? ESCAPE '\' OR chat_title LIKE ? ESCAPE '\')`)
		args = append(args, pattern, pattern, pattern, pattern)
	}
	if filter.MinScore > 0 {
		clauses = append(clauses, "score >= ?")
		args = append(args, filter.MinScore)
	}
	if filter.MaxScore > 0 {
		clauses = append(clauses, "score <= ?")
		args = append(args, filter.MaxScore)
	}
	if len(clauses) == 0 {
		return "", args
	}
	return " WHERE " + strings.Join(clauses, " AND "), args
}

func (s *SQLiteStore) UpdateLeadStatus(ctx context.Context, id int64, status domain.LeadStatus) error {
	res, err := s.exec.ExecContext(ctx, "UPDATE leads SET status = ?, updated_at = ? WHERE id = ?",
		string(status), formatTime(nowUTC()), id)
	if err != nil {
		return fmt.Errorf("sqlite: update lead status: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("sqlite: update lead status rows: %w", err)
	}
	if affected == 0 {
		return notFound("lead", id)
	}
	return nil
}

func (s *SQLiteStore) MarkLeadViewed(ctx context.Context, id int64, at time.Time) error {
	return s.markLeadTimestamp(ctx, "viewed_at", id, at)
}

func (s *SQLiteStore) MarkLeadProfileOpened(ctx context.Context, id int64, at time.Time) error {
	return s.markLeadTimestamp(ctx, "profile_opened_at", id, at)
}

func (s *SQLiteStore) markLeadTimestamp(ctx context.Context, column string, id int64, at time.Time) error {
	// column is a fixed internal identifier, never user input.
	res, err := s.exec.ExecContext(ctx, "UPDATE leads SET "+column+" = COALESCE("+column+", ?), updated_at = ? WHERE id = ?",
		formatTime(at), formatTime(nowUTC()), id)
	if err != nil {
		return fmt.Errorf("sqlite: mark lead %s: %w", column, err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("sqlite: mark lead %s rows: %w", column, err)
	}
	if affected == 0 {
		return notFound("lead", id)
	}
	return nil
}

func (s *SQLiteStore) AppendLeadIcebreaker(ctx context.Context, id int64, text string, savedSecondsDelta int) error {
	if savedSecondsDelta < 0 {
		savedSecondsDelta = 0
	}
	res, err := s.exec.ExecContext(ctx, `UPDATE leads SET
		icebreakers_json = json_insert(icebreakers_json, '$[#]', ?),
		icebreaker_count = icebreaker_count + 1,
		saved_time_seconds = saved_time_seconds + ?,
		updated_at = ?
		WHERE id = ?`, text, savedSecondsDelta, formatTime(nowUTC()), id)
	if err != nil {
		return fmt.Errorf("sqlite: append lead icebreaker: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("sqlite: append lead icebreaker rows: %w", err)
	}
	if affected == 0 {
		return notFound("lead", id)
	}
	return nil
}

func scanLead(row scanner) (domain.Lead, error) {
	var lead domain.Lead
	var category, status, tagsRaw, icebreakersRaw, createdRaw, updatedRaw string
	var viewedAt, profileOpenedAt sql.NullString
	if err := row.Scan(&lead.ID, &lead.RadarID, &lead.ChatID, &lead.TGChatID, &lead.ChatTitle,
		&lead.ChatUsername, &lead.MsgID, &lead.MsgLink, &lead.AuthorID, &lead.AuthorUsername,
		&lead.AuthorName, &lead.Text, &lead.Lang, &lead.Score, &category, &status, &lead.Reason,
		&tagsRaw, &lead.Tier0Score, &lead.LLMModel, &icebreakersRaw, &viewedAt, &profileOpenedAt,
		&lead.IcebreakerCount, &lead.SavedTimeSeconds, &createdRaw, &updatedRaw); err != nil {
		return domain.Lead{}, err
	}
	lead.Category = domain.LeadCategory(category)
	lead.Status = domain.LeadStatus(status)
	var err error
	if lead.MatchedTags, err = unmarshalStringSlice(tagsRaw); err != nil {
		return domain.Lead{}, fmt.Errorf("parse lead matched tags: %w", err)
	}
	if lead.Icebreakers, err = unmarshalIcebreakers(icebreakersRaw); err != nil {
		return domain.Lead{}, fmt.Errorf("parse lead icebreakers: %w", err)
	}
	if lead.ViewedAt, err = timePtrFromNull(viewedAt); err != nil {
		return domain.Lead{}, fmt.Errorf("parse lead viewed_at: %w", err)
	}
	if lead.ProfileOpenedAt, err = timePtrFromNull(profileOpenedAt); err != nil {
		return domain.Lead{}, fmt.Errorf("parse lead profile_opened_at: %w", err)
	}
	if lead.CreatedAt, err = scanTime(createdRaw); err != nil {
		return domain.Lead{}, fmt.Errorf("parse lead created_at: %w", err)
	}
	if lead.UpdatedAt, err = scanTime(updatedRaw); err != nil {
		return domain.Lead{}, fmt.Errorf("parse lead updated_at: %w", err)
	}
	return lead, nil
}

func unmarshalIcebreakers(raw string) ([]string, error) {
	if strings.TrimSpace(raw) == "" {
		return []string{}, nil
	}
	var out []string
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, err
	}
	if out == nil {
		out = []string{}
	}
	return out, nil
}
