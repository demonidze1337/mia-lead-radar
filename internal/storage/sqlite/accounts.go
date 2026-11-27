package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/demonidze1337/mia-lead-radar/internal/domain"
	"github.com/demonidze1337/mia-lead-radar/internal/storage"
)

const accountColumns = `id, label, phone_masked, tg_user_id, username, first_name, session_path,
	status, flood_until, joined_chats_count, daily_joins_count, daily_joins_reset_at,
	last_active_at, last_error, created_at, updated_at`

func (s *SQLiteStore) UpsertAccount(ctx context.Context, acc domain.TelegramAccount) (domain.TelegramAccount, error) {
	var out domain.TelegramAccount
	err := s.WithTx(ctx, func(tx storage.Store) error {
		bound := tx.(*SQLiteStore)
		existingID := acc.ID
		if existingID <= 0 && acc.TGUserID != 0 {
			err := bound.exec.QueryRowContext(ctx, "SELECT id FROM tg_accounts WHERE tg_user_id = ? ORDER BY id ASC LIMIT 1", acc.TGUserID).Scan(&existingID)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("sqlite: lookup account: %w", err)
			}
			if errors.Is(err, sql.ErrNoRows) {
				existingID = 0
			}
		}
		if existingID > 0 {
			existing, err := bound.getAccount(ctx, existingID)
			if err != nil {
				return err
			}
			merged := acc
			merged.ID = existing.ID
			merged.CreatedAt = existing.CreatedAt
			merged.UpdatedAt = nowUTC()
			if merged.Label == "" {
				merged.Label = existing.Label
			}
			if merged.PhoneMasked == "" {
				merged.PhoneMasked = existing.PhoneMasked
			}
			if merged.TGUserID == 0 {
				merged.TGUserID = existing.TGUserID
			}
			if merged.Username == "" {
				merged.Username = existing.Username
			}
			if merged.FirstName == "" {
				merged.FirstName = existing.FirstName
			}
			if merged.SessionPath == "" {
				merged.SessionPath = existing.SessionPath
			}
			if merged.Status == "" {
				merged.Status = existing.Status
			}
			if merged.FloodUntil == nil {
				merged.FloodUntil = existing.FloodUntil
			}
			if merged.DailyJoinsResetAt == nil {
				merged.DailyJoinsResetAt = existing.DailyJoinsResetAt
			}
			if merged.LastActiveAt == nil {
				merged.LastActiveAt = existing.LastActiveAt
			}
			if merged.JoinedChatsCount < existing.JoinedChatsCount {
				merged.JoinedChatsCount = existing.JoinedChatsCount
			}
			if merged.DailyJoinsCount < existing.DailyJoinsCount {
				merged.DailyJoinsCount = existing.DailyJoinsCount
			}
			if merged.LastError == "" {
				merged.LastError = existing.LastError
			}
			if _, err := bound.exec.ExecContext(ctx, `UPDATE tg_accounts SET
				label = ?, phone_masked = ?, tg_user_id = ?, username = ?, first_name = ?, session_path = ?,
				status = ?, flood_until = ?, joined_chats_count = ?, daily_joins_count = ?,
				daily_joins_reset_at = ?, last_active_at = ?, last_error = ?, updated_at = ?
				WHERE id = ?`,
				merged.Label, merged.PhoneMasked, merged.TGUserID, merged.Username, merged.FirstName,
				merged.SessionPath, merged.Status, formatTimePtr(merged.FloodUntil), merged.JoinedChatsCount,
				merged.DailyJoinsCount, formatTimePtr(merged.DailyJoinsResetAt), formatTimePtr(merged.LastActiveAt),
				merged.LastError, formatTime(merged.UpdatedAt), merged.ID); err != nil {
				return fmt.Errorf("sqlite: update account: %w", err)
			}
			out, err = bound.getAccount(ctx, merged.ID)
			return err
		}

		now := nowUTC()
		acc.ID = 0
		acc.CreatedAt = normalizeTime(acc.CreatedAt)
		acc.UpdatedAt = now
		if acc.Status == "" {
			acc.Status = domain.AccountStatusPendingAuth
		}
		res, err := bound.exec.ExecContext(ctx, `INSERT INTO tg_accounts(
			label, phone_masked, tg_user_id, username, first_name, session_path, status, flood_until,
			joined_chats_count, daily_joins_count, daily_joins_reset_at, last_active_at, last_error,
			created_at, updated_at)
			VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			acc.Label, acc.PhoneMasked, acc.TGUserID, acc.Username, acc.FirstName, acc.SessionPath,
			string(acc.Status), formatTimePtr(acc.FloodUntil), acc.JoinedChatsCount, acc.DailyJoinsCount,
			formatTimePtr(acc.DailyJoinsResetAt), formatTimePtr(acc.LastActiveAt), acc.LastError,
			formatTime(acc.CreatedAt), formatTime(acc.UpdatedAt))
		if err != nil {
			return fmt.Errorf("sqlite: insert account: %w", err)
		}
		id, err := res.LastInsertId()
		if err != nil {
			return fmt.Errorf("sqlite: account id: %w", err)
		}
		out, err = bound.getAccount(ctx, id)
		return err
	})
	return out, err
}

func (s *SQLiteStore) GetAccount(ctx context.Context, id int64) (domain.TelegramAccount, error) {
	return s.getAccount(ctx, id)
}

func (s *SQLiteStore) getAccount(ctx context.Context, id int64) (domain.TelegramAccount, error) {
	row := s.exec.QueryRowContext(ctx, "SELECT "+accountColumns+" FROM tg_accounts WHERE id = ?", id)
	acc, err := scanAccount(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.TelegramAccount{}, notFound("account", id)
		}
		return domain.TelegramAccount{}, fmt.Errorf("sqlite: get account: %w", err)
	}
	return acc, nil
}

func (s *SQLiteStore) ListAccounts(ctx context.Context) ([]domain.TelegramAccount, error) {
	rows, err := s.exec.QueryContext(ctx, "SELECT "+accountColumns+" FROM tg_accounts ORDER BY id ASC")
	if err != nil {
		return nil, fmt.Errorf("sqlite: list accounts: %w", err)
	}
	defer rows.Close()
	accounts := make([]domain.TelegramAccount, 0)
	for rows.Next() {
		acc, err := scanAccount(rows)
		if err != nil {
			return nil, fmt.Errorf("sqlite: scan account: %w", err)
		}
		accounts = append(accounts, acc)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite: iterate accounts: %w", err)
	}
	return accounts, nil
}

func (s *SQLiteStore) SetAccountStatus(ctx context.Context, id int64, status domain.AccountStatus, lastError string) error {
	res, err := s.exec.ExecContext(ctx, "UPDATE tg_accounts SET status = ?, last_error = ?, updated_at = ? WHERE id = ?",
		string(status), lastError, formatTime(nowUTC()), id)
	if err != nil {
		return fmt.Errorf("sqlite: set account status: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("sqlite: set account status rows: %w", err)
	}
	if affected == 0 {
		return notFound("account", id)
	}
	return nil
}

func (s *SQLiteStore) SetAccountFloodUntil(ctx context.Context, id int64, floodUntil time.Time, reason string) error {
	res, err := s.exec.ExecContext(ctx, `UPDATE tg_accounts SET flood_until = ?, status = ?, last_error = ?, updated_at = ?
		WHERE id = ?`, formatTime(floodUntil), string(domain.AccountStatusFloodWait), reason, formatTime(nowUTC()), id)
	if err != nil {
		return fmt.Errorf("sqlite: set account flood gate: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("sqlite: set account flood gate rows: %w", err)
	}
	if affected == 0 {
		return notFound("account", id)
	}
	return nil
}

func (s *SQLiteStore) ClearExpiredFloods(ctx context.Context, now time.Time) (int64, error) {
	res, err := s.exec.ExecContext(ctx, `UPDATE tg_accounts SET
		flood_until = NULL,
		status = CASE WHEN status = ? THEN ? ELSE status END,
		last_error = CASE WHEN status = ? THEN '' ELSE last_error END,
		updated_at = ?
		WHERE flood_until IS NOT NULL AND flood_until <> '' AND flood_until <= ?`,
		string(domain.AccountStatusFloodWait), string(domain.AccountStatusActive),
		string(domain.AccountStatusFloodWait), formatTime(now), formatTime(now))
	if err != nil {
		return 0, fmt.Errorf("sqlite: clear expired floods: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("sqlite: clear expired floods rows: %w", err)
	}
	return affected, nil
}

func (s *SQLiteStore) IncrementAccountJoins(ctx context.Context, id int64, now time.Time) error {
	now = now.UTC()
	date := utcDate(now)
	res, err := s.exec.ExecContext(ctx, `UPDATE tg_accounts SET
		daily_joins_count = CASE
			WHEN daily_joins_reset_at IS NULL OR substr(daily_joins_reset_at, 1, 10) <> ? THEN 1
			ELSE daily_joins_count + 1 END,
		daily_joins_reset_at = CASE
			WHEN daily_joins_reset_at IS NULL OR substr(daily_joins_reset_at, 1, 10) <> ? THEN ?
			ELSE daily_joins_reset_at END,
		joined_chats_count = joined_chats_count + 1,
		updated_at = ?
		WHERE id = ?`, date, date, formatTime(now), formatTime(now), id)
	if err != nil {
		return fmt.Errorf("sqlite: increment account joins: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("sqlite: increment account joins rows: %w", err)
	}
	if affected == 0 {
		return notFound("account", id)
	}
	return nil
}

func (s *SQLiteStore) DeleteAccount(ctx context.Context, id int64) error {
	res, err := s.exec.ExecContext(ctx, "DELETE FROM tg_accounts WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("sqlite: delete account: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("sqlite: delete account rows: %w", err)
	}
	if affected == 0 {
		return notFound("account", id)
	}
	return nil
}

func scanAccount(row scanner) (domain.TelegramAccount, error) {
	var acc domain.TelegramAccount
	var status, createdRaw, updatedRaw string
	var floodUntil, resetAt, lastActive sql.NullString
	if err := row.Scan(&acc.ID, &acc.Label, &acc.PhoneMasked, &acc.TGUserID, &acc.Username, &acc.FirstName,
		&acc.SessionPath, &status, &floodUntil, &acc.JoinedChatsCount, &acc.DailyJoinsCount, &resetAt,
		&lastActive, &acc.LastError, &createdRaw, &updatedRaw); err != nil {
		return domain.TelegramAccount{}, err
	}
	acc.Status = domain.AccountStatus(status)
	var err error
	if acc.FloodUntil, err = timePtrFromNull(floodUntil); err != nil {
		return domain.TelegramAccount{}, fmt.Errorf("parse flood_until: %w", err)
	}
	if acc.DailyJoinsResetAt, err = timePtrFromNull(resetAt); err != nil {
		return domain.TelegramAccount{}, fmt.Errorf("parse daily_joins_reset_at: %w", err)
	}
	if acc.LastActiveAt, err = timePtrFromNull(lastActive); err != nil {
		return domain.TelegramAccount{}, fmt.Errorf("parse last_active_at: %w", err)
	}
	if acc.CreatedAt, err = scanTime(createdRaw); err != nil {
		return domain.TelegramAccount{}, fmt.Errorf("parse account created_at: %w", err)
	}
	if acc.UpdatedAt, err = scanTime(updatedRaw); err != nil {
		return domain.TelegramAccount{}, fmt.Errorf("parse account updated_at: %w", err)
	}
	return acc, nil
}
