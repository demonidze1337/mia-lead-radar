package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/demonidze1337/mia-lead-radar/internal/domain"
	"github.com/demonidze1337/mia-lead-radar/internal/storage"
)

const joinQueueColumns = `id, normalized_link, link_type, raw_link, source_chat_id, priority,
	status, attempts, next_attempt_at, last_error, created_at, updated_at`

// EnqueueJoinJob inserts a new queue item or returns the existing row for the
// same normalized link. The bool result reports whether a row was inserted.
func (s *SQLiteStore) EnqueueJoinJob(ctx context.Context, item domain.JoinQueueItem) (domain.JoinQueueItem, bool, error) {
	if strings.TrimSpace(item.NormalizedLink) == "" {
		return domain.JoinQueueItem{}, false, fmt.Errorf("%w: normalized link is required", domain.ErrInvalidInput)
	}
	var out domain.JoinQueueItem
	var inserted bool
	err := s.WithTx(ctx, func(tx storage.Store) error {
		bound := tx.(*SQLiteStore)
		now := nowUTC()
		if item.LinkType == "" {
			item.LinkType = inferLinkType(item.NormalizedLink)
		}
		if item.Status == "" {
			item.Status = domain.JoinJobPending
		}
		if item.NextAttemptAt.IsZero() {
			item.NextAttemptAt = now
		}
		created := normalizeTime(item.CreatedAt)
		res, err := bound.exec.ExecContext(ctx, `INSERT INTO join_queue(
			normalized_link, link_type, raw_link, source_chat_id, priority, status, attempts,
			next_attempt_at, last_error, created_at, updated_at)
			VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(normalized_link) DO NOTHING`,
			item.NormalizedLink, string(item.LinkType), item.RawLink, item.SourceChatID, item.Priority,
			string(item.Status), item.Attempts, formatTime(item.NextAttemptAt), item.LastError,
			formatTime(created), formatTime(now))
		if err != nil {
			return fmt.Errorf("sqlite: enqueue join job: %w", err)
		}
		affected, err := res.RowsAffected()
		if err != nil {
			return fmt.Errorf("sqlite: enqueue join job rows: %w", err)
		}
		if affected > 0 {
			id, err := res.LastInsertId()
			if err != nil {
				return fmt.Errorf("sqlite: join job id: %w", err)
			}
			inserted = true
			out, err = bound.getJoinJob(ctx, id)
			return err
		}
		out, err = bound.getJoinJobByLink(ctx, item.NormalizedLink)
		return err
	})
	return out, inserted, err
}

func inferLinkType(normalized string) domain.JoinLinkType {
	lower := strings.ToLower(normalized)
	switch {
	case strings.Contains(lower, "addlist"):
		return domain.JoinLinkAddList
	case strings.Contains(lower, "t.me/+"), strings.HasPrefix(lower, "+"), strings.Contains(lower, "joinchat"):
		return domain.JoinLinkInvite
	case strings.Contains(lower, "t.me/"), strings.Contains(lower, "telegram.me/"):
		return domain.JoinLinkMessage
	default:
		return domain.JoinLinkUsername
	}
}

func (s *SQLiteStore) getJoinJob(ctx context.Context, id int64) (domain.JoinQueueItem, error) {
	row := s.exec.QueryRowContext(ctx, "SELECT "+joinQueueColumns+" FROM join_queue WHERE id = ?", id)
	return scanJoinJob(row)
}

func (s *SQLiteStore) getJoinJobByLink(ctx context.Context, link string) (domain.JoinQueueItem, error) {
	row := s.exec.QueryRowContext(ctx, "SELECT "+joinQueueColumns+" FROM join_queue WHERE normalized_link = ?", link)
	item, err := scanJoinJob(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.JoinQueueItem{}, fmt.Errorf("%w: join job %q", domain.ErrNotFound, link)
		}
		return domain.JoinQueueItem{}, fmt.Errorf("sqlite: get join job: %w", err)
	}
	return item, nil
}

// ClaimNextJoinJob atomically selects and marks the highest-priority due pending
// job as in_progress, incrementing attempts. Returns (nil, nil) when the queue
// has no due work.
func (s *SQLiteStore) ClaimNextJoinJob(ctx context.Context, now time.Time) (*domain.JoinQueueItem, error) {
	var item *domain.JoinQueueItem
	err := s.WithTx(ctx, func(tx storage.Store) error {
		bound := tx.(*SQLiteStore)
		row := bound.exec.QueryRowContext(ctx, `UPDATE join_queue SET
			status = ?, attempts = attempts + 1, updated_at = ?
			WHERE id = (
				SELECT id FROM join_queue
				WHERE status = ? AND next_attempt_at <= ?
				ORDER BY priority DESC, next_attempt_at ASC, id ASC
				LIMIT 1
			)
			RETURNING `+joinQueueColumns,
			string(domain.JoinJobInProgress), formatTime(now), string(domain.JoinJobPending), formatTime(now))
		claimed, err := scanJoinJob(row)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				item = nil
				return nil
			}
			return fmt.Errorf("sqlite: claim join job: %w", err)
		}
		item = &claimed
		return nil
	})
	if err != nil {
		return nil, err
	}
	return item, nil
}

func (s *SQLiteStore) CompleteJoinJob(ctx context.Context, id int64, status domain.JoinJobStatus, nextAttempt *time.Time, lastErr string) error {
	switch status {
	case domain.JoinJobPending, domain.JoinJobInProgress, domain.JoinJobDone, domain.JoinJobFailed, domain.JoinJobDead:
	default:
		return fmt.Errorf("%w: invalid join job status %q", domain.ErrInvalidInput, status)
	}
	res, err := s.exec.ExecContext(ctx, `UPDATE join_queue SET
		status = ?, next_attempt_at = COALESCE(?, next_attempt_at), last_error = ?, updated_at = ?
		WHERE id = ?`, string(status), formatTimePtr(nextAttempt), lastErr, formatTime(nowUTC()), id)
	if err != nil {
		return fmt.Errorf("sqlite: complete join job: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("sqlite: complete join job rows: %w", err)
	}
	if affected == 0 {
		return notFound("join job", id)
	}
	return nil
}

func (s *SQLiteStore) ListJoinQueue(ctx context.Context, status domain.JoinJobStatus, limit int) ([]domain.JoinQueueItem, error) {
	query := "SELECT " + joinQueueColumns + " FROM join_queue"
	args := make([]any, 0, 2)
	if status != "" {
		query += " WHERE status = ?"
		args = append(args, string(status))
	}
	query += " ORDER BY priority DESC, next_attempt_at ASC, id ASC LIMIT ?"
	args = append(args, normalizeLimit(limit, defaultListLimit))
	rows, err := s.exec.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("sqlite: list join queue: %w", err)
	}
	defer rows.Close()
	items := make([]domain.JoinQueueItem, 0)
	for rows.Next() {
		item, err := scanJoinJob(rows)
		if err != nil {
			return nil, fmt.Errorf("sqlite: scan join job: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite: iterate join queue: %w", err)
	}
	return items, nil
}

func scanJoinJob(row scanner) (domain.JoinQueueItem, error) {
	var item domain.JoinQueueItem
	var linkType, status, nextAttempt, createdRaw, updatedRaw string
	if err := row.Scan(&item.ID, &item.NormalizedLink, &linkType, &item.RawLink, &item.SourceChatID,
		&item.Priority, &status, &item.Attempts, &nextAttempt, &item.LastError, &createdRaw, &updatedRaw); err != nil {
		return domain.JoinQueueItem{}, err
	}
	item.LinkType = domain.JoinLinkType(linkType)
	item.Status = domain.JoinJobStatus(status)
	var err error
	if item.NextAttemptAt, err = scanTime(nextAttempt); err != nil {
		return domain.JoinQueueItem{}, fmt.Errorf("parse next_attempt_at: %w", err)
	}
	if item.CreatedAt, err = scanTime(createdRaw); err != nil {
		return domain.JoinQueueItem{}, fmt.Errorf("parse join job created_at: %w", err)
	}
	if item.UpdatedAt, err = scanTime(updatedRaw); err != nil {
		return domain.JoinQueueItem{}, fmt.Errorf("parse join job updated_at: %w", err)
	}
	return item, nil
}
