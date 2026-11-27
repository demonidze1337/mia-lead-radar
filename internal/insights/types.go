// Package insights provides Smart Noise Guard throttling, funnel & saved-time
// telemetry, false-lead stop-word learning, and CSV/JSON/Webhook lead exports.
package insights

import (
	"context"
	"io"
	"time"

	"github.com/demonidze1337/mia-lead-radar/internal/domain"
)

// ExportFormat identifies supported lead export serializations.
type ExportFormat string

const (
	ExportCSV  ExportFormat = "csv"
	ExportJSON ExportFormat = "json"
)

// NoiseAlert describes a Smart Noise Guard trigger with offending tags.
type NoiseAlert struct {
	RadarID           int64            `json:"radar_id"`
	TrashRatioPct     float64          `json:"trash_ratio_pct"`
	ThresholdPct      float64          `json:"threshold_pct"`
	ThrottledTags     []string         `json:"throttled_tags"`
	NoisyTags         []domain.TagStat `json:"noisy_tags"`
	RecommendedAction string           `json:"recommended_action"`
	TriggeredAt       time.Time        `json:"triggered_at"`
}

// FunnelAction enumerates trackable user funnel interactions on a lead.
type FunnelAction string

const (
	FunnelViewed        FunnelAction = "viewed"
	FunnelProfileOpened FunnelAction = "profile_opened"
	FunnelIcebreaker    FunnelAction = "icebreaker"
)

// WebhookPayload is the JSON envelope delivered to user-configured webhooks.
type WebhookPayload struct {
	Event     string      `json:"event"`
	SentAt    time.Time   `json:"sent_at"`
	RadarName string      `json:"radar_name,omitempty"`
	Lead      domain.Lead `json:"lead"`
}

// Service defines analytics, Smart Noise Guard, false-lead learning, and export operations.
type Service interface {
	RecordTier0Outcome(ctx context.Context, radarID int64, passed bool, matchedTags []string) error
	RecordLLMOutcome(ctx context.Context, radarID int64, category domain.LeadCategory, matchedTags []string) error
	RecordFunnelAction(ctx context.Context, radarID, leadID int64, action FunnelAction) error
	MarkFalsePositive(ctx context.Context, radarID, leadID int64, learnStopWords bool) (domain.RadarProfile, error)
	EvaluateNoiseGuard(ctx context.Context, radarID int64) (*NoiseAlert, error)
	GetSummary(ctx context.Context, radarID int64, days int) (domain.FunnelSummary, error)
	ExportLeads(ctx context.Context, radarID int64, format ExportFormat, w io.Writer) error
	DispatchLeadWebhook(ctx context.Context, webhookURL, secret string, radarName string, lead domain.Lead) error
}
