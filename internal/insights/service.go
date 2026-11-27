// Package insights implements Smart Noise Guard telemetry, funnel analytics,
// false-lead stop-word learning, exports, and webhooks.
package insights

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/demonidze1337/mia-lead-radar/internal/domain"
	"github.com/demonidze1337/mia-lead-radar/internal/events"
	tier0filter "github.com/demonidze1337/mia-lead-radar/internal/filter"
	"github.com/demonidze1337/mia-lead-radar/internal/storage"
)

const (
	qualifiedLeadSavedSeconds = 45
	rejectedNoiseSavedSeconds = 8
	icebreakerSavedSeconds    = 30
	defaultNoiseMinHits       = int64(8)
	defaultNoiseCooldown      = 6 * time.Hour
	defaultNoiseWindowDays    = 7
	defaultWebhookTimeout     = 10 * time.Second
	exportPageSize            = 500
	maxLearnedStopWords       = 5
	maxWebhookResponseBytes   = 4096
)

// Option customizes Manager behavior.
type Option func(*managerOptions)

type managerOptions struct {
	now            func() time.Time
	httpClient     *http.Client
	webhookTimeout time.Duration
	noiseMinHits   int64
	noiseCooldown  time.Duration
	funnelDays     int
}

// WithClock injects a deterministic clock for tests.
func WithClock(now func() time.Time) Option {
	return func(opts *managerOptions) {
		if now != nil {
			opts.now = now
		}
	}
}

// WithHTTPClient injects the HTTP client used for webhook delivery.
func WithHTTPClient(client *http.Client) Option {
	return func(opts *managerOptions) {
		if client != nil {
			opts.httpClient = client
		}
	}
}

// WithWebhookTimeout changes the per-delivery timeout.
func WithWebhookTimeout(timeout time.Duration) Option {
	return func(opts *managerOptions) {
		if timeout > 0 {
			opts.webhookTimeout = timeout
		}
	}
}

// WithNoiseGuardPolicy tunes minimum tag hits, cooldown, and funnel window.
func WithNoiseGuardPolicy(minHits int64, cooldown time.Duration, windowDays int) Option {
	return func(opts *managerOptions) {
		if minHits > 0 {
			opts.noiseMinHits = minHits
		}
		if cooldown > 0 {
			opts.noiseCooldown = cooldown
		}
		if windowDays > 0 {
			opts.funnelDays = windowDays
		}
	}
}

// Manager implements Service backed by storage.Store and events.Bus.
type Manager struct {
	store          storage.Store
	bus            events.Bus
	now            func() time.Time
	httpClient     *http.Client
	webhookTimeout time.Duration
	noiseMinHits   int64
	noiseCooldown  time.Duration
	funnelDays     int
}

var _ Service = (*Manager)(nil)

// NewService builds the telemetry/insights manager.
func NewService(store storage.Store, bus events.Bus, opts ...Option) *Manager {
	options := managerOptions{
		now:            time.Now,
		httpClient:     &http.Client{},
		webhookTimeout: defaultWebhookTimeout,
		noiseMinHits:   defaultNoiseMinHits,
		noiseCooldown:  defaultNoiseCooldown,
		funnelDays:     defaultNoiseWindowDays,
	}
	for _, opt := range opts {
		opt(&options)
	}
	return &Manager{
		store:          store,
		bus:            bus,
		now:            options.now,
		httpClient:     options.httpClient,
		webhookTimeout: options.webhookTimeout,
		noiseMinHits:   options.noiseMinHits,
		noiseCooldown:  options.noiseCooldown,
		funnelDays:     options.funnelDays,
	}
}

func (m *Manager) nowUTC() time.Time {
	return m.now().UTC()
}

func metricDate(t time.Time) string {
	return t.UTC().Format("2006-01-02")
}

func uniqueTags(tags []string) []string {
	out := make([]string, 0, len(tags))
	seen := make(map[string]struct{}, len(tags))
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag == "" {
			continue
		}
		if _, exists := seen[tag]; exists {
			continue
		}
		seen[tag] = struct{}{}
		out = append(out, tag)
	}
	return out
}

// RecordTier0Outcome atomically increments Tier-0 funnel counters and trash tag hits.
func (m *Manager) RecordTier0Outcome(ctx context.Context, radarID int64, passed bool, matchedTags []string) error {
	if m.store == nil {
		return fmt.Errorf("%w: nil store", domain.ErrInvalidInput)
	}
	if radarID <= 0 {
		return fmt.Errorf("%w: radar id is required", domain.ErrInvalidInput)
	}
	now := m.nowUTC()
	delta := domain.DailyMetric{MessagesSeen: 1}
	if passed {
		delta.Tier0Passed = 1
	} else {
		delta.Tier0Rejected = 1
	}
	tags := uniqueTags(matchedTags)
	return m.store.WithTx(ctx, func(tx storage.Store) error {
		if err := tx.IncrementDailyMetric(ctx, metricDate(now), radarID, delta); err != nil {
			return err
		}
		if passed {
			return nil
		}
		for _, tag := range tags {
			if err := tx.RecordTagHit(ctx, radarID, tag, false, false, now); err != nil {
				return err
			}
		}
		return nil
	})
}

// RecordLLMOutcome increments LLM/category counters, saved time, and tag quality stats.
func (m *Manager) RecordLLMOutcome(ctx context.Context, radarID int64, category domain.LeadCategory, matchedTags []string) error {
	if m.store == nil {
		return fmt.Errorf("%w: nil store", domain.ErrInvalidInput)
	}
	if radarID <= 0 {
		return fmt.Errorf("%w: radar id is required", domain.ErrInvalidInput)
	}
	now := m.nowUTC()
	delta := domain.DailyMetric{LLMCalled: 1}
	isLead := category.IsLead()
	switch category {
	case domain.LeadCategoryTrash:
		delta.TrashCount = 1
		delta.SavedTimeSeconds = rejectedNoiseSavedSeconds
	case domain.LeadCategoryDoubtful:
		delta.DoubtfulCount = 1
		delta.SavedTimeSeconds = qualifiedLeadSavedSeconds
	case domain.LeadCategoryPotential:
		delta.PotentialCount = 1
		delta.SavedTimeSeconds = qualifiedLeadSavedSeconds
	case domain.LeadCategoryClear:
		delta.ClearCount = 1
		delta.SavedTimeSeconds = qualifiedLeadSavedSeconds
	default:
		return fmt.Errorf("%w: unknown lead category %q", domain.ErrInvalidInput, category)
	}
	tags := uniqueTags(matchedTags)
	return m.store.WithTx(ctx, func(tx storage.Store) error {
		if err := tx.IncrementDailyMetric(ctx, metricDate(now), radarID, delta); err != nil {
			return err
		}
		for _, tag := range tags {
			if err := tx.RecordTagHit(ctx, radarID, tag, isLead, false, now); err != nil {
				return err
			}
		}
		return nil
	})
}

// RecordFunnelAction records viewed/profile/icebreaker telemetry. Viewed and
// profile_opened are idempotent per lead: repeated calls never double-count.
func (m *Manager) RecordFunnelAction(ctx context.Context, radarID, leadID int64, action FunnelAction) error {
	if m.store == nil {
		return fmt.Errorf("%w: nil store", domain.ErrInvalidInput)
	}
	now := m.nowUTC()
	return m.store.WithTx(ctx, func(tx storage.Store) error {
		lead, err := tx.GetLeadByID(ctx, leadID)
		if err != nil {
			return err
		}
		targetRadarID := lead.RadarID
		if targetRadarID <= 0 {
			targetRadarID = radarID
		}
		if targetRadarID <= 0 {
			return fmt.Errorf("%w: radar id is required", domain.ErrInvalidInput)
		}
		switch action {
		case FunnelViewed:
			if lead.ViewedAt != nil {
				return nil
			}
			if err := tx.MarkLeadViewed(ctx, leadID, now); err != nil {
				return err
			}
			return tx.IncrementDailyMetric(ctx, metricDate(now), targetRadarID, domain.DailyMetric{ViewedCount: 1})
		case FunnelProfileOpened:
			if lead.ProfileOpenedAt != nil {
				return nil
			}
			if err := tx.MarkLeadProfileOpened(ctx, leadID, now); err != nil {
				return err
			}
			return tx.IncrementDailyMetric(ctx, metricDate(now), targetRadarID, domain.DailyMetric{ProfileOpenedCount: 1})
		case FunnelIcebreaker:
			// The interface carries no draft text, so this action tracks the
			// generated-draft telemetry; RecordIcebreaker persists the actual text.
			return tx.IncrementDailyMetric(ctx, metricDate(now), targetRadarID, domain.DailyMetric{
				IcebreakerCount:  1,
				SavedTimeSeconds: icebreakerSavedSeconds,
			})
		default:
			return fmt.Errorf("%w: unknown funnel action %q", domain.ErrInvalidInput, action)
		}
	})
}

// RecordIcebreaker appends an actual generated draft and records saved time.
func (m *Manager) RecordIcebreaker(ctx context.Context, radarID, leadID int64, text string, savedSeconds int) error {
	if m.store == nil {
		return fmt.Errorf("%w: nil store", domain.ErrInvalidInput)
	}
	if savedSeconds < 0 {
		savedSeconds = 0
	}
	now := m.nowUTC()
	return m.store.WithTx(ctx, func(tx storage.Store) error {
		lead, err := tx.GetLeadByID(ctx, leadID)
		if err != nil {
			return err
		}
		targetRadarID := lead.RadarID
		if targetRadarID <= 0 {
			targetRadarID = radarID
		}
		if targetRadarID <= 0 {
			return fmt.Errorf("%w: radar id is required", domain.ErrInvalidInput)
		}
		if err := tx.AppendLeadIcebreaker(ctx, leadID, text, savedSeconds); err != nil {
			return err
		}
		return tx.IncrementDailyMetric(ctx, metricDate(now), targetRadarID, domain.DailyMetric{
			IcebreakerCount:  1,
			SavedTimeSeconds: int64(savedSeconds),
		})
	})
}

// MarkFalsePositive marks a lead false, updates tag quality, and optionally learns stop words.
func (m *Manager) MarkFalsePositive(ctx context.Context, radarID, leadID int64, learnStopWords bool) (domain.RadarProfile, error) {
	if m.store == nil {
		return domain.RadarProfile{}, fmt.Errorf("%w: nil store", domain.ErrInvalidInput)
	}
	now := m.nowUTC()
	var result domain.RadarProfile
	err := m.store.WithTx(ctx, func(tx storage.Store) error {
		lead, err := tx.GetLeadByID(ctx, leadID)
		if err != nil {
			return err
		}
		targetRadarID := lead.RadarID
		if targetRadarID <= 0 {
			targetRadarID = radarID
		}
		radar, err := tx.GetRadarByID(ctx, targetRadarID)
		if err != nil {
			return err
		}
		result = radar
		if lead.Status == domain.LeadStatusFalsePositive {
			return nil
		}
		if err := tx.UpdateLeadStatus(ctx, leadID, domain.LeadStatusFalsePositive); err != nil {
			return err
		}
		if err := tx.IncrementDailyMetric(ctx, metricDate(now), targetRadarID, domain.DailyMetric{FalsePositiveCount: 1}); err != nil {
			return err
		}
		for _, tag := range uniqueTags(lead.MatchedTags) {
			if err := tx.RecordTagHit(ctx, targetRadarID, tag, true, true, now); err != nil {
				return err
			}
		}
		if !learnStopWords {
			return nil
		}
		candidates := extractStopWordCandidates(lead.Text, radar)
		if len(candidates) == 0 {
			return nil
		}
		updated, err := tx.AddLearnedStopWords(ctx, targetRadarID, candidates)
		if err != nil {
			return err
		}
		result = updated
		return nil
	})
	if err != nil {
		return domain.RadarProfile{}, err
	}
	return result, nil
}

func extractStopWordCandidates(text string, radar domain.RadarProfile) []string {
	tokens := tier0filter.Tokenize(tier0filter.Normalize(text))
	if len(tokens) == 0 {
		return nil
	}
	excluded := make(map[string]struct{})
	for _, tag := range radar.Tags {
		if !tag.Enabled {
			continue
		}
		for _, token := range tier0filter.Tokenize(tier0filter.Normalize(tag.Phrase)) {
			excluded[token] = struct{}{}
		}
	}
	for _, word := range radar.LearnedStopWords {
		for _, token := range tier0filter.Tokenize(tier0filter.Normalize(word)) {
			excluded[token] = struct{}{}
		}
	}
	seen := make(map[string]struct{})
	candidates := make([]string, 0, maxLearnedStopWords)
	for _, token := range tokens {
		if len([]rune(token)) < 4 {
			continue
		}
		if strings.Trim(token, "0123456789_") == "" {
			continue
		}
		if _, ok := commonStopWords[token]; ok {
			continue
		}
		if _, ok := excluded[token]; ok {
			continue
		}
		if _, ok := seen[token]; ok {
			continue
		}
		seen[token] = struct{}{}
		candidates = append(candidates, token)
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		return len([]rune(candidates[i])) > len([]rune(candidates[j]))
	})
	if len(candidates) > maxLearnedStopWords {
		candidates = candidates[:maxLearnedStopWords]
	}
	return candidates
}

// EvaluateNoiseGuard detects noisy tags, applies cooldowns, and publishes alerts.
func (m *Manager) EvaluateNoiseGuard(ctx context.Context, radarID int64) (*NoiseAlert, error) {
	if m.store == nil {
		return nil, fmt.Errorf("%w: nil store", domain.ErrInvalidInput)
	}
	if radarID <= 0 {
		return nil, fmt.Errorf("%w: radar id is required", domain.ErrInvalidInput)
	}
	settings, err := m.store.GetSettings(ctx)
	if err != nil {
		return nil, err
	}
	if !settings.NoiseGuardEnabled {
		return nil, nil
	}
	threshold := settings.NoiseTrashThresholdPct
	if threshold <= 0 {
		threshold = 75
	}
	summary, err := m.store.GetFunnelSummary(ctx, radarID, m.funnelDays, threshold)
	if err != nil {
		return nil, err
	}
	stats, err := m.store.ListTagStats(ctx, radarID)
	if err != nil {
		return nil, err
	}
	now := m.nowUTC()
	until := now.Add(m.noiseCooldown)
	noisy := make([]domain.TagStat, 0, len(stats))
	newlyThrottled := make([]string, 0, len(stats))
	plan := make([]domain.TagStat, 0, len(stats))
	for _, stat := range stats {
		if stat.TotalHits < m.noiseMinHits {
			continue
		}
		noiseHits := stat.TrashHits + stat.FalsePositiveHits
		ratio := float64(noiseHits) / float64(stat.TotalHits) * 100
		if ratio < threshold {
			continue
		}
		copyStat := stat
		if stat.ThrottledUntil == nil || !stat.ThrottledUntil.After(now) {
			copyStat.ThrottledUntil = &until
			newlyThrottled = append(newlyThrottled, stat.Tag)
			plan = append(plan, stat)
		}
		noisy = append(noisy, copyStat)
	}
	if len(noisy) == 0 {
		return nil, nil
	}
	if len(plan) > 0 {
		err := m.store.WithTx(ctx, func(tx storage.Store) error {
			for _, stat := range plan {
				if err := tx.SetTagThrottle(ctx, radarID, stat.Tag, &until); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	alert := &NoiseAlert{
		RadarID:           radarID,
		TrashRatioPct:     summary.TrashRatioPct,
		ThresholdPct:      threshold,
		ThrottledTags:     newlyThrottled,
		NoisyTags:         noisy,
		RecommendedAction: noiseRecommendation(len(newlyThrottled), m.noiseCooldown),
		TriggeredAt:       now,
	}
	if len(newlyThrottled) > 0 && m.bus != nil {
		m.bus.Publish(ctx, domain.Event{
			Type:    domain.EventNoiseGuard,
			Level:   "warn",
			Message: fmt.Sprintf("Noise Guard throttled %d noisy tag(s) on radar %d", len(newlyThrottled), radarID),
			Data: map[string]any{
				"radar_id":        radarID,
				"trash_ratio_pct": summary.TrashRatioPct,
				"threshold_pct":   threshold,
				"throttled_tags":  newlyThrottled,
				"noisy_tags":      noisy,
			},
			Timestamp: now,
		})
	}
	return alert, nil
}

func noiseRecommendation(newCount int, cooldown time.Duration) string {
	if newCount > 0 {
		return fmt.Sprintf("Paused %d noisy tag(s) for %s. Review trash samples and tighten or replace broad radar phrases.", newCount, cooldown)
	}
	return "Noisy tags are already throttled. Review trash samples before the cooldown expires."
}

// GetSummary returns aggregated funnel analytics for a radar.
func (m *Manager) GetSummary(ctx context.Context, radarID int64, days int) (domain.FunnelSummary, error) {
	if m.store == nil {
		return domain.FunnelSummary{}, fmt.Errorf("%w: nil store", domain.ErrInvalidInput)
	}
	settings, err := m.store.GetSettings(ctx)
	if err != nil {
		return domain.FunnelSummary{}, err
	}
	threshold := settings.NoiseTrashThresholdPct
	if threshold <= 0 {
		threshold = 75
	}
	return m.store.GetFunnelSummary(ctx, radarID, days, threshold)
}

// ExportLeads serializes all leads for a radar (or all radars when radarID == 0).
func (m *Manager) ExportLeads(ctx context.Context, radarID int64, format ExportFormat, w io.Writer) error {
	if m.store == nil {
		return fmt.Errorf("%w: nil store", domain.ErrInvalidInput)
	}
	if w == nil {
		return fmt.Errorf("%w: nil export writer", domain.ErrInvalidInput)
	}
	leads, err := m.loadLeads(ctx, radarID)
	if err != nil {
		return err
	}
	switch strings.ToLower(string(format)) {
	case string(ExportCSV):
		return writeCSV(w, leads)
	case string(ExportJSON):
		encoder := json.NewEncoder(w)
		return encoder.Encode(leads)
	default:
		return fmt.Errorf("%w: unsupported export format %q", domain.ErrInvalidInput, format)
	}
}

func (m *Manager) loadLeads(ctx context.Context, radarID int64) ([]domain.Lead, error) {
	leads := make([]domain.Lead, 0, exportPageSize)
	offset := 0
	for {
		page, total, err := m.store.ListLeads(ctx, storage.LeadFilter{
			RadarID:       radarID,
			IncludeHidden: true,
			Limit:         exportPageSize,
			Offset:        offset,
		})
		if err != nil {
			return nil, err
		}
		leads = append(leads, page...)
		offset += len(page)
		if len(page) == 0 || len(page) < exportPageSize || int64(offset) >= total {
			break
		}
	}
	return leads, nil
}

func writeCSV(w io.Writer, leads []domain.Lead) error {
	writer := csv.NewWriter(w)
	writer.UseCRLF = true
	header := []string{
		"id", "radar_id", "chat_id", "tg_chat_id", "chat_title", "chat_username", "msg_id", "msg_link",
		"author_id", "author_username", "author_name", "text", "lang", "score", "category", "status",
		"reason", "matched_tags", "tier0_score", "llm_model", "icebreakers", "viewed_at",
		"profile_opened_at", "icebreaker_count", "saved_time_seconds", "created_at", "updated_at",
	}
	if err := writer.Write(header); err != nil {
		return fmt.Errorf("insights: write CSV header: %w", err)
	}
	for _, lead := range leads {
		row := []string{
			strconv.FormatInt(lead.ID, 10),
			strconv.FormatInt(lead.RadarID, 10),
			strconv.FormatInt(lead.ChatID, 10),
			strconv.FormatInt(lead.TGChatID, 10),
			neutralizeCSV(lead.ChatTitle),
			neutralizeCSV(lead.ChatUsername),
			strconv.Itoa(lead.MsgID),
			neutralizeCSV(lead.MsgLink),
			strconv.FormatInt(lead.AuthorID, 10),
			neutralizeCSV(lead.AuthorUsername),
			neutralizeCSV(lead.AuthorName),
			neutralizeCSV(lead.Text),
			neutralizeCSV(lead.Lang),
			strconv.Itoa(lead.Score),
			neutralizeCSV(string(lead.Category)),
			neutralizeCSV(string(lead.Status)),
			neutralizeCSV(lead.Reason),
			neutralizeCSV(strings.Join(lead.MatchedTags, "; ")),
			strconv.FormatFloat(lead.Tier0Score, 'f', 2, 64),
			neutralizeCSV(lead.LLMModel),
			neutralizeCSV(strings.Join(lead.Icebreakers, " | ")),
			formatOptionalTime(lead.ViewedAt),
			formatOptionalTime(lead.ProfileOpenedAt),
			strconv.Itoa(lead.IcebreakerCount),
			strconv.Itoa(lead.SavedTimeSeconds),
			lead.CreatedAt.UTC().Format(time.RFC3339),
			lead.UpdatedAt.UTC().Format(time.RFC3339),
		}
		if err := writer.Write(row); err != nil {
			return fmt.Errorf("insights: write CSV row: %w", err)
		}
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return fmt.Errorf("insights: flush CSV: %w", err)
	}
	return nil
}

func formatOptionalTime(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func neutralizeCSV(value string) string {
	if value == "" {
		return value
	}
	switch value[0] {
	case '=', '+', '-', '@', '\t', '\r':
		return "'" + value
	default:
		return value
	}
}

// DispatchLeadWebhook posts a lead payload with optional HMAC-SHA256 signing.
func (m *Manager) DispatchLeadWebhook(ctx context.Context, webhookURL, secret string, radarName string, lead domain.Lead) error {
	parsed, err := url.Parse(strings.TrimSpace(webhookURL))
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return fmt.Errorf("%w: webhook URL must be http or https", domain.ErrInvalidInput)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if m.webhookTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, m.webhookTimeout)
		defer cancel()
	}
	payload := WebhookPayload{
		Event:     string(domain.EventLeadNew),
		SentAt:    m.nowUTC(),
		RadarName: radarName,
		Lead:      lead,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("insights: encode webhook payload: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, parsed.String(), bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("insights: build webhook request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "mia-lead-radar/webhook")
	secret = strings.TrimSpace(secret)
	if secret != "" {
		signature := signWebhook(body, secret)
		req.Header.Set("X-MIA-Signature", "sha256="+signature)
	}
	resp, err := m.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("insights: dispatch webhook: %w", err)
	}
	defer resp.Body.Close()
	responseBody, _ := io.ReadAll(io.LimitReader(resp.Body, maxWebhookResponseBytes))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		excerpt := strings.TrimSpace(string(responseBody))
		if len(excerpt) > 300 {
			excerpt = excerpt[:300] + "..."
		}
		return fmt.Errorf("insights: webhook returned HTTP %d: %s", resp.StatusCode, excerpt)
	}
	return nil
}

func signWebhook(body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

var commonStopWords = map[string]struct{}{
	// English
	"that": {}, "this": {}, "with": {}, "from": {}, "have": {}, "will": {}, "would": {},
	"could": {}, "should": {}, "been": {}, "being": {}, "they": {}, "them": {}, "their": {},
	"there": {}, "here": {}, "what": {}, "when": {}, "where": {}, "which": {}, "who": {},
	"whom": {}, "whose": {}, "why": {}, "about": {}, "into": {}, "over": {}, "under": {},
	"after": {}, "before": {}, "than": {}, "then": {}, "also": {}, "just": {}, "very": {},
	"much": {}, "many": {}, "more": {}, "most": {}, "some": {}, "any": {}, "all": {}, "not": {},
	"you": {}, "your": {}, "our": {}, "its": {}, "are": {}, "was": {}, "were": {}, "does": {},
	"did": {}, "can": {}, "may": {}, "might": {}, "must": {}, "please": {}, "thanks": {},
	"thank": {}, "hello": {}, "looking": {}, "need": {}, "want": {},
	// Russian / Ukrainian
	"что": {}, "это": {}, "как": {}, "для": {}, "мне": {}, "мой": {}, "мы": {}, "вы": {},
	"они": {}, "все": {}, "есть": {}, "был": {}, "была": {}, "быть": {}, "можно": {},
	"очень": {}, "просто": {}, "тоже": {}, "уже": {}, "если": {}, "когда": {}, "где": {},
	"кто": {}, "почему": {}, "чтобы": {}, "мені": {}, "дуже": {}, "коли": {}, "щоб": {},
	// Spanish / Portuguese / Italian
	"para": {}, "que": {}, "con": {}, "alguien": {}, "hola": {}, "gracias": {}, "por": {},
	"como": {}, "pero": {}, "este": {}, "esta": {}, "muy": {}, "preciso": {}, "procuro": {},
	"obrigado": {}, "você": {}, "voce": {}, "meu": {}, "cerco": {}, "bisogno": {}, "qualcuno": {},
	"ciao": {}, "grazie": {}, "questo": {}, "della": {},
	// German / French / Polish / Turkish
	"ich": {}, "suche": {}, "brauche": {}, "jemand": {}, "bitte": {}, "danke": {},
	"cherche": {}, "besoin": {}, "pour": {}, "avec": {}, "merci": {}, "bonjour": {},
	"szukam": {}, "potrzebuje": {}, "potrzebuję": {}, "proszę": {}, "prosze": {},
	"arıyorum": {}, "ariyorum": {}, "lazım": {}, "lazim": {}, "için": {}, "icin": {},
	"merhaba": {},
	// Arabic common words
	"هذا": {}, "هذه": {}, "التي": {}, "الذي": {}, "على": {}, "عن": {}, "هناك": {},
}
