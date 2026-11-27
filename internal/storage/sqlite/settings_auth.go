package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/demonidze1337/mia-lead-radar/internal/domain"
	"github.com/demonidze1337/mia-lead-radar/internal/storage"
)

const settingsKeyRuntime = "runtime"

// GetSettings returns persisted runtime settings, layered over defaults so new
// fields are populated when older settings JSON is loaded.
func (s *SQLiteStore) GetSettings(ctx context.Context) (domain.RuntimeSettings, error) {
	settings := domain.DefaultRuntimeSettings()
	var raw string
	err := s.exec.QueryRowContext(ctx, "SELECT value_json FROM app_settings WHERE key = ?", settingsKeyRuntime).Scan(&raw)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return settings, nil
		}
		return domain.RuntimeSettings{}, fmt.Errorf("sqlite: get settings: %w", err)
	}
	if err := json.Unmarshal([]byte(raw), &settings); err != nil {
		return domain.RuntimeSettings{}, fmt.Errorf("sqlite: decode settings: %w", err)
	}
	if settings.TargetLanguages == nil {
		settings.TargetLanguages = []string{}
	}
	return settings, nil
}

// SaveSettings persists runtime settings as a single JSON document.
func (s *SQLiteStore) SaveSettings(ctx context.Context, settings domain.RuntimeSettings) error {
	raw, err := marshalJSON(settings)
	if err != nil {
		return err
	}
	_, err = s.exec.ExecContext(ctx, `INSERT INTO app_settings(key, value_json, updated_at)
		VALUES(?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET value_json = excluded.value_json, updated_at = excluded.updated_at`,
		settingsKeyRuntime, raw, formatTime(nowUTC()))
	if err != nil {
		return fmt.Errorf("sqlite: save settings: %w", err)
	}
	return nil
}

func (s *SQLiteStore) CreateAuthSession(ctx context.Context, sess domain.AuthSession) (int64, error) {
	if sess.TokenHash == "" {
		return 0, fmt.Errorf("%w: auth session token hash is required", domain.ErrInvalidInput)
	}
	if sess.Role == "" {
		sess.Role = "owner"
	}
	created := normalizeTime(sess.CreatedAt)
	lastSeen := normalizeTime(sess.LastSeenAt)
	if sess.ExpiresAt.IsZero() {
		return 0, fmt.Errorf("%w: auth session expiry is required", domain.ErrInvalidInput)
	}
	res, err := s.exec.ExecContext(ctx, `INSERT INTO auth_sessions(token_hash, label, tg_user_id, role, created_at, expires_at, last_seen_at)
		VALUES(?, ?, ?, ?, ?, ?, ?)`,
		sess.TokenHash, sess.Label, sess.TGUserID, sess.Role,
		formatTime(created), formatTime(sess.ExpiresAt), formatTime(lastSeen))
	if err != nil {
		return 0, fmt.Errorf("sqlite: create auth session: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("sqlite: auth session id: %w", err)
	}
	return id, nil
}

func (s *SQLiteStore) GetAuthSessionByHash(ctx context.Context, tokenHash string) (domain.AuthSession, error) {
	row := s.exec.QueryRowContext(ctx, `SELECT id, token_hash, label, tg_user_id, role, created_at, expires_at, last_seen_at
		FROM auth_sessions WHERE token_hash = ?`, tokenHash)
	var sess domain.AuthSession
	var createdAt, expiresAt, lastSeenAt string
	if err := row.Scan(&sess.ID, &sess.TokenHash, &sess.Label, &sess.TGUserID, &sess.Role, &createdAt, &expiresAt, &lastSeenAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.AuthSession{}, fmt.Errorf("%w: auth session", domain.ErrNotFound)
		}
		return domain.AuthSession{}, fmt.Errorf("sqlite: get auth session: %w", err)
	}
	var err error
	if sess.CreatedAt, err = scanTime(createdAt); err != nil {
		return domain.AuthSession{}, fmt.Errorf("sqlite: auth session created_at: %w", err)
	}
	if sess.ExpiresAt, err = scanTime(expiresAt); err != nil {
		return domain.AuthSession{}, fmt.Errorf("sqlite: auth session expires_at: %w", err)
	}
	if sess.LastSeenAt, err = scanTime(lastSeenAt); err != nil {
		return domain.AuthSession{}, fmt.Errorf("sqlite: auth session last_seen_at: %w", err)
	}
	return sess, nil
}

func (s *SQLiteStore) TouchAuthSession(ctx context.Context, id int64, seenAt time.Time) error {
	res, err := s.exec.ExecContext(ctx, "UPDATE auth_sessions SET last_seen_at = ? WHERE id = ?", formatTime(seenAt), id)
	if err != nil {
		return fmt.Errorf("sqlite: touch auth session: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("sqlite: touch auth session rows: %w", err)
	}
	if affected == 0 {
		return notFound("auth session", id)
	}
	return nil
}

func (s *SQLiteStore) DeleteExpiredAuthSessions(ctx context.Context, now time.Time) (int64, error) {
	res, err := s.exec.ExecContext(ctx, "DELETE FROM auth_sessions WHERE expires_at <= ?", formatTime(now))
	if err != nil {
		return 0, fmt.Errorf("sqlite: delete expired auth sessions: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("sqlite: delete expired auth sessions rows: %w", err)
	}
	return affected, nil
}

var _ storage.Store = (*SQLiteStore)(nil)
