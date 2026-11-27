// Package config loads process configuration from an optional .env file and
// environment variables without pulling in heavyweight config frameworks.
package config

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"
)

// Defaults applied when the corresponding environment variable is unset.
const (
	DefaultHTTPAddr = ":8080"
	DefaultDataDir  = "./data"
)

// Config holds immutable startup configuration for the MIA Lead Radar process.
type Config struct {
	HTTPAddr        string
	DataDir         string
	DBPath          string
	SessionsDir     string
	TelegramAppID   int
	TelegramAppHash string
	WebAppBaseURL   string
	Debug           bool
}

// Load reads ./.env (when present), then resolves configuration from the
// process environment. Variables already present in the environment always
// win over .env values.
func Load() (Config, error) {
	return LoadFromDir(".")
}

// LoadFromDir behaves like Load but looks for <dir>/.env.
func LoadFromDir(dir string) (Config, error) {
	if err := ApplyEnvFile(filepath.Join(dir, ".env"), os.LookupEnv); err != nil {
		return Config{}, err
	}
	return FromEnv(os.LookupEnv)
}

// FromEnv builds a Config purely from a lookup function (no file IO), which
// keeps environment precedence and defaults unit-testable.
func FromEnv(lookup func(key string) (string, bool)) (Config, error) {
	if lookup == nil {
		lookup = func(string) (string, bool) { return "", false }
	}
	get := func(key string) string {
		v, _ := lookup(key)
		return strings.TrimSpace(v)
	}

	cfg := Config{
		HTTPAddr:        DefaultHTTPAddr,
		DataDir:         DefaultDataDir,
		TelegramAppHash: get("TELEGRAM_APP_HASH"),
		WebAppBaseURL:   get("WEBAPP_BASE_URL"),
	}

	if v := get("HTTP_ADDR"); v != "" {
		cfg.HTTPAddr = v
	}
	if v := get("DATA_DIR"); v != "" {
		cfg.DataDir = filepath.Clean(v)
	}
	cfg.DBPath = filepath.Join(cfg.DataDir, "mia.db")
	cfg.SessionsDir = filepath.Join(cfg.DataDir, "sessions")
	if v := get("DB_PATH"); v != "" {
		cfg.DBPath = filepath.Clean(v)
	}
	if v := get("SESSIONS_DIR"); v != "" {
		cfg.SessionsDir = filepath.Clean(v)
	}

	if v := get("TELEGRAM_APP_ID"); v != "" {
		id, err := strconv.Atoi(v)
		if err != nil || id < 0 {
			return Config{}, fmt.Errorf("config: invalid TELEGRAM_APP_ID %q", v)
		}
		cfg.TelegramAppID = id
	}
	if cfg.TelegramAppID != 0 && cfg.TelegramAppHash == "" {
		return Config{}, fmt.Errorf("config: TELEGRAM_APP_HASH is required when TELEGRAM_APP_ID is set")
	}
	if cfg.TelegramAppID == 0 && cfg.TelegramAppHash != "" {
		return Config{}, fmt.Errorf("config: TELEGRAM_APP_ID is required when TELEGRAM_APP_HASH is set")
	}
	if cfg.WebAppBaseURL == "" {
		cfg.WebAppBaseURL = deriveWebAppBaseURL(cfg.HTTPAddr)
	}

	if v := get("DEBUG"); v != "" {
		debug, err := parseBool(v)
		if err != nil {
			return Config{}, fmt.Errorf("config: invalid DEBUG %q: %w", v, err)
		}
		cfg.Debug = debug
	}

	return cfg, nil
}

// EnsureDirs creates the data and MTProto session directories if missing.
func (c Config) EnsureDirs() error {
	for _, dir := range []string{c.DataDir, c.SessionsDir} {
		if strings.TrimSpace(dir) == "" {
			continue
		}
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return fmt.Errorf("config: create %s: %w", dir, err)
		}
	}
	return nil
}

// ApplyEnvFile parses path and sets each key in the process environment only
// when it is not already defined (existing OS variables are preserved).
// A missing file is not an error: .env is optional.
func ApplyEnvFile(path string, lookup func(string) (string, bool)) error {
	values, err := ReadEnvFile(path)
	if err != nil {
		return err
	}
	return ApplyEnv(values, lookup, os.Setenv)
}

// ApplyEnv merges parsed dotenv values using lookup/set hooks. Keys already
// visible through lookup are preserved. It is exported for deterministic
// testing of precedence without mutating the process environment.
func ApplyEnv(values map[string]string, lookup func(string) (string, bool), set func(string, string) error) error {
	if lookup == nil {
		lookup = os.LookupEnv
	}
	if set == nil {
		set = os.Setenv
	}
	for key, value := range values {
		if _, exists := lookup(key); exists {
			continue
		}
		if err := set(key, value); err != nil {
			return fmt.Errorf("config: set %s: %w", key, err)
		}
	}
	return nil
}

// ReadEnvFile parses a dotenv-style file. It ignores blank lines and full-line
// comments, allows an optional "export " prefix, supports single/double quoted
// values, and strips inline comments outside quotes.
func ReadEnvFile(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]string{}, nil
		}
		return nil, fmt.Errorf("config: open %s: %w", path, err)
	}
	defer f.Close()
	return ParseEnvFile(f)
}

// ParseEnvFile parses dotenv content from r.
func ParseEnvFile(r io.Reader) (map[string]string, error) {
	values := make(map[string]string)
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 4096), 1024*1024)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		key, raw, ok := strings.Cut(line, "=")
		if !ok {
			return nil, fmt.Errorf("config: %s: line %d: expected KEY=VALUE", pathName(r), lineNo)
		}
		key = strings.TrimSpace(key)
		if !validEnvKey(key) {
			return nil, fmt.Errorf("config: %s: line %d: invalid key %q", pathName(r), lineNo, key)
		}
		value, err := parseEnvValue(strings.TrimSpace(raw))
		if err != nil {
			return nil, fmt.Errorf("config: %s: line %d: %w", pathName(r), lineNo, err)
		}
		values[key] = value
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("config: read env file: %w", err)
	}
	return values, nil
}

func pathName(r io.Reader) string {
	if f, ok := r.(*os.File); ok {
		return f.Name()
	}
	return "dotenv"
}

func validEnvKey(key string) bool {
	if key == "" {
		return false
	}
	for i, r := range key {
		switch {
		case r == '_' || unicode.IsLetter(r):
		case i > 0 && unicode.IsDigit(r):
		default:
			return false
		}
	}
	return true
}

func parseEnvValue(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	switch raw[0] {
	case '"':
		end := closingDoubleQuote(raw)
		if end < 0 {
			return "", fmt.Errorf("unterminated double-quoted value")
		}
		if !onlyComment(raw[end+1:]) {
			return "", fmt.Errorf("unexpected characters after double-quoted value")
		}
		value, err := strconv.Unquote(raw[:end+1])
		if err != nil {
			return "", fmt.Errorf("invalid double-quoted value: %w", err)
		}
		return value, nil
	case '\'':
		end := strings.IndexByte(raw[1:], '\'')
		if end < 0 {
			return "", fmt.Errorf("unterminated single-quoted value")
		}
		end++ // index in raw
		if !onlyComment(raw[end+1:]) {
			return "", fmt.Errorf("unexpected characters after single-quoted value")
		}
		return raw[1:end], nil
	default:
		if idx := indexInlineComment(raw); idx >= 0 {
			raw = strings.TrimSpace(raw[:idx])
		}
		return raw, nil
	}
}

func closingDoubleQuote(raw string) int {
	escaped := false
	for i := 1; i < len(raw); i++ {
		switch {
		case escaped:
			escaped = false
		case raw[i] == '\\':
			escaped = true
		case raw[i] == '"':
			return i
		}
	}
	return -1
}

func onlyComment(rest string) bool {
	rest = strings.TrimSpace(rest)
	return rest == "" || strings.HasPrefix(rest, "#")
}

func indexInlineComment(s string) int {
	for i := 1; i < len(s); i++ {
		if s[i] == '#' && unicode.IsSpace(rune(s[i-1])) {
			return i
		}
	}
	return -1
}

func parseBool(raw string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "true", "yes", "on":
		return true, nil
	case "0", "false", "no", "off":
		return false, nil
	default:
		return false, fmt.Errorf("expected true/false/1/0/yes/no/on/off")
	}
}

func deriveWebAppBaseURL(httpAddr string) string {
	addr := strings.TrimSpace(httpAddr)
	if strings.HasPrefix(addr, ":") {
		return "http://localhost" + addr
	}
	if strings.HasPrefix(addr, "0.0.0.0:") {
		return "http://localhost:" + strings.TrimPrefix(addr, "0.0.0.0:")
	}
	if strings.HasPrefix(addr, "[::]:") {
		return "http://localhost:" + strings.TrimPrefix(addr, "[::]:")
	}
	if strings.Contains(addr, "://") {
		return addr
	}
	return "http://" + addr
}
