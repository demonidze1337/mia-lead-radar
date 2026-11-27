package telegram

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/demonidze1337/mia-lead-radar/internal/llm"
	"github.com/demonidze1337/mia-lead-radar/internal/prompts"
)

var (
	mathChallengePattern = regexp.MustCompile(`(-?\d{1,6})\s*([+\-*/])\s*(-?\d{1,6})`)
	verificationMarkers  = []string{
		"captcha", "verification", "verify you are human", "not a robot", "are you human",
		"капча", "подтвердите", "подтвердить", "вы не робот", "не робот", "проверка",
		"验证码", "验证", "認証", "確認", "인증", "로봇이 아닙니다",
	}
	safeButtonTexts = []string{
		"i am human", "i'm human", "im human", "not a robot", "verify", "confirm",
		"continue", "start", "check", "human", "click", "я не робот", "подтвердить",
		"продолжить", "проверить", "验证", "确认", "認証", "確認", "인증", "확인",
	}
)

// CaptchaSolver detects and solves Telegram verification challenges with a
// bounded seen-cache to prevent infinite loops on the same message.
type CaptchaSolver struct {
	transport AccountTransport
	governor  *RateGovernor
	provider  llm.Provider
	now       func() time.Time
	ttl       time.Duration
	maxSeen   int

	mu   sync.Mutex
	seen map[string]time.Time
}

// NewCaptchaSolver builds a solver bound to one account transport.
func NewCaptchaSolver(transport AccountTransport, governor *RateGovernor, provider llm.Provider) *CaptchaSolver {
	return &CaptchaSolver{
		transport: transport,
		governor:  governor,
		provider:  provider,
		now:       time.Now,
		ttl:       10 * time.Minute,
		maxSeen:   1000,
		seen:      make(map[string]time.Time),
	}
}

// Solve attempts to resolve a verification challenge. It returns true when a
// button press or reply was successfully sent.
func (c *CaptchaSolver) Solve(ctx context.Context, msg IncomingMessage) (bool, error) {
	if c == nil || c.transport == nil {
		return false, fmt.Errorf("captcha solver is not configured")
	}
	if msg.MsgID <= 0 || !isVerificationPrompt(msg) {
		return false, nil
	}
	key := fmt.Sprintf("%d:%d", msg.TGChatID, msg.MsgID)
	if c.alreadySeen(key) {
		return false, nil
	}
	if c.governor != nil {
		if err := c.governor.Wait(ctx, c.transport.AccountID(), OpCaptcha); err != nil {
			return false, err
		}
	}

	if answer, ok := solveArithmetic(msg.Text); ok {
		return c.deliverAnswer(ctx, msg, answer)
	}
	if button, ok := findSafeButton(msg.Buttons); ok {
		return true, c.press(ctx, msg, button)
	}
	if c.provider != nil && (msg.HasPhoto || msg.CaptchaImageURL != "") {
		answer, err := c.solveWithVision(ctx, msg)
		if err != nil {
			return false, err
		}
		if strings.TrimSpace(answer) != "" {
			return c.deliverAnswer(ctx, msg, answer)
		}
	}
	return false, nil
}

func (c *CaptchaSolver) deliverAnswer(ctx context.Context, msg IncomingMessage, answer string) (bool, error) {
	answer = strings.TrimSpace(answer)
	if answer == "" {
		return false, nil
	}
	for _, button := range msg.Buttons {
		if strings.Contains(strings.ToLower(button.Text), strings.ToLower(answer)) {
			return true, c.press(ctx, msg, button)
		}
	}
	if err := c.transport.SendReply(ctx, msg.TGChatID, msg.AccessHash, msg.MsgID, answer); err != nil {
		return false, err
	}
	return true, nil
}

func (c *CaptchaSolver) press(ctx context.Context, msg IncomingMessage, button CaptchaButton) error {
	if len(button.CallbackData) > 0 {
		return c.transport.PressButton(ctx, msg.TGChatID, msg.AccessHash, msg.MsgID, button.CallbackData)
	}
	return c.transport.SendReply(ctx, msg.TGChatID, msg.AccessHash, msg.MsgID, button.Text)
}

func (c *CaptchaSolver) solveWithVision(ctx context.Context, msg IncomingMessage) (string, error) {
	imageURL := msg.CaptchaImageURL
	if imageURL == "" {
		dataURL, err := c.transport.DownloadMessageImage(ctx, msg.TGChatID, msg.AccessHash, msg.MsgID)
		if err != nil {
			return "", err
		}
		imageURL = dataURL
	}
	if imageURL == "" {
		return "", nil
	}
	var result prompts.VisionCaptchaResult
	_, err := c.provider.CompleteJSON(ctx, llm.ChatRequest{
		Tier:        llm.TierVision,
		Messages:    prompts.BuildVisionCaptchaMessages(msg.Text, imageURL),
		Temperature: 0,
		Schema:      prompts.VisionCaptchaSchema(),
	}, &result)
	if err != nil {
		return "", err
	}
	return result.Answer, nil
}

func (c *CaptchaSolver) alreadySeen(key string) bool {
	now := c.now().UTC()
	c.mu.Lock()
	defer c.mu.Unlock()
	if seenAt, ok := c.seen[key]; ok && now.Sub(seenAt) < c.ttl {
		return true
	}
	if len(c.seen) >= c.maxSeen {
		for k, seenAt := range c.seen {
			if now.Sub(seenAt) >= c.ttl {
				delete(c.seen, k)
			}
		}
		if len(c.seen) >= c.maxSeen {
			for k := range c.seen {
				delete(c.seen, k)
				break
			}
		}
	}
	c.seen[key] = now
	return false
}

func isVerificationPrompt(msg IncomingMessage) bool {
	text := strings.ToLower(msg.Text)
	for _, marker := range verificationMarkers {
		if strings.Contains(text, marker) {
			return true
		}
	}
	if match := mathChallengePattern.FindStringSubmatch(msg.Text); len(match) > 0 && (strings.Contains(msg.Text, "?") || strings.Contains(msg.Text, "=")) {
		return true
	}
	for _, button := range msg.Buttons {
		if _, ok := findSafeButton([]CaptchaButton{button}); ok {
			return true
		}
	}
	return false
}

func findSafeButton(buttons []CaptchaButton) (CaptchaButton, bool) {
	for _, button := range buttons {
		text := strings.ToLower(strings.TrimSpace(button.Text))
		if text == "" {
			continue
		}
		for _, marker := range safeButtonTexts {
			if strings.Contains(text, marker) {
				return button, true
			}
		}
	}
	return CaptchaButton{}, false
}

func solveArithmetic(text string) (string, bool) {
	match := mathChallengePattern.FindStringSubmatch(text)
	if len(match) != 4 {
		return "", false
	}
	left, err := strconv.Atoi(match[1])
	if err != nil {
		return "", false
	}
	right, err := strconv.Atoi(match[3])
	if err != nil {
		return "", false
	}
	switch match[2] {
	case "+":
		return strconv.Itoa(left + right), true
	case "-":
		return strconv.Itoa(left - right), true
	case "*":
		return strconv.Itoa(left * right), true
	case "/":
		if right == 0 {
			return "", false
		}
		return strconv.Itoa(left / right), true
	default:
		return "", false
	}
}
