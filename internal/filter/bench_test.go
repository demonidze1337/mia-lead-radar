package filter

import (
	"testing"
	"time"

	"github.com/demonidze1337/mia-lead-radar/internal/domain"
)

var benchmarkSink float64

func BenchmarkEvaluate(b *testing.B) {
	engine := NewEngine(WithDedupCapacity(2048))
	radar := domain.RadarProfile{
		ID:              1,
		Name:            "benchmark",
		TargetLanguages: []string{"en"},
	}
	for _, phrase := range []string{
		"website", "web development", "developer", "landing page", "ecommerce store",
		"shopify", "wordpress", "seo audit", "api integration", "mobile app",
		"ui design", "who can build",
	} {
		radar.Tags = append(radar.Tags, domain.RadarTag{
			Phrase:  phrase,
			Kind:    domain.RadarTagRoot,
			Weight:  1,
			Enabled: true,
		})
	}
	// Warm the dedup history so Jaccard checks execute during the benchmark.
	warmTexts := []string{
		"Looking for a designer to refresh our brand book and landing pages this quarter.",
		"Any recommendations for a reliable accountant familiar with EU VAT filings?",
		"Our team needs help migrating a legacy CRM to a modern cloud platform.",
		"Can someone build an automated report from Google Sheets to Slack every morning?",
		"Seeking a copywriter for a SaaS onboarding email sequence in German and English.",
		"I want to hire a photographer for a product catalog of about forty items.",
		"Does anyone know a good agency for paid social campaigns in the DACH region?",
		"Need advice on choosing between Postgres and MySQL for a small analytics app.",
		"Looking for a translator to localize our mobile app into Japanese.",
		"We are evaluating vendors for a customer support chatbot with handoff.",
		"Any freelancers available for a two week React dashboard sprint?",
		"Seeking recommendations for a DevOps consultant to set up CI pipelines.",
		"Who can help with a Shopify theme customization and speed optimization?",
		"Need an experienced QA engineer for a manual regression pass next week.",
		"Can anyone recommend a motion designer for a short product launch video?",
		"Looking for a data analyst to build a churn dashboard in Looker Studio.",
		"Our startup needs a privacy lawyer to review a SaaS agreement.",
		"Any agency recommendations for a B2B LinkedIn lead generation pilot?",
		"Need a Stripe integration specialist for a subscription billing migration.",
		"Who does accessibility audits for public sector websites in the UK?",
		"Seeking a part time bookkeeper for a small ecommerce business.",
		"Can someone set up email deliverability with SPF DKIM and DMARC?",
		"Looking for a Flutter developer to ship an MVP in six weeks.",
		"Any recommendations for a technical writer with API documentation experience?",
		"Need help configuring a self hosted analytics stack without cookies.",
		"Who can build a small internal tool with authentication and audit logs?",
		"Seeking a consultant for ISO 27001 preparation and policy templates.",
		"Looking for a Portuguese speaking customer support contractor.",
		"Can anyone review our AWS bill and suggest cost optimizations?",
		"Need a designer who understands Telegram Mini App constraints.",
		"Any recommendations for a video editor for short form social clips?",
		"Looking for a backend engineer familiar with event driven architecture.",
	}
	for i, text := range warmTexts {
		engine.dedup.Check(
			Fingerprint(Normalize(text)),
			Tokenize(Normalize(text)),
			int64(1000+i),
			false,
		)
	}

	target := CandidateInput{
		ChatID:   1,
		MsgID:    1,
		AuthorID: 42,
		Text:     "We are looking for a developer to build an ecommerce store and landing page. Budget 5000 dollars, need help with Shopify integration.",
		DryRun:   true,
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		result := engine.Evaluate(target, radar, nil)
		benchmarkSink = result.Score
	}
	elapsed := b.Elapsed() / time.Duration(b.N)
	if elapsed > time.Millisecond {
		b.Fatalf("Tier-0 evaluation took %v per message, want < 1ms", elapsed)
	}
}

func BenchmarkEvaluateCJK(b *testing.B) {
	engine := NewEngine()
	radar := domain.RadarProfile{
		ID:   1,
		Name: "cjk",
		Tags: []domain.RadarTag{
			{Phrase: "网站", Kind: domain.RadarTagRoot, Weight: 1, Enabled: true},
			{Phrase: "开发者", Kind: domain.RadarTagRoot, Weight: 1, Enabled: true},
		},
	}
	target := CandidateInput{
		ChatID:   1,
		MsgID:    1,
		AuthorID: 7,
		Text:     "我需要一个开发者帮我做网站，预算3000元。",
		DryRun:   true,
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		result := engine.Evaluate(target, radar, nil)
		benchmarkSink = result.Score
	}
	elapsed := b.Elapsed() / time.Duration(b.N)
	if elapsed > time.Millisecond {
		b.Fatalf("Tier-0 CJK evaluation took %v per message, want < 1ms", elapsed)
	}
}
