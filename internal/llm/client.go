package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/demonidze1337/mia-lead-radar/internal/domain"
)

const (
	defaultRequestTimeout = 60 * time.Second
	defaultRetryBase      = 250 * time.Millisecond
	defaultRetryMax       = 5 * time.Second
	maxResponseBody       = 16 << 20
	maxErrorBody          = 600
)

type responseFormatMode int

const (
	formatNone responseFormatMode = iota
	formatJSONObject
	formatJSONSchema
)

type clientOptions struct {
	httpClient *http.Client
	maxRetries int
	retryBase  time.Duration
	retryMax   time.Duration
	jitter     func(time.Duration) time.Duration
}

// Option customizes the OpenAI-compatible client used by NewClient.
type Option func(*clientOptions)

// WithHTTPClient injects a custom HTTP client (useful for tests and proxies).
func WithHTTPClient(client *http.Client) Option {
	return func(opts *clientOptions) {
		if client != nil {
			opts.httpClient = client
		}
	}
}

// WithRetryPolicy overrides retry count and exponential backoff bounds.
func WithRetryPolicy(maxRetries int, base, max time.Duration) Option {
	return func(opts *clientOptions) {
		if maxRetries >= 0 {
			opts.maxRetries = maxRetries
		}
		if base > 0 {
			opts.retryBase = base
		}
		if max > 0 {
			opts.retryMax = max
		}
	}
}

// WithJitter overrides the jitter function applied to retry delays.
func WithJitter(fn func(time.Duration) time.Duration) Option {
	return func(opts *clientOptions) {
		opts.jitter = fn
	}
}

// Client implements Provider for OpenAI-compatible chat completion endpoints.
type Client struct {
	cfg        domain.LLMConfig
	http       *http.Client
	chatURL    string
	modelsURL  string
	tagsURL    string
	isOllama   bool
	maxRetries int
	retryBase  time.Duration
	retryMax   time.Duration
	jitter     func(time.Duration) time.Duration
}

var _ Provider = (*Client)(nil)

// NewClient validates configuration and builds a provider client.
func NewClient(cfg domain.LLMConfig, opts ...Option) (*Client, error) {
	if cfg.Provider == "" {
		cfg.Provider = inferProvider(cfg.BaseURL)
	}
	if strings.TrimSpace(cfg.BaseURL) == "" {
		cfg.BaseURL = defaultBaseURL(cfg.Provider)
	}
	if strings.TrimSpace(cfg.FastModel) == "" && strings.TrimSpace(cfg.SmartModel) == "" && strings.TrimSpace(cfg.VisionModel) == "" {
		return nil, fmt.Errorf("%w: at least one LLM model must be configured", domain.ErrInvalidInput)
	}
	chatURL, modelsURL, tagsURL, isOllama, err := resolveEndpoints(cfg.BaseURL, cfg.Provider)
	if err != nil {
		return nil, err
	}

	options := clientOptions{
		maxRetries: cfg.MaxRetries,
		retryBase:  defaultRetryBase,
		retryMax:   defaultRetryMax,
		jitter:     defaultJitter,
	}
	for _, opt := range opts {
		opt(&options)
	}
	if options.maxRetries < 0 {
		options.maxRetries = 0
	}
	if options.httpClient == nil {
		timeout := defaultRequestTimeout
		if cfg.RequestTimeoutSec > 0 {
			timeout = time.Duration(cfg.RequestTimeoutSec) * time.Second
		}
		options.httpClient = &http.Client{Timeout: timeout}
	}
	cfg.BaseURL = strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	cfg.APIKey = strings.TrimSpace(cfg.APIKey)
	return &Client{
		cfg:        cfg,
		http:       options.httpClient,
		chatURL:    chatURL,
		modelsURL:  modelsURL,
		tagsURL:    tagsURL,
		isOllama:   isOllama,
		maxRetries: options.maxRetries,
		retryBase:  options.retryBase,
		retryMax:   options.retryMax,
		jitter:     options.jitter,
	}, nil
}

func inferProvider(baseURL string) domain.LLMProviderType {
	lower := strings.ToLower(baseURL)
	switch {
	case strings.Contains(lower, "openrouter.ai"):
		return domain.LLMProviderOpenRouter
	case strings.Contains(lower, "api.openai.com"):
		return domain.LLMProviderOpenAI
	case strings.Contains(lower, "generativelanguage.googleapis.com"):
		return domain.LLMProviderGemini
	case strings.Contains(lower, "11434"), strings.Contains(lower, "ollama"):
		return domain.LLMProviderOllama
	default:
		return domain.LLMProviderCustom
	}
}

func defaultBaseURL(provider domain.LLMProviderType) string {
	switch provider {
	case domain.LLMProviderOllama:
		return ollamaBaseURL
	case domain.LLMProviderOpenRouter:
		return "https://openrouter.ai/api/v1"
	case domain.LLMProviderOpenAI:
		return "https://api.openai.com/v1"
	case domain.LLMProviderGemini:
		return "https://generativelanguage.googleapis.com/v1beta/openai"
	default:
		return ""
	}
}

func resolveEndpoints(raw string, provider domain.LLMProviderType) (chatURL, modelsURL, tagsURL string, isOllama bool, err error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", "", "", false, fmt.Errorf("%w: invalid LLM base URL: %v", domain.ErrInvalidInput, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", "", "", false, fmt.Errorf("%w: LLM base URL must use http or https", domain.ErrInvalidInput)
	}
	if u.Host == "" {
		return "", "", "", false, fmt.Errorf("%w: LLM base URL has no host", domain.ErrInvalidInput)
	}
	u.RawQuery = ""
	u.Fragment = ""
	path := strings.TrimRight(u.Path, "/")
	basePath := path
	if strings.HasSuffix(path, "/chat/completions") {
		basePath = strings.TrimSuffix(path, "/chat/completions")
	} else if path == "" {
		switch provider {
		case domain.LLMProviderOpenRouter:
			basePath = "/api/v1"
		case domain.LLMProviderGemini:
			basePath = "/v1beta/openai"
		default:
			basePath = "/v1"
		}
	} else if strings.HasSuffix(path, "/api") {
		basePath = path + "/v1"
	}

	chat := *u
	chat.Path = basePath + "/chat/completions"
	models := *u
	models.Path = basePath + "/models"
	tags := url.URL{Scheme: u.Scheme, Host: u.Host, Path: "/api/tags"}

	host := strings.ToLower(u.Hostname())
	isOllama = provider == domain.LLMProviderOllama || u.Port() == "11434" || strings.Contains(host, "ollama")
	return chat.String(), models.String(), tags.String(), isOllama, nil
}

func defaultJitter(delay time.Duration) time.Duration {
	if delay <= 0 {
		return 0
	}
	half := delay / 2
	if half <= 0 {
		return delay
	}
	return half + time.Duration(rand.Int63n(int64(half)+1))
}

func (c *Client) modelForTier(tier ModelTier) string {
	switch tier {
	case TierSmart:
		if c.cfg.SmartModel != "" {
			return c.cfg.SmartModel
		}
		return c.cfg.FastModel
	case TierVision:
		if c.cfg.VisionModel != "" {
			return c.cfg.VisionModel
		}
		if c.cfg.SmartModel != "" {
			return c.cfg.SmartModel
		}
		return c.cfg.FastModel
	default:
		if c.cfg.FastModel != "" {
			return c.cfg.FastModel
		}
		return c.cfg.SmartModel
	}
}

type wireMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

type contentPart struct {
	Type     string    `json:"type"`
	Text     string    `json:"text,omitempty"`
	ImageURL *imageURL `json:"image_url,omitempty"`
}

type imageURL struct {
	URL string `json:"url"`
}

type wirePayload struct {
	Model          string        `json:"model"`
	Messages       []wireMessage `json:"messages"`
	Temperature    float64       `json:"temperature"`
	MaxTokens      int           `json:"max_tokens,omitempty"`
	ResponseFormat any           `json:"response_format,omitempty"`
}

func (c *Client) buildPayload(model string, req ChatRequest, mode responseFormatMode) ([]byte, error) {
	messages := make([]wireMessage, 0, len(req.Messages))
	for _, msg := range req.Messages {
		content := any(msg.Content)
		if msg.ImageURL != "" {
			parts := make([]contentPart, 0, 2)
			if strings.TrimSpace(msg.Content) != "" {
				parts = append(parts, contentPart{Type: "text", Text: msg.Content})
			}
			parts = append(parts, contentPart{Type: "image_url", ImageURL: &imageURL{URL: msg.ImageURL}})
			content = parts
		}
		messages = append(messages, wireMessage{Role: msg.Role, Content: content})
	}
	payload := wirePayload{
		Model:       model,
		Messages:    messages,
		Temperature: req.Temperature,
		MaxTokens:   req.MaxTokens,
	}
	switch mode {
	case formatJSONSchema:
		name := "response"
		strict := false
		if req.Schema != nil {
			if req.Schema.Name != "" {
				name = req.Schema.Name
			}
			strict = req.Schema.Strict
			payload.ResponseFormat = map[string]any{
				"type": "json_schema",
				"json_schema": map[string]any{
					"name":   name,
					"strict": strict,
					"schema": req.Schema.Schema,
				},
			}
		}
	case formatJSONObject:
		payload.ResponseFormat = map[string]any{"type": "json_object"}
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("llm: encode request: %w", err)
	}
	return raw, nil
}

func nextFormatMode(mode responseFormatMode) responseFormatMode {
	switch mode {
	case formatJSONSchema:
		return formatJSONObject
	case formatJSONObject:
		return formatNone
	default:
		return formatNone
	}
}

// Complete executes a chat completion with tier routing and structured-output fallback.
func (c *Client) Complete(ctx context.Context, req ChatRequest) (ChatResponse, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	model := c.modelForTier(req.Tier)
	if model == "" {
		return ChatResponse{}, fmt.Errorf("%w: no model configured for tier %q", domain.ErrInvalidInput, req.Tier)
	}
	mode := formatNone
	if req.Schema != nil {
		mode = formatJSONSchema
	} else if req.JSONMode {
		mode = formatJSONObject
	}
	start := time.Now()
	for {
		payload, err := c.buildPayload(model, req, mode)
		if err != nil {
			return ChatResponse{}, err
		}
		result, err := c.doWithRetries(ctx, http.MethodPost, c.chatURL, payload)
		if err != nil {
			return ChatResponse{}, err
		}
		if result.status >= 200 && result.status < 300 {
			resp, err := parseChatResponse(result.body)
			if err != nil {
				return ChatResponse{}, err
			}
			if resp.Model == "" {
				resp.Model = model
			}
			resp.LatencyMs = time.Since(start).Milliseconds()
			return resp, nil
		}
		if (result.status == http.StatusBadRequest || result.status == http.StatusUnprocessableEntity) && mode != formatNone {
			mode = nextFormatMode(mode)
			continue
		}
		return ChatResponse{}, httpStatusError(result)
	}
}

// CompleteJSON requests JSON output, extracts the first balanced JSON value, and
// decodes it into target. Reasoning blocks and markdown fences are stripped.
func (c *Client) CompleteJSON(ctx context.Context, req ChatRequest, target any) (ChatResponse, error) {
	if target == nil {
		return ChatResponse{}, fmt.Errorf("%w: nil JSON target", domain.ErrInvalidInput)
	}
	value := reflect.ValueOf(target)
	if value.Kind() != reflect.Ptr || value.IsNil() {
		return ChatResponse{}, fmt.Errorf("%w: JSON target must be a non-nil pointer", domain.ErrInvalidInput)
	}
	if req.Schema == nil {
		req.JSONMode = true
	}
	resp, err := c.Complete(ctx, req)
	if err != nil {
		return ChatResponse{}, err
	}
	if err := decodeJSONContent(resp.Content, target); err != nil {
		return ChatResponse{}, err
	}
	return resp, nil
}

// Ping verifies provider connectivity and credentials via the models endpoint.
func (c *Client) Ping(ctx context.Context) error {
	_, err := c.ListModels(ctx)
	return err
}

// ListModels returns available model identifiers with an Ollama /api/tags fallback.
func (c *Client) ListModels(ctx context.Context) ([]string, error) {
	result, err := c.doWithRetries(ctx, http.MethodGet, c.modelsURL, nil)
	if err == nil && result.status >= 200 && result.status < 300 {
		names, parseErr := parseModelNames(result.body)
		if parseErr == nil && len(names) > 0 {
			return names, nil
		}
		if parseErr == nil && !c.isOllama {
			return names, nil
		}
		if parseErr != nil && !c.isOllama {
			return nil, parseErr
		}
	}
	if !c.isOllama || c.tagsURL == "" {
		if err != nil {
			return nil, err
		}
		if result.status < 200 || result.status >= 300 {
			return nil, httpStatusError(result)
		}
		return []string{}, nil
	}

	tagsResult, tagsErr := c.doWithRetries(ctx, http.MethodGet, c.tagsURL, nil)
	if tagsErr != nil {
		if err != nil {
			return nil, err
		}
		if result.status >= 200 && result.status < 300 {
			return []string{}, nil
		}
		if result.status != 0 {
			return nil, httpStatusError(result)
		}
		return nil, tagsErr
	}
	if tagsResult.status < 200 || tagsResult.status >= 300 {
		if err == nil && result.status >= 200 && result.status < 300 {
			return []string{}, nil
		}
		return nil, httpStatusError(tagsResult)
	}
	names, parseErr := parseModelNames(tagsResult.body)
	if parseErr != nil {
		return nil, parseErr
	}
	return names, nil
}

type httpResult struct {
	status  int
	headers http.Header
	body    []byte
}

func (c *Client) doWithRetries(ctx context.Context, method, endpoint string, payload []byte) (httpResult, error) {
	for attempt := 0; ; attempt++ {
		result, err := c.doOnce(ctx, method, endpoint, payload)
		if err != nil {
			if ctx.Err() != nil {
				return httpResult{}, ctx.Err()
			}
			if attempt >= c.maxRetries {
				return httpResult{}, fmt.Errorf("llm: %s %s: %w", method, endpoint, err)
			}
			if waitErr := sleepContext(ctx, c.backoffDelay(attempt, 0)); waitErr != nil {
				return httpResult{}, waitErr
			}
			continue
		}
		if (result.status == http.StatusTooManyRequests || result.status == http.StatusRequestTimeout || result.status >= 500) && attempt < c.maxRetries {
			delay := c.backoffDelay(attempt, parseRetryAfter(result.headers.Get("Retry-After"), time.Now()))
			if waitErr := sleepContext(ctx, delay); waitErr != nil {
				return httpResult{}, waitErr
			}
			continue
		}
		return result, nil
	}
}

func (c *Client) doOnce(ctx context.Context, method, endpoint string, payload []byte) (httpResult, error) {
	var body io.Reader
	if payload != nil {
		body = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return httpResult{}, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "mia-lead-radar/1.0")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return httpResult{}, err
	}
	defer resp.Body.Close()
	data, err := readLimited(resp.Body, maxResponseBody)
	if err != nil {
		return httpResult{}, err
	}
	return httpResult{status: resp.StatusCode, headers: resp.Header.Clone(), body: data}, nil
}

func (c *Client) backoffDelay(attempt int, retryAfter time.Duration) time.Duration {
	delay := retryAfter
	if delay <= 0 {
		delay = c.retryBase
		for i := 0; i < attempt && delay < c.retryMax; i++ {
			delay *= 2
		}
		if c.jitter != nil {
			delay = c.jitter(delay)
		}
	}
	if delay < 0 {
		delay = 0
	}
	if delay > c.retryMax {
		delay = c.retryMax
	}
	return delay
}

func parseRetryAfter(value string, now time.Time) time.Duration {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	if seconds, err := strconv.Atoi(value); err == nil {
		if seconds <= 0 {
			return 0
		}
		return time.Duration(seconds) * time.Second
	}
	if when, err := http.ParseTime(value); err == nil {
		if when.After(now) {
			return when.Sub(now)
		}
	}
	return 0
}

func sleepContext(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			return nil
		}
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func readLimited(reader io.Reader, limit int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("llm: response exceeds %d bytes", limit)
	}
	return data, nil
}

func parseChatResponse(body []byte) (ChatResponse, error) {
	var payload struct {
		Model   string `json:"model"`
		Choices []struct {
			Message struct {
				Content          json.RawMessage `json:"content"`
				ReasoningContent string          `json:"reasoning_content"`
			} `json:"message"`
			Text string `json:"text"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return ChatResponse{}, fmt.Errorf("llm: decode chat response: %w", err)
	}
	if payload.Error != nil && payload.Error.Message != "" {
		return ChatResponse{}, fmt.Errorf("llm: provider error: %s", payload.Error.Message)
	}
	if len(payload.Choices) == 0 {
		return ChatResponse{}, errors.New("llm: response has no choices")
	}
	content := decodeMessageContent(payload.Choices[0].Message.Content)
	if content == "" {
		content = payload.Choices[0].Text
	}
	return ChatResponse{
		Content:          content,
		Model:            payload.Model,
		PromptTokens:     payload.Usage.PromptTokens,
		CompletionTokens: payload.Usage.CompletionTokens,
	}, nil
}

func decodeMessageContent(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &parts); err == nil {
		var builder strings.Builder
		for _, part := range parts {
			if part.Text != "" {
				builder.WriteString(part.Text)
			}
		}
		return builder.String()
	}
	return string(raw)
}

func parseModelNames(body []byte) ([]string, error) {
	var payload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
		Models []struct {
			Name  string `json:"name"`
			Model string `json:"model"`
		} `json:"models"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("llm: decode models response: %w", err)
	}
	names := make([]string, 0, len(payload.Data)+len(payload.Models))
	for _, item := range payload.Data {
		if item.ID != "" {
			names = append(names, item.ID)
		}
	}
	for _, item := range payload.Models {
		name := item.Name
		if name == "" {
			name = item.Model
		}
		if name != "" {
			names = append(names, name)
		}
	}
	return names, nil
}

func httpStatusError(result httpResult) error {
	body := strings.TrimSpace(string(result.body))
	if len(body) > maxErrorBody {
		body = body[:maxErrorBody] + "..."
	}
	if body == "" {
		return fmt.Errorf("llm: provider returned HTTP %d", result.status)
	}
	return fmt.Errorf("llm: provider returned HTTP %d: %s", result.status, body)
}

var (
	thinkBlockPattern = regexp.MustCompile(`(?is)<think\b[^>]*>.*?</think\s*>|<thinking\b[^>]*>.*?</thinking\s*>`)
	codeFencePattern  = regexp.MustCompile("(?s)```(?:json|JSON|javascript|js)?\\s*(.*?)```")
)

func decodeJSONContent(content string, target any) error {
	cleaned := strings.TrimSpace(strings.TrimPrefix(content, "\ufeff"))
	cleaned = thinkBlockPattern.ReplaceAllString(cleaned, "")
	fenced := extractFencedJSON(cleaned)
	for _, candidate := range []string{fenced, cleaned} {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" || !json.Valid([]byte(candidate)) {
			continue
		}
		if err := json.Unmarshal([]byte(candidate), target); err == nil {
			return nil
		}
	}
	for _, text := range []string{fenced, cleaned} {
		for _, candidate := range balancedJSONCandidates(text) {
			if !json.Valid([]byte(candidate)) {
				continue
			}
			if err := json.Unmarshal([]byte(candidate), target); err == nil {
				return nil
			}
		}
	}
	return errors.New("llm: no valid JSON object or array found in response")
}

func extractFencedJSON(text string) string {
	matches := codeFencePattern.FindAllStringSubmatch(text, -1)
	if len(matches) == 0 {
		return text
	}
	first := ""
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		candidate := strings.TrimSpace(match[1])
		if candidate == "" {
			continue
		}
		if first == "" {
			first = candidate
		}
		if strings.ContainsAny(candidate, "{[") {
			return candidate
		}
	}
	if first != "" {
		return first
	}
	return text
}

func balancedJSONCandidates(text string) []string {
	candidates := make([]string, 0, 2)
	for offset := 0; offset < len(text); {
		relative := strings.IndexAny(text[offset:], "{[")
		if relative < 0 {
			break
		}
		start := offset + relative
		if candidate, ok := balancedJSONAt(text, start); ok {
			candidates = append(candidates, candidate)
			offset = start + len(candidate)
			continue
		}
		offset = start + 1
	}
	return candidates
}

func balancedJSONAt(text string, start int) (string, bool) {
	if start < 0 || start >= len(text) {
		return "", false
	}
	stack := make([]byte, 0, 8)
	inString := false
	escaped := false
	for i := start; i < len(text); i++ {
		ch := text[i]
		if inString {
			switch {
			case escaped:
				escaped = false
			case ch == '\\':
				escaped = true
			case ch == '"':
				inString = false
			}
			continue
		}
		switch ch {
		case '"':
			inString = true
		case '{', '[':
			stack = append(stack, ch)
		case '}', ']':
			if len(stack) == 0 {
				return "", false
			}
			open := stack[len(stack)-1]
			if (open == '{' && ch != '}') || (open == '[' && ch != ']') {
				return "", false
			}
			stack = stack[:len(stack)-1]
			if len(stack) == 0 {
				return text[start : i+1], true
			}
		}
	}
	return "", false
}
