package config

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/demonidze1337/mia-lead-radar/internal/domain"
)

func TestParseEnvFile(t *testing.T) {
	input := `# full line comment
HTTP_ADDR=127.0.0.1:9090

export DATA_DIR=./custom-data
DEBUG=yes
QUOTED="hello world"
QUOTED_COMMENT="quoted value" # trailing note
SINGLE='raw # value'
SINGLE_COMMENT='single value' # trailing note
INLINE=value # trailing comment
HASH_IN_URL=https://example.com/#frag
ESCAPED="line\nbreak"
EMPTY=
`
	values, err := ParseEnvFile(strings.NewReader(input))
	if err != nil {
		t.Fatalf("ParseEnvFile: %v", err)
	}
	want := map[string]string{
		"HTTP_ADDR":      "127.0.0.1:9090",
		"DATA_DIR":       "./custom-data",
		"DEBUG":          "yes",
		"QUOTED":         "hello world",
		"QUOTED_COMMENT": "quoted value",
		"SINGLE":         "raw # value",
		"SINGLE_COMMENT": "single value",
		"INLINE":         "value",
		"HASH_IN_URL":    "https://example.com/#frag",
		"ESCAPED":        "line\nbreak",
		"EMPTY":          "",
	}
	if len(values) != len(want) {
		t.Fatalf("got %d values, want %d: %#v", len(values), len(want), values)
	}
	for key, expected := range want {
		if got := values[key]; got != expected {
			t.Errorf("%s = %q, want %q", key, got, expected)
		}
	}
}

func TestParseEnvFileErrors(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{name: "missing equals", input: "JUST_A_KEY\n"},
		{name: "invalid key", input: "BAD-KEY=value\n"},
		{name: "empty key", input: "=value\n"},
		{name: "unterminated double quote", input: "KEY=\"value\n"},
		{name: "unterminated single quote", input: "KEY='value\n"},
		{name: "garbage after double quote", input: "KEY=\"value\"x\n"},
		{name: "garbage after single quote", input: "KEY='value'x\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ParseEnvFile(strings.NewReader(tc.input)); err == nil {
				t.Fatal("expected error, got nil")
			}
		})
	}
}

func TestReadEnvFileMissingIsOptional(t *testing.T) {
	values, err := ReadEnvFile(filepath.Join(t.TempDir(), "missing.env"))
	if err != nil {
		t.Fatalf("ReadEnvFile: %v", err)
	}
	if len(values) != 0 {
		t.Fatalf("expected empty map, got %#v", values)
	}
}

func TestApplyEnvPreservesExistingValues(t *testing.T) {
	values := map[string]string{"A": "from-file", "B": "from-file", "C": "from-file"}
	existing := map[string]string{"A": "from-os"}
	set := make(map[string]string)
	err := ApplyEnv(values, func(key string) (string, bool) {
		v, ok := existing[key]
		return v, ok
	}, func(key, value string) error {
		set[key] = value
		return nil
	})
	if err != nil {
		t.Fatalf("ApplyEnv: %v", err)
	}
	if set["A"] != "" {
		t.Errorf("existing env key A was overwritten: %q", set["A"])
	}
	if set["B"] != "from-file" || set["C"] != "from-file" {
		t.Errorf("missing keys were not applied: %#v", set)
	}
}

func TestApplyEnvPropagatesSetError(t *testing.T) {
	sentinel := errors.New("boom")
	err := ApplyEnv(map[string]string{"A": "1"}, func(string) (string, bool) { return "", false }, func(string, string) error {
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected wrapped sentinel, got %v", err)
	}
}

func TestFromEnvDefaults(t *testing.T) {
	cfg, err := FromEnv(func(string) (string, bool) { return "", false })
	if err != nil {
		t.Fatalf("FromEnv: %v", err)
	}
	if cfg.HTTPAddr != ":8080" {
		t.Errorf("HTTPAddr = %q", cfg.HTTPAddr)
	}
	if cfg.DataDir != "./data" {
		t.Errorf("DataDir = %q", cfg.DataDir)
	}
	if cfg.DBPath != filepath.Join("data", "mia.db") {
		t.Errorf("DBPath = %q", cfg.DBPath)
	}
	if cfg.SessionsDir != filepath.Join("data", "sessions") {
		t.Errorf("SessionsDir = %q", cfg.SessionsDir)
	}
	if cfg.WebAppBaseURL != "http://localhost:8080" {
		t.Errorf("WebAppBaseURL = %q", cfg.WebAppBaseURL)
	}
	if cfg.Debug {
		t.Error("Debug should default to false")
	}
}

func TestLoadFromDirAppliesEnvFileWithOSPrecedence(t *testing.T) {
	dir := t.TempDir()
	envBody := []byte("HTTP_ADDR=:7777\nDATA_DIR=./from-file\nDEBUG=true\n")
	if err := os.WriteFile(filepath.Join(dir, ".env"), envBody, 0o600); err != nil {
		t.Fatalf("write .env: %v", err)
	}
	t.Setenv("HTTP_ADDR", ":9999")

	restoreEnv := func(key string) {
		old, had := os.LookupEnv(key)
		if err := os.Unsetenv(key); err != nil {
			t.Fatalf("unset %s: %v", key, err)
		}
		t.Cleanup(func() {
			if had {
				_ = os.Setenv(key, old)
				return
			}
			_ = os.Unsetenv(key)
		})
	}
	restoreEnv("DATA_DIR")
	restoreEnv("DEBUG")

	cfg, err := LoadFromDir(dir)
	if err != nil {
		t.Fatalf("LoadFromDir: %v", err)
	}
	if cfg.HTTPAddr != ":9999" {
		t.Errorf("HTTPAddr = %q, .env must not override OS env", cfg.HTTPAddr)
	}
	if cfg.DataDir != filepath.Clean("./from-file") {
		t.Errorf("DataDir = %q", cfg.DataDir)
	}
	if !cfg.Debug {
		t.Error("DEBUG from .env was not applied")
	}
	if cfg.DBPath != filepath.Join(filepath.Clean("./from-file"), "mia.db") {
		t.Errorf("DBPath = %q", cfg.DBPath)
	}
}

func TestFromEnvOverrides(t *testing.T) {
	env := map[string]string{
		"HTTP_ADDR":         "127.0.0.1:9999",
		"DATA_DIR":          "/var/lib/mia",
		"DB_PATH":           "/var/lib/mia/custom.db",
		"SESSIONS_DIR":      "/var/lib/mia/sessions",
		"TELEGRAM_APP_ID":   "12345",
		"TELEGRAM_APP_HASH": "abcdef",
		"WEBAPP_BASE_URL":   "https://radar.example.com",
		"ACCESS_TOKEN":      "local-code",
		"DEBUG":             "TRUE",
	}
	cfg, err := FromEnv(func(key string) (string, bool) {
		v, ok := env[key]
		return v, ok
	})
	if err != nil {
		t.Fatalf("FromEnv: %v", err)
	}
	if cfg.HTTPAddr != "127.0.0.1:9999" || cfg.DataDir != filepath.FromSlash("/var/lib/mia") {
		t.Fatalf("unexpected addresses: %+v", cfg)
	}
	if cfg.DBPath != filepath.FromSlash("/var/lib/mia/custom.db") || cfg.SessionsDir != filepath.FromSlash("/var/lib/mia/sessions") {
		t.Fatalf("unexpected paths: %+v", cfg)
	}
	if cfg.TelegramAppID != 12345 || cfg.TelegramAppHash != "abcdef" {
		t.Fatalf("unexpected telegram creds: %+v", cfg)
	}
	if cfg.WebAppBaseURL != "https://radar.example.com" || !cfg.Debug {
		t.Fatalf("unexpected webapp/debug: %+v", cfg)
	}
	if cfg.AccessToken != "local-code" {
		t.Fatalf("AccessToken = %q", cfg.AccessToken)
	}
}

func TestFromEnvDerivedPaths(t *testing.T) {
	cfg, err := FromEnv(func(key string) (string, bool) {
		if key == "DATA_DIR" {
			return "/tmp/mia-data", true
		}
		return "", false
	})
	if err != nil {
		t.Fatalf("FromEnv: %v", err)
	}
	if cfg.DBPath != filepath.Join("/tmp/mia-data", "mia.db") {
		t.Errorf("DBPath = %q", cfg.DBPath)
	}
	if cfg.SessionsDir != filepath.Join("/tmp/mia-data", "sessions") {
		t.Errorf("SessionsDir = %q", cfg.SessionsDir)
	}
}

func TestFromEnvValidation(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
	}{
		{name: "invalid app id", env: map[string]string{"TELEGRAM_APP_ID": "abc"}},
		{name: "negative app id", env: map[string]string{"TELEGRAM_APP_ID": "-1"}},
		{name: "hash without id", env: map[string]string{"TELEGRAM_APP_HASH": "x"}},
		{name: "id without hash", env: map[string]string{"TELEGRAM_APP_ID": "1"}},
		{name: "invalid debug", env: map[string]string{"DEBUG": "maybe"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := FromEnv(func(key string) (string, bool) {
				v, ok := tc.env[key]
				return v, ok
			}); err == nil {
				t.Fatal("expected validation error, got nil")
			}
		})
	}
}

func TestDeriveWebAppBaseURL(t *testing.T) {
	tests := map[string]string{
		":8080":            "http://localhost:8080",
		"0.0.0.0:9000":     "http://localhost:9000",
		"[::]:9001":        "http://localhost:9001",
		"127.0.0.1:7000":   "http://127.0.0.1:7000",
		"https://mia.test": "https://mia.test",
	}
	for input, want := range tests {
		if got := deriveWebAppBaseURL(input); got != want {
			t.Errorf("deriveWebAppBaseURL(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestEnsureDirs(t *testing.T) {
	root := t.TempDir()
	cfg := Config{DataDir: filepath.Join(root, "data"), SessionsDir: filepath.Join(root, "data", "sessions")}
	if err := cfg.EnsureDirs(); err != nil {
		t.Fatalf("EnsureDirs: %v", err)
	}
	for _, dir := range []string{cfg.DataDir, cfg.SessionsDir} {
		info, err := os.Stat(dir)
		if err != nil || !info.IsDir() {
			t.Fatalf("directory %s missing: %v", dir, err)
		}
	}
}

func TestTierForRAM(t *testing.T) {
	giB := uint64(1) << 30
	tests := []struct {
		ram  uint64
		want domain.HardwareTier
	}{
		{ram: 0, want: domain.HardwareTierBalanced},
		{ram: 4 * giB, want: domain.HardwareTierWeak},
		{ram: 11*giB + giB/2, want: domain.HardwareTierWeak},
		{ram: 12 * giB, want: domain.HardwareTierBalanced},
		{ram: 23*giB + giB - 1, want: domain.HardwareTierBalanced},
		{ram: 24 * giB, want: domain.HardwareTierWorkstation},
		{ram: 64 * giB, want: domain.HardwareTierWorkstation},
	}
	for _, tc := range tests {
		if got := TierForRAM(tc.ram); got != tc.want {
			t.Errorf("TierForRAM(%d) = %q, want %q", tc.ram, got, tc.want)
		}
	}
}

func TestBuildHardwareProfile(t *testing.T) {
	profile := buildHardwareProfile(16*(1<<30), 8, "linux", "amd64")
	if profile.RAMGB != 16 || profile.CPUCores != 8 || profile.OS != "linux" || profile.Arch != "amd64" {
		t.Fatalf("unexpected profile: %+v", profile)
	}
	if profile.RecommendedTier != domain.HardwareTierBalanced {
		t.Fatalf("tier = %q", profile.RecommendedTier)
	}
}

func TestDetectHardware(t *testing.T) {
	profile := DetectHardware()
	if profile.CPUCores < 1 {
		t.Errorf("CPUCores = %d", profile.CPUCores)
	}
	if profile.OS != runtime.GOOS || profile.Arch != runtime.GOARCH {
		t.Errorf("platform mismatch: %+v", profile)
	}
	if profile.RecommendedTier != TierForRAM(profile.RAMBytes) {
		t.Errorf("tier %q inconsistent with RAM %d", profile.RecommendedTier, profile.RAMBytes)
	}
	if profile.RAMBytes > 0 && profile.RAMGB <= 0 {
		t.Errorf("RAMGB = %v for %d bytes", profile.RAMGB, profile.RAMBytes)
	}
}
