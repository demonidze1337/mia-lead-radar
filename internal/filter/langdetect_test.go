package filter

import "testing"

func TestDetectLanguageAllPacks(t *testing.T) {
	tests := []struct {
		lang string
		text string
		min  float64
	}{
		{lang: "en", text: "I am looking for a developer to build a website for my business. Budget is 3000 dollars.", min: 0.4},
		{lang: "ru", text: "Ищу разработчика для сайта, нужен специалист. Бюджет 50000 рублей.", min: 0.5},
		{lang: "uk", text: "Шукаю розробника для сайту, потрібен спеціаліст. Бюджет 50000 гривень.", min: 0.5},
		{lang: "es", text: "Busco un desarrollador para mi página web. Presupuesto 3000 euros.", min: 0.5},
		{lang: "de", text: "Ich suche einen Entwickler für meine Webseite. Budget 3000 Euro.", min: 0.5},
		{lang: "fr", text: "Je cherche un développeur pour mon site web. Budget 3000 euros.", min: 0.5},
		{lang: "pt", text: "Procuro um desenvolvedor para o meu site. Orçamento de 3000 euros.", min: 0.5},
		{lang: "pl", text: "Szukam programisty do zrobienia strony internetowej. Budżet 5000 zł.", min: 0.5},
		{lang: "tr", text: "Web sitem için bir geliştirici arıyorum. Bütçem 3000 dolar.", min: 0.5},
		{lang: "it", text: "Cerco uno sviluppatore per il mio sito web. Budget 3000 euro.", min: 0.5},
		{lang: "ar", text: "أبحث عن مطور لموقعي الإلكتروني. الميزانية 3000 دولار.", min: 0.8},
		{lang: "zh", text: "我需要一个开发者帮我做网站，预算3000元。", min: 0.8},
		{lang: "ko", text: "웹사이트 개발자를 찾고 있습니다. 예산은 300만원입니다.", min: 0.8},
		{lang: "ja", text: "ウェブサイトを作ってくれる開発者を探しています。予算は30万円です。", min: 0.8},
	}
	engine := NewEngine()
	for _, tc := range tests {
		t.Run(tc.lang, func(t *testing.T) {
			lang, confidence := engine.DetectLanguage(tc.text)
			if lang != tc.lang {
				t.Fatalf("DetectLanguage = %q (%.2f), want %q", lang, confidence, tc.lang)
			}
			if confidence < tc.min {
				t.Fatalf("confidence = %.2f, want >= %.2f", confidence, tc.min)
			}
		})
	}
}

func TestDetectLanguageEdgeCases(t *testing.T) {
	engine := NewEngine()
	if lang, conf := engine.DetectLanguage(""); lang != "und" || conf != 0 {
		t.Errorf("empty detection = %q %.2f", lang, conf)
	}
	if lang, conf := engine.DetectLanguage("123 456"); lang != "und" || conf != 0 {
		t.Errorf("numeric detection = %q %.2f", lang, conf)
	}
	lang, _ := engine.DetectLanguage("Привет, как дела? Что нового?")
	if lang != "ru" {
		t.Errorf("russian small talk detected as %q", lang)
	}
	lang, _ = engine.DetectLanguage("Привіт, як справи? Що нового?")
	if lang != "uk" {
		t.Errorf("ukrainian small talk detected as %q", lang)
	}
}
