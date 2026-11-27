package filter

import (
	"reflect"
	"strings"
	"testing"
)

func TestNormalize(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "empty", input: "", want: ""},
		{name: "lowercase", input: "I NEED A Website", want: "i need a website"},
		{name: "nfkc fullwidth", input: "Ｗｅｂ Ｓｉｔｅ", want: "web site"},
		{name: "zero width", input: "web\u200bsite\u200d\u2060", want: "website"},
		{name: "control chars", input: "web\n\t\x00site", want: "web site"},
		{name: "punctuation collapse", input: "Hello!!!   World???", want: "hello world"},
		{name: "apostrophe keeps word", input: "Don't stop", want: "dont stop"},
		{name: "curly apostrophe", input: "L’offre", want: "loffre"},
		{name: "mention keeps name", input: "@John_Doe please help", want: "john_doe please help"},
		{name: "hashtag keeps word", input: "#WebDesign is fun", want: "webdesign is fun"},
		{name: "url becomes host", input: "visit https://Example.COM/path?q=1 now", want: "visit example com now"},
		{name: "www url becomes host", input: "see www.my-site.dev/a", want: "see my site dev"},
		{name: "telegram link dropped", input: "join https://t.me/+abc123 now", want: "join now"},
		{name: "trailing url punctuation", input: "check site.com.", want: "check site com"},
		{name: "arabic preserved", input: "أحتاج مطور", want: "أحتاج مطور"},
		{name: "cjk preserved", input: "我需要开发者", want: "我需要开发者"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := Normalize(tc.input); got != tc.want {
				t.Fatalf("Normalize(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestTokenize(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{name: "empty", input: "", want: nil},
		{name: "english", input: "i need a website developer", want: []string{"i", "need", "a", "website", "developer"}},
		{name: "underscore mention", input: "john_doe hello", want: []string{"john_doe", "hello"}},
		{name: "numbers", input: "budget 3000 usd", want: []string{"budget", "3000", "usd"}},
		{name: "chinese bigrams", input: "网站开发", want: []string{"网站", "站开", "开发"}},
		{name: "single chinese char", input: "好", want: []string{"好"}},
		{name: "mixed chinese", input: "做网站 预算", want: []string{"做网", "网站", "预算"}},
		{name: "japanese bigrams", input: "開発者を探しています", want: []string{"開発", "発者", "者を", "を探", "探し", "して", "てい", "いま", "ます"}},
		{name: "korean bigrams", input: "웹사이트", want: []string{"웹사", "사이", "이트"}},
		{name: "punctuation separators", input: "hello, world!", want: []string{"hello", "world"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Tokenize(tc.input)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("Tokenize(%q) = %#v, want %#v", tc.input, got, tc.want)
			}
		})
	}
}

func TestURLDomainTokensSurvive(t *testing.T) {
	norm := Normalize("Go to https://shop.example.co.uk/products?ref=1")
	tokens := Tokenize(norm)
	joined := strings.Join(tokens, " ")
	for _, want := range []string{"shop", "example", "co", "uk"} {
		if !strings.Contains(joined, want) {
			t.Errorf("domain token %q lost from %q", want, joined)
		}
	}
	if strings.Contains(joined, "https") || strings.Contains(joined, "products") {
		t.Errorf("URL scheme/path leaked into tokens: %q", joined)
	}
}

func TestStemmerEquivalence(t *testing.T) {
	tests := []struct {
		lang  string
		words []string
	}{
		{lang: "en", words: []string{"service", "services"}},
		{lang: "en", words: []string{"developer", "developers", "developing", "development"}},
		{lang: "en", words: []string{"recommendation", "recommendations"}},
		{lang: "ru", words: []string{"разработчик", "разработчика", "разработчиков"}},
		{lang: "ru", words: []string{"сайт", "сайта", "сайтов"}},
		{lang: "uk", words: []string{"сайт", "сайту"}},
		{lang: "es", words: []string{"desarrollador", "desarrolladores"}},
		{lang: "de", words: []string{"webseite", "webseiten"}},
		{lang: "fr", words: []string{"développeur", "développeurs"}},
		{lang: "pt", words: []string{"desenvolvedor", "desenvolvedores"}},
		{lang: "pl", words: []string{"strona", "strony"}},
		{lang: "tr", words: []string{"site", "sitem", "sitesi"}},
		{lang: "it", words: []string{"sviluppatore", "sviluppatori"}},
		{lang: "ar", words: []string{"ميزانية", "الميزانية"}},
	}
	for _, tc := range tests {
		t.Run(tc.lang+"/"+tc.words[0], func(t *testing.T) {
			want := stemToken(tc.words[0], tc.lang)
			for _, word := range tc.words[1:] {
				if got := stemToken(word, tc.lang); got != want {
					t.Errorf("stemToken(%q, %q) = %q, want %q (from %q)", word, tc.lang, got, want, tc.words[0])
				}
			}
		})
	}
}

func TestStemmerDoesNotMangleShortTokens(t *testing.T) {
	for _, token := range []string{"to", "go", "hi", "мы", "لا"} {
		if got := stemToken(token, "en"); got != token {
			t.Errorf("short token %q changed to %q", token, got)
		}
	}
}

func TestTokenScript(t *testing.T) {
	tests := map[string]scriptKind{
		"website":    scriptLatin,
		"разработка": scriptCyrillic,
		"مطور":       scriptArabic,
		"网站":         scriptCJK,
		"123":        scriptOther,
	}
	for token, want := range tests {
		if got := tokenScript(token); got != want {
			t.Errorf("tokenScript(%q) = %v, want %v", token, got, want)
		}
	}
}
