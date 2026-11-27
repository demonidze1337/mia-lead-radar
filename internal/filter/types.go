// Package filter implements the sub-millisecond, zero-network multilingual Tier-0
// prefilter, BM25 + buyer-intent scoring, and exact/fuzzy deduplication.
package filter

import (
	"time"

	"github.com/demonidze1337/mia-lead-radar/internal/domain"
)

// RejectReason identifies why Tier-0 rejected or suppressed a candidate message.
type RejectReason string

const (
	RejectNone            RejectReason = ""
	RejectEmptyOrShort    RejectReason = "empty_or_short"
	RejectServiceBot      RejectReason = "service_or_bot"
	RejectDuplicate       RejectReason = "duplicate"
	RejectMarketplaceSpam RejectReason = "marketplace_spam"
	RejectHRVacancy       RejectReason = "hr_or_resume"
	RejectSellerPromo     RejectReason = "seller_self_promo"
	RejectStopPhrase      RejectReason = "stop_phrase"
	RejectTagThrottled    RejectReason = "tag_throttled"
	RejectNoTagMatch      RejectReason = "no_tag_match"
	RejectBroadTagNoProof RejectReason = "broad_tag_without_intent"
	RejectLowScore        RejectReason = "low_tier0_score"
)

// CandidateInput is the message payload evaluated by Tier-0 (live pipeline or UI simulator).
type CandidateInput struct {
	ChatID     int64     `json:"chat_id"`
	MsgID      int       `json:"msg_id"`
	AuthorID   int64     `json:"author_id"`
	AuthorName string    `json:"author_name,omitempty"`
	IsBot      bool      `json:"is_bot,omitempty"`
	IsService  bool      `json:"is_service,omitempty"`
	Text       string    `json:"text"`
	Timestamp  time.Time `json:"timestamp,omitempty"`
	DryRun     bool      `json:"dry_run,omitempty"` // DryRun skips mutating dedup caches for UI Simulator tests.
}

// SignalBreakdown gives transparent numeric attribution for UI Simulator & Smart Noise Guard.
type SignalBreakdown struct {
	BM25Score         float64  `json:"bm25_score"`
	IntentScore       float64  `json:"intent_score"`
	DIYFatigueScore   float64  `json:"diy_fatigue_score"`
	SellerPenalty     float64  `json:"seller_penalty"`
	HRPenalty         float64  `json:"hr_penalty"`
	SpamPenalty       float64  `json:"spam_penalty"`
	StopWordPenalty   float64  `json:"stop_word_penalty"`
	MatchedIntentHits []string `json:"matched_intent_hits,omitempty"`
	MatchedSellerHits []string `json:"matched_seller_hits,omitempty"`
	MatchedHRHits     []string `json:"matched_hr_hits,omitempty"`
}

// EvalResult holds the decision, scoring diagnostics, and sub-millisecond timing of Tier-0.
type EvalResult struct {
	Passed        bool            `json:"passed"`
	Score         float64         `json:"score"` // Normalized 0..100 Tier-0 confidence score
	RejectReason  RejectReason    `json:"reject_reason,omitempty"`
	DetectedLang  string          `json:"detected_lang"`
	LangConf      float64         `json:"lang_confidence"`
	MatchedTags   []string        `json:"matched_tags,omitempty"`
	ThrottledTags []string        `json:"throttled_tags,omitempty"`
	Breakdown     SignalBreakdown `json:"breakdown"`
	Fingerprint   string          `json:"fingerprint"`
	LatencyMicros int64           `json:"latency_micros"`
}

// Engine evaluates incoming messages against a radar profile and optional tag throttles.
type Engine interface {
	Evaluate(input CandidateInput, radar domain.RadarProfile, throttledTags map[string]time.Time) EvalResult
	DetectLanguage(text string) (lang string, confidence float64)
}
