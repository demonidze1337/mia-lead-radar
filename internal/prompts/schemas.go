// Package prompts holds Boss-owned system prompts, prompt-injection guards,
// and JSON Schemas shared with llm.Provider structured outputs.
package prompts

import (
	"github.com/demonidze1337/mia-lead-radar/internal/domain"
	"github.com/demonidze1337/mia-lead-radar/internal/llm"
)

// QualifyResult is the structured output returned by the LLM lead qualifier.
type QualifyResult struct {
	Score           int      `json:"score"`
	IsLead          bool     `json:"is_lead"`
	Category        string   `json:"category"`
	Lang            string   `json:"lang"`
	Reason          string   `json:"reason"`
	MatchedConcepts []string `json:"matched_concepts"`
}

// SuggestedTag represents a generated root (70%) or conversational live (30%) tag.
type SuggestedTag struct {
	Phrase string  `json:"phrase"`
	Kind   string  `json:"kind"` // "root" | "live"
	Weight float64 `json:"weight"`
}

// PhraseSuggestResult is the structured output of AI radar phrase generation.
type PhraseSuggestResult struct {
	Tags         []SuggestedTag `json:"tags"`
	StopPhrases  []string       `json:"stop_phrases"`
	QualityScore int            `json:"quality_score"`
	QualityNotes string         `json:"quality_notes"`
}

// IcebreakerResult holds tailored first-touch outreach drafts in the lead's language.
type IcebreakerResult struct {
	Lang   string   `json:"lang"`
	Drafts []string `json:"drafts"`
}

// RadarPatch proposes concrete radar updates parsed from AI Audit (APPLY_RADAR_SETTINGS).
type RadarPatch struct {
	Apply       bool              `json:"apply"`
	Description string            `json:"description,omitempty"`
	Tags        []domain.RadarTag `json:"tags,omitempty"`
	StopPhrases []string          `json:"stop_phrases,omitempty"`
	Summary     string            `json:"summary,omitempty"`
}

// AuditResult is returned by AI Radar Audit / Consultant.
type AuditResult struct {
	Reply              string      `json:"reply"`
	Diagnosis          string      `json:"diagnosis"`
	Recommendations    []string    `json:"recommendations"`
	ApplyRadarSettings *RadarPatch `json:"apply_radar_settings,omitempty"`
}

// RoleplayReply simulates a Telegram buyer responding to the user's pitch.
type RoleplayReply struct {
	Message    string `json:"message"`
	Mood       string `json:"mood"` // "interested" | "skeptical" | "busy" | "ready_to_call"
	HiddenNote string `json:"hidden_note"`
}

// RoleplayReview grades the user's outreach conversation and gives actionable feedback.
type RoleplayReview struct {
	Score        int      `json:"score"`
	Strengths    []string `json:"strengths"`
	Weaknesses   []string `json:"weaknesses"`
	ImprovedText string   `json:"improved_text"`
}

// VisionCaptchaResult holds the extracted answer for an image/button captcha.
type VisionCaptchaResult struct {
	Answer     string  `json:"answer"`
	Confidence float64 `json:"confidence"`
	Reasoning  string  `json:"reasoning"`
}

// QualifySchema returns the JSON Schema for QualifyResult.
func QualifySchema() *llm.ResponseSchema {
	return &llm.ResponseSchema{
		Name:   "lead_qualification",
		Strict: true,
		Schema: map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"required":             []string{"score", "is_lead", "category", "lang", "reason", "matched_concepts"},
			"properties": map[string]any{
				"score":            map[string]any{"type": "integer", "minimum": 0, "maximum": 100},
				"is_lead":          map[string]any{"type": "boolean"},
				"category":         map[string]any{"type": "string", "enum": []string{"trash", "doubtful", "potential", "clear"}},
				"lang":             map[string]any{"type": "string"},
				"reason":           map[string]any{"type": "string"},
				"matched_concepts": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			},
		},
	}
}

// PhraseSuggestSchema returns the JSON Schema for PhraseSuggestResult.
func PhraseSuggestSchema() *llm.ResponseSchema {
	return &llm.ResponseSchema{
		Name:   "radar_phrase_suggestions",
		Strict: true,
		Schema: map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"required":             []string{"tags", "stop_phrases", "quality_score", "quality_notes"},
			"properties": map[string]any{
				"tags": map[string]any{
					"type": "array",
					"items": map[string]any{
						"type":                 "object",
						"additionalProperties": false,
						"required":             []string{"phrase", "kind", "weight"},
						"properties": map[string]any{
							"phrase": map[string]any{"type": "string"},
							"kind":   map[string]any{"type": "string", "enum": []string{"root", "live"}},
							"weight": map[string]any{"type": "number"},
						},
					},
				},
				"stop_phrases":  map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
				"quality_score": map[string]any{"type": "integer", "minimum": 0, "maximum": 100},
				"quality_notes": map[string]any{"type": "string"},
			},
		},
	}
}

// IcebreakerSchema returns the JSON Schema for IcebreakerResult.
func IcebreakerSchema() *llm.ResponseSchema {
	return &llm.ResponseSchema{
		Name:   "lead_icebreakers",
		Strict: true,
		Schema: map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"required":             []string{"lang", "drafts"},
			"properties": map[string]any{
				"lang":   map[string]any{"type": "string"},
				"drafts": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			},
		},
	}
}

// AuditSchema returns the JSON Schema for AuditResult.
func AuditSchema() *llm.ResponseSchema {
	return &llm.ResponseSchema{
		Name:   "radar_audit",
		Strict: false,
		Schema: map[string]any{
			"type":     "object",
			"required": []string{"reply", "diagnosis", "recommendations"},
			"properties": map[string]any{
				"reply":           map[string]any{"type": "string"},
				"diagnosis":       map[string]any{"type": "string"},
				"recommendations": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
				"apply_radar_settings": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"apply":        map[string]any{"type": "boolean"},
						"description":  map[string]any{"type": "string"},
						"stop_phrases": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
						"summary":      map[string]any{"type": "string"},
						"tags": map[string]any{
							"type": "array",
							"items": map[string]any{
								"type":     "object",
								"required": []string{"phrase", "kind", "weight", "enabled"},
								"properties": map[string]any{
									"phrase":  map[string]any{"type": "string"},
									"kind":    map[string]any{"type": "string"},
									"weight":  map[string]any{"type": "number"},
									"enabled": map[string]any{"type": "boolean"},
								},
							},
						},
					},
				},
			},
		},
	}
}

// RoleplayReplySchema returns the JSON Schema for simulated buyer turns.
func RoleplayReplySchema() *llm.ResponseSchema {
	return &llm.ResponseSchema{
		Name:   "roleplay_reply",
		Strict: true,
		Schema: map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"required":             []string{"message", "mood", "hidden_note"},
			"properties": map[string]any{
				"message":     map[string]any{"type": "string"},
				"mood":        map[string]any{"type": "string", "enum": []string{"interested", "skeptical", "busy", "ready_to_call"}},
				"hidden_note": map[string]any{"type": "string"},
			},
		},
	}
}

// RoleplayReviewSchema returns the JSON Schema for roleplay coaching feedback.
func RoleplayReviewSchema() *llm.ResponseSchema {
	return &llm.ResponseSchema{
		Name:   "roleplay_review",
		Strict: true,
		Schema: map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"required":             []string{"score", "strengths", "weaknesses", "improved_text"},
			"properties": map[string]any{
				"score":         map[string]any{"type": "integer", "minimum": 0, "maximum": 100},
				"strengths":     map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
				"weaknesses":    map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
				"improved_text": map[string]any{"type": "string"},
			},
		},
	}
}

// VisionCaptchaSchema returns the JSON Schema for image captcha resolution.
func VisionCaptchaSchema() *llm.ResponseSchema {
	return &llm.ResponseSchema{
		Name:   "vision_captcha",
		Strict: true,
		Schema: map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"required":             []string{"answer", "confidence", "reasoning"},
			"properties": map[string]any{
				"answer":     map[string]any{"type": "string"},
				"confidence": map[string]any{"type": "number", "minimum": 0, "maximum": 1},
				"reasoning":  map[string]any{"type": "string"},
			},
		},
	}
}
