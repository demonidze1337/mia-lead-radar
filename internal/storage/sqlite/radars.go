package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/demonidze1337/mia-lead-radar/internal/domain"
	"github.com/demonidze1337/mia-lead-radar/internal/storage"
)

const radarColumns = `id, name, description, tags_json, target_languages_json, stop_phrases_json,
	learned_stop_words_json, allow_hr_vacancies, allow_partnerships, quality_score, quality_notes,
	version, is_active, created_at, updated_at`

func (s *SQLiteStore) CreateRadar(ctx context.Context, radar domain.RadarProfile, source domain.RadarChangeSource, summary string) (domain.RadarProfile, error) {
	if strings.TrimSpace(radar.Name) == "" {
		return domain.RadarProfile{}, fmt.Errorf("%w: radar name is required", domain.ErrInvalidInput)
	}
	var out domain.RadarProfile
	err := s.WithTx(ctx, func(tx storage.Store) error {
		bound := tx.(*SQLiteStore)
		now := nowUTC()
		radar.Version = 1
		radar.CreatedAt = normalizeTime(radar.CreatedAt)
		radar.UpdatedAt = now
		if radar.IsActive {
			if _, err := bound.exec.ExecContext(ctx, "UPDATE radars SET is_active = 0 WHERE is_active = 1"); err != nil {
				return fmt.Errorf("sqlite: deactivate radars: %w", err)
			}
		}
		tags, err := marshalJSON(nonNilTags(radar.Tags))
		if err != nil {
			return err
		}
		languages, err := marshalJSON(nonNilStrings(radar.TargetLanguages))
		if err != nil {
			return err
		}
		stops, err := marshalJSON(nonNilStrings(radar.StopPhrases))
		if err != nil {
			return err
		}
		learned, err := marshalJSON(nonNilStrings(radar.LearnedStopWords))
		if err != nil {
			return err
		}
		res, err := bound.exec.ExecContext(ctx, `INSERT INTO radars(
			name, description, tags_json, target_languages_json, stop_phrases_json, learned_stop_words_json,
			allow_hr_vacancies, allow_partnerships, quality_score, quality_notes, version, is_active,
			created_at, updated_at)
			VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			radar.Name, radar.Description, tags, languages, stops, learned,
			boolToInt(radar.AllowHRVacancies), boolToInt(radar.AllowPartnerships),
			radar.QualityScore, radar.QualityNotes, radar.Version, boolToInt(radar.IsActive),
			formatTime(radar.CreatedAt), formatTime(radar.UpdatedAt))
		if err != nil {
			return fmt.Errorf("sqlite: create radar: %w", err)
		}
		id, err := res.LastInsertId()
		if err != nil {
			return fmt.Errorf("sqlite: radar id: %w", err)
		}
		if err := bound.insertRadarVersion(ctx, id, radar, source, summary); err != nil {
			return err
		}
		out, err = bound.getRadarByID(ctx, id)
		return err
	})
	return out, err
}

func (s *SQLiteStore) UpdateRadar(ctx context.Context, radar domain.RadarProfile, source domain.RadarChangeSource, summary string) (domain.RadarProfile, error) {
	if radar.ID <= 0 {
		return domain.RadarProfile{}, fmt.Errorf("%w: radar id is required", domain.ErrInvalidInput)
	}
	var out domain.RadarProfile
	err := s.WithTx(ctx, func(tx storage.Store) error {
		bound := tx.(*SQLiteStore)
		existing, err := bound.getRadarByID(ctx, radar.ID)
		if err != nil {
			return err
		}
		if strings.TrimSpace(radar.Name) == "" {
			radar.Name = existing.Name
		}
		radar.Version = existing.Version + 1
		radar.CreatedAt = existing.CreatedAt
		radar.UpdatedAt = nowUTC()
		if radar.IsActive {
			if _, err := bound.exec.ExecContext(ctx, "UPDATE radars SET is_active = 0 WHERE id <> ? AND is_active = 1", radar.ID); err != nil {
				return fmt.Errorf("sqlite: deactivate other radars: %w", err)
			}
		}
		tags, err := marshalJSON(nonNilTags(radar.Tags))
		if err != nil {
			return err
		}
		languages, err := marshalJSON(nonNilStrings(radar.TargetLanguages))
		if err != nil {
			return err
		}
		stops, err := marshalJSON(nonNilStrings(radar.StopPhrases))
		if err != nil {
			return err
		}
		learned, err := marshalJSON(nonNilStrings(radar.LearnedStopWords))
		if err != nil {
			return err
		}
		res, err := bound.exec.ExecContext(ctx, `UPDATE radars SET
			name = ?, description = ?, tags_json = ?, target_languages_json = ?, stop_phrases_json = ?,
			learned_stop_words_json = ?, allow_hr_vacancies = ?, allow_partnerships = ?,
			quality_score = ?, quality_notes = ?, version = ?, is_active = ?, updated_at = ?
			WHERE id = ?`,
			radar.Name, radar.Description, tags, languages, stops, learned,
			boolToInt(radar.AllowHRVacancies), boolToInt(radar.AllowPartnerships),
			radar.QualityScore, radar.QualityNotes, radar.Version, boolToInt(radar.IsActive),
			formatTime(radar.UpdatedAt), radar.ID)
		if err != nil {
			return fmt.Errorf("sqlite: update radar: %w", err)
		}
		affected, err := res.RowsAffected()
		if err != nil {
			return fmt.Errorf("sqlite: update radar rows: %w", err)
		}
		if affected == 0 {
			return notFound("radar", radar.ID)
		}
		if err := bound.insertRadarVersion(ctx, radar.ID, radar, source, summary); err != nil {
			return err
		}
		out, err = bound.getRadarByID(ctx, radar.ID)
		return err
	})
	return out, err
}

func (s *SQLiteStore) insertRadarVersion(ctx context.Context, radarID int64, radar domain.RadarProfile, source domain.RadarChangeSource, summary string) error {
	if source == "" {
		source = domain.RadarChangeManual
	}
	tags, err := marshalJSON(nonNilTags(radar.Tags))
	if err != nil {
		return err
	}
	stops, err := marshalJSON(nonNilStrings(radar.StopPhrases))
	if err != nil {
		return err
	}
	learned, err := marshalJSON(nonNilStrings(radar.LearnedStopWords))
	if err != nil {
		return err
	}
	_, err = s.exec.ExecContext(ctx, `INSERT INTO radar_versions(
		radar_id, version, description, tags_json, stop_phrases_json, learned_stop_words_json,
		change_source, audit_summary, created_at)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		radarID, radar.Version, radar.Description, tags, stops, learned,
		string(source), summary, formatTime(normalizeTime(radar.UpdatedAt)))
	if err != nil {
		return fmt.Errorf("sqlite: insert radar version %d: %w", radar.Version, err)
	}
	return nil
}

func (s *SQLiteStore) GetActiveRadar(ctx context.Context) (domain.RadarProfile, error) {
	row := s.exec.QueryRowContext(ctx, "SELECT "+radarColumns+" FROM radars WHERE is_active = 1 ORDER BY id DESC LIMIT 1")
	radar, err := scanRadar(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.RadarProfile{}, fmt.Errorf("%w: active radar", domain.ErrNotFound)
		}
		return domain.RadarProfile{}, fmt.Errorf("sqlite: get active radar: %w", err)
	}
	return radar, nil
}

func (s *SQLiteStore) GetRadarByID(ctx context.Context, id int64) (domain.RadarProfile, error) {
	return s.getRadarByID(ctx, id)
}

func (s *SQLiteStore) getRadarByID(ctx context.Context, id int64) (domain.RadarProfile, error) {
	row := s.exec.QueryRowContext(ctx, "SELECT "+radarColumns+" FROM radars WHERE id = ?", id)
	radar, err := scanRadar(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.RadarProfile{}, notFound("radar", id)
		}
		return domain.RadarProfile{}, fmt.Errorf("sqlite: get radar: %w", err)
	}
	return radar, nil
}

func (s *SQLiteStore) ListRadars(ctx context.Context) ([]domain.RadarProfile, error) {
	rows, err := s.exec.QueryContext(ctx, "SELECT "+radarColumns+" FROM radars ORDER BY is_active DESC, updated_at DESC, id DESC")
	if err != nil {
		return nil, fmt.Errorf("sqlite: list radars: %w", err)
	}
	defer rows.Close()
	radars := make([]domain.RadarProfile, 0)
	for rows.Next() {
		radar, err := scanRadar(rows)
		if err != nil {
			return nil, fmt.Errorf("sqlite: scan radar: %w", err)
		}
		radars = append(radars, radar)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite: iterate radars: %w", err)
	}
	return radars, nil
}

func (s *SQLiteStore) ListRadarVersions(ctx context.Context, radarID int64, limit int) ([]domain.RadarVersion, error) {
	limit = normalizeLimit(limit, 50)
	rows, err := s.exec.QueryContext(ctx, `SELECT id, radar_id, version, description, tags_json,
		stop_phrases_json, learned_stop_words_json, change_source, audit_summary, created_at
		FROM radar_versions WHERE radar_id = ? ORDER BY version DESC LIMIT ?`, radarID, limit)
	if err != nil {
		return nil, fmt.Errorf("sqlite: list radar versions: %w", err)
	}
	defer rows.Close()
	versions := make([]domain.RadarVersion, 0)
	for rows.Next() {
		var v domain.RadarVersion
		var tagsRaw, stopsRaw, learnedRaw, createdRaw string
		if err := rows.Scan(&v.ID, &v.RadarID, &v.Version, &v.Description, &tagsRaw, &stopsRaw,
			&learnedRaw, &v.ChangeSource, &v.AuditSummary, &createdRaw); err != nil {
			return nil, fmt.Errorf("sqlite: scan radar version: %w", err)
		}
		if v.Tags, err = unmarshalTags(tagsRaw); err != nil {
			return nil, err
		}
		if v.StopPhrases, err = unmarshalStringSlice(stopsRaw); err != nil {
			return nil, err
		}
		if v.LearnedStopWords, err = unmarshalStringSlice(learnedRaw); err != nil {
			return nil, err
		}
		if v.CreatedAt, err = scanTime(createdRaw); err != nil {
			return nil, fmt.Errorf("sqlite: radar version created_at: %w", err)
		}
		versions = append(versions, v)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite: iterate radar versions: %w", err)
	}
	return versions, nil
}

func (s *SQLiteStore) AddLearnedStopWords(ctx context.Context, radarID int64, words []string) (domain.RadarProfile, error) {
	var out domain.RadarProfile
	err := s.WithTx(ctx, func(tx storage.Store) error {
		bound := tx.(*SQLiteStore)
		radar, err := bound.getRadarByID(ctx, radarID)
		if err != nil {
			return err
		}
		seen := make(map[string]struct{}, len(radar.LearnedStopWords))
		for _, word := range radar.LearnedStopWords {
			seen[strings.ToLower(strings.TrimSpace(word))] = struct{}{}
		}
		added := 0
		for _, word := range words {
			word = strings.TrimSpace(word)
			key := strings.ToLower(word)
			if word == "" {
				continue
			}
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			radar.LearnedStopWords = append(radar.LearnedStopWords, word)
			added++
		}
		if added == 0 {
			out = radar
			return nil
		}
		raw, err := marshalJSON(nonNilStrings(radar.LearnedStopWords))
		if err != nil {
			return err
		}
		radar.Version++
		radar.UpdatedAt = nowUTC()
		if _, err := bound.exec.ExecContext(ctx,
			"UPDATE radars SET learned_stop_words_json = ?, version = ?, updated_at = ? WHERE id = ?",
			raw, radar.Version, formatTime(radar.UpdatedAt), radarID); err != nil {
			return fmt.Errorf("sqlite: update learned stop words: %w", err)
		}
		summary := fmt.Sprintf("learned %d stop word(s) from false leads", added)
		if err := bound.insertRadarVersion(ctx, radarID, radar, domain.RadarChangeAutoLearn, summary); err != nil {
			return err
		}
		out, err = bound.getRadarByID(ctx, radarID)
		return err
	})
	return out, err
}

func scanRadar(row scanner) (domain.RadarProfile, error) {
	var radar domain.RadarProfile
	var tagsRaw, languagesRaw, stopsRaw, learnedRaw, createdRaw, updatedRaw string
	var allowHR, allowPartnerships, active int
	if err := row.Scan(&radar.ID, &radar.Name, &radar.Description, &tagsRaw, &languagesRaw,
		&stopsRaw, &learnedRaw, &allowHR, &allowPartnerships, &radar.QualityScore,
		&radar.QualityNotes, &radar.Version, &active, &createdRaw, &updatedRaw); err != nil {
		return domain.RadarProfile{}, err
	}
	var err error
	if radar.Tags, err = unmarshalTags(tagsRaw); err != nil {
		return domain.RadarProfile{}, err
	}
	if radar.TargetLanguages, err = unmarshalStringSlice(languagesRaw); err != nil {
		return domain.RadarProfile{}, err
	}
	if radar.StopPhrases, err = unmarshalStringSlice(stopsRaw); err != nil {
		return domain.RadarProfile{}, err
	}
	if radar.LearnedStopWords, err = unmarshalStringSlice(learnedRaw); err != nil {
		return domain.RadarProfile{}, err
	}
	if radar.CreatedAt, err = scanTime(createdRaw); err != nil {
		return domain.RadarProfile{}, fmt.Errorf("parse radar created_at: %w", err)
	}
	if radar.UpdatedAt, err = scanTime(updatedRaw); err != nil {
		return domain.RadarProfile{}, fmt.Errorf("parse radar updated_at: %w", err)
	}
	radar.AllowHRVacancies = allowHR != 0
	radar.AllowPartnerships = allowPartnerships != 0
	radar.IsActive = active != 0
	return radar, nil
}

func nonNilTags(tags []domain.RadarTag) []domain.RadarTag {
	if tags == nil {
		return []domain.RadarTag{}
	}
	return tags
}

func nonNilStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}
