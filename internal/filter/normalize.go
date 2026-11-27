package filter

import (
	"regexp"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// urlPattern matches scheme/`www.` URLs and Telegram links. It runs on the
// already NFKC-normalized, lowercase text.
var urlPattern = regexp.MustCompile(`(?:https?://|www\.)[^\s]+|(?:t|telegram)\.me/[^\s]+|(?:t|telegram)\.dog/[^\s]+`)

// Normalize performs NFKC normalization, lowercases text, removes zero-width and
// control characters, reduces URLs to their host (keeping domain tokens), and
// collapses punctuation/whitespace into single separators.
func Normalize(raw string) string {
	if raw == "" {
		return ""
	}
	s := norm.NFKC.String(raw)
	s = strings.ToLower(s)
	s = urlPattern.ReplaceAllStringFunc(s, normalizeURL)

	var b strings.Builder
	b.Grow(len(s))
	lastSpace := true
	for _, r := range s {
		switch {
		case isInvisible(r):
			continue
		case r == '\'' || r == '\u2019':
			// Keep contractions/elisions as one token (don't -> dont).
			continue
		case unicode.IsSpace(r) || unicode.IsControl(r):
			if !lastSpace {
				b.WriteByte(' ')
				lastSpace = true
			}
		case unicode.IsPunct(r) || unicode.IsSymbol(r):
			if r == '_' {
				b.WriteRune(r)
				lastSpace = false
				continue
			}
			if !lastSpace {
				b.WriteByte(' ')
				lastSpace = true
			}
		default:
			b.WriteRune(r)
			lastSpace = false
		}
	}
	return strings.TrimSpace(b.String())
}

func isInvisible(r rune) bool {
	switch r {
	case '\u200b', '\u200c', '\u200d', '\u200e', '\u200f', '\u2060', '\ufeff':
		return true
	}
	return false
}

func normalizeURL(raw string) string {
	s := raw
	for _, prefix := range []string{"https://", "http://", "www."} {
		if strings.HasPrefix(s, prefix) {
			s = s[len(prefix):]
			break
		}
	}
	lower := strings.ToLower(s)
	if strings.HasPrefix(lower, "t.me/") || strings.HasPrefix(lower, "telegram.me/") ||
		strings.HasPrefix(lower, "telegram.dog/") {
		// Telegram links are navigation noise, not buyer language.
		return ""
	}
	if idx := strings.IndexAny(s, "/?#"); idx >= 0 {
		s = s[:idx]
	}
	s = strings.Trim(s, ".,;:!?()[]{}<>\"'")
	if s == "" || !strings.Contains(s, ".") {
		return ""
	}
	return s
}

// Tokenize splits normalized text into word tokens. Alphabetic scripts produce
// words; contiguous CJK runs produce overlapping character bigrams (unigrams for
// single-character runs), which gives useful recall without a dictionary.
func Tokenize(normalized string) []string {
	if normalized == "" {
		return nil
	}
	runes := []rune(normalized)
	tokens := make([]string, 0, len(runes)/3+4)
	for i := 0; i < len(runes); {
		r := runes[i]
		if unicode.IsSpace(r) {
			i++
			continue
		}
		if isCJK(r) {
			j := i
			for j < len(runes) && isCJK(runes[j]) {
				j++
			}
			run := runes[i:j]
			if len(run) == 1 {
				tokens = append(tokens, string(run[0]))
			} else {
				for k := 0; k+1 < len(run); k++ {
					tokens = append(tokens, string(run[k:k+2]))
				}
			}
			i = j
			continue
		}
		j := i
		for j < len(runes) {
			rj := runes[j]
			if unicode.IsLetter(rj) || unicode.IsDigit(rj) || unicode.IsMark(rj) || rj == '_' {
				j++
				continue
			}
			break
		}
		if j > i {
			tokens = append(tokens, string(runes[i:j]))
			i = j
			continue
		}
		i++
	}
	return tokens
}

func isCJK(r rune) bool {
	return unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana, unicode.Hangul)
}

func hasCJK(s string) bool {
	for _, r := range s {
		if isCJK(r) {
			return true
		}
	}
	return false
}

func runeLen(s string) int {
	return len([]rune(s))
}
