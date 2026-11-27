package sqlite

import (
	"context"
	"fmt"
	"strings"

	"github.com/demonidze1337/mia-lead-radar/internal/domain"
)

func (s *SQLiteStore) AppendConsultantMessage(ctx context.Context, msg domain.ConsultantMessage) (domain.ConsultantMessage, error) {
	if strings.TrimSpace(msg.SessionKey) == "" {
		return domain.ConsultantMessage{}, fmt.Errorf("%w: consultant session key is required", domain.ErrInvalidInput)
	}
	if strings.TrimSpace(msg.Role) == "" {
		return domain.ConsultantMessage{}, fmt.Errorf("%w: consultant role is required", domain.ErrInvalidInput)
	}
	if msg.Mode == "" {
		msg.Mode = "chat"
	}
	if strings.TrimSpace(msg.MetadataJSON) == "" {
		msg.MetadataJSON = "{}"
	}
	msg.CreatedAt = normalizeTime(msg.CreatedAt)
	res, err := s.exec.ExecContext(ctx, `INSERT INTO consultant_messages(
		session_key, role, mode, lead_id, content, metadata_json, created_at)
		VALUES(?, ?, ?, ?, ?, ?, ?)`,
		msg.SessionKey, msg.Role, msg.Mode, msg.LeadID, msg.Content, msg.MetadataJSON, formatTime(msg.CreatedAt))
	if err != nil {
		return domain.ConsultantMessage{}, fmt.Errorf("sqlite: append consultant message: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return domain.ConsultantMessage{}, fmt.Errorf("sqlite: consultant message id: %w", err)
	}
	msg.ID = id
	return msg, nil
}

// ListConsultantMessages returns the latest limit messages for a session in
// chronological order (oldest first), suitable for LLM context assembly.
func (s *SQLiteStore) ListConsultantMessages(ctx context.Context, sessionKey string, limit int) ([]domain.ConsultantMessage, error) {
	limit = normalizeLimit(limit, 50)
	rows, err := s.exec.QueryContext(ctx, `SELECT id, session_key, role, mode, lead_id, content, metadata_json, created_at
		FROM (
			SELECT id, session_key, role, mode, lead_id, content, metadata_json, created_at
			FROM consultant_messages WHERE session_key = ? ORDER BY id DESC LIMIT ?
		) ORDER BY id ASC`, sessionKey, limit)
	if err != nil {
		return nil, fmt.Errorf("sqlite: list consultant messages: %w", err)
	}
	defer rows.Close()
	messages := make([]domain.ConsultantMessage, 0)
	for rows.Next() {
		var msg domain.ConsultantMessage
		var createdRaw string
		if err := rows.Scan(&msg.ID, &msg.SessionKey, &msg.Role, &msg.Mode, &msg.LeadID,
			&msg.Content, &msg.MetadataJSON, &createdRaw); err != nil {
			return nil, fmt.Errorf("sqlite: scan consultant message: %w", err)
		}
		var err error
		if msg.CreatedAt, err = scanTime(createdRaw); err != nil {
			return nil, fmt.Errorf("parse consultant message created_at: %w", err)
		}
		messages = append(messages, msg)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite: iterate consultant messages: %w", err)
	}
	return messages, nil
}
