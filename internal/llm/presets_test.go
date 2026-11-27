package llm

import (
	"errors"
	"testing"

	"github.com/demonidze1337/mia-lead-radar/internal/domain"
)

func TestAllPresetsCatalog(t *testing.T) {
	presets := AllPresets()
	if len(presets) != 12 {
		t.Fatalf("presets = %d, want 12", len(presets))
	}
	seen := make(map[string]struct{}, len(presets))
	for _, preset := range presets {
		if preset.ID == "" || preset.Title == "" || preset.Description == "" {
			t.Errorf("incomplete preset: %+v", preset)
		}
		if preset.FastModel == "" || preset.SmartModel == "" {
			t.Errorf("preset %s has no models", preset.ID)
		}
		if _, dup := seen[preset.ID]; dup {
			t.Errorf("duplicate preset id %q", preset.ID)
		}
		seen[preset.ID] = struct{}{}
	}

	required := []string{
		"weak_qwen3_1_7b", "weak_qwen3_0_6b", "weak_phi4_mini",
		"balanced_qwen35_4b", "balanced_gemma4_e4b", "balanced_qwen3_8b",
		"workstation_inkling_2_mini", "workstation_raon_1_mini", "workstation_qwen3_14b",
		"byok_openrouter_free", "byok_openai", "byok_gemini",
	}
	for _, id := range required {
		if _, ok := PresetByID(id); !ok {
			t.Errorf("missing required preset %q", id)
		}
	}

	balanced, _ := PresetByID("balanced_qwen35_4b")
	if !balanced.Recommended || balanced.FastModel != "qwen3.5:4b" {
		t.Errorf("balanced default wrong: %+v", balanced)
	}
	if balanced.Tier != domain.HardwareTierBalanced || balanced.RequiresKey {
		t.Errorf("balanced tier/key wrong: %+v", balanced)
	}
	weak, _ := PresetByID("weak_qwen3_0_6b")
	if weak.FastModel != "qwen3:0.6b" || weak.Tier != domain.HardwareTierWeak {
		t.Errorf("weak preset wrong: %+v", weak)
	}
	workstation, _ := PresetByID("workstation_inkling_2_mini")
	if workstation.FastModel != "thinkingmachines/inkling-2-mini" {
		t.Errorf("workstation model wrong: %+v", workstation)
	}
	openrouter, _ := PresetByID("byok_openrouter_free")
	if !openrouter.RequiresKey || openrouter.BaseURL != "https://openrouter.ai/api/v1" || openrouter.Provider != domain.LLMProviderOpenRouter {
		t.Errorf("openrouter preset wrong: %+v", openrouter)
	}
	openai, _ := PresetByID("byok_openai")
	if openai.BaseURL != "https://api.openai.com/v1" || openai.Provider != domain.LLMProviderOpenAI {
		t.Errorf("openai preset wrong: %+v", openai)
	}
	gemini, _ := PresetByID("byok_gemini")
	if gemini.BaseURL != "https://generativelanguage.googleapis.com/v1beta/openai" || gemini.Provider != domain.LLMProviderGemini {
		t.Errorf("gemini preset wrong: %+v", gemini)
	}
	if _, ok := PresetByID("does-not-exist"); ok {
		t.Error("unknown preset reported as found")
	}
}

func TestRecommendPresets(t *testing.T) {
	tests := []struct {
		name      string
		hw        domain.HardwareProfile
		wantLocal int
		wantFirst string
	}{
		{name: "weak", hw: domain.HardwareProfile{RecommendedTier: domain.HardwareTierWeak}, wantLocal: 3, wantFirst: "weak_qwen3_1_7b"},
		{name: "balanced", hw: domain.HardwareProfile{RecommendedTier: domain.HardwareTierBalanced}, wantLocal: 3, wantFirst: "balanced_qwen35_4b"},
		{name: "workstation", hw: domain.HardwareProfile{RecommendedTier: domain.HardwareTierWorkstation}, wantLocal: 3, wantFirst: "workstation_inkling_2_mini"},
		{name: "byok", hw: domain.HardwareProfile{RecommendedTier: domain.HardwareTierBYOK}, wantLocal: 0, wantFirst: "byok_openrouter_free"},
		{name: "ram fallback weak", hw: domain.HardwareProfile{RAMGB: 8}, wantLocal: 3, wantFirst: "weak_qwen3_1_7b"},
		{name: "ram fallback workstation", hw: domain.HardwareProfile{RAMGB: 32}, wantLocal: 3, wantFirst: "workstation_inkling_2_mini"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := RecommendPresets(tc.hw)
			if tc.wantLocal == 0 {
				if len(got) != 3 {
					t.Fatalf("recommended = %d, want 3 BYOK", len(got))
				}
			} else if len(got) != tc.wantLocal+3 {
				t.Fatalf("recommended = %d, want %d local + 3 BYOK", len(got), tc.wantLocal)
			}
			if got[0].ID != tc.wantFirst {
				t.Errorf("first = %q, want %q", got[0].ID, tc.wantFirst)
			}
			for _, preset := range got[tc.wantLocal:] {
				if preset.Tier != domain.HardwareTierBYOK {
					t.Errorf("expected BYOK at tail, got %+v", preset)
				}
			}
			if tc.wantLocal > 0 && !got[0].Recommended && tc.wantFirst == "balanced_qwen35_4b" {
				t.Error("balanced recommended preset not first")
			}
		})
	}
}

func TestConfigFromPreset(t *testing.T) {
	cfg, err := ConfigFromPreset("balanced_qwen35_4b", "")
	if err != nil {
		t.Fatalf("ConfigFromPreset local: %v", err)
	}
	if cfg.PresetID != "balanced_qwen35_4b" || cfg.BaseURL != ollamaBaseURL ||
		cfg.FastModel != "qwen3.5:4b" || cfg.SmartModel != "qwen3.5:4b" ||
		cfg.RequestTimeoutSec != 45 || cfg.MaxRetries != 2 {
		t.Fatalf("unexpected local config: %+v", cfg)
	}

	if _, err := ConfigFromPreset("byok_openai", ""); !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatalf("missing BYOK key err = %v", err)
	}
	cfg, err = ConfigFromPreset("byok_openai", "  sk-test  ")
	if err != nil {
		t.Fatalf("ConfigFromPreset BYOK: %v", err)
	}
	if cfg.APIKey != "sk-test" || cfg.Provider != domain.LLMProviderOpenAI {
		t.Fatalf("unexpected BYOK config: %+v", cfg)
	}
	if _, err := ConfigFromPreset("nope", "key"); !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatalf("unknown preset err = %v", err)
	}
}
