package filter

import (
	"math"
	"strings"
	"unicode"
)

// detectLanguage applies fast script detection first, then function/intent word
// scoring for Latin and Cyrillic languages. It never performs network or file IO.
func detectLanguage(norm string) (string, float64) {
	if norm == "" {
		return "und", 0
	}
	var latin, cyrillic, arabic, han, kana, hangul, letters int
	for _, r := range norm {
		if unicode.IsLetter(r) {
			letters++
		}
		switch {
		case unicode.In(r, unicode.Hangul):
			hangul++
		case unicode.In(r, unicode.Hiragana, unicode.Katakana):
			kana++
		case unicode.In(r, unicode.Han):
			han++
		case unicode.In(r, unicode.Arabic):
			arabic++
		case unicode.In(r, unicode.Cyrillic):
			cyrillic++
		case unicode.In(r, unicode.Latin):
			latin++
		}
	}
	if letters == 0 {
		return "und", 0
	}
	switch {
	case kana > 0 && han+kana >= 2:
		return "ja", scriptConfidence(han+kana, letters)
	case hangul >= 2:
		return "ko", scriptConfidence(hangul, letters)
	case han >= 2:
		return "zh", scriptConfidence(han, letters)
	case arabic >= 2:
		return "ar", scriptConfidence(arabic, letters)
	}

	tokens := Tokenize(norm)
	if cyrillic >= 2 {
		return detectCyrillic(norm, tokens)
	}
	if latin >= 2 {
		return detectLatin(norm, tokens)
	}
	return "und", 0
}

func scriptConfidence(scriptRunes, letters int) float64 {
	if letters <= 0 {
		return 0
	}
	ratio := float64(scriptRunes) / float64(letters)
	conf := 0.7 + 0.29*ratio
	if conf > 0.99 {
		conf = 0.99
	}
	return conf
}

func detectCyrillic(norm string, tokens []string) (string, float64) {
	scores := map[string]int{"ru": 0, "uk": 0}
	for _, token := range tokens {
		if pack, ok := compiledByCode["ru"]; ok {
			if _, hit := pack.detectWords[token]; hit {
				scores["ru"]++
			}
		}
		if pack, ok := compiledByCode["uk"]; ok {
			if _, hit := pack.detectWords[token]; hit {
				scores["uk"]++
			}
		}
	}
	// Distinctive letters are strong evidence even in short messages.
	for _, r := range norm {
		switch r {
		case 'і', 'ї', 'є', 'ґ':
			scores["uk"] += 2
		case 'ы', 'э', 'ё', 'ъ':
			scores["ru"] += 2
		}
	}
	return pickLanguage(scores, "ru", 0.35)
}

func detectLatin(norm string, tokens []string) (string, float64) {
	scores := make(map[string]int, 8)
	for _, code := range []string{"en", "es", "de", "fr", "pt", "pl", "tr", "it"} {
		pack, ok := compiledByCode[code]
		if !ok {
			continue
		}
		score := 0
		for _, token := range tokens {
			if _, hit := pack.detectWords[token]; hit {
				score++
			}
		}
		scores[code] = score
	}
	// Diacritics disambiguate closely related Latin languages.
	for _, r := range norm {
		switch r {
		case 'ñ':
			scores["es"] += 3
		case 'ã', 'õ':
			scores["pt"] += 3
		case 'ß':
			scores["de"] += 4
		case 'ą', 'ć', 'ę', 'ł', 'ń', 'ś', 'ź', 'ż':
			scores["pl"] += 4
		case 'ı', 'ş', 'ğ':
			scores["tr"] += 4
		case 'œ':
			scores["fr"] += 3
		}
	}
	return pickLanguage(scores, "und", 0.2)
}

func pickLanguage(scores map[string]int, fallback string, fallbackConf float64) (string, float64) {
	bestCode := ""
	best, second := 0, 0
	// Stable iteration order keeps detection deterministic on ties.
	for _, code := range []string{"en", "ru", "uk", "es", "de", "fr", "pt", "pl", "tr", "it"} {
		score, ok := scores[code]
		if !ok {
			continue
		}
		if score > best {
			second = best
			best = score
			bestCode = code
		} else if score > second {
			second = score
		}
	}
	if best == 0 {
		return fallback, fallbackConf
	}
	conf := 0.55 + 0.44*float64(best-second)/float64(best)
	if best < 2 {
		conf = math.Min(conf, 0.5)
	}
	if conf > 0.99 {
		conf = 0.99
	}
	return bestCode, conf
}

func padText(norm string) string {
	return " " + strings.TrimSpace(norm) + " "
}
