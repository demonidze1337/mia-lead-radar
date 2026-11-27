// Package llm provides an OpenAI-compatible provider abstraction, hardware presets,
// dual-model routing (Fast vs Smart), and structured JSON output handling without billing.
package llm

import (
	"context"

	"github.com/demonidze1337/mia-lead-radar/internal/domain"
)

// ModelTier selects whether a call uses FastModel (bulk qualification) or SmartModel (audit/icebreaker/roleplay).
type ModelTier string

const (
	TierFast   ModelTier = "fast"
	TierSmart  ModelTier = "smart"
	TierVision ModelTier = "vision"
)

// Message represents a chat completion message (supporting optional base64 image URL for Vision captchas).
type Message struct {
	Role     string `json:"role"`
	Content  string `json:"content"`
	ImageURL string `json:"image_url,omitempty"`
}

// ResponseSchema describes an optional JSON Schema for Structured Outputs with json_object fallback.
type ResponseSchema struct {
	Name   string         `json:"name"`
	Strict bool           `json:"strict"`
	Schema map[string]any `json:"schema"`
}

// ChatRequest configures a completion call.
type ChatRequest struct {
	Tier        ModelTier       `json:"tier"`
	Messages    []Message       `json:"messages"`
	Temperature float64         `json:"temperature"`
	MaxTokens   int             `json:"max_tokens,omitempty"`
	Schema      *ResponseSchema `json:"schema,omitempty"`
	JSONMode    bool            `json:"json_mode"`
}

// ChatResponse returns raw model output and accounting metadata.
type ChatResponse struct {
	Content          string `json:"content"`
	Model            string `json:"model"`
	PromptTokens     int    `json:"prompt_tokens"`
	CompletionTokens int    `json:"completion_tokens"`
	LatencyMs        int64  `json:"latency_ms"`
}

// HardwarePreset defines a curated model profile for local Ollama or BYOK cloud APIs.
type HardwarePreset struct {
	ID          string                 `json:"id"`
	Tier        domain.HardwareTier    `json:"tier"`
	Provider    domain.LLMProviderType `json:"provider"`
	Title       string                 `json:"title"`
	Description string                 `json:"description"`
	MinRAMGB    int                    `json:"min_ram_gb"`
	BaseURL     string                 `json:"base_url"`
	FastModel   string                 `json:"fast_model"`
	SmartModel  string                 `json:"smart_model"`
	VisionModel string                 `json:"vision_model,omitempty"`
	Recommended bool                   `json:"recommended"`
	RequiresKey bool                   `json:"requires_key"`
}

// Provider executes OpenAI-compatible chat completions and connectivity checks.
type Provider interface {
	Complete(ctx context.Context, req ChatRequest) (ChatResponse, error)
	CompleteJSON(ctx context.Context, req ChatRequest, target any) (ChatResponse, error)
	Ping(ctx context.Context) error
	ListModels(ctx context.Context) ([]string, error)
}
