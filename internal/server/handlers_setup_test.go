package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/demonidze1337/mia-lead-radar/internal/domain"
)

type llmTestUpstream struct {
	server *httptest.Server
	mu     sync.Mutex
	auth   string
	path   string
}

func newLLMTestUpstream(t *testing.T) *llmTestUpstream {
	t.Helper()
	upstream := &llmTestUpstream{}
	upstream.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstream.mu.Lock()
		upstream.auth = r.Header.Get("Authorization")
		upstream.path = r.URL.Path
		upstream.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"fast-model"}]}`))
	}))
	t.Cleanup(upstream.server.Close)
	return upstream
}

func (u *llmTestUpstream) baseURL() string { return u.server.URL + "/v1" }

func (u *llmTestUpstream) seen() (auth, path string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.auth, u.path
}

func configureStoredLLM(t *testing.T, env *serverTestEnv, baseURL string) {
	t.Helper()
	settings, err := env.store.GetSettings(context.Background())
	if err != nil {
		t.Fatalf("GetSettings: %v", err)
	}
	settings.LLM.Provider = domain.LLMProviderCustom
	settings.LLM.BaseURL = baseURL
	settings.LLM.APIKey = "sk-secret"
	if err := env.store.SaveSettings(context.Background(), settings); err != nil {
		t.Fatalf("SaveSettings: %v", err)
	}
}

func decodeLLMTestResponse(t *testing.T, body []byte) (bool, []string, string) {
	t.Helper()
	var response struct {
		OK     bool     `json:"ok"`
		Models []string `json:"models"`
		Error  string   `json:"error"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		t.Fatalf("decode: %v (%s)", err, body)
	}
	return response.OK, response.Models, response.Error
}

// TestLLMTestRestoresMaskedAPIKey ensures the setup wizard can re-test a saved
// connection after the settings endpoint masked the key as "***".
func TestLLMTestRestoresMaskedAPIKey(t *testing.T) {
	env := newServerTest(t)
	upstream := newLLMTestUpstream(t)
	configureStoredLLM(t, env, upstream.baseURL())

	recorder := env.request(t, http.MethodPost, "/api/setup/llm/test", map[string]any{
		"provider":    "custom",
		"base_url":    upstream.baseURL(),
		"api_key":     "***",
		"fast_model":  "fast-model",
		"smart_model": "fast-model",
	}, true)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	ok, models, message := decodeLLMTestResponse(t, recorder.Body.Bytes())
	if !ok {
		t.Fatalf("ok = false, error = %q", message)
	}
	if len(models) != 1 || models[0] != "fast-model" {
		t.Fatalf("models = %#v", models)
	}
	auth, path := upstream.seen()
	if path != "/v1/models" {
		t.Fatalf("upstream path = %q", path)
	}
	if auth != "Bearer sk-secret" {
		t.Fatalf("masked key was not restored: Authorization = %q", auth)
	}
}

// TestLLMTestDoesNotLeakMaskedKeyToAnotherEndpoint ensures changing provider or
// base URL never forwards the stored credential to the new destination.
func TestLLMTestDoesNotLeakMaskedKeyToAnotherEndpoint(t *testing.T) {
	env := newServerTest(t)
	stored := newLLMTestUpstream(t)
	configureStoredLLM(t, env, stored.baseURL())
	other := newLLMTestUpstream(t)

	recorder := env.request(t, http.MethodPost, "/api/setup/llm/test", map[string]any{
		"provider":    "custom",
		"base_url":    other.baseURL(),
		"api_key":     "***",
		"fast_model":  "fast-model",
		"smart_model": "fast-model",
	}, true)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	ok, _, message := decodeLLMTestResponse(t, recorder.Body.Bytes())
	if !ok {
		t.Fatalf("ok = false, error = %q", message)
	}
	if auth, _ := other.seen(); auth != "" {
		t.Fatalf("stored key leaked to a different endpoint: Authorization = %q", auth)
	}
	if auth, _ := stored.seen(); auth != "" {
		t.Fatalf("stored endpoint was called: Authorization = %q", auth)
	}
}
