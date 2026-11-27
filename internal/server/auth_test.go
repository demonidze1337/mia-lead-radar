package server

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/demonidze1337/mia-lead-radar/internal/domain"
	"github.com/demonidze1337/mia-lead-radar/internal/storage/sqlite"
)

func signedInitData(t *testing.T, botToken string, user TelegramUser, authDate time.Time) string {
	t.Helper()
	userJSON, err := json.Marshal(user)
	if err != nil {
		t.Fatalf("marshal user: %v", err)
	}
	values := url.Values{
		"auth_date": {strconv.FormatInt(authDate.Unix(), 10)},
		"query_id":  {"AAExampleQuery"},
		"user":      {string(userJSON)},
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	lines := make([]string, 0, len(keys))
	for _, key := range keys {
		lines = append(lines, key+"="+values.Get(key))
	}
	secretMAC := hmac.New(sha256.New, []byte("WebAppData"))
	_, _ = secretMAC.Write([]byte(botToken))
	checkMAC := hmac.New(sha256.New, secretMAC.Sum(nil))
	_, _ = checkMAC.Write([]byte(strings.Join(lines, "\n")))
	values.Set("hash", hex.EncodeToString(checkMAC.Sum(nil)))
	return values.Encode()
}

func TestVerifyTelegramInitData(t *testing.T) {
	now := time.Now().UTC()
	user := TelegramUser{ID: 42, Username: "owner", FirstName: "Owner"}
	raw := signedInitData(t, "123:token", user, now)
	got, err := VerifyTelegramInitData(raw, "123:token", 42, now, 24*time.Hour)
	if err != nil || got.ID != 42 || got.Username != "owner" {
		t.Fatalf("valid initData = %+v, %v", got, err)
	}
	if _, err := VerifyTelegramInitData(raw, "wrong:token", 42, now, 24*time.Hour); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("wrong token err = %v", err)
	}
	if _, err := VerifyTelegramInitData(raw, "123:token", 99, now, 24*time.Hour); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("wrong owner err = %v", err)
	}
	if _, err := VerifyTelegramInitData(raw, "123:token", 0, now.Add(2*24*time.Hour), 24*time.Hour); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("expired err = %v", err)
	}
	values, _ := url.ParseQuery(raw)
	values.Set("query_id", "tampered")
	if _, err := VerifyTelegramInitData(values.Encode(), "123:token", 42, now, 24*time.Hour); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("tampered err = %v", err)
	}
	if _, err := VerifyTelegramInitData(raw, "", 42, now, 24*time.Hour); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("missing bot token err = %v", err)
	}
}

func TestAuthenticatorSessionsAndIDOR(t *testing.T) {
	store, err := sqlite.New(filepath.Join(t.TempDir(), "auth.db"))
	if err != nil {
		t.Fatalf("sqlite.New: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	ctx := context.Background()
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	now := time.Now().UTC()
	auth := &Authenticator{Store: store, Now: func() time.Time { return now }}
	token, sess, err := auth.IssueBrowserSession(ctx, "browser", 42)
	if err != nil || token == "" || sess.ID <= 0 {
		t.Fatalf("IssueBrowserSession = %q %+v %v", token, sess, err)
	}
	request := httptest.NewRequest("GET", "/api/leads?user_id=999", nil)
	request.Header.Set("Authorization", "Bearer "+token)
	ac, err := auth.AuthenticateRequest(request)
	if err != nil || ac.Method != "session" || ac.TGUserID != 42 {
		t.Fatalf("AuthenticateRequest = %+v, %v", ac, err)
	}

	bare := httptest.NewRequest("GET", "/api/leads?user_id=42", nil)
	if _, err := auth.AuthenticateRequest(bare); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("bare user_id IDOR err = %v", err)
	}

	now = now.Add(DefaultSessionTTL + time.Hour)
	expired := httptest.NewRequest("GET", "/api/leads", nil)
	expired.Header.Set("Authorization", "Bearer "+token)
	if _, err := auth.AuthenticateRequest(expired); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("expired session err = %v", err)
	}
}

func TestCanIssueLocalSession(t *testing.T) {
	auth := &Authenticator{AccessToken: "secret"}
	local := httptest.NewRequest("POST", "/api/auth/local", nil)
	local.RemoteAddr = "127.0.0.1:12345"
	remote := httptest.NewRequest("POST", "/api/auth/local", nil)
	remote.RemoteAddr = "203.0.113.10:12345"
	remote.Header.Set("X-Forwarded-For", "127.0.0.1")

	if !auth.CanIssueLocalSession(local, "secret") {
		t.Fatal("configured access token should work")
	}
	if auth.CanIssueLocalSession(local, "wrong") {
		t.Fatal("wrong access token accepted")
	}
	open := &Authenticator{}
	if !open.CanIssueLocalSession(local, "") {
		t.Fatal("loopback without configured token should work")
	}
	if open.CanIssueLocalSession(remote, "") {
		t.Fatal("remote request without token obtained local session")
	}
	if IsLoopbackRequest(nil) {
		t.Fatal("nil request reported loopback")
	}
}
