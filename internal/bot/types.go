// Package bot implements the optional self-hosted Owner Control Bot (lead alerts
// with deep links, /status, /pause, /resume, /stats) without any SaaS billing.
package bot

import (
	"context"

	"github.com/demonidze1337/mia-lead-radar/internal/domain"
	"github.com/demonidze1337/mia-lead-radar/internal/telegram"
)

// Controller exposes runtime pipeline pause/resume/status controls to the Owner Bot.
type Controller interface {
	Pause()
	Resume()
	Stats() telegram.PipelineStats
}

// Service defines owner notification and command polling lifecycle.
type Service interface {
	Start(ctx context.Context) error
	NotifyLead(ctx context.Context, lead domain.Lead, radarName string) error
	NotifyAccountAlert(ctx context.Context, acc domain.TelegramAccount, message string) error
}
