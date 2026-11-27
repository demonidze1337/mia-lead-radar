// Package storage defines the persistence contract and embedded SQL migrations
// for MIA Lead Radar (SQLite WAL by default, PostgreSQL-compatible design).
package storage

import (
	"context"
	"embed"
	"time"

	"github.com/demonidze1337/mia-lead-radar/internal/domain"
)

//go:embed migrations/*.sql
var MigrationsFS embed.FS

// TimeLayout is fixed-width UTC microsecond RFC3339 so SQLite lexical sorting
// and range queries are strictly chronological across all drivers.
const TimeLayout = "2006-01-02T15:04:05.000000Z"

// FormatTime formats a timestamp in canonical UTC fixed-width string form.
func FormatTime(t time.Time) string {
	return t.UTC().Format(TimeLayout)
}

// ParseTime parses a canonical timestamp string (accepting standard RFC3339 fallbacks).
func ParseTime(s string) (time.Time, error) {
	if t, err := time.Parse(TimeLayout, s); err == nil {
		return t.UTC(), nil
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}, err
	}
	return t.UTC(), nil
}

// LeadFilter controls pagination and filtering for lead feed queries.
type LeadFilter struct {
	RadarID       int64
	Statuses      []domain.LeadStatus
	Categories    []domain.LeadCategory
	IncludeHidden bool
	Query         string
	MinScore      int
	MaxScore      int
	Limit         int
	Offset        int
}

// ChatFilter controls chat listing and status selection.
type ChatFilter struct {
	Statuses          []domain.ChatStatus
	AssignedAccountID int64
	Query             string
	Limit             int
	Offset            int
}

// DedupRecord stores an exact/fuzzy fingerprint for cross-restart deduplication.
type DedupRecord struct {
	Fingerprint string
	AuthorID    int64
	TokenSig    string
	CreatedAt   time.Time
	ExpiresAt   time.Time
}

// Store is the persistence contract implemented by sqlite (and optionally postgres).
// All timestamps stored and compared use UTC.
type Store interface {
	Close() error
	Migrate(ctx context.Context) error
	WithTx(ctx context.Context, fn func(tx Store) error) error

	// Runtime Settings
	GetSettings(ctx context.Context) (domain.RuntimeSettings, error)
	SaveSettings(ctx context.Context, s domain.RuntimeSettings) error

	// Browser Auth Sessions
	CreateAuthSession(ctx context.Context, sess domain.AuthSession) (int64, error)
	GetAuthSessionByHash(ctx context.Context, tokenHash string) (domain.AuthSession, error)
	TouchAuthSession(ctx context.Context, id int64, seenAt time.Time) error
	DeleteExpiredAuthSessions(ctx context.Context, now time.Time) (int64, error)

	// Radar Profiles & History
	CreateRadar(ctx context.Context, r domain.RadarProfile, source domain.RadarChangeSource, summary string) (domain.RadarProfile, error)
	UpdateRadar(ctx context.Context, r domain.RadarProfile, source domain.RadarChangeSource, summary string) (domain.RadarProfile, error)
	GetActiveRadar(ctx context.Context) (domain.RadarProfile, error)
	GetRadarByID(ctx context.Context, id int64) (domain.RadarProfile, error)
	ListRadars(ctx context.Context) ([]domain.RadarProfile, error)
	ListRadarVersions(ctx context.Context, radarID int64, limit int) ([]domain.RadarVersion, error)
	AddLearnedStopWords(ctx context.Context, radarID int64, words []string) (domain.RadarProfile, error)

	// Telegram Accounts & Persistent FloodGate
	UpsertAccount(ctx context.Context, acc domain.TelegramAccount) (domain.TelegramAccount, error)
	GetAccount(ctx context.Context, id int64) (domain.TelegramAccount, error)
	ListAccounts(ctx context.Context) ([]domain.TelegramAccount, error)
	SetAccountStatus(ctx context.Context, id int64, status domain.AccountStatus, lastError string) error
	SetAccountFloodUntil(ctx context.Context, id int64, floodUntil time.Time, reason string) error
	ClearExpiredFloods(ctx context.Context, now time.Time) (int64, error)
	IncrementAccountJoins(ctx context.Context, id int64, now time.Time) error
	DeleteAccount(ctx context.Context, id int64) error

	// Chats & Persistent Join Queue
	UpsertChat(ctx context.Context, c domain.Chat) (domain.Chat, error)
	GetChatByID(ctx context.Context, id int64) (domain.Chat, error)
	GetChatByTGID(ctx context.Context, tgChatID int64) (domain.Chat, error)
	ListChats(ctx context.Context, f ChatFilter) ([]domain.Chat, error)
	UpdateChatProgress(ctx context.Context, id int64, lastMsgID int, readAt *time.Time, scannedAt *time.Time) error
	EnqueueJoinJob(ctx context.Context, item domain.JoinQueueItem) (domain.JoinQueueItem, bool, error)
	ClaimNextJoinJob(ctx context.Context, now time.Time) (*domain.JoinQueueItem, error)
	CompleteJoinJob(ctx context.Context, id int64, status domain.JoinJobStatus, nextAttempt *time.Time, lastErr string) error
	ListJoinQueue(ctx context.Context, status domain.JoinJobStatus, limit int) ([]domain.JoinQueueItem, error)

	// Leads & Funnel Telemetry
	SaveLead(ctx context.Context, lead domain.Lead) (domain.Lead, bool, error)
	GetLeadByID(ctx context.Context, id int64) (domain.Lead, error)
	ListLeads(ctx context.Context, f LeadFilter) ([]domain.Lead, int64, error)
	UpdateLeadStatus(ctx context.Context, id int64, status domain.LeadStatus) error
	MarkLeadViewed(ctx context.Context, id int64, at time.Time) error
	MarkLeadProfileOpened(ctx context.Context, id int64, at time.Time) error
	AppendLeadIcebreaker(ctx context.Context, id int64, text string, savedSecondsDelta int) error

	// Trash Samples & Smart Noise Guard
	SaveTrashSample(ctx context.Context, sample domain.TrashSample, keepLatest int) error
	ListTrashSamples(ctx context.Context, radarID int64, limit int) ([]domain.TrashSample, error)
	RecordTagHit(ctx context.Context, radarID int64, tag string, isLead bool, isFalsePositive bool, now time.Time) error
	SetTagThrottle(ctx context.Context, radarID int64, tag string, until *time.Time) error
	ListTagStats(ctx context.Context, radarID int64) ([]domain.TagStat, error)

	// Daily Metrics & Funnel Analytics
	IncrementDailyMetric(ctx context.Context, date string, radarID int64, delta domain.DailyMetric) error
	GetFunnelSummary(ctx context.Context, radarID int64, days int, trashThresholdPct float64) (domain.FunnelSummary, error)
	ListDailyMetrics(ctx context.Context, radarID int64, days int) ([]domain.DailyMetric, error)

	// Persistent Dedup Fingerprints
	HasDedupFingerprint(ctx context.Context, fingerprint string, now time.Time) (bool, error)
	ListRecentAuthorFingerprints(ctx context.Context, authorID int64, now time.Time, limit int) ([]DedupRecord, error)
	SaveDedupFingerprint(ctx context.Context, rec DedupRecord) error
	PruneExpiredDedup(ctx context.Context, now time.Time) (int64, error)

	// Consultant / Audit / Roleplay History
	AppendConsultantMessage(ctx context.Context, msg domain.ConsultantMessage) (domain.ConsultantMessage, error)
	ListConsultantMessages(ctx context.Context, sessionKey string, limit int) ([]domain.ConsultantMessage, error)
}
