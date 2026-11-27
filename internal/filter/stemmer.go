package filter

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

type scriptKind int

const (
	scriptOther scriptKind = iota
	scriptLatin
	scriptCyrillic
	scriptArabic
	scriptCJK
)

var (
	latinSuffixes = map[string][]string{
		"en": {
			"ational", "ization", "iveness", "fulness", "ousness", "ements", "ement", "ations",
			"ation", "ments", "ment", "ings", "ing", "edly", "ally", "ness", "less", "able",
			"ible", "ers", "er", "ies", "ied", "es", "ed", "ly", "s", "e",
		},
		"es": {
			"ciones", "ción", "cion", "idades", "idad", "mente", "amientos", "amiento", "adores",
			"ador", "ados", "idos", "ando", "iendo", "ado", "ido", "as", "os", "es", "a", "o", "s",
		},
		"de": {
			"ungen", "ung", "keiten", "keit", "heiten", "heit", "schaften", "schaft", "liche",
			"lich", "ische", "isch", "esten", "en", "er", "es", "em", "e", "n", "s", "t",
		},
		"fr": {
			"ements", "ement", "ations", "ation", "iques", "ique", "euses", "euse", "eurs",
			"eur", "ées", "és", "é", "e", "s", "x",
		},
		"pt": {
			"ções", "coes", "ção", "cao", "idades", "idade", "mente", "amentos", "amento",
			"edores", "edor", "adores", "ador", "ados", "idos", "ando", "endo", "ado", "ido",
			"as", "os", "es", "a", "o", "s",
		},
		"pl": {
			"owania", "owanie", "owego", "owej", "owych", "ami", "ach", "ów", "ow", "om",
			"em", "ie", "owa", "owe", "owy", "y", "i", "a", "e", "u", "ą", "ę",
		},
		"tr": {
			"larında", "lerinde", "ları", "leri", "lar", "ler", "yorum", "yorsun", "yor",
			"ıyor", "iyor", "uyor", "üyor", "dan", "den", "tan", "ten", "da", "de", "ta",
			"te", "ın", "in", "un", "ün", "ım", "im", "um", "üm", "ı", "i", "u", "ü",
			"a", "e", "m", "n", "s",
		},
		"it": {
			"zioni", "zione", "mente", "amenti", "amento", "atori", "atore", "ando", "endo",
			"ati", "iti", "ato", "ito", "i", "o", "a", "e",
		},
	}
	cyrillicSuffixes = []string{
		"иями", "ями", "ами", "ыми", "ими", "его", "ого", "ему", "ому", "ую", "юю", "ая",
		"яя", "ое", "ее", "ые", "ие", "ой", "ый", "ий", "ей", "ям", "ам", "ом", "ем",
		"ться", "тся", "ешь", "ишь", "ете", "ите", "ут", "ют", "ат", "ят", "ил", "ила",
		"ило", "или", "ть", "ся", "сь", "ах", "ях", "ов", "ев", "у", "ю", "а", "я", "ы",
		"и", "е", "о", "ь",
	}
	arabicSuffixes = []string{
		"ات", "ون", "ين", "ان", "ها", "هم", "هن", "كم", "كن", "نا", "ه", "ي", "ك",
	}
	arabicPrefixes = []string{
		"وال", "بال", "كال", "فال", "ال", "و", "ب", "ل", "ف", "ك",
	}
	arabicReplacer = strings.NewReplacer(
		"أ", "ا", "إ", "ا", "آ", "ا", "ى", "ي", "ة", "ه",
	)
)

// stemToken reduces a token using a lightweight language-aware suffix stemmer.
// CJK bigrams and unknown scripts are returned unchanged.
func stemToken(token, lang string) string {
	if token == "" {
		return ""
	}
	if utf8.RuneCountInString(token) <= 2 {
		return token
	}
	switch tokenScript(token) {
	case scriptCJK:
		return token
	case scriptCyrillic:
		return stemWithSuffixes(token, cyrillicSuffixes, 3)
	case scriptArabic:
		return stemArabic(token)
	case scriptLatin:
		suffixes, ok := latinSuffixes[lang]
		if !ok {
			suffixes = latinSuffixes["en"]
		}
		return stemWithSuffixes(token, suffixes, 3)
	default:
		return token
	}
}

func stemWithSuffixes(word string, suffixes []string, minStem int) string {
	current := word
	for pass := 0; pass < 3; pass++ {
		best := ""
		for _, suffix := range suffixes {
			if len(suffix) <= len(best) || !strings.HasSuffix(current, suffix) {
				continue
			}
			candidate := strings.TrimSuffix(current, suffix)
			if utf8.RuneCountInString(candidate) < minStem {
				continue
			}
			best = suffix
		}
		if best == "" {
			break
		}
		current = strings.TrimSuffix(current, best)
	}
	return current
}

func stemArabic(token string) string {
	word := arabicReplacer.Replace(token)
	word = stemWithSuffixes(word, arabicSuffixes, 3)
	if utf8.RuneCountInString(word) > 4 {
		for _, prefix := range arabicPrefixes {
			if strings.HasPrefix(word, prefix) && utf8.RuneCountInString(strings.TrimPrefix(word, prefix)) >= 3 {
				word = strings.TrimPrefix(word, prefix)
				break
			}
		}
	}
	return word
}

func tokenScript(token string) scriptKind {
	for _, r := range token {
		switch {
		case unicode.In(r, unicode.Cyrillic):
			return scriptCyrillic
		case unicode.In(r, unicode.Arabic):
			return scriptArabic
		case unicode.In(r, unicode.Latin):
			return scriptLatin
		case isCJK(r):
			return scriptCJK
		}
	}
	return scriptOther
}
