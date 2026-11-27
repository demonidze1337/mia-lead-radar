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

const chatColumns = `id, tg_chat_id, access_hash, username, invite_hash, title, chat_type, source,
	status, member_count, msg_velocity_per_hour, has_comments, linked_chat_id, assigned_account_id,
	last_message_id, last_read_at, last_scanned_at, next_retry_at, error_reason, created_at, updated_at`

func (s *SQLiteStore) UpsertChat(ctx context.Context, chat domain.Chat) (domain.Chat, error) {
	var out domain.Chat
	err := s.WithTx(ctx, func(tx storage.Store) error {
		bound := tx.(*SQLiteStore)
		existingID, err := bound.lookupChatID(ctx, chat)
		if err != nil {
			return err
		}
		if existingID > 0 {
			existing, err := bound.getChatByID(ctx, existingID)
			if err != nil {
				return err
			}
			merged := chat
			merged.ID = existing.ID
			merged.CreatedAt = existing.CreatedAt
			merged.UpdatedAt = nowUTC()
			if merged.Title == "" {
				merged.Title = existing.Title
			}
			if merged.Username == "" {
				merged.Username = existing.Username
			}
			if merged.InviteHash == "" {
				merged.InviteHash = existing.InviteHash
			}
			if merged.TGChatID == 0 {
				merged.TGChatID = existing.TGChatID
			}
			if merged.ChatType == "" {
				merged.ChatType = existing.ChatType
			}
			if merged.Source == "" {
				merged.Source = existing.Source
			}
			if merged.Status == "" {
				merged.Status = existing.Status
			}
			if merged.LastReadAt == nil {
				merged.LastReadAt = existing.LastReadAt
			}
			if merged.LastScannedAt == nil {
				merged.LastScannedAt = existing.LastScannedAt
			}
			if merged.NextRetryAt == nil {
				merged.NextRetryAt = existing.NextRetryAt
			}
			if merged.MemberCount < existing.MemberCount {
				merged.MemberCount = existing.MemberCount
			}
			if _, err := bound.exec.ExecContext(ctx, `UPDATE chats SET
				tg_chat_id = ?, access_hash = ?, username = ?, invite_hash = ?, title = ?, chat_type = ?,
				source = ?, status = ?, member_count = ?, msg_velocity_per_hour = ?, has_comments = ?,
				linked_chat_id = ?, assigned_account_id = ?, last_message_id = ?, last_read_at = ?,
				last_scanned_at = ?, next_retry_at = ?, error_reason = ?, updated_at = ?
				WHERE id = ?`,
				merged.TGChatID, merged.AccessHash, merged.Username, merged.InviteHash, merged.Title,
				string(merged.ChatType), string(merged.Source), string(merged.Status), merged.MemberCount,
				merged.MsgVelocityPerHour, boolToInt(merged.HasComments), merged.LinkedChatID,
				merged.AssignedAccountID, merged.LastMessageID, formatTimePtr(merged.LastReadAt),
				formatTimePtr(merged.LastScannedAt), formatTimePtr(merged.NextRetryAt), merged.ErrorReason,
				formatTime(merged.UpdatedAt), merged.ID); err != nil {
				return fmt.Errorf("sqlite: update chat: %w", err)
			}
			out, err = bound.getChatByID(ctx, merged.ID)
			return err
		}

		now := nowUTC()
		chat.ID = 0
		chat.CreatedAt = normalizeTime(chat.CreatedAt)
		chat.UpdatedAt = now
		if chat.ChatType == "" {
			chat.ChatType = domain.ChatTypeGroup
		}
		if chat.Source == "" {
			chat.Source = domain.ChatSourceSeed
		}
		if chat.Status == "" {
			chat.Status = domain.ChatStatusQueued
		}
		res, err := bound.exec.ExecContext(ctx, `INSERT INTO chats(
			tg_chat_id, access_hash, username, invite_hash, title, chat_type, source, status,
			member_count, msg_velocity_per_hour, has_comments, linked_chat_id, assigned_account_id,
			last_message_id, last_read_at, last_scanned_at, next_retry_at, error_reason,
			created_at, updated_at)
			VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			chat.TGChatID, chat.AccessHash, chat.Username, chat.InviteHash, chat.Title,
			string(chat.ChatType), string(chat.Source), string(chat.Status), chat.MemberCount,
			chat.MsgVelocityPerHour, boolToInt(chat.HasComments), chat.LinkedChatID, chat.AssignedAccountID,
			chat.LastMessageID, formatTimePtr(chat.LastReadAt), formatTimePtr(chat.LastScannedAt),
			formatTimePtr(chat.NextRetryAt), chat.ErrorReason, formatTime(chat.CreatedAt), formatTime(chat.UpdatedAt))
		if err != nil {
			return fmt.Errorf("sqlite: insert chat: %w", err)
		}
		id, err := res.LastInsertId()
		if err != nil {
			return fmt.Errorf("sqlite: chat id: %w", err)
		}
		out, err = bound.getChatByID(ctx, id)
		return err
	})
	return out, err
}

func (s *SQLiteStore) lookupChatID(ctx context.Context, chat domain.Chat) (int64, error) {
	if chat.ID > 0 {
		return chat.ID, nil
	}
	if chat.TGChatID != 0 {
		var id int64
		err := s.exec.QueryRowContext(ctx, "SELECT id FROM chats WHERE tg_chat_id = ? LIMIT 1", chat.TGChatID).Scan(&id)
		if err == nil {
			return id, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return 0, fmt.Errorf("sqlite: lookup chat by tg id: %w", err)
		}
	}
	if chat.Username != "" {
		var id int64
		err := s.exec.QueryRowContext(ctx, "SELECT id FROM chats WHERE LOWER(username) = LOWER(?) LIMIT 1", chat.Username).Scan(&id)
		if err == nil {
			return id, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return 0, fmt.Errorf("sqlite: lookup chat by username: %w", err)
		}
	}
	if chat.InviteHash != "" {
		var id int64
		err := s.exec.QueryRowContext(ctx, "SELECT id FROM chats WHERE invite_hash = ? LIMIT 1", chat.InviteHash).Scan(&id)
		if err == nil {
			return id, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return 0, fmt.Errorf("sqlite: lookup chat by invite: %w", err)
		}
	}
	return 0, nil
}

func (s *SQLiteStore) GetChatByID(ctx context.Context, id int64) (domain.Chat, error) {
	return s.getChatByID(ctx, id)
}

func (s *SQLiteStore) getChatByID(ctx context.Context, id int64) (domain.Chat, error) {
	row := s.exec.QueryRowContext(ctx, "SELECT "+chatColumns+" FROM chats WHERE id = ?", id)
	chat, err := scanChat(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.Chat{}, notFound("chat", id)
		}
		return domain.Chat{}, fmt.Errorf("sqlite: get chat: %w", err)
	}
	return chat, nil
}

func (s *SQLiteStore) GetChatByTGID(ctx context.Context, tgChatID int64) (domain.Chat, error) {
	row := s.exec.QueryRowContext(ctx, "SELECT "+chatColumns+" FROM chats WHERE tg_chat_id = ?", tgChatID)
	chat, err := scanChat(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.Chat{}, fmt.Errorf("%w: chat tg_id %d", domain.ErrNotFound, tgChatID)
		}
		return domain.Chat{}, fmt.Errorf("sqlite: get chat by tg id: %w", err)
	}
	return chat, nil
}

func (s *SQLiteStore) ListChats(ctx context.Context, filter storage.ChatFilter) ([]domain.Chat, error) {
	query := "SELECT " + chatColumns + " FROM chats WHERE 1 = 1"
	args := make([]any, 0, 8)
	if len(filter.Statuses) > 0 {
		placeholders := make([]string, len(filter.Statuses))
		for i, status := range filter.Statuses {
			placeholders[i] = "?"
			args = append(args, string(status))
		}
		query += " AND status IN (" + strings.Join(placeholders, ",") + ")"
	}
	if filter.AssignedAccountID != 0 {
		query += " AND assigned_account_id = ?"
		args = append(args, filter.AssignedAccountID)
	}
	if filter.Query != "" {
		pattern := "%" + escapeLike(filter.Query) + "%"
		query += ` AND (title LIKE ? ESCAPE '\' OR username LIKE ? ESCAPE '\')`
		args = append(args, pattern, pattern)
	}
	query += " ORDER BY updated_at DESC, id DESC LIMIT ? OFFSET ?"
	args = append(args, normalizeLimit(filter.Limit, defaultListLimit), normalizeOffset(filter.Offset))

	rows, err := s.exec.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("sqlite: list chats: %w", err)
	}
	defer rows.Close()
	chats := make([]domain.Chat, 0)
	for rows.Next() {
		chat, err := scanChat(rows)
		if err != nil {
			return nil, fmt.Errorf("sqlite: scan chat: %w", err)
		}
		chats = append(chats, chat)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite: iterate chats: %w", err)
	}
	return chats, nil
}

func (s *SQLiteStore) UpdateChatProgress(ctx context.Context, id int64, lastMsgID int, readAt *time.Time, scannedAt *time.Time) error {
	res, err := s.exec.ExecContext(ctx, `UPDATE chats SET
		last_message_id = ?,
		last_read_at = COALESCE(?, last_read_at),
		last_scanned_at = COALESCE(?, last_scanned_at),
		updated_at = ?
		WHERE id = ?`, lastMsgID, formatTimePtr(readAt), formatTimePtr(scannedAt), formatTime(nowUTC()), id)
	if err != nil {
		return fmt.Errorf("sqlite: update chat progress: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("sqlite: update chat progress rows: %w", err)
	}
	if affected == 0 {
		return notFound("chat", id)
	}
	return nil
}

func scanChat(row scanner) (domain.Chat, error) {
	var chat domain.Chat
	var chatType, source, status, createdRaw, updatedRaw string
	var readAt, scannedAt, retryAt sql.NullString
	var hasComments int
	if err := row.Scan(&chat.ID, &chat.TGChatID, &chat.AccessHash, &chat.Username, &chat.InviteHash,
		&chat.Title, &chatType, &source, &status, &chat.MemberCount, &chat.MsgVelocityPerHour,
		&hasComments, &chat.LinkedChatID, &chat.AssignedAccountID, &chat.LastMessageID, &readAt,
		&scannedAt, &retryAt, &chat.ErrorReason, &createdRaw, &updatedRaw); err != nil {
		return domain.Chat{}, err
	}
	chat.ChatType = domain.ChatType(chatType)
	chat.Source = domain.ChatSource(source)
	chat.Status = domain.ChatStatus(status)
	chat.HasComments = hasComments != 0
	var err error
	if chat.LastReadAt, err = timePtrFromNull(readAt); err != nil {
		return domain.Chat{}, fmt.Errorf("parse last_read_at: %w", err)
	}
	if chat.LastScannedAt, err = timePtrFromNull(scannedAt); err != nil {
		return domain.Chat{}, fmt.Errorf("parse last_scanned_at: %w", err)
	}
	if chat.NextRetryAt, err = timePtrFromNull(retryAt); err != nil {
		return domain.Chat{}, fmt.Errorf("parse next_retry_at: %w", err)
	}
	if chat.CreatedAt, err = scanTime(createdRaw); err != nil {
		return domain.Chat{}, fmt.Errorf("parse chat created_at: %w", err)
	}
	if chat.UpdatedAt, err = scanTime(updatedRaw); err != nil {
		return domain.Chat{}, fmt.Errorf("parse chat updated_at: %w", err)
	}
	return chat, nil
}
