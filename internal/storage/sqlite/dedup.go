package sqlite

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/demonidze1337/mia-lead-radar/internal/domain"
	"github.com/demonidze1337/mia-lead-radar/internal/storage"
)

func (s *SQLiteStore) HasDedupFingerprint(ctx context.Context, fingerprint string, now time.Time) (bool, error) {
	if strings.TrimSpace(fingerprint) == "" {
		return false, fmt.Errorf("sqlite: empty dedup fingerprint")
	}
	var exists int
	err := s.exec.QueryRowContext(ctx,
		"SELECT EXISTS(SELECT 1 FROM dedup_fingerprints WHERE fingerprint = ? AND expires_at > ?)",
		fingerprint, formatTime(now)).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("sqlite: check dedup fingerprint: %w", err)
	}
	return exists != 0, nil
}

func (s *SQLiteStore) ListRecentAuthorFingerprints(ctx context.Context, authorID int64, now time.Time, limit int) ([]storage.DedupRecord, error) {
	limit = normalizeLimit(limit, 50)
	rows, err := s.exec.QueryContext(ctx, `SELECT fingerprint, author_id, token_sig, created_at, expires_at
		FROM dedup_fingerprints WHERE author_id = ? AND expires_at > ?
		ORDER BY created_at DESC LIMIT ?`, authorID, formatTime(now), limit)
	if err != nil {
		return nil, fmt.Errorf("sqlite: list author fingerprints: %w", err)
	}
	defer rows.Close()
	records := make([]storage.DedupRecord, 0)
	for rows.Next() {
		var rec storage.DedupRecord
		var createdRaw, expiresRaw string
		if err := rows.Scan(&rec.Fingerprint, &rec.AuthorID, &rec.TokenSig, &createdRaw, &expiresRaw); err != nil {
			return nil, fmt.Errorf("sqlite: scan author fingerprint: %w", err)
		}
		var err error
		if rec.CreatedAt, err = scanTime(createdRaw); err != nil {
			return nil, fmt.Errorf("parse dedup created_at: %w", err)
		}
		if rec.ExpiresAt, err = scanTime(expiresRaw); err != nil {
			return nil, fmt.Errorf("parse dedup expires_at: %w", err)
		}
		records = append(records, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite: iterate author fingerprints: %w", err)
	}
	return records, nil
}

func (s *SQLiteStore) SaveDedupFingerprint(ctx context.Context, rec storage.DedupRecord) error {
	if strings.TrimSpace(rec.Fingerprint) == "" {
		return fmt.Errorf("sqlite: empty dedup fingerprint")
	}
	created := normalizeTime(rec.CreatedAt)
	if rec.ExpiresAt.IsZero() {
		return fmt.Errorf("%w: dedup expiry is required", domain.ErrInvalidInput)
	}
	_, err := s.exec.ExecContext(ctx, `INSERT INTO dedup_fingerprints(fingerprint, author_id, token_sig, created_at, expires_at)
		VALUES(?, ?, ?, ?, ?)
		ON CONFLICT(fingerprint) DO UPDATE SET
			author_id = excluded.author_id,
			token_sig = excluded.token_sig,
			created_at = excluded.created_at,
			expires_at = excluded.expires_at`,
		rec.Fingerprint, rec.AuthorID, rec.TokenSig, formatTime(created), formatTime(rec.ExpiresAt))
	if err != nil {
		return fmt.Errorf("sqlite: save dedup fingerprint: %w", err)
	}
	return nil
}

func (s *SQLiteStore) PruneExpiredDedup(ctx context.Context, now time.Time) (int64, error) {
	res, err := s.exec.ExecContext(ctx, "DELETE FROM dedup_fingerprints WHERE expires_at <= ?", formatTime(now))
	if err != nil {
		return 0, fmt.Errorf("sqlite: prune dedup fingerprints: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("sqlite: prune dedup rows: %w", err)
	}
	return affected, nil
}
