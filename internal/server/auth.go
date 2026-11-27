// Package server implements the HTTP/REST API, Telegram initData HMAC-SHA256
// and browser session authentication, non-blocking WebSocket live terminal, and
// embedded SPA hosting.
package server

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/demonidze1337/mia-lead-radar/internal/domain"
	"github.com/demonidze1337/mia-lead-radar/internal/storage"
)

const (
	SessionCookieName = "mia_session"
	HeaderInitData    = "X-Telegram-Init-Data"
	DefaultSessionTTL = 7 * 24 * time.Hour
	MaxInitDataAge    = 24 * time.Hour
)

// AuthContext identifies the verified caller attached to an HTTP/WS request.
type AuthContext struct {
	Method   string `json:"method"` // "telegram_init_data" | "session"
	TGUserID int64  `json:"tg_user_id,omitempty"`
	Role     string `json:"role"`
	Label    string `json:"label,omitempty"`
}

type authContextKey struct{}

// AuthFromContext extracts verified AuthContext if present.
func AuthFromContext(ctx context.Context) (AuthContext, bool) {
	if ctx == nil {
		return AuthContext{}, false
	}
	v, ok := ctx.Value(authContextKey{}).(AuthContext)
	return v, ok
}

// WithAuthContext attaches verified AuthContext to ctx.
func WithAuthContext(ctx context.Context, ac AuthContext) context.Context {
	return context.WithValue(ctx, authContextKey{}, ac)
}

// TelegramUser is the subset of Telegram WebAppInitData.user verified via HMAC.
type TelegramUser struct {
	ID        int64  `json:"id"`
	Username  string `json:"username,omitempty"`
	FirstName string `json:"first_name,omitempty"`
	LastName  string `json:"last_name,omitempty"`
	LangCode  string `json:"language_code,omitempty"`
}

// VerifyTelegramInitData validates Telegram.WebApp.initData per official spec:
// secret = HMAC_SHA256("WebAppData", botToken), hash = HMAC_SHA256(secret, data_check_string).
// Also enforces auth_date freshness (<= maxAge) and optional ownerUserID pinning.
func VerifyTelegramInitData(rawInitData, botToken string, ownerUserID int64, now time.Time, maxAge time.Duration) (TelegramUser, error) {
	if strings.TrimSpace(botToken) == "" {
		return TelegramUser{}, fmt.Errorf("%w: bot token is not configured for initData verification", domain.ErrUnauthorized)
	}
	rawInitData = strings.TrimSpace(rawInitData)
	if rawInitData == "" {
		return TelegramUser{}, fmt.Errorf("%w: missing Telegram initData", domain.ErrUnauthorized)
	}
	values, err := url.ParseQuery(rawInitData)
	if err != nil {
		return TelegramUser{}, fmt.Errorf("%w: malformed initData", domain.ErrUnauthorized)
	}
	receivedHash := strings.ToLower(strings.TrimSpace(values.Get("hash")))
	if len(receivedHash) != 64 {
		return TelegramUser{}, fmt.Errorf("%w: invalid initData hash", domain.ErrUnauthorized)
	}
	if _, err := hex.DecodeString(receivedHash); err != nil {
		return TelegramUser{}, fmt.Errorf("%w: non-hex initData hash", domain.ErrUnauthorized)
	}

	authDateStr := strings.TrimSpace(values.Get("auth_date"))
	authUnix, err := strconv.ParseInt(authDateStr, 10, 64)
	if err != nil || authUnix <= 0 {
		return TelegramUser{}, fmt.Errorf("%w: invalid initData auth_date", domain.ErrUnauthorized)
	}
	authTime := time.Unix(authUnix, 0).UTC()
	if maxAge <= 0 {
		maxAge = MaxInitDataAge
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if now.UTC().Sub(authTime) > maxAge || authTime.Sub(now.UTC()) > 5*time.Minute {
		return TelegramUser{}, fmt.Errorf("%w: expired initData", domain.ErrUnauthorized)
	}

	keys := make([]string, 0, len(values))
	for k := range values {
		if k == "hash" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	lines := make([]string, 0, len(keys))
	for _, k := range keys {
		lines = append(lines, k+"="+values.Get(k))
	}
	dataCheckString := strings.Join(lines, "\n")

	secretMAC := hmac.New(sha256.New, []byte("WebAppData"))
	_, _ = secretMAC.Write([]byte(botToken))
	secretKey := secretMAC.Sum(nil)

	checkMAC := hmac.New(sha256.New, secretKey)
	_, _ = checkMAC.Write([]byte(dataCheckString))
	expectedHash := hex.EncodeToString(checkMAC.Sum(nil))

	if subtle.ConstantTimeCompare([]byte(expectedHash), []byte(receivedHash)) != 1 {
		return TelegramUser{}, fmt.Errorf("%w: initData signature mismatch", domain.ErrUnauthorized)
	}

	var user TelegramUser
	if rawUser := strings.TrimSpace(values.Get("user")); rawUser != "" {
		if err := json.Unmarshal([]byte(rawUser), &user); err != nil || user.ID <= 0 {
			return TelegramUser{}, fmt.Errorf("%w: invalid initData user payload", domain.ErrUnauthorized)
		}
	}
	if user.ID <= 0 {
		return TelegramUser{}, fmt.Errorf("%w: initData missing user id", domain.ErrUnauthorized)
	}
	if ownerUserID > 0 && user.ID != ownerUserID {
		return TelegramUser{}, fmt.Errorf("%w: telegram user %d is not the owner", domain.ErrUnauthorized, user.ID)
	}
	return user, nil
}

// HashToken derives the SHA-256 hex digest stored in auth_sessions.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// GenerateSessionToken creates a 32-byte cryptographically random token.
func GenerateSessionToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("auth: generate token: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

// Authenticator enforces HMAC initData and persisted browser session tokens
// with zero bare user_id IDOR trust.
type Authenticator struct {
	Store       storage.Store
	AccessToken string // Optional configured local dashboard passphrase/token
	Now         func() time.Time
}

func (a *Authenticator) nowUTC() time.Time {
	if a != nil && a.Now != nil {
		return a.Now().UTC()
	}
	return time.Now().UTC()
}

// IssueBrowserSession creates and persists a new browser auth session token.
func (a *Authenticator) IssueBrowserSession(ctx context.Context, label string, tgUserID int64) (string, domain.AuthSession, error) {
	if a == nil || a.Store == nil {
		return "", domain.AuthSession{}, fmt.Errorf("%w: auth store not configured", domain.ErrInvalidInput)
	}
	token, err := GenerateSessionToken()
	if err != nil {
		return "", domain.AuthSession{}, err
	}
	now := a.nowUTC()
	sess := domain.AuthSession{
		TokenHash:  HashToken(token),
		Label:      strings.TrimSpace(label),
		TGUserID:   tgUserID,
		Role:       "owner",
		CreatedAt:  now,
		ExpiresAt:  now.Add(DefaultSessionTTL),
		LastSeenAt: now,
	}
	id, err := a.Store.CreateAuthSession(ctx, sess)
	if err != nil {
		return "", domain.AuthSession{}, err
	}
	sess.ID = id
	return token, sess, nil
}

// AuthenticateRequest verifies either Bearer/cookie browser session token OR
// Telegram WebApp initData (never bare user_id parameters).
func (a *Authenticator) AuthenticateRequest(r *http.Request) (AuthContext, error) {
	if a == nil || a.Store == nil {
		return AuthContext{}, domain.ErrUnauthorized
	}
	ctx := r.Context()
	now := a.nowUTC()

	// 1. Check Bearer / Cookie / WS query session token.
	if token := extractSessionToken(r); token != "" {
		sess, err := a.Store.GetAuthSessionByHash(ctx, HashToken(token))
		if err == nil && sess.ExpiresAt.After(now) {
			_ = a.Store.TouchAuthSession(ctx, sess.ID, now)
			return AuthContext{
				Method:   "session",
				TGUserID: sess.TGUserID,
				Role:     sess.Role,
				Label:    sess.Label,
			}, nil
		}
	}

	// 2. Check Telegram WebApp initData HMAC-SHA256.
	if initData := extractInitData(r); initData != "" {
		settings, err := a.Store.GetSettings(ctx)
		if err != nil {
			return AuthContext{}, domain.ErrUnauthorized
		}
		if settings.OwnerTelegramID <= 0 {
			return AuthContext{}, fmt.Errorf("%w: owner Telegram id is not configured", domain.ErrUnauthorized)
		}
		user, err := VerifyTelegramInitData(initData, settings.OwnerBotToken, settings.OwnerTelegramID, now, MaxInitDataAge)
		if err != nil {
			return AuthContext{}, err
		}
		return AuthContext{
			Method:   "telegram_init_data",
			TGUserID: user.ID,
			Role:     "owner",
			Label:    user.Username,
		}, nil
	}

	return AuthContext{}, domain.ErrUnauthorized
}

// RequireAuth middleware protects API and WebSocket endpoints.
func (a *Authenticator) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ac, err := a.AuthenticateRequest(r)
		if err != nil {
			status := http.StatusUnauthorized
			if !errors.Is(err, domain.ErrUnauthorized) {
				status = http.StatusUnauthorized
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
			return
		}
		next.ServeHTTP(w, r.WithContext(WithAuthContext(r.Context(), ac)))
	})
}

// CanIssueLocalSession validates local browser authentication:
// - If AccessToken is configured on the server, constant-time match is required.
// - Otherwise, only loopback clients (127.0.0.1 / ::1) may obtain a local session.
func (a *Authenticator) CanIssueLocalSession(r *http.Request, suppliedCode string) bool {
	expected := strings.TrimSpace(a.AccessToken)
	if expected != "" {
		return subtle.ConstantTimeCompare([]byte(expected), []byte(strings.TrimSpace(suppliedCode))) == 1
	}
	return IsLoopbackRequest(r)
}

// IsLoopbackRequest checks whether the TCP peer address is loopback without trusting
// spoofable X-Forwarded-For headers.
func IsLoopbackRequest(r *http.Request) bool {
	if r == nil {
		return false
	}
	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err != nil {
		host = strings.TrimSpace(r.RemoteAddr)
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func extractSessionToken(r *http.Request) string {
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	if len(auth) > 7 && strings.EqualFold(auth[:7], "Bearer ") {
		return strings.TrimSpace(auth[7:])
	}
	if cookie, err := r.Cookie(SessionCookieName); err == nil {
		if tok := strings.TrimSpace(cookie.Value); tok != "" {
			return tok
		}
	}
	// WebSocket browser handshakes cannot set custom Authorization headers.
	if q := strings.TrimSpace(r.URL.Query().Get("token")); q != "" {
		return q
	}
	return ""
}

func extractInitData(r *http.Request) string {
	if v := strings.TrimSpace(r.Header.Get(HeaderInitData)); v != "" {
		return v
	}
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	if len(auth) > 4 && strings.EqualFold(auth[:4], "tma ") {
		return strings.TrimSpace(auth[4:])
	}
	if q := strings.TrimSpace(r.URL.Query().Get("initData")); q != "" {
		return q
	}
	return ""
}
