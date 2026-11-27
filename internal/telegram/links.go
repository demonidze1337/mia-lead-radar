package telegram

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/demonidze1337/mia-lead-radar/internal/domain"
)

var telegramHosts = map[string]struct{}{
	"t.me":             {},
	"www.t.me":         {},
	"telegram.me":      {},
	"www.telegram.me":  {},
	"telegram.dog":     {},
	"www.telegram.dog": {},
}

// ParseLink normalizes a t.me/telegram.me/@username reference into canonical fields.
func ParseLink(raw string) (ParsedLink, error) {
	original := strings.TrimSpace(raw)
	cleaned := trimLinkPunctuation(original)
	if cleaned == "" {
		return ParsedLink{}, fmt.Errorf("%w: empty Telegram link", domain.ErrInvalidInput)
	}
	if strings.HasPrefix(cleaned, "@") {
		username := strings.ToLower(strings.TrimPrefix(cleaned, "@"))
		if !validUsername(username) {
			return ParsedLink{}, fmt.Errorf("%w: invalid username %q", domain.ErrInvalidInput, username)
		}
		return ParsedLink{
			Raw:        original,
			Normalized: "https://t.me/" + username,
			Kind:       LinkKindUsername,
			Username:   username,
		}, nil
	}

	candidate := cleaned
	lower := strings.ToLower(candidate)
	if !strings.Contains(lower, "://") {
		candidate = "https://" + strings.TrimLeft(candidate, "/")
	}
	u, err := url.Parse(candidate)
	if err != nil {
		return ParsedLink{}, fmt.Errorf("%w: invalid link: %v", domain.ErrInvalidInput, err)
	}
	host := strings.ToLower(u.Hostname())
	if _, ok := telegramHosts[host]; !ok {
		return ParsedLink{}, fmt.Errorf("%w: not a Telegram link: %q", domain.ErrInvalidInput, original)
	}
	segments := splitPath(u.Path)
	if len(segments) >= 2 && segments[0] == "s" {
		segments = segments[1:]
	}
	if len(segments) == 0 {
		return ParsedLink{}, fmt.Errorf("%w: Telegram link has no target", domain.ErrInvalidInput)
	}

	firstRaw := segments[0]
	first := strings.ToLower(firstRaw)
	switch {
	case strings.HasPrefix(firstRaw, "+"):
		hash := strings.TrimPrefix(firstRaw, "+")
		if hash == "" {
			return ParsedLink{}, fmt.Errorf("%w: empty invite hash", domain.ErrInvalidInput)
		}
		return ParsedLink{
			Raw:        original,
			Normalized: "https://t.me/+" + hash,
			Kind:       LinkKindInvite,
			InviteHash: hash,
		}, nil
	case first == "joinchat":
		if len(segments) < 2 || segments[1] == "" {
			return ParsedLink{}, fmt.Errorf("%w: empty joinchat hash", domain.ErrInvalidInput)
		}
		hash := segments[1]
		return ParsedLink{
			Raw:        original,
			Normalized: "https://t.me/+" + hash,
			Kind:       LinkKindInvite,
			InviteHash: hash,
		}, nil
	case first == "addlist":
		if len(segments) < 2 || segments[1] == "" {
			return ParsedLink{}, fmt.Errorf("%w: empty addlist slug", domain.ErrInvalidInput)
		}
		slug := segments[1]
		return ParsedLink{
			Raw:         original,
			Normalized:  "https://t.me/addlist/" + slug,
			Kind:        LinkKindAddList,
			AddListSlug: slug,
		}, nil
	case first == "c":
		if len(segments) < 3 {
			return ParsedLink{}, fmt.Errorf("%w: malformed t.me/c link", domain.ErrInvalidInput)
		}
		channelID, err := parsePositiveInt64(segments[1])
		if err != nil {
			return ParsedLink{}, fmt.Errorf("%w: invalid channel id", domain.ErrInvalidInput)
		}
		msgID, err := parsePositiveInt(segments[2])
		if err != nil {
			return ParsedLink{}, fmt.Errorf("%w: invalid message id", domain.ErrInvalidInput)
		}
		return ParsedLink{
			Raw:        original,
			Normalized: fmt.Sprintf("https://t.me/c/%d/%d", channelID, msgID),
			Kind:       LinkKindMessage,
			ChannelID:  channelID,
			MessageID:  msgID,
		}, nil
	default:
		username := first
		if !validUsername(username) {
			return ParsedLink{}, fmt.Errorf("%w: invalid username %q", domain.ErrInvalidInput, username)
		}
		if len(segments) >= 2 {
			msgID, err := parsePositiveInt(segments[1])
			if err != nil {
				return ParsedLink{}, fmt.Errorf("%w: invalid message id", domain.ErrInvalidInput)
			}
			return ParsedLink{
				Raw:        original,
				Normalized: fmt.Sprintf("https://t.me/%s/%d", username, msgID),
				Kind:       LinkKindMessage,
				Username:   username,
				MessageID:  msgID,
			}, nil
		}
		return ParsedLink{
			Raw:        original,
			Normalized: "https://t.me/" + username,
			Kind:       LinkKindUsername,
			Username:   username,
		}, nil
	}
}

// ExtractLinks finds and deduplicates Telegram references embedded in free text.
func ExtractLinks(text string) []ParsedLink {
	fields := strings.Fields(text)
	out := make([]ParsedLink, 0, 2)
	seen := make(map[string]struct{}, 2)
	for _, field := range fields {
		candidate := trimLinkPunctuation(field)
		if candidate == "" {
			continue
		}
		lower := strings.ToLower(candidate)
		if !strings.HasPrefix(candidate, "@") &&
			!strings.Contains(lower, "t.me/") &&
			!strings.Contains(lower, "telegram.me/") &&
			!strings.Contains(lower, "telegram.dog/") {
			continue
		}
		parsed, err := ParseLink(candidate)
		if err != nil {
			continue
		}
		if _, exists := seen[parsed.Normalized]; exists {
			continue
		}
		seen[parsed.Normalized] = struct{}{}
		out = append(out, parsed)
	}
	return out
}

// BuildMessageLink creates a deterministic Telegram message deep link. Supergroup
// IDs with the -100 prefix are reduced to the t.me/c form Telegram expects.
func BuildMessageLink(chatUsername string, tgChatID int64, msgID int) string {
	username := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(chatUsername), "@"))
	if idx := strings.LastIndex(username, "/"); idx >= 0 {
		username = username[idx+1:]
	}
	username = strings.ToLower(username)
	if validUsername(username) {
		if msgID > 0 {
			return fmt.Sprintf("https://t.me/%s/%d", username, msgID)
		}
		return "https://t.me/" + username
	}
	if tgChatID == 0 {
		return ""
	}
	id := tgChatID
	if id < 0 {
		id = -id
	}
	raw := strconv.FormatInt(id, 10)
	if strings.HasPrefix(raw, "100") && len(raw) > 3 {
		trimmed, err := strconv.ParseInt(raw[3:], 10, 64)
		if err == nil {
			id = trimmed
		}
	}
	if msgID > 0 {
		return fmt.Sprintf("https://t.me/c/%d/%d", id, msgID)
	}
	return fmt.Sprintf("https://t.me/c/%d", id)
}

func splitPath(path string) []string {
	trimmed := strings.Trim(path, "/")
	if trimmed == "" {
		return nil
	}
	parts := strings.Split(trimmed, "/")
	out := parts[:0]
	for _, part := range parts {
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func trimLinkPunctuation(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimLeft(value, "([{<\"'")
	value = strings.TrimRight(value, ")]}>\"'.,;:!?")
	return value
}

func validUsername(username string) bool {
	if username == "" || len(username) > 64 {
		return false
	}
	for _, r := range username {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
			continue
		}
		return false
	}
	return true
}

func parsePositiveInt64(value string) (int64, error) {
	id, err := strconv.ParseInt(value, 10, 64)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("invalid positive integer")
	}
	return id, nil
}

func parsePositiveInt(value string) (int, error) {
	id, err := strconv.Atoi(value)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("invalid positive integer")
	}
	return id, nil
}
