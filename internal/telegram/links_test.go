package telegram

import (
	"errors"
	"testing"

	"github.com/demonidze1337/mia-lead-radar/internal/domain"
)

func TestParseLinkForms(t *testing.T) {
	tests := []struct {
		raw        string
		kind       LinkKind
		normalized string
		username   string
		invite     string
		addlist    string
		channelID  int64
		messageID  int
	}{
		{"@DevChat", LinkKindUsername, "https://t.me/devchat", "devchat", "", "", 0, 0},
		{"t.me/devchat", LinkKindUsername, "https://t.me/devchat", "devchat", "", "", 0, 0},
		{"https://t.me/DevChat?utm_source=x", LinkKindUsername, "https://t.me/devchat", "devchat", "", "", 0, 0},
		{"https://telegram.me/devchat/", LinkKindUsername, "https://t.me/devchat", "devchat", "", "", 0, 0},
		{"t.me/+AbCdEf123", LinkKindInvite, "https://t.me/+AbCdEf123", "", "AbCdEf123", "", 0, 0},
		{"https://t.me/joinchat/AbCdEf123?join=1", LinkKindInvite, "https://t.me/+AbCdEf123", "", "AbCdEf123", "", 0, 0},
		{"t.me/addlist/summer-2026", LinkKindAddList, "https://t.me/addlist/summer-2026", "", "", "summer-2026", 0, 0},
		{"https://t.me/DevChat/123", LinkKindMessage, "https://t.me/devchat/123", "devchat", "", "", 0, 123},
		{"t.me/c/1234567890/77?single", LinkKindMessage, "https://t.me/c/1234567890/77", "", "", "", 1234567890, 77},
		{"https://telegram.dog/DevChat/9", LinkKindMessage, "https://t.me/devchat/9", "devchat", "", "", 0, 9},
	}
	for _, tc := range tests {
		t.Run(tc.raw, func(t *testing.T) {
			parsed, err := ParseLink(tc.raw)
			if err != nil {
				t.Fatalf("ParseLink: %v", err)
			}
			if parsed.Kind != tc.kind || parsed.Normalized != tc.normalized {
				t.Fatalf("parsed = %+v", parsed)
			}
			if parsed.Username != tc.username || parsed.InviteHash != tc.invite || parsed.AddListSlug != tc.addlist {
				t.Fatalf("fields = %+v", parsed)
			}
			if parsed.ChannelID != tc.channelID || parsed.MessageID != tc.messageID {
				t.Fatalf("ids = %+v", parsed)
			}
		})
	}
}

func TestParseLinkInvalid(t *testing.T) {
	for _, raw := range []string{"", "@", "https://example.com/foo", "t.me/", "t.me/+", "t.me/addlist", "t.me/c/abc/1", "t.me/devchat/not-a-number"} {
		if _, err := ParseLink(raw); !errors.Is(err, domain.ErrInvalidInput) {
			t.Errorf("ParseLink(%q) err = %v", raw, err)
		}
	}
}

func TestExtractLinks(t *testing.T) {
	text := `Join (https://t.me/DevChat), also t.me/+abc123 and @SecondChat! ` +
		`Duplicate https://t.me/devchat?utm=1 and mail me at not@telegram.link.`
	links := ExtractLinks(text)
	if len(links) != 3 {
		t.Fatalf("links = %+v", links)
	}
	if links[0].Username != "devchat" || links[1].InviteHash != "abc123" || links[2].Username != "secondchat" {
		t.Fatalf("unexpected links: %+v", links)
	}
	if got := ExtractLinks("no links here"); len(got) != 0 {
		t.Fatalf("unexpected links: %+v", got)
	}
}

func TestBuildMessageLink(t *testing.T) {
	tests := []struct {
		username string
		chatID   int64
		msgID    int
		want     string
	}{
		{"DevChat", 0, 12, "https://t.me/devchat/12"},
		{"@DevChat", 0, 0, "https://t.me/devchat"},
		{"", -1001234567890, 55, "https://t.me/c/1234567890/55"},
		{"", 123456, 7, "https://t.me/c/123456/7"},
		{"", -1001234567890, 0, "https://t.me/c/1234567890"},
		{"", 0, 3, ""},
	}
	for _, tc := range tests {
		if got := BuildMessageLink(tc.username, tc.chatID, tc.msgID); got != tc.want {
			t.Errorf("BuildMessageLink(%q,%d,%d) = %q, want %q", tc.username, tc.chatID, tc.msgID, got, tc.want)
		}
	}
}
