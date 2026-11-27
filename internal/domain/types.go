// Package domain defines core business types, enums, contracts, and errors
// shared across all MIA Lead Radar packages without circular dependencies.
package domain

import (
	"errors"
	"time"
)

// Canonical domain errors used across storage, filter, llm, telegram, and server layers.
var (
	ErrNotFound     = errors.New("not found")
	ErrConflict     = errors.New("conflict")
	ErrUnauthorized = errors.New("unauthorized")
	ErrInvalidInput = errors.New("invalid input")
	ErrFloodWait    = errors.New("telegram flood wait")
)

// HardwareTier classifies the host machine to recommend optimal local or BYOK LLM presets.
type HardwareTier string

const (
	HardwareTierWeak        HardwareTier = "weak"
	HardwareTierBalanced    HardwareTier = "balanced"
	HardwareTierWorkstation HardwareTier = "workstation"
	HardwareTierBYOK        HardwareTier = "byok"
)

// HardwareProfile describes host hardware capabilities detected at startup.
type HardwareProfile struct {
	RAMBytes        uint64       `json:"ram_bytes"`
	RAMGB           float64      `json:"ram_gb"`
	CPUCores        int          `json:"cpu_cores"`
	OS              string       `json:"os"`
	Arch            string       `json:"arch"`
	RecommendedTier HardwareTier `json:"recommended_tier"`
}

// LLMProviderType identifies the OpenAI-compatible backend provider.
type LLMProviderType string

const (
	LLMProviderOllama     LLMProviderType = "ollama"
	LLMProviderOpenRouter LLMProviderType = "openrouter"
	LLMProviderOpenAI     LLMProviderType = "openai"
	LLMProviderGemini     LLMProviderType = "gemini"
	LLMProviderCustom     LLMProviderType = "custom"
)

// LLMConfig stores model routing and provider credentials (kept local, never billed).
type LLMConfig struct {
	Provider          LLMProviderType `json:"provider"`
	BaseURL           string          `json:"base_url"`
	APIKey            string          `json:"api_key,omitempty"`
	PresetID          string          `json:"preset_id"`
	FastModel         string          `json:"fast_model"`
	SmartModel        string          `json:"smart_model"`
	VisionModel       string          `json:"vision_model,omitempty"`
	RequestTimeoutSec int             `json:"request_timeout_sec"`
	MaxRetries        int             `json:"max_retries"`
}

// RuntimeSettings holds mutable self-hosted runtime configuration stored in DB.
type RuntimeSettings struct {
	UILanguage             string    `json:"ui_language"`
	TargetLanguages        []string  `json:"target_languages"`
	LLM                    LLMConfig `json:"llm"`
	OwnerBotToken          string    `json:"owner_bot_token,omitempty"`
	OwnerTelegramID        int64     `json:"owner_telegram_id"`
	AutoReadEnabled        bool      `json:"auto_read_enabled"`
	AutoJoinEnabled        bool      `json:"auto_join_enabled"`
	AllowHRVacancies       bool      `json:"allow_hr_vacancies"`
	AllowPartnerships      bool      `json:"allow_partnerships"`
	MinLeadScore           int       `json:"min_lead_score"`
	SoundEnabled           bool      `json:"sound_enabled"`
	HapticEnabled          bool      `json:"haptic_enabled"`
	NoiseGuardEnabled      bool      `json:"noise_guard_enabled"`
	NoiseTrashThresholdPct float64   `json:"noise_trash_threshold_pct"`
	WebhookURL             string    `json:"webhook_url,omitempty"`
	SetupCompleted         bool      `json:"setup_completed"`
}

// DefaultRuntimeSettings returns safe out-of-the-box local defaults.
func DefaultRuntimeSettings() RuntimeSettings {
	return RuntimeSettings{
		UILanguage:      "en",
		TargetLanguages: []string{"en", "ru"},
		LLM: LLMConfig{
			Provider:          LLMProviderOllama,
			BaseURL:           "http://localhost:11434/v1",
			PresetID:          "balanced_qwen35_4b",
			FastModel:         "qwen3.5:4b",
			SmartModel:        "qwen3.5:4b",
			RequestTimeoutSec: 45,
			MaxRetries:        2,
		},
		AutoReadEnabled:        true,
		AutoJoinEnabled:        true,
		AllowHRVacancies:       false,
		AllowPartnerships:      false,
		MinLeadScore:           31,
		SoundEnabled:           true,
		HapticEnabled:          true,
		NoiseGuardEnabled:      true,
		NoiseTrashThresholdPct: 75.0,
		SetupCompleted:         false,
	}
}

// RadarTagKind differentiates core niche roots (70%) from conversational phrases (30%).
type RadarTagKind string

const (
	RadarTagRoot RadarTagKind = "root"
	RadarTagLive RadarTagKind = "live"
)

// RadarTag represents a weighted search/filter phrase in a radar profile.
type RadarTag struct {
	Phrase  string       `json:"phrase"`
	Kind    RadarTagKind `json:"kind"`
	Weight  float64      `json:"weight"`
	Enabled bool         `json:"enabled"`
}

// RadarProfile defines the owner's offer, target languages, matching tags, and negative filters.
type RadarProfile struct {
	ID                int64      `json:"id"`
	Name              string     `json:"name"`
	Description       string     `json:"description"`
	Tags              []RadarTag `json:"tags"`
	TargetLanguages   []string   `json:"target_languages"`
	StopPhrases       []string   `json:"stop_phrases"`
	LearnedStopWords  []string   `json:"learned_stop_words"`
	AllowHRVacancies  bool       `json:"allow_hr_vacancies"`
	AllowPartnerships bool       `json:"allow_partnerships"`
	QualityScore      int        `json:"quality_score"`
	QualityNotes      string     `json:"quality_notes"`
	Version           int        `json:"version"`
	IsActive          bool       `json:"is_active"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

// RadarChangeSource records what triggered a radar revision.
type RadarChangeSource string

const (
	RadarChangeManual    RadarChangeSource = "manual"
	RadarChangeAIAudit   RadarChangeSource = "ai_audit"
	RadarChangeWizard    RadarChangeSource = "wizard"
	RadarChangeAutoLearn RadarChangeSource = "auto_learn"
)

// RadarVersion captures historical snapshots of a radar for AI Audit comparison.
type RadarVersion struct {
	ID               int64             `json:"id"`
	RadarID          int64             `json:"radar_id"`
	Version          int               `json:"version"`
	Description      string            `json:"description"`
	Tags             []RadarTag        `json:"tags"`
	StopPhrases      []string          `json:"stop_phrases"`
	LearnedStopWords []string          `json:"learned_stop_words"`
	ChangeSource     RadarChangeSource `json:"change_source"`
	AuditSummary     string            `json:"audit_summary"`
	CreatedAt        time.Time         `json:"created_at"`
}

// AccountStatus tracks operational state of a connected MTProto account.
type AccountStatus string

const (
	AccountStatusPendingAuth  AccountStatus = "pending_auth"
	AccountStatusActive       AccountStatus = "active"
	AccountStatusFloodWait    AccountStatus = "flood_wait"
	AccountStatusBanned       AccountStatus = "banned"
	AccountStatusChannelsFull AccountStatus = "channels_full"
	AccountStatusDisabled     AccountStatus = "disabled"
)

// TelegramAccount represents a user's MTProto session managed by the radar.
type TelegramAccount struct {
	ID                int64         `json:"id"`
	Label             string        `json:"label"`
	PhoneMasked       string        `json:"phone_masked"`
	TGUserID          int64         `json:"tg_user_id"`
	Username          string        `json:"username"`
	FirstName         string        `json:"first_name"`
	SessionPath       string        `json:"-"`
	Status            AccountStatus `json:"status"`
	FloodUntil        *time.Time    `json:"flood_until,omitempty"`
	JoinedChatsCount  int           `json:"joined_chats_count"`
	DailyJoinsCount   int           `json:"daily_joins_count"`
	DailyJoinsResetAt *time.Time    `json:"daily_joins_reset_at,omitempty"`
	LastActiveAt      *time.Time    `json:"last_active_at,omitempty"`
	LastError         string        `json:"last_error,omitempty"`
	CreatedAt         time.Time     `json:"created_at"`
	UpdatedAt         time.Time     `json:"updated_at"`
}

// ChatType distinguishes Telegram peer kinds.
type ChatType string

const (
	ChatTypeGroup      ChatType = "group"
	ChatTypeSupergroup ChatType = "supergroup"
	ChatTypeChannel    ChatType = "channel"
	ChatTypeComments   ChatType = "comments"
)

// ChatSource records how a chat was discovered.
type ChatSource string

const (
	ChatSourceSeed          ChatSource = "seed"
	ChatSourceSearchGlobal  ChatSource = "search_global"
	ChatSourceExtractedLink ChatSource = "extracted_link"
	ChatSourceAddList       ChatSource = "addlist"
	ChatSourceUserImport    ChatSource = "user_import"
)

// ChatStatus tracks membership and validation lifecycle of a chat.
type ChatStatus string

const (
	ChatStatusQueued           ChatStatus = "queued"
	ChatStatusValidating       ChatStatus = "validating"
	ChatStatusJoined           ChatStatus = "joined"
	ChatStatusRequiresApproval ChatStatus = "requires_approval"
	ChatStatusBad              ChatStatus = "bad"
	ChatStatusBanned           ChatStatus = "banned"
	ChatStatusSkipped          ChatStatus = "skipped"
)

// Chat represents a monitored or discovered Telegram chat/channel.
type Chat struct {
	ID                 int64      `json:"id"`
	TGChatID           int64      `json:"tg_chat_id"`
	AccessHash         int64      `json:"access_hash,omitempty"`
	Username           string     `json:"username,omitempty"`
	InviteHash         string     `json:"invite_hash,omitempty"`
	Title              string     `json:"title"`
	ChatType           ChatType   `json:"chat_type"`
	Source             ChatSource `json:"source"`
	Status             ChatStatus `json:"status"`
	MemberCount        int        `json:"member_count"`
	MsgVelocityPerHour float64    `json:"msg_velocity_per_hour"`
	HasComments        bool       `json:"has_comments"`
	LinkedChatID       int64      `json:"linked_chat_id,omitempty"`
	AssignedAccountID  int64      `json:"assigned_account_id,omitempty"`
	LastMessageID      int        `json:"last_message_id"`
	LastReadAt         *time.Time `json:"last_read_at,omitempty"`
	LastScannedAt      *time.Time `json:"last_scanned_at,omitempty"`
	NextRetryAt        *time.Time `json:"next_retry_at,omitempty"`
	ErrorReason        string     `json:"error_reason,omitempty"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
}

// JoinLinkType classifies normalized Telegram links in the join queue.
type JoinLinkType string

const (
	JoinLinkUsername JoinLinkType = "username"
	JoinLinkInvite   JoinLinkType = "invite"
	JoinLinkAddList  JoinLinkType = "addlist"
	JoinLinkMessage  JoinLinkType = "message"
)

// JoinJobStatus represents execution state of a persistent join queue item.
type JoinJobStatus string

const (
	JoinJobPending    JoinJobStatus = "pending"
	JoinJobInProgress JoinJobStatus = "in_progress"
	JoinJobDone       JoinJobStatus = "done"
	JoinJobFailed     JoinJobStatus = "failed"
	JoinJobDead       JoinJobStatus = "dead"
)

// JoinQueueItem represents a persistent task to validate/join a Telegram chat or folder.
type JoinQueueItem struct {
	ID             int64         `json:"id"`
	NormalizedLink string        `json:"normalized_link"`
	LinkType       JoinLinkType  `json:"link_type"`
	RawLink        string        `json:"raw_link"`
	SourceChatID   int64         `json:"source_chat_id,omitempty"`
	Priority       int           `json:"priority"`
	Status         JoinJobStatus `json:"status"`
	Attempts       int           `json:"attempts"`
	NextAttemptAt  time.Time     `json:"next_attempt_at"`
	LastError      string        `json:"last_error,omitempty"`
	CreatedAt      time.Time     `json:"created_at"`
	UpdatedAt      time.Time     `json:"updated_at"`
}

// LeadCategory maps numeric qualification score [0..100] to domain tiers.
type LeadCategory string

const (
	LeadCategoryTrash     LeadCategory = "trash"     // 0..30 (is_lead = false)
	LeadCategoryDoubtful  LeadCategory = "doubtful"  // 31..60
	LeadCategoryPotential LeadCategory = "potential" // 61..84
	LeadCategoryClear     LeadCategory = "clear"     // 85..100
)

// CategoryFromScore converts a 0..100 score into the canonical LeadCategory.
func CategoryFromScore(score int) LeadCategory {
	switch {
	case score >= 85:
		return LeadCategoryClear
	case score >= 61:
		return LeadCategoryPotential
	case score >= 31:
		return LeadCategoryDoubtful
	default:
		return LeadCategoryTrash
	}
}

// IsLead returns true if the category represents an actionable lead (score >= 31).
func (c LeadCategory) IsLead() bool {
	return c == LeadCategoryDoubtful || c == LeadCategoryPotential || c == LeadCategoryClear
}

// LeadStatus tracks user workflow & funnel state for a qualified lead.
type LeadStatus string

const (
	LeadStatusNew           LeadStatus = "new"
	LeadStatusInProgress    LeadStatus = "in_progress"
	LeadStatusReplied       LeadStatus = "replied"
	LeadStatusClosed        LeadStatus = "closed"
	LeadStatusFalsePositive LeadStatus = "false_positive"
	LeadStatusHidden        LeadStatus = "hidden"
)

// Lead stores a qualified buyer-intent message with deterministic navigation links and funnel telemetry.
type Lead struct {
	ID               int64        `json:"id"`
	RadarID          int64        `json:"radar_id"`
	ChatID           int64        `json:"chat_id"`
	TGChatID         int64        `json:"tg_chat_id"`
	ChatTitle        string       `json:"chat_title"`
	ChatUsername     string       `json:"chat_username,omitempty"`
	MsgID            int          `json:"msg_id"`
	MsgLink          string       `json:"msg_link"`
	AuthorID         int64        `json:"author_id"`
	AuthorUsername   string       `json:"author_username,omitempty"`
	AuthorName       string       `json:"author_name"`
	Text             string       `json:"text"`
	Lang             string       `json:"lang"`
	Score            int          `json:"score"`
	Category         LeadCategory `json:"category"`
	Status           LeadStatus   `json:"status"`
	Reason           string       `json:"reason"`
	MatchedTags      []string     `json:"matched_tags"`
	Tier0Score       float64      `json:"tier0_score"`
	LLMModel         string       `json:"llm_model"`
	Icebreakers      []string     `json:"icebreakers"`
	ViewedAt         *time.Time   `json:"viewed_at,omitempty"`
	ProfileOpenedAt  *time.Time   `json:"profile_opened_at,omitempty"`
	IcebreakerCount  int          `json:"icebreaker_count"`
	SavedTimeSeconds int          `json:"saved_time_seconds"`
	CreatedAt        time.Time    `json:"created_at"`
	UpdatedAt        time.Time    `json:"updated_at"`
}

// TrashSample retains recent rejected messages for AI Audit and Smart Noise Guard diagnostics.
type TrashSample struct {
	ID          int64     `json:"id"`
	RadarID     int64     `json:"radar_id"`
	Source      string    `json:"source"` // "tier0" | "llm"
	Reason      string    `json:"reason"`
	MatchedTags []string  `json:"matched_tags"`
	Score       int       `json:"score"`
	ChatTitle   string    `json:"chat_title"`
	AuthorID    int64     `json:"author_id"`
	TextExcerpt string    `json:"text_excerpt"`
	Lang        string    `json:"lang"`
	CreatedAt   time.Time `json:"created_at"`
}

// TagStat tracks hit quality per radar tag for Smart Noise Guard auto-throttling.
type TagStat struct {
	RadarID           int64      `json:"radar_id"`
	Tag               string     `json:"tag"`
	TotalHits         int64      `json:"total_hits"`
	TrashHits         int64      `json:"trash_hits"`
	LeadHits          int64      `json:"lead_hits"`
	FalsePositiveHits int64      `json:"false_positive_hits"`
	ThrottledUntil    *time.Time `json:"throttled_until,omitempty"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

// DailyMetric aggregates funnel counts per UTC day and radar.
type DailyMetric struct {
	Date               string `json:"date"` // YYYY-MM-DD (UTC)
	RadarID            int64  `json:"radar_id"`
	MessagesSeen       int64  `json:"messages_seen"`
	Tier0Passed        int64  `json:"tier0_passed"`
	Tier0Rejected      int64  `json:"tier0_rejected"`
	LLMCalled          int64  `json:"llm_called"`
	TrashCount         int64  `json:"trash_count"`
	DoubtfulCount      int64  `json:"doubtful_count"`
	PotentialCount     int64  `json:"potential_count"`
	ClearCount         int64  `json:"clear_count"`
	ViewedCount        int64  `json:"viewed_count"`
	ProfileOpenedCount int64  `json:"profile_opened_count"`
	IcebreakerCount    int64  `json:"icebreaker_count"`
	FalsePositiveCount int64  `json:"false_positive_count"`
	SavedTimeSeconds   int64  `json:"saved_time_seconds"`
}

// FunnelSummary provides aggregated analytics and Smart Noise Guard signals.
type FunnelSummary struct {
	RadarID              int64     `json:"radar_id"`
	Days                 int       `json:"days"`
	MessagesSeen         int64     `json:"messages_seen"`
	Tier0Passed          int64     `json:"tier0_passed"`
	Tier0Rejected        int64     `json:"tier0_rejected"`
	LLMCalled            int64     `json:"llm_called"`
	TrashCount           int64     `json:"trash_count"`
	DoubtfulCount        int64     `json:"doubtful_count"`
	PotentialCount       int64     `json:"potential_count"`
	ClearCount           int64     `json:"clear_count"`
	TotalLeads           int64     `json:"total_leads"`
	ViewedCount          int64     `json:"viewed_count"`
	ProfileOpenedCount   int64     `json:"profile_opened_count"`
	IcebreakerCount      int64     `json:"icebreaker_count"`
	FalsePositiveCount   int64     `json:"false_positive_count"`
	SavedTimeSeconds     int64     `json:"saved_time_seconds"`
	TrashRatioPct        float64   `json:"trash_ratio_pct"`
	ViewConversionPct    float64   `json:"view_conversion_pct"`
	ProfileConversionPct float64   `json:"profile_conversion_pct"`
	NoisyTags            []TagStat `json:"noisy_tags"`
	NoiseGuardTriggered  bool      `json:"noise_guard_triggered"`
}

// AuthSession represents a browser-mode authenticated session token.
type AuthSession struct {
	ID         int64     `json:"id"`
	TokenHash  string    `json:"-"`
	Label      string    `json:"label"`
	TGUserID   int64     `json:"tg_user_id"`
	Role       string    `json:"role"`
	CreatedAt  time.Time `json:"created_at"`
	ExpiresAt  time.Time `json:"expires_at"`
	LastSeenAt time.Time `json:"last_seen_at"`
}

// ConsultantMessage stores persisted messages for AI Consultant / Audit / Roleplay threads.
type ConsultantMessage struct {
	ID           int64     `json:"id"`
	SessionKey   string    `json:"session_key"`
	Role         string    `json:"role"` // "user" | "assistant" | "system"
	Mode         string    `json:"mode"` // "audit" | "chat" | "roleplay"
	LeadID       int64     `json:"lead_id,omitempty"`
	Content      string    `json:"content"`
	MetadataJSON string    `json:"metadata_json,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

// EventType enumerates Live AI Terminal and dashboard real-time events.
type EventType string

const (
	EventTerminalLog   EventType = "terminal.log"
	EventTier0Rejected EventType = "tier0.rejected"
	EventTier0Passed   EventType = "tier0.passed"
	EventLLMQualified  EventType = "llm.qualified"
	EventLeadNew       EventType = "lead.new"
	EventLeadUpdated   EventType = "lead.updated"
	EventMTProtoStatus EventType = "mtproto.status"
	EventMTProtoFlood  EventType = "mtproto.flood"
	EventNoiseGuard    EventType = "noise_guard.alert"
	EventRadarUpdated  EventType = "radar.updated"
)

// Event represents a typed real-time message broadcast over the EventBus and WebSocket hub.
type Event struct {
	ID        int64          `json:"id"`
	Type      EventType      `json:"type"`
	Level     string         `json:"level"` // "info" | "warn" | "error" | "success"
	Message   string         `json:"message"`
	Data      map[string]any `json:"data,omitempty"`
	Timestamp time.Time      `json:"timestamp"`
}
