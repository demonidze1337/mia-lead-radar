package llm

import (
	"fmt"
	"sort"
	"strings"

	"github.com/demonidze1337/mia-lead-radar/internal/domain"
)

const ollamaBaseURL = "http://localhost:11434/v1"

var presetCatalog = []HardwarePreset{
	// Weak hardware (<12 GB RAM).
	{
		ID:          "weak_qwen3_1_7b",
		Tier:        domain.HardwareTierWeak,
		Provider:    domain.LLMProviderOllama,
		Title:       "Qwen3 1.7B - Economy",
		Description: "Fast CPU-friendly model for weak machines and quick bulk screening.",
		MinRAMGB:    6,
		BaseURL:     ollamaBaseURL,
		FastModel:   "qwen3:1.7b",
		SmartModel:  "qwen3:1.7b",
	},
	{
		ID:          "weak_qwen3_0_6b",
		Tier:        domain.HardwareTierWeak,
		Provider:    domain.LLMProviderOllama,
		Title:       "Qwen3 0.6B - Minimal",
		Description: "Smallest local option for very low-memory hosts.",
		MinRAMGB:    4,
		BaseURL:     ollamaBaseURL,
		FastModel:   "qwen3:0.6b",
		SmartModel:  "qwen3:0.6b",
	},
	{
		ID:          "weak_phi4_mini",
		Tier:        domain.HardwareTierWeak,
		Provider:    domain.LLMProviderOllama,
		Title:       "Phi-4 Mini - Efficient",
		Description: "Compact reasoning model for CPU-only or low-RAM setups.",
		MinRAMGB:    6,
		BaseURL:     ollamaBaseURL,
		FastModel:   "phi4-mini",
		SmartModel:  "phi4-mini",
	},

	// Balanced hardware (12-23 GB RAM): the universal default.
	{
		ID:          "balanced_qwen35_4b",
		Tier:        domain.HardwareTierBalanced,
		Provider:    domain.LLMProviderOllama,
		Title:       "Qwen3.5 4B - Recommended",
		Description: "Best balance of speed, multilingual quality, and reliable structured JSON.",
		MinRAMGB:    12,
		BaseURL:     ollamaBaseURL,
		FastModel:   "qwen3.5:4b",
		SmartModel:  "qwen3.5:4b",
		Recommended: true,
	},
	{
		ID:          "balanced_gemma4_e4b",
		Tier:        domain.HardwareTierBalanced,
		Provider:    domain.LLMProviderOllama,
		Title:       "Gemma 4 E4B - Balanced",
		Description: "Efficient multilingual model with a small memory footprint.",
		MinRAMGB:    12,
		BaseURL:     ollamaBaseURL,
		FastModel:   "gemma4:e4b",
		SmartModel:  "gemma4:e4b",
	},
	{
		ID:          "balanced_qwen3_8b",
		Tier:        domain.HardwareTierBalanced,
		Provider:    domain.LLMProviderOllama,
		Title:       "Qwen3 8B - Quality",
		Description: "Higher quality local reasoning for 16 GB+ balanced machines.",
		MinRAMGB:    16,
		BaseURL:     ollamaBaseURL,
		FastModel:   "qwen3:8b",
		SmartModel:  "qwen3:8b",
	},

	// Workstation / GPU hardware (>=24 GB RAM).
	{
		ID:          "workstation_inkling_2_mini",
		Tier:        domain.HardwareTierWorkstation,
		Provider:    domain.LLMProviderOllama,
		Title:       "Inkling 2 Mini - Workstation",
		Description: "Mixture-of-experts model for strong multilingual qualification on 24 GB+ hosts.",
		MinRAMGB:    24,
		BaseURL:     ollamaBaseURL,
		FastModel:   "thinkingmachines/inkling-2-mini",
		SmartModel:  "thinkingmachines/inkling-2-mini",
	},
	{
		ID:          "workstation_raon_1_mini",
		Tier:        domain.HardwareTierWorkstation,
		Provider:    domain.LLMProviderOllama,
		Title:       "Raon-1 Mini - Workstation",
		Description: "Sparse 30B-class model for high-quality local audit and roleplay.",
		MinRAMGB:    24,
		BaseURL:     ollamaBaseURL,
		FastModel:   "raon-1-mini",
		SmartModel:  "raon-1-mini",
	},
	{
		ID:          "workstation_qwen3_14b",
		Tier:        domain.HardwareTierWorkstation,
		Provider:    domain.LLMProviderOllama,
		Title:       "Qwen3 14B - Workstation",
		Description: "Strong general model for 24 GB+ GPUs with excellent JSON adherence.",
		MinRAMGB:    24,
		BaseURL:     ollamaBaseURL,
		FastModel:   "qwen3:14b",
		SmartModel:  "qwen3:14b",
	},

	// Cloud BYOK (zero local hardware, user-supplied key).
	{
		ID:          "byok_openrouter_free",
		Tier:        domain.HardwareTierBYOK,
		Provider:    domain.LLMProviderOpenRouter,
		Title:       "OpenRouter - Free Models",
		Description: "Use OpenRouter free models with your own API key; no billing in the app.",
		BaseURL:     "https://openrouter.ai/api/v1",
		FastModel:   "qwen/qwen3-8b:free",
		SmartModel:  "qwen/qwen3-8b:free",
		RequiresKey: true,
	},
	{
		ID:          "byok_openai",
		Tier:        domain.HardwareTierBYOK,
		Provider:    domain.LLMProviderOpenAI,
		Title:       "OpenAI BYOK",
		Description: "Bring your own OpenAI key for fast and smart model routing.",
		BaseURL:     "https://api.openai.com/v1",
		FastModel:   "gpt-5-mini",
		SmartModel:  "gpt-5-mini",
		VisionModel: "gpt-4o-mini",
		RequiresKey: true,
	},
	{
		ID:          "byok_gemini",
		Tier:        domain.HardwareTierBYOK,
		Provider:    domain.LLMProviderGemini,
		Title:       "Google Gemini BYOK",
		Description: "Use the Gemini OpenAI-compatible endpoint with your own key.",
		BaseURL:     "https://generativelanguage.googleapis.com/v1beta/openai",
		FastModel:   "gemini-2.5-flash",
		SmartModel:  "gemini-2.5-flash",
		VisionModel: "gemini-2.5-flash",
		RequiresKey: true,
	},
}

// AllPresets returns an isolated copy of the hardware preset catalog.
func AllPresets() []HardwarePreset {
	out := make([]HardwarePreset, len(presetCatalog))
	copy(out, presetCatalog)
	return out
}

// PresetByID finds a hardware preset by its stable ID.
func PresetByID(id string) (HardwarePreset, bool) {
	for _, preset := range presetCatalog {
		if preset.ID == id {
			return preset, true
		}
	}
	return HardwarePreset{}, false
}

// RecommendPresets returns fitting local presets for the detected hardware tier
// plus all BYOK cloud presets. Recommended entries are ordered first.
func RecommendPresets(hw domain.HardwareProfile) []HardwarePreset {
	tier := hw.RecommendedTier
	if tier != domain.HardwareTierBYOK && !knownTier(tier) {
		tier = tierFromRAMGB(hw.RAMGB)
	}
	local := make([]HardwarePreset, 0, 6)
	byok := make([]HardwarePreset, 0, 3)
	for _, preset := range presetCatalog {
		switch {
		case preset.Tier == domain.HardwareTierBYOK:
			byok = append(byok, preset)
		case preset.Tier == tier:
			local = append(local, preset)
		}
	}
	sort.SliceStable(local, func(i, j int) bool {
		return local[i].Recommended && !local[j].Recommended
	})
	return append(local, byok...)
}

// ConfigFromPreset builds a ready-to-use LLMConfig for a preset ID.
func ConfigFromPreset(id, apiKey string) (domain.LLMConfig, error) {
	preset, ok := PresetByID(id)
	if !ok {
		return domain.LLMConfig{}, fmt.Errorf("%w: unknown LLM preset %q", domain.ErrInvalidInput, id)
	}
	key := strings.TrimSpace(apiKey)
	if preset.RequiresKey && key == "" {
		return domain.LLMConfig{}, fmt.Errorf("%w: preset %q requires an API key", domain.ErrInvalidInput, id)
	}
	return domain.LLMConfig{
		Provider:          preset.Provider,
		BaseURL:           preset.BaseURL,
		APIKey:            key,
		PresetID:          preset.ID,
		FastModel:         preset.FastModel,
		SmartModel:        preset.SmartModel,
		VisionModel:       preset.VisionModel,
		RequestTimeoutSec: 45,
		MaxRetries:        2,
	}, nil
}

func knownTier(tier domain.HardwareTier) bool {
	switch tier {
	case domain.HardwareTierWeak, domain.HardwareTierBalanced, domain.HardwareTierWorkstation, domain.HardwareTierBYOK:
		return true
	default:
		return false
	}
}

func tierFromRAMGB(ramGB float64) domain.HardwareTier {
	switch {
	case ramGB <= 0:
		return domain.HardwareTierBalanced
	case ramGB < 12:
		return domain.HardwareTierWeak
	case ramGB < 24:
		return domain.HardwareTierBalanced
	default:
		return domain.HardwareTierWorkstation
	}
}
