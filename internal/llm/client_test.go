package llm

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/demonidze1337/mia-lead-radar/internal/domain"
)

func testConfig(base string) domain.LLMConfig {
	return domain.LLMConfig{
		Provider:   domain.LLMProviderCustom,
		BaseURL:    base,
		FastModel:  "fast-model",
		SmartModel: "smart-model",
		MaxRetries: 0,
	}
}

func writeCompletion(w http.ResponseWriter, content, model string) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"model": model,
		"choices": []map[string]any{
			{"message": map[string]any{"role": "assistant", "content": content}},
		},
		"usage": map[string]any{"prompt_tokens": 11, "completion_tokens": 7},
	})
}

func TestResolveEndpoints(t *testing.T) {
	tests := []struct {
		name       string
		base       string
		provider   domain.LLMProviderType
		wantChat   string
		wantModels string
		ollama     bool
	}{
		{"root host", "http://example.com", domain.LLMProviderCustom, "http://example.com/v1/chat/completions", "http://example.com/v1/models", false},
		{"v1 base", "http://example.com/v1/", domain.LLMProviderOpenAI, "http://example.com/v1/chat/completions", "http://example.com/v1/models", false},
		{"openai suffix", "http://example.com/openai", domain.LLMProviderGemini, "http://example.com/openai/chat/completions", "http://example.com/openai/models", false},
		{"full endpoint", "http://example.com/v1/chat/completions", domain.LLMProviderCustom, "http://example.com/v1/chat/completions", "http://example.com/v1/models", false},
		{"ollama root", "http://localhost:11434", domain.LLMProviderOllama, "http://localhost:11434/v1/chat/completions", "http://localhost:11434/v1/models", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			chat, models, tags, isOllama, err := resolveEndpoints(tc.base, tc.provider)
			if err != nil {
				t.Fatalf("resolveEndpoints: %v", err)
			}
			if chat != tc.wantChat || models != tc.wantModels {
				t.Fatalf("chat=%q models=%q, want %q/%q", chat, models, tc.wantChat, tc.wantModels)
			}
			if isOllama != tc.ollama {
				t.Errorf("isOllama = %v, want %v", isOllama, tc.ollama)
			}
			if tc.ollama && !strings.HasSuffix(tags, "/api/tags") {
				t.Errorf("tags URL = %q", tags)
			}
		})
	}
}

func TestCompleteTierRoutingAndPayload(t *testing.T) {
	var mu sync.Mutex
	models := make([]string, 0, 3)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Model string `json:"model"`
		}
		_ = json.NewDecoder(r.Body).Decode(&payload)
		mu.Lock()
		models = append(models, payload.Model)
		mu.Unlock()
		writeCompletion(w, "ok", payload.Model)
	}))
	defer server.Close()

	client, err := NewClient(testConfig(server.URL), WithHTTPClient(server.Client()))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	for _, tier := range []ModelTier{TierFast, TierSmart} {
		resp, err := client.Complete(context.Background(), ChatRequest{Tier: tier, Messages: []Message{{Role: "user", Content: "hi"}}})
		if err != nil {
			t.Fatalf("Complete(%s): %v", tier, err)
		}
		if resp.Content != "ok" || resp.PromptTokens != 11 || resp.CompletionTokens != 7 {
			t.Fatalf("unexpected response: %+v", resp)
		}
	}
	mu.Lock()
	got := append([]string{}, models...)
	mu.Unlock()
	if len(got) != 2 || got[0] != "fast-model" || got[1] != "smart-model" {
		t.Fatalf("routing = %#v", got)
	}

	fallbackCfg := testConfig(server.URL)
	fallbackCfg.SmartModel = ""
	fallbackCfg.VisionModel = ""
	fallback, err := NewClient(fallbackCfg, WithHTTPClient(server.Client()))
	if err != nil {
		t.Fatalf("NewClient fallback: %v", err)
	}
	if _, err := fallback.Complete(context.Background(), ChatRequest{Tier: TierSmart, Messages: []Message{{Role: "user", Content: "hi"}}}); err != nil {
		t.Fatalf("smart fallback: %v", err)
	}
	if _, err := fallback.Complete(context.Background(), ChatRequest{Tier: TierVision, Messages: []Message{{Role: "user", Content: "hi"}}}); err != nil {
		t.Fatalf("vision fallback: %v", err)
	}
}

func TestSchemaFallbackSequence(t *testing.T) {
	var mu sync.Mutex
	modes := make([]string, 0, 3)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			ResponseFormat *struct {
				Type string `json:"type"`
			} `json:"response_format"`
		}
		_ = json.NewDecoder(r.Body).Decode(&payload)
		mode := ""
		if payload.ResponseFormat != nil {
			mode = payload.ResponseFormat.Type
		}
		mu.Lock()
		modes = append(modes, mode)
		attempt := len(modes)
		mu.Unlock()
		if attempt <= 2 {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"message":"response_format unsupported"}}`))
			return
		}
		writeCompletion(w, `{"status":"ok","score":5}`, "smart-model")
	}))
	defer server.Close()

	client, err := NewClient(testConfig(server.URL), WithHTTPClient(server.Client()))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	var target struct {
		Status string `json:"status"`
		Score  int    `json:"score"`
	}
	_, err = client.CompleteJSON(context.Background(), ChatRequest{
		Tier:     TierSmart,
		Messages: []Message{{Role: "user", Content: "qualify"}},
		Schema:   &ResponseSchema{Name: "test", Strict: true, Schema: map[string]any{"type": "object"}},
	}, &target)
	if err != nil {
		t.Fatalf("CompleteJSON: %v", err)
	}
	if target.Status != "ok" || target.Score != 5 {
		t.Fatalf("target = %+v", target)
	}
	mu.Lock()
	got := append([]string{}, modes...)
	mu.Unlock()
	want := []string{"json_schema", "json_object", ""}
	if len(got) != len(want) {
		t.Fatalf("modes = %#v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("modes = %#v, want %#v", got, want)
		}
	}
}

func TestJSONObjectFallbackWithoutSchema(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		var payload struct {
			ResponseFormat *struct {
				Type string `json:"type"`
			} `json:"response_format"`
		}
		_ = json.NewDecoder(r.Body).Decode(&payload)
		if payload.ResponseFormat != nil {
			w.WriteHeader(http.StatusUnprocessableEntity)
			return
		}
		writeCompletion(w, `{"ok":true}`, "fast-model")
	}))
	defer server.Close()

	client, err := NewClient(testConfig(server.URL), WithHTTPClient(server.Client()))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	var target struct {
		OK bool `json:"ok"`
	}
	if _, err := client.CompleteJSON(context.Background(), ChatRequest{Messages: []Message{{Role: "user", Content: "json"}}}, &target); err != nil {
		t.Fatalf("CompleteJSON: %v", err)
	}
	if attempts != 2 || !target.OK {
		t.Fatalf("attempts=%d target=%+v", attempts, target)
	}
}

func TestCompleteJSONThinkFenceAndBalanced(t *testing.T) {
	content := "Reasoning first: <think data=\"x\">ignore {bad} json</think >\n" +
		"```json\n{\"score\": 87, \"reason\": \"brace } bracket ] ok\", \"items\": [1, 2, {\"nested\": true}]}\n```\nDone."
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeCompletion(w, content, "smart-model")
	}))
	defer server.Close()
	client, err := NewClient(testConfig(server.URL), WithHTTPClient(server.Client()))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	var target struct {
		Score  int    `json:"score"`
		Reason string `json:"reason"`
		Items  []any  `json:"items"`
	}
	if _, err := client.CompleteJSON(context.Background(), ChatRequest{Messages: []Message{{Role: "user", Content: "x"}}}, &target); err != nil {
		t.Fatalf("CompleteJSON: %v", err)
	}
	if target.Score != 87 || target.Reason != "brace } bracket ] ok" || len(target.Items) != 3 {
		t.Fatalf("target = %+v", target)
	}

	prose := "Here is the result: {\"a\":{\"b\":[1,2]},\"s\":\"ends }\"} trailing prose."
	server2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeCompletion(w, prose, "fast-model")
	}))
	defer server2.Close()
	client2, err := NewClient(testConfig(server2.URL), WithHTTPClient(server2.Client()))
	if err != nil {
		t.Fatalf("NewClient2: %v", err)
	}
	var mapTarget map[string]any
	if _, err := client2.CompleteJSON(context.Background(), ChatRequest{Messages: []Message{{Role: "user", Content: "x"}}}, &mapTarget); err != nil {
		t.Fatalf("CompleteJSON prose: %v", err)
	}
	if _, ok := mapTarget["a"]; !ok {
		t.Fatalf("map target = %#v", mapTarget)
	}
}

func TestMultimodalPayload(t *testing.T) {
	var captured struct {
		Model    string `json:"model"`
		Messages []struct {
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&captured)
		writeCompletion(w, "solved", "vision-model")
	}))
	defer server.Close()

	cfg := testConfig(server.URL)
	cfg.VisionModel = "vision-model"
	client, err := NewClient(cfg, WithHTTPClient(server.Client()))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	_, err = client.Complete(context.Background(), ChatRequest{
		Tier: TierVision,
		Messages: []Message{
			{Role: "system", Content: "solve"},
			{Role: "user", Content: "what number?", ImageURL: "data:image/png;base64,abc"},
		},
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if captured.Model != "vision-model" {
		t.Fatalf("model = %q", captured.Model)
	}
	if len(captured.Messages) != 2 {
		t.Fatalf("messages = %d", len(captured.Messages))
	}
	var parts []contentPart
	if err := json.Unmarshal(captured.Messages[1].Content, &parts); err != nil {
		t.Fatalf("content is not multimodal: %s (%v)", captured.Messages[1].Content, err)
	}
	if len(parts) != 2 || parts[0].Type != "text" || parts[1].ImageURL == nil || parts[1].ImageURL.URL != "data:image/png;base64,abc" {
		t.Fatalf("parts = %+v", parts)
	}
}

func TestRetriesAndAuthHandling(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 3 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		writeCompletion(w, "recovered", "fast-model")
	}))
	defer server.Close()

	client, err := NewClient(testConfig(server.URL),
		WithHTTPClient(server.Client()),
		WithRetryPolicy(3, time.Millisecond, 5*time.Millisecond),
		WithJitter(func(time.Duration) time.Duration { return 0 }),
	)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	resp, err := client.Complete(context.Background(), ChatRequest{Messages: []Message{{Role: "user", Content: "hi"}}})
	if err != nil || resp.Content != "recovered" {
		t.Fatalf("Complete = %+v, %v", resp, err)
	}
	if attempts != 3 {
		t.Fatalf("attempts = %d, want 3", attempts)
	}

	var authSeen string
	authServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authSeen = r.Header.Get("Authorization")
		writeCompletion(w, "ok", "fast-model")
	}))
	defer authServer.Close()
	cfg := testConfig(authServer.URL)
	cfg.APIKey = "secret-key"
	authClient, err := NewClient(cfg, WithHTTPClient(authServer.Client()))
	if err != nil {
		t.Fatalf("NewClient auth: %v", err)
	}
	if _, err := authClient.Complete(context.Background(), ChatRequest{Messages: []Message{{Role: "user", Content: "hi"}}}); err != nil {
		t.Fatalf("auth Complete: %v", err)
	}
	if authSeen != "Bearer secret-key" {
		t.Fatalf("Authorization = %q", authSeen)
	}
}

func TestNoRetryOnUnauthorized(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"bad key"}}`))
	}))
	defer server.Close()
	client, err := NewClient(testConfig(server.URL),
		WithHTTPClient(server.Client()),
		WithRetryPolicy(5, time.Millisecond, 2*time.Millisecond),
		WithJitter(func(time.Duration) time.Duration { return 0 }),
	)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if _, err := client.Complete(context.Background(), ChatRequest{Messages: []Message{{Role: "user", Content: "hi"}}}); err == nil {
		t.Fatal("expected auth error")
	}
	if attempts != 1 {
		t.Fatalf("attempts = %d, want 1", attempts)
	}
}

func TestRetryCancellation(t *testing.T) {
	first := make(chan struct{})
	var once sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		once.Do(func() { close(first) })
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client, err := NewClient(testConfig(server.URL),
		WithHTTPClient(server.Client()),
		WithRetryPolicy(5, 200*time.Millisecond, 200*time.Millisecond),
		WithJitter(func(time.Duration) time.Duration { return 0 }),
	)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		<-first
		cancel()
	}()
	_, err = client.Complete(ctx, ChatRequest{Messages: []Message{{Role: "user", Content: "hi"}}})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestListModelsPingAndOllamaFallback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{{"id": "model-a"}, {"id": "model-b"}},
		})
	}))
	defer server.Close()
	client, err := NewClient(testConfig(server.URL), WithHTTPClient(server.Client()))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	names, err := client.ListModels(context.Background())
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}
	if len(names) != 2 || names[0] != "model-a" || names[1] != "model-b" {
		t.Fatalf("names = %#v", names)
	}
	if err := client.Ping(context.Background()); err != nil {
		t.Fatalf("Ping: %v", err)
	}

	ollama := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"models": []map[string]any{{"name": "qwen3:4b"}},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer ollama.Close()
	ollamaCfg := testConfig(ollama.URL)
	ollamaCfg.Provider = domain.LLMProviderOllama
	ollamaClient, err := NewClient(ollamaCfg, WithHTTPClient(ollama.Client()))
	if err != nil {
		t.Fatalf("NewClient ollama: %v", err)
	}
	names, err = ollamaClient.ListModels(context.Background())
	if err != nil {
		t.Fatalf("ListModels ollama: %v", err)
	}
	if len(names) != 1 || names[0] != "qwen3:4b" {
		t.Fatalf("ollama names = %#v", names)
	}

	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer failing.Close()
	failingClient, err := NewClient(testConfig(failing.URL), WithHTTPClient(failing.Client()))
	if err != nil {
		t.Fatalf("NewClient failing: %v", err)
	}
	if err := failingClient.Ping(context.Background()); err == nil {
		t.Fatal("expected Ping error")
	}
}

func TestCompleteJSONErrorsAndValidation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeCompletion(w, "not json at all", "fast-model")
	}))
	defer server.Close()
	client, err := NewClient(testConfig(server.URL), WithHTTPClient(server.Client()))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	var target map[string]any
	if _, err := client.CompleteJSON(context.Background(), ChatRequest{Messages: []Message{{Role: "user", Content: "x"}}}, &target); err == nil {
		t.Fatal("expected JSON extraction error")
	}
	if _, err := client.CompleteJSON(context.Background(), ChatRequest{Messages: []Message{{Role: "user", Content: "x"}}}, nil); err == nil {
		t.Fatal("expected nil target error")
	}
	if _, err := NewClient(domain.LLMConfig{Provider: domain.LLMProviderCustom, BaseURL: server.URL}); !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatalf("missing models err = %v", err)
	}
	if _, err := NewClient(domain.LLMConfig{Provider: domain.LLMProviderCustom, BaseURL: "ftp://example.com", FastModel: "x"}); !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatalf("bad scheme err = %v", err)
	}
}

func TestParseRetryAfter(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if got := parseRetryAfter("2", now); got != 2*time.Second {
		t.Errorf("seconds = %v", got)
	}
	when := now.Add(3 * time.Second)
	if got := parseRetryAfter(when.Format(http.TimeFormat), now); got < 2*time.Second || got > 3*time.Second {
		t.Errorf("date = %v", got)
	}
	if got := parseRetryAfter("", now); got != 0 {
		t.Errorf("empty = %v", got)
	}
	if got := parseRetryAfter("garbage", now); got != 0 {
		t.Errorf("garbage = %v", got)
	}
}

func TestCompleteContentArrayResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model": "fast-model",
			"choices": []map[string]any{
				{"message": map[string]any{"content": []map[string]any{
					{"type": "text", "text": `{"ok":true}`},
				}}},
			},
		})
	}))
	defer server.Close()
	client, err := NewClient(testConfig(server.URL), WithHTTPClient(server.Client()))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	var target struct {
		OK bool `json:"ok"`
	}
	if _, err := client.CompleteJSON(context.Background(), ChatRequest{Messages: []Message{{Role: "user", Content: "x"}}}, &target); err != nil {
		t.Fatalf("CompleteJSON content array: %v", err)
	}
	if !target.OK {
		t.Fatalf("target = %+v", target)
	}
}
