package sqlite

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/demonidze1337/mia-lead-radar/internal/domain"
	"github.com/demonidze1337/mia-lead-radar/internal/storage"
)

const (
	defaultListLimit = 100
	maxListLimit     = 500
)

type scanner interface {
	Scan(dest ...any) error
}

func nowUTC() time.Time {
	return time.Now().UTC()
}

func normalizeTime(t time.Time) time.Time {
	if t.IsZero() {
		return nowUTC()
	}
	return t.UTC()
}

func formatTime(t time.Time) string {
	return storage.FormatTime(t)
}

func formatTimePtr(t *time.Time) any {
	if t == nil {
		return nil
	}
	return storage.FormatTime(*t)
}

func parseTimeValue(raw string) (time.Time, error) {
	t, err := storage.ParseTime(raw)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse timestamp %q: %w", raw, err)
	}
	return t, nil
}

func timePtrFromNull(ns sql.NullString) (*time.Time, error) {
	if !ns.Valid || strings.TrimSpace(ns.String) == "" {
		return nil, nil
	}
	t, err := parseTimeValue(ns.String)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func scanTime(raw string) (time.Time, error) {
	return parseTimeValue(raw)
}

func normalizeLimit(limit, fallback int) int {
	if limit <= 0 {
		limit = fallback
	}
	if limit > maxListLimit {
		return maxListLimit
	}
	return limit
}

func normalizeOffset(offset int) int {
	if offset < 0 {
		return 0
	}
	return offset
}

func marshalJSON(value any) (string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("marshal json: %w", err)
	}
	return string(raw), nil
}

func unmarshalStringSlice(raw string) ([]string, error) {
	if strings.TrimSpace(raw) == "" {
		return []string{}, nil
	}
	var out []string
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, fmt.Errorf("unmarshal string list: %w", err)
	}
	if out == nil {
		out = []string{}
	}
	return out, nil
}

func unmarshalTags(raw string) ([]domain.RadarTag, error) {
	if strings.TrimSpace(raw) == "" {
		return []domain.RadarTag{}, nil
	}
	var out []domain.RadarTag
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, fmt.Errorf("unmarshal radar tags: %w", err)
	}
	if out == nil {
		out = []domain.RadarTag{}
	}
	return out, nil
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

// escapeLike escapes user input for SQL LIKE patterns using backslash.
func escapeLike(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `%`, `\%`)
	s = strings.ReplaceAll(s, `_`, `\_`)
	return s
}

func notFound(entity string, id int64) error {
	return fmt.Errorf("%w: %s %d", domain.ErrNotFound, entity, id)
}

func validateDate(date string) (string, error) {
	if strings.TrimSpace(date) == "" {
		return time.Now().UTC().Format("2006-01-02"), nil
	}
	if _, err := time.Parse("2006-01-02", date); err != nil {
		return "", fmt.Errorf("%w: invalid metric date %q", domain.ErrInvalidInput, date)
	}
	return date, nil
}

func utcDate(t time.Time) string {
	return t.UTC().Format("2006-01-02")
}
