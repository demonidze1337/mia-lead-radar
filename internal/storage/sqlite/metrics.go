package sqlite

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/demonidze1337/mia-lead-radar/internal/domain"
)

const (
	defaultFunnelDays = 7
	defaultMetricDays = 30
	maxMetricDays     = 3650
	minNoisyTagHits   = 5
	maxNoisyTags      = 10
)

func (s *SQLiteStore) IncrementDailyMetric(ctx context.Context, date string, radarID int64, delta domain.DailyMetric) error {
	if radarID <= 0 {
		return fmt.Errorf("%w: radar id is required", domain.ErrInvalidInput)
	}
	date, err := validateDate(date)
	if err != nil {
		return err
	}
	_, err = s.exec.ExecContext(ctx, `INSERT INTO daily_metrics(
		date, radar_id, messages_seen, tier0_passed, tier0_rejected, llm_called,
		trash_count, doubtful_count, potential_count, clear_count, viewed_count,
		profile_opened_count, icebreaker_count, false_positive_count, saved_time_seconds)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(date, radar_id) DO UPDATE SET
			messages_seen = daily_metrics.messages_seen + excluded.messages_seen,
			tier0_passed = daily_metrics.tier0_passed + excluded.tier0_passed,
			tier0_rejected = daily_metrics.tier0_rejected + excluded.tier0_rejected,
			llm_called = daily_metrics.llm_called + excluded.llm_called,
			trash_count = daily_metrics.trash_count + excluded.trash_count,
			doubtful_count = daily_metrics.doubtful_count + excluded.doubtful_count,
			potential_count = daily_metrics.potential_count + excluded.potential_count,
			clear_count = daily_metrics.clear_count + excluded.clear_count,
			viewed_count = daily_metrics.viewed_count + excluded.viewed_count,
			profile_opened_count = daily_metrics.profile_opened_count + excluded.profile_opened_count,
			icebreaker_count = daily_metrics.icebreaker_count + excluded.icebreaker_count,
			false_positive_count = daily_metrics.false_positive_count + excluded.false_positive_count,
			saved_time_seconds = daily_metrics.saved_time_seconds + excluded.saved_time_seconds`,
		date, radarID, delta.MessagesSeen, delta.Tier0Passed, delta.Tier0Rejected, delta.LLMCalled,
		delta.TrashCount, delta.DoubtfulCount, delta.PotentialCount, delta.ClearCount, delta.ViewedCount,
		delta.ProfileOpenedCount, delta.IcebreakerCount, delta.FalsePositiveCount, delta.SavedTimeSeconds)
	if err != nil {
		return fmt.Errorf("sqlite: increment daily metric: %w", err)
	}
	return nil
}

func (s *SQLiteStore) GetFunnelSummary(ctx context.Context, radarID int64, days int, trashThresholdPct float64) (domain.FunnelSummary, error) {
	if days <= 0 {
		days = defaultFunnelDays
	}
	if days > maxMetricDays {
		days = maxMetricDays
	}
	if trashThresholdPct <= 0 {
		trashThresholdPct = 75
	}
	cutoff := metricCutoff(days)
	query := `SELECT
		COALESCE(SUM(messages_seen), 0), COALESCE(SUM(tier0_passed), 0), COALESCE(SUM(tier0_rejected), 0),
		COALESCE(SUM(llm_called), 0), COALESCE(SUM(trash_count), 0), COALESCE(SUM(doubtful_count), 0),
		COALESCE(SUM(potential_count), 0), COALESCE(SUM(clear_count), 0), COALESCE(SUM(viewed_count), 0),
		COALESCE(SUM(profile_opened_count), 0), COALESCE(SUM(icebreaker_count), 0),
		COALESCE(SUM(false_positive_count), 0), COALESCE(SUM(saved_time_seconds), 0)
		FROM daily_metrics WHERE date >= ?`
	args := []any{cutoff}
	if radarID != 0 {
		query += " AND radar_id = ?"
		args = append(args, radarID)
	}
	summary := domain.FunnelSummary{RadarID: radarID, Days: days}
	if err := s.exec.QueryRowContext(ctx, query, args...).Scan(
		&summary.MessagesSeen, &summary.Tier0Passed, &summary.Tier0Rejected, &summary.LLMCalled,
		&summary.TrashCount, &summary.DoubtfulCount, &summary.PotentialCount, &summary.ClearCount,
		&summary.ViewedCount, &summary.ProfileOpenedCount, &summary.IcebreakerCount,
		&summary.FalsePositiveCount, &summary.SavedTimeSeconds); err != nil {
		return domain.FunnelSummary{}, fmt.Errorf("sqlite: funnel summary: %w", err)
	}
	summary.TotalLeads = summary.DoubtfulCount + summary.PotentialCount + summary.ClearCount
	classified := summary.TrashCount + summary.TotalLeads
	if classified > 0 {
		summary.TrashRatioPct = ratioPct(summary.TrashCount, classified)
	}
	if summary.TotalLeads > 0 {
		summary.ViewConversionPct = ratioPct(summary.ViewedCount, summary.TotalLeads)
		summary.ProfileConversionPct = ratioPct(summary.ProfileOpenedCount, summary.TotalLeads)
	}
	noisyTags, err := s.noisyTags(ctx, radarID, trashThresholdPct)
	if err != nil {
		return domain.FunnelSummary{}, err
	}
	summary.NoisyTags = noisyTags
	summary.NoiseGuardTriggered = classified > 0 && summary.TrashRatioPct >= trashThresholdPct
	return summary, nil
}

func (s *SQLiteStore) noisyTags(ctx context.Context, radarID int64, thresholdPct float64) ([]domain.TagStat, error) {
	stats, err := s.ListTagStats(ctx, radarID)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	grouped := make(map[string]*domain.TagStat)
	for _, stat := range stats {
		agg, ok := grouped[stat.Tag]
		if !ok {
			agg = &domain.TagStat{RadarID: radarID, Tag: stat.Tag}
			grouped[stat.Tag] = agg
		}
		agg.TotalHits += stat.TotalHits
		agg.TrashHits += stat.TrashHits
		agg.LeadHits += stat.LeadHits
		agg.FalsePositiveHits += stat.FalsePositiveHits
		if stat.ThrottledUntil != nil && (agg.ThrottledUntil == nil || stat.ThrottledUntil.After(*agg.ThrottledUntil)) {
			throttled := *stat.ThrottledUntil
			agg.ThrottledUntil = &throttled
		}
		if stat.UpdatedAt.After(agg.UpdatedAt) {
			agg.UpdatedAt = stat.UpdatedAt
		}
	}
	out := make([]domain.TagStat, 0, len(grouped))
	for _, stat := range grouped {
		if stat.TotalHits < minNoisyTagHits {
			// A manually throttled tag is always surfaced to the UI.
			if stat.ThrottledUntil == nil || !stat.ThrottledUntil.After(now) {
				continue
			}
		} else if ratioPct(stat.TrashHits, stat.TotalHits) < thresholdPct {
			if stat.ThrottledUntil == nil || !stat.ThrottledUntil.After(now) {
				continue
			}
		}
		out = append(out, *stat)
	}
	sort.Slice(out, func(i, j int) bool {
		left := ratioPct(out[i].TrashHits, out[i].TotalHits)
		right := ratioPct(out[j].TrashHits, out[j].TotalHits)
		if left != right {
			return left > right
		}
		return out[i].TotalHits > out[j].TotalHits
	})
	if len(out) > maxNoisyTags {
		out = out[:maxNoisyTags]
	}
	return out, nil
}

func (s *SQLiteStore) ListDailyMetrics(ctx context.Context, radarID int64, days int) ([]domain.DailyMetric, error) {
	if days <= 0 {
		days = defaultMetricDays
	}
	if days > maxMetricDays {
		days = maxMetricDays
	}
	cutoff := metricCutoff(days)
	var query string
	args := []any{cutoff}
	if radarID == 0 {
		query = `SELECT date, 0,
			COALESCE(SUM(messages_seen), 0), COALESCE(SUM(tier0_passed), 0), COALESCE(SUM(tier0_rejected), 0),
			COALESCE(SUM(llm_called), 0), COALESCE(SUM(trash_count), 0), COALESCE(SUM(doubtful_count), 0),
			COALESCE(SUM(potential_count), 0), COALESCE(SUM(clear_count), 0), COALESCE(SUM(viewed_count), 0),
			COALESCE(SUM(profile_opened_count), 0), COALESCE(SUM(icebreaker_count), 0),
			COALESCE(SUM(false_positive_count), 0), COALESCE(SUM(saved_time_seconds), 0)
			FROM daily_metrics WHERE date >= ? GROUP BY date ORDER BY date ASC`
	} else {
		query = `SELECT date, radar_id, messages_seen, tier0_passed, tier0_rejected, llm_called,
			trash_count, doubtful_count, potential_count, clear_count, viewed_count,
			profile_opened_count, icebreaker_count, false_positive_count, saved_time_seconds
			FROM daily_metrics WHERE date >= ? AND radar_id = ? ORDER BY date ASC`
		args = append(args, radarID)
	}
	rows, err := s.exec.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("sqlite: list daily metrics: %w", err)
	}
	defer rows.Close()
	metrics := make([]domain.DailyMetric, 0)
	for rows.Next() {
		var metric domain.DailyMetric
		if err := rows.Scan(&metric.Date, &metric.RadarID, &metric.MessagesSeen, &metric.Tier0Passed,
			&metric.Tier0Rejected, &metric.LLMCalled, &metric.TrashCount, &metric.DoubtfulCount,
			&metric.PotentialCount, &metric.ClearCount, &metric.ViewedCount, &metric.ProfileOpenedCount,
			&metric.IcebreakerCount, &metric.FalsePositiveCount, &metric.SavedTimeSeconds); err != nil {
			return nil, fmt.Errorf("sqlite: scan daily metric: %w", err)
		}
		metrics = append(metrics, metric)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite: iterate daily metrics: %w", err)
	}
	return metrics, nil
}

func metricCutoff(days int) string {
	return time.Now().UTC().AddDate(0, 0, -(days - 1)).Format("2006-01-02")
}

func ratioPct(part, total int64) float64 {
	if total <= 0 {
		return 0
	}
	return float64(part) / float64(total) * 100
}
