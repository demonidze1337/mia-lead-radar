// Package bot implements the optional self-hosted Owner Control Bot.
package bot

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/demonidze1337/mia-lead-radar/internal/domain"
)

const (
	defaultAPIBase     = "https://api.telegram.org"
	defaultPollTimeout = 25 * time.Second
	defaultHTTPTimeout = 40 * time.Second
	maxMessageRunes    = 3800
)

// Config configures the optional Owner Control Bot.
type Config struct {
	Token           string
	OwnerTelegramID int64
	APIBase         string
	WebAppBaseURL   string
	HTTPClient      *http.Client
	PollTimeout     time.Duration
	Summary         func(ctx context.Context) (domain.FunnelSummary, error)
}

// Manager implements Service using the Telegram Bot API over net/http.
type Manager struct {
	cfg        Config
	controller Controller
	client     *http.Client
	offset     int64
}

var _ Service = (*Manager)(nil)

// New creates the Owner Control Bot manager.
func New(cfg Config, controller Controller) *Manager {
	if strings.TrimSpace(cfg.APIBase) == "" {
		cfg.APIBase = defaultAPIBase
	}
	cfg.APIBase = strings.TrimRight(cfg.APIBase, "/")
	if cfg.PollTimeout <= 0 {
		cfg.PollTimeout = defaultPollTimeout
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: defaultHTTPTimeout}
	}
	return &Manager{cfg: cfg, controller: controller, client: client}
}

// Start long-polls getUpdates until ctx is cancelled.
func (m *Manager) Start(ctx context.Context) error {
	if m == nil || strings.TrimSpace(m.cfg.Token) == "" {
		return fmt.Errorf("%w: owner bot token is required", domain.ErrInvalidInput)
	}
	if m.controller == nil {
		return fmt.Errorf("%w: owner bot controller is required", domain.ErrInvalidInput)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	for {
		updates, err := m.getUpdates(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			if err := sleepContext(ctx, 2*time.Second); err != nil {
				return nil
			}
			continue
		}
		for _, update := range updates {
			m.offset = update.UpdateID + 1
			m.handleUpdate(ctx, update)
		}
		if len(updates) == 0 {
			if err := sleepContext(ctx, 250*time.Millisecond); err != nil {
				return nil
			}
		}
	}
}

// NotifyLead sends a formatted, HTML-escaped lead alert to the owner.
func (m *Manager) NotifyLead(ctx context.Context, lead domain.Lead, radarName string) error {
	if m == nil || !m.enabled() {
		return nil
	}
	var b strings.Builder
	b.WriteString("🎯 <b>New qualified lead</b>\n")
	if radarName != "" {
		b.WriteString("Radar: <b>" + html.EscapeString(radarName) + "</b>\n")
	}
	if lead.ChatTitle != "" {
		b.WriteString("Chat: " + html.EscapeString(lead.ChatTitle) + "\n")
	}
	author := strings.TrimSpace(lead.AuthorName)
	if lead.AuthorUsername != "" {
		author += " (@" + lead.AuthorUsername + ")"
	}
	if author != "" {
		b.WriteString("Author: " + html.EscapeString(author) + "\n")
	}
	b.WriteString("Score: <b>" + strconv.Itoa(lead.Score) + "</b> (" + string(lead.Category) + ")\n")
	if lead.Reason != "" {
		b.WriteString("Reason: " + html.EscapeString(lead.Reason) + "\n")
	}
	if lead.Text != "" {
		b.WriteString("\n<i>" + html.EscapeString(truncateText(lead.Text, 700)) + "</i>")
	}
	var keyboard *inlineKeyboard
	if url := lead.MsgLink; url != "" {
		keyboard = &inlineKeyboard{InlineKeyboard: [][]inlineButton{{{Text: "Open message", URL: url}}}}
	}
	if m.cfg.WebAppBaseURL != "" {
		if keyboard == nil {
			keyboard = &inlineKeyboard{}
		}
		keyboard.InlineKeyboard = append(keyboard.InlineKeyboard, []inlineButton{{Text: "Open Dashboard", URL: m.cfg.WebAppBaseURL}})
	}
	return m.sendMessage(ctx, truncateText(b.String(), maxMessageRunes), keyboard)
}

// NotifyAccountAlert sends an account status or FloodWait alert to the owner.
func (m *Manager) NotifyAccountAlert(ctx context.Context, acc domain.TelegramAccount, message string) error {
	if m == nil || !m.enabled() {
		return nil
	}
	text := "⚠️ <b>Account alert</b>\n" +
		"Account: <b>" + html.EscapeString(acc.Label) + "</b>\n" +
		"Status: " + html.EscapeString(string(acc.Status)) + "\n" +
		html.EscapeString(message)
	return m.sendMessage(ctx, truncateText(text, maxMessageRunes), nil)
}

func (m *Manager) enabled() bool {
	return strings.TrimSpace(m.cfg.Token) != "" && m.cfg.OwnerTelegramID > 0
}

func (m *Manager) getUpdates(ctx context.Context) ([]botUpdate, error) {
	payload := map[string]any{
		"offset":          m.offset,
		"timeout":         int(m.cfg.PollTimeout.Seconds()),
		"allowed_updates": []string{"message"},
	}
	var response updatesResponse
	if err := m.call(ctx, "getUpdates", payload, &response); err != nil {
		return nil, err
	}
	return response.Result, nil
}

func (m *Manager) handleUpdate(ctx context.Context, update botUpdate) {
	if update.Message == nil {
		return
	}
	sender := update.Message.Chat.ID
	if update.Message.From != nil {
		sender = update.Message.From.ID
	}
	if m.cfg.OwnerTelegramID <= 0 || sender != m.cfg.OwnerTelegramID {
		// Strangers are ignored without any reply or data leakage.
		return
	}
	text := strings.TrimSpace(update.Message.Text)
	if !strings.HasPrefix(text, "/") {
		return
	}
	command := strings.Fields(text)[0]
	if idx := strings.IndexByte(command, '@'); idx >= 0 {
		command = command[:idx]
	}
	switch command {
	case "/start", "/help":
		_ = m.sendMessage(ctx, helpText(), nil)
	case "/status":
		_ = m.sendMessage(ctx, m.statusText(), nil)
	case "/pause":
		m.controller.Pause()
		_ = m.sendMessage(ctx, "⏸ Pipeline paused.", nil)
	case "/resume":
		m.controller.Resume()
		_ = m.sendMessage(ctx, "▶️ Pipeline resumed.", nil)
	case "/stats":
		_ = m.sendMessage(ctx, m.statsText(ctx), nil)
	default:
		_ = m.sendMessage(ctx, "Unknown command.\n\n"+helpText(), nil)
	}
}

func (m *Manager) statusText() string {
	stats := m.controller.Stats()
	state := "running"
	if stats.Paused {
		state = "paused"
	}
	if !stats.Running {
		state = "stopped"
	}
	return fmt.Sprintf("📡 <b>MIA Lead Radar</b>\nState: <b>%s</b>\nAccounts: %d active, %d flooded\nProcessed: %d messages\nQualified leads: %d\nLast error: %s",
		state, stats.ActiveAccounts, stats.FloodedAccount, stats.ProcessedMsgs, stats.QualifiedLeads,
		html.EscapeString(firstNonEmpty(stats.LastError, "none")))
}

func (m *Manager) statsText(ctx context.Context) string {
	if m.cfg.Summary == nil {
		return "📊 Funnel summary is not configured."
	}
	summary, err := m.cfg.Summary(ctx)
	if err != nil {
		return "📊 Failed to load funnel summary: " + html.EscapeString(err.Error())
	}
	return fmt.Sprintf("📊 <b>Funnel summary</b>\nMessages: %d\nTier-0 passed: %d\nTier-0 rejected: %d\nLeads: %d (potential %d, clear %d)\nTrash: %d (%.1f%%)\nNoise Guard: %t",
		summary.MessagesSeen, summary.Tier0Passed, summary.Tier0Rejected, summary.TotalLeads,
		summary.PotentialCount, summary.ClearCount, summary.TrashCount, summary.TrashRatioPct,
		summary.NoiseGuardTriggered)
}

func helpText() string {
	return "🤖 <b>Owner commands</b>\n/status - pipeline and account status\n/pause - pause scanning and joining\n/resume - resume work\n/stats - funnel summary"
}

func (m *Manager) sendMessage(ctx context.Context, text string, keyboard *inlineKeyboard) error {
	payload := map[string]any{
		"chat_id":                  m.cfg.OwnerTelegramID,
		"text":                     text,
		"parse_mode":               "HTML",
		"disable_web_page_preview": true,
	}
	if keyboard != nil && len(keyboard.InlineKeyboard) > 0 {
		payload["reply_markup"] = keyboard
	}
	var response apiResponse
	return m.call(ctx, "sendMessage", payload, &response)
}

func (m *Manager) call(ctx context.Context, method string, payload any, target any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("owner bot: encode %s: %w", method, err)
	}
	endpoint := fmt.Sprintf("%s/bot%s/%s", m.cfg.APIBase, m.cfg.Token, method)
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("owner bot: build %s: %w", method, redactTokenErr(err, m.cfg.Token))
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := m.client.Do(request)
	if err != nil {
		return fmt.Errorf("owner bot: %s: %w", method, redactTokenErr(err, m.cfg.Token))
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("owner bot: read %s: %w", method, err)
	}
	var envelope struct {
		OK          bool   `json:"ok"`
		Description string `json:"description"`
	}
	_ = json.Unmarshal(data, &envelope)
	if response.StatusCode < 200 || response.StatusCode >= 300 || !envelope.OK {
		description := strings.TrimSpace(envelope.Description)
		if description == "" {
			description = strings.TrimSpace(string(data))
		}
		return fmt.Errorf("owner bot: %s failed (HTTP %d): %s", method, response.StatusCode, description)
	}
	if target != nil {
		if err := json.Unmarshal(data, target); err != nil {
			return fmt.Errorf("owner bot: decode %s: %w", method, err)
		}
	}
	return nil
}

func sleepContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func truncateText(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	if limit <= 1 {
		return string(runes[:limit])
	}
	return string(runes[:limit-1]) + "…"
}

func redactTokenErr(err error, token string) error {
	var urlErr *url.Error
	if errors.As(err, &urlErr) && urlErr.Err != nil {
		return urlErr.Err
	}
	if token != "" && strings.Contains(err.Error(), token) {
		return errors.New(strings.ReplaceAll(err.Error(), token, "[REDACTED]"))
	}
	return err
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

type botUpdate struct {
	UpdateID int64 `json:"update_id"`
	Message  *struct {
		MessageID int `json:"message_id"`
		From      *struct {
			ID int64 `json:"id"`
		} `json:"from"`
		Chat struct {
			ID int64 `json:"id"`
		} `json:"chat"`
		Text string `json:"text"`
	} `json:"message"`
}

type updatesResponse struct {
	OK     bool        `json:"ok"`
	Result []botUpdate `json:"result"`
}

type apiResponse struct {
	OK bool `json:"ok"`
}

type inlineKeyboard struct {
	InlineKeyboard [][]inlineButton `json:"inline_keyboard"`
}

type inlineButton struct {
	Text string `json:"text"`
	URL  string `json:"url,omitempty"`
}
