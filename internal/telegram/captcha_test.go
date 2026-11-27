package telegram

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/demonidze1337/mia-lead-radar/internal/llm"
	"github.com/demonidze1337/mia-lead-radar/internal/prompts"
)

type captchaProvider struct {
	answer string
	err    error
}

func (p *captchaProvider) Complete(context.Context, llm.ChatRequest) (llm.ChatResponse, error) {
	return llm.ChatResponse{Content: "{}"}, p.err
}

func (p *captchaProvider) CompleteJSON(_ context.Context, _ llm.ChatRequest, target any) (llm.ChatResponse, error) {
	if p.err != nil {
		return llm.ChatResponse{}, p.err
	}
	result, ok := target.(*prompts.VisionCaptchaResult)
	if !ok {
		return llm.ChatResponse{}, errors.New("unexpected target")
	}
	result.Answer = p.answer
	result.Confidence = 0.9
	return llm.ChatResponse{Content: `{"answer":"` + p.answer + `"}`, Model: "vision"}, nil
}

func (p *captchaProvider) Ping(context.Context) error { return nil }
func (p *captchaProvider) ListModels(context.Context) ([]string, error) {
	return []string{"vision"}, nil
}

func TestCaptchaArithmeticButton(t *testing.T) {
	var pressed []byte
	transport := &mockTransport{
		id: 1,
		pressButton: func(_ context.Context, _ int64, _ int64, _ int, data []byte) error {
			pressed = data
			return nil
		},
	}
	solver := NewCaptchaSolver(transport, nil, nil)
	msg := IncomingMessage{
		TGChatID: 10, MsgID: 5, AccessHash: 2,
		Text:    "Please solve captcha: 3 + 7 = ?",
		Buttons: []CaptchaButton{{Text: "10", CallbackData: []byte("ten")}},
	}
	solved, err := solver.Solve(context.Background(), msg)
	if err != nil || !solved {
		t.Fatalf("Solve = %v, %v", solved, err)
	}
	if string(pressed) != "ten" {
		t.Fatalf("pressed = %q", pressed)
	}
}

func TestCaptchaArithmeticReplyAndDuplicate(t *testing.T) {
	var replies []string
	transport := &mockTransport{
		id: 1,
		sendReply: func(_ context.Context, _ int64, _ int64, _ int, text string) error {
			replies = append(replies, text)
			return nil
		},
	}
	solver := NewCaptchaSolver(transport, nil, nil)
	msg := IncomingMessage{TGChatID: 11, MsgID: 8, Text: "Verification: 12 - 5 = ?"}
	if solved, err := solver.Solve(context.Background(), msg); err != nil || !solved {
		t.Fatalf("Solve = %v, %v", solved, err)
	}
	if len(replies) != 1 || replies[0] != "7" {
		t.Fatalf("replies = %v", replies)
	}
	if solved, err := solver.Solve(context.Background(), msg); err != nil || solved {
		t.Fatalf("duplicate Solve = %v, %v", solved, err)
	}
	if len(replies) != 1 {
		t.Fatalf("duplicate produced reply: %v", replies)
	}
}

func TestCaptchaVerificationButtonRussian(t *testing.T) {
	var pressed bool
	transport := &mockTransport{
		id: 1,
		pressButton: func(context.Context, int64, int64, int, []byte) error {
			pressed = true
			return nil
		},
	}
	solver := NewCaptchaSolver(transport, nil, nil)
	msg := IncomingMessage{
		TGChatID: 12, MsgID: 9,
		Text:    "Подтвердите, что вы не робот",
		Buttons: []CaptchaButton{{Text: "Я не робот", CallbackData: []byte("y")}},
	}
	solved, err := solver.Solve(context.Background(), msg)
	if err != nil || !solved || !pressed {
		t.Fatalf("Solve = %v, %v pressed=%v", solved, err, pressed)
	}
}

func TestCaptchaNonChallengeIgnored(t *testing.T) {
	calls := 0
	transport := &mockTransport{
		pressButton: func(context.Context, int64, int64, int, []byte) error { calls++; return nil },
		sendReply:   func(context.Context, int64, int64, int, string) error { calls++; return nil },
	}
	solver := NewCaptchaSolver(transport, nil, nil)
	if solved, err := solver.Solve(context.Background(), IncomingMessage{TGChatID: 1, MsgID: 1, Text: "Hello everyone, need a website"}); err != nil || solved {
		t.Fatalf("non-captcha Solve = %v, %v", solved, err)
	}
	if calls != 0 {
		t.Fatalf("unexpected calls = %d", calls)
	}
}

func TestCaptchaVisionFallback(t *testing.T) {
	var pressed []byte
	transport := &mockTransport{
		id: 3,
		downloadImage: func(context.Context, int64, int64, int) (string, error) {
			return "data:image/png;base64,abc", nil
		},
		pressButton: func(_ context.Context, _ int64, _ int64, _ int, data []byte) error {
			pressed = data
			return nil
		},
	}
	solver := NewCaptchaSolver(transport, nil, &captchaProvider{answer: "42"})
	msg := IncomingMessage{
		TGChatID: 20, MsgID: 4, HasPhoto: true, Text: "captcha",
		Buttons: []CaptchaButton{{Text: "42", CallbackData: []byte("answer")}},
	}
	solved, err := solver.Solve(context.Background(), msg)
	if err != nil || !solved || string(pressed) != "answer" {
		t.Fatalf("vision Solve = %v, %v pressed=%q", solved, err, pressed)
	}
}

func TestCaptchaVisionReplyAndFailure(t *testing.T) {
	var reply string
	transport := &mockTransport{
		id: 4,
		downloadImage: func(context.Context, int64, int64, int) (string, error) {
			return "data:image/png;base64,abc", nil
		},
		sendReply: func(_ context.Context, _ int64, _ int64, _ int, text string) error {
			reply = text
			return nil
		},
	}
	solver := NewCaptchaSolver(transport, nil, &captchaProvider{answer: "blue"})
	solved, err := solver.Solve(context.Background(), IncomingMessage{TGChatID: 21, MsgID: 2, HasPhoto: true, Text: "verify captcha"})
	if err != nil || !solved || reply != "blue" {
		t.Fatalf("vision reply = %v, %v %q", solved, err, reply)
	}
	failing := NewCaptchaSolver(transport, nil, &captchaProvider{err: errors.New("llm down")})
	if _, err := failing.Solve(context.Background(), IncomingMessage{TGChatID: 22, MsgID: 3, HasPhoto: true, Text: "verify you are human"}); err == nil {
		t.Fatal("expected vision error")
	}
}

func TestCaptchaPressErrorAndSeenBound(t *testing.T) {
	transport := &mockTransport{
		id: 5,
		pressButton: func(context.Context, int64, int64, int, []byte) error {
			return errors.New("press failed")
		},
	}
	solver := NewCaptchaSolver(transport, nil, nil)
	if _, err := solver.Solve(context.Background(), IncomingMessage{
		TGChatID: 30, MsgID: 1, Text: "verify you are human",
		Buttons: []CaptchaButton{{Text: "Verify", CallbackData: []byte("v")}},
	}); err == nil {
		t.Fatal("expected press error")
	}

	solver.maxSeen = 2
	solver.now = func() time.Time { return time.Unix(1000, 0) }
	for i := 0; i < 5; i++ {
		transport := &mockTransport{id: 6, sendReply: func(context.Context, int64, int64, int, string) error { return nil }}
		s := NewCaptchaSolver(transport, nil, nil)
		s.maxSeen = 2
		s.now = func() time.Time { return time.Unix(1000, 0) }
		_, _ = s.Solve(context.Background(), IncomingMessage{TGChatID: int64(i + 1), MsgID: 1, Text: "captcha 1 + 1 = ?"})
		if len(s.seen) > 2 {
			t.Fatalf("seen cache grew to %d", len(s.seen))
		}
	}
}

func TestSolveArithmetic(t *testing.T) {
	tests := map[string]string{
		"3 + 7 = ?": "10",
		"12 - 5":    "7",
		"4 * 3":     "12",
		"20 / 4":    "5",
		"2+2":       "4",
		"1 / 0":     "",
		"no math":   "",
	}
	for input, want := range tests {
		got, ok := solveArithmetic(input)
		if want == "" {
			if ok {
				t.Errorf("solveArithmetic(%q) = %q, want failure", input, got)
			}
			continue
		}
		if !ok || got != want {
			t.Errorf("solveArithmetic(%q) = %q/%v, want %q", input, got, ok, want)
		}
	}
}
