package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/demonidze1337/mia-lead-radar/internal/domain"
	"github.com/demonidze1337/mia-lead-radar/internal/storage"
)

const maxTrashSamples = 10000

func (s *SQLiteStore) SaveTrashSample(ctx context.Context, sample domain.TrashSample, keepLatest int) error {
	if sample.RadarID <= 0 {
		return fmt.Errorf("%w: radar id is required", domain.ErrInvalidInput)
	}
	if keepLatest > maxTrashSamples {
		keepLatest = maxTrashSamples
	}
	return s.WithTx(ctx, func(tx storage.Store) error {
		bound := tx.(*SQLiteStore)
		now := normalizeTime(sample.CreatedAt)
		tags, err := marshalJSON(nonNilStrings(sample.MatchedTags))
		if err != nil {
			return err
		}
		if _, err := bound.exec.ExecContext(ctx, `INSERT INTO trash_samples(
			radar_id, source, reason, matched_tags_json, score, chat_title, author_id,
			text_excerpt, lang, created_at)
			VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			sample.RadarID, sample.Source, sample.Reason, tags, sample.Score, sample.ChatTitle,
			sample.AuthorID, sample.TextExcerpt, sample.Lang, formatTime(now)); err != nil {
			return fmt.Errorf("sqlite: save trash sample: %w", err)
		}
		if keepLatest <= 0 {
			return nil
		}
		if _, err := bound.exec.ExecContext(ctx, `DELETE FROM trash_samples
			WHERE radar_id = ? AND id NOT IN (
				SELECT id FROM trash_samples WHERE radar_id = ? ORDER BY created_at DESC, id DESC LIMIT ?
			)`, sample.RadarID, sample.RadarID, keepLatest); err != nil {
			return fmt.Errorf("sqlite: prune trash samples: %w", err)
		}
		return nil
	})
}

func (s *SQLiteStore) ListTrashSamples(ctx context.Context, radarID int64, limit int) ([]domain.TrashSample, error) {
	query := `SELECT id, radar_id, source, reason, matched_tags_json, score, chat_title,
		author_id, text_excerpt, lang, created_at FROM trash_samples`
	args := make([]any, 0, 2)
	if radarID != 0 {
		query += " WHERE radar_id = ?"
		args = append(args, radarID)
	}
	query += " ORDER BY created_at DESC, id DESC LIMIT ?"
	args = append(args, normalizeLimit(limit, 50))
	rows, err := s.exec.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("sqlite: list trash samples: %w", err)
	}
	defer rows.Close()
	samples := make([]domain.TrashSample, 0)
	for rows.Next() {
		var sample domain.TrashSample
		var tagsRaw, createdRaw string
		if err := rows.Scan(&sample.ID, &sample.RadarID, &sample.Source, &sample.Reason, &tagsRaw,
			&sample.Score, &sample.ChatTitle, &sample.AuthorID, &sample.TextExcerpt, &sample.Lang,
			&createdRaw); err != nil {
			return nil, fmt.Errorf("sqlite: scan trash sample: %w", err)
		}
		if sample.MatchedTags, err = unmarshalStringSlice(tagsRaw); err != nil {
			return nil, fmt.Errorf("parse trash sample tags: %w", err)
		}
		if sample.CreatedAt, err = scanTime(createdRaw); err != nil {
			return nil, fmt.Errorf("parse trash sample created_at: %w", err)
		}
		samples = append(samples, sample)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite: iterate trash samples: %w", err)
	}
	return samples, nil
}

func (s *SQLiteStore) RecordTagHit(ctx context.Context, radarID int64, tag string, isLead bool, isFalsePositive bool, now time.Time) error {
	tag = strings.TrimSpace(tag)
	if radarID <= 0 || tag == "" {
		return fmt.Errorf("%w: radar id and tag are required", domain.ErrInvalidInput)
	}
	var trashHits, leadHits, fp int
	if isLead {
		leadHits = 1
	} else {
		trashHits = 1
	}
	if isFalsePositive {
		fp = 1
	}
	_, err := s.exec.ExecContext(ctx, `INSERT INTO tag_stats(
		radar_id, tag, total_hits, trash_hits, lead_hits, false_positive_hits, throttled_until, updated_at)
		VALUES(?, ?, 1, ?, ?, ?, NULL, ?)
		ON CONFLICT(radar_id, tag) DO UPDATE SET
			total_hits = tag_stats.total_hits + excluded.total_hits,
			trash_hits = tag_stats.trash_hits + excluded.trash_hits,
			lead_hits = tag_stats.lead_hits + excluded.lead_hits,
			false_positive_hits = tag_stats.false_positive_hits + excluded.false_positive_hits,
			updated_at = excluded.updated_at`,
		radarID, tag, trashHits, leadHits, fp, formatTime(now))
	if err != nil {
		return fmt.Errorf("sqlite: record tag hit: %w", err)
	}
	return nil
}

func (s *SQLiteStore) SetTagThrottle(ctx context.Context, radarID int64, tag string, until *time.Time) error {
	tag = strings.TrimSpace(tag)
	if radarID <= 0 || tag == "" {
		return fmt.Errorf("%w: radar id and tag are required", domain.ErrInvalidInput)
	}
	_, err := s.exec.ExecContext(ctx, `INSERT INTO tag_stats(
		radar_id, tag, total_hits, trash_hits, lead_hits, false_positive_hits, throttled_until, updated_at)
		VALUES(?, ?, 0, 0, 0, 0, ?, ?)
		ON CONFLICT(radar_id, tag) DO UPDATE SET
			throttled_until = excluded.throttled_until,
			updated_at = excluded.updated_at`,
		radarID, tag, formatTimePtr(until), formatTime(nowUTC()))
	if err != nil {
		return fmt.Errorf("sqlite: set tag throttle: %w", err)
	}
	return nil
}

func (s *SQLiteStore) ListTagStats(ctx context.Context, radarID int64) ([]domain.TagStat, error) {
	query := `SELECT radar_id, tag, total_hits, trash_hits, lead_hits, false_positive_hits,
		throttled_until, updated_at FROM tag_stats`
	args := make([]any, 0, 1)
	if radarID != 0 {
		query += " WHERE radar_id = ?"
		args = append(args, radarID)
	}
	query += " ORDER BY total_hits DESC, tag ASC"
	rows, err := s.exec.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("sqlite: list tag stats: %w", err)
	}
	defer rows.Close()
	stats := make([]domain.TagStat, 0)
	for rows.Next() {
		var stat domain.TagStat
		var throttled sql.NullString
		var updatedRaw string
		if err := rows.Scan(&stat.RadarID, &stat.Tag, &stat.TotalHits, &stat.TrashHits, &stat.LeadHits,
			&stat.FalsePositiveHits, &throttled, &updatedRaw); err != nil {
			return nil, fmt.Errorf("sqlite: scan tag stat: %w", err)
		}
		var err error
		if stat.ThrottledUntil, err = timePtrFromNull(throttled); err != nil {
			return nil, fmt.Errorf("parse tag throttle: %w", err)
		}
		if stat.UpdatedAt, err = scanTime(updatedRaw); err != nil {
			return nil, fmt.Errorf("parse tag stat updated_at: %w", err)
		}
		stats = append(stats, stat)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite: iterate tag stats: %w", err)
	}
	return stats, nil
}
