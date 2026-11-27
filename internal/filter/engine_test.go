package filter

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/demonidze1337/mia-lead-radar/internal/domain"
)

func testRadar(tags ...string) domain.RadarProfile {
	radar := domain.RadarProfile{
		ID:              1,
		Name:            "test",
		TargetLanguages: []string{"en"},
	}
	for _, phrase := range tags {
		radar.Tags = append(radar.Tags, domain.RadarTag{
			Phrase:  phrase,
			Kind:    domain.RadarTagRoot,
			Weight:  1,
			Enabled: true,
		})
	}
	return radar
}

func evaluateText(t *testing.T, engine *Tier0Engine, radar domain.RadarProfile, text string) EvalResult {
	t.Helper()
	return engine.Evaluate(CandidateInput{
		ChatID:   1,
		MsgID:    1,
		AuthorID: 42,
		Text:     text,
	}, radar, nil)
}

func TestEvaluateAllFourteenLanguages(t *testing.T) {
	tests := []struct {
		lang string
		tags []string
		text string
	}{
		{
			lang: "en",
			tags: []string{"website", "developer"},
			text: "I am looking for a developer to build a website for my business. Budget is 3000 dollars.",
		},
		{
			lang: "ru",
			tags: []string{"сайт", "разработчик"},
			text: "Ищу разработчика для сайта. Бюджет 50000 рублей, нужен специалист.",
		},
		{
			lang: "uk",
			tags: []string{"сайт", "розробник"},
			text: "Шукаю розробника для сайту. Бюджет 50000 гривень, потрібен спеціаліст.",
		},
		{
			lang: "es",
			tags: []string{"página web", "desarrollador"},
			text: "Busco un desarrollador para hacer una página web. Presupuesto 3000 euros.",
		},
		{
			lang: "de",
			tags: []string{"webseite", "entwickler"},
			text: "Ich suche einen Entwickler für meine Webseite. Budget 3000 Euro.",
		},
		{
			lang: "fr",
			tags: []string{"site web", "développeur"},
			text: "Je cherche un développeur pour mon site web. Budget 3000 euros.",
		},
		{
			lang: "pt",
			tags: []string{"site", "desenvolvedor"},
			text: "Procuro um desenvolvedor para o meu site. Orçamento de 3000 euros.",
		},
		{
			lang: "pl",
			tags: []string{"strona internetowa", "programista"},
			text: "Szukam programisty do zrobienia strony internetowej. Budżet 5000 zł.",
		},
		{
			lang: "tr",
			tags: []string{"web sitesi", "geliştirici"},
			text: "Web sitem için bir geliştirici arıyorum. Bütçem 3000 dolar.",
		},
		{
			lang: "it",
			tags: []string{"sito web", "sviluppatore"},
			text: "Cerco uno sviluppatore per il mio sito web. Budget 3000 euro.",
		},
		{
			lang: "ar",
			tags: []string{"موقع", "مطور"},
			text: "أبحث عن مطور لموقعي الإلكتروني. الميزانية 3000 دولار.",
		},
		{
			lang: "zh",
			tags: []string{"网站", "开发者"},
			text: "我需要一个开发者帮我做网站，预算3000元。",
		},
		{
			lang: "ko",
			tags: []string{"웹사이트", "개발자"},
			text: "웹사이트 개발자를 찾고 있습니다. 예산은 300만원입니다.",
		},
		{
			lang: "ja",
			tags: []string{"ウェブサイト", "開発者"},
			text: "ウェブサイトを作ってくれる開発者を探しています。予算は30万円です。",
		},
	}

	for _, tc := range tests {
		t.Run(tc.lang, func(t *testing.T) {
			engine := NewEngine()
			result := evaluateText(t, engine, testRadar(tc.tags...), tc.text)
			if !result.Passed {
				t.Fatalf("expected pass for %s, got reason=%q score=%.1f lang=%q breakdown=%+v",
					tc.lang, result.RejectReason, result.Score, result.DetectedLang, result.Breakdown)
			}
			if result.DetectedLang != tc.lang {
				t.Errorf("DetectedLang = %q, want %q", result.DetectedLang, tc.lang)
			}
			if len(result.MatchedTags) < 2 {
				t.Errorf("MatchedTags = %#v, want >= 2", result.MatchedTags)
			}
			if result.Fingerprint == "" {
				t.Error("Fingerprint is empty")
			}
			if result.Score < 31 || result.Score > 100 {
				t.Errorf("Score = %.1f, want [31,100]", result.Score)
			}
		})
	}
}

func TestSellerVersusBuyerDisambiguation(t *testing.T) {
	engine := NewEngine()
	radar := testRadar("website development", "developer")

	buyer := "I need a website developer for my project. Budget 3000 dollars, willing to pay."
	buyerResult := evaluateText(t, engine, radar, buyer)
	if !buyerResult.Passed {
		t.Fatalf("buyer rejected: reason=%q score=%.1f", buyerResult.RejectReason, buyerResult.Score)
	}

	seller := "I offer website development services. DM me for price list and portfolio, best price guaranteed."
	sellerResult := evaluateText(t, engine, radar, seller)
	if sellerResult.Passed || sellerResult.RejectReason != RejectSellerPromo {
		t.Fatalf("seller not rejected as promo: passed=%v reason=%q", sellerResult.Passed, sellerResult.RejectReason)
	}
	if sellerResult.Breakdown.SellerPenalty >= 0 {
		t.Errorf("SellerPenalty = %v, want negative", sellerResult.Breakdown.SellerPenalty)
	}
}

func TestDIYFatigueWithoutIntent(t *testing.T) {
	engine := NewEngine()
	radar := testRadar("website")

	text := "I have tried everything to fix my website and nothing works. I am frustrated and wasted hours on this."
	result := evaluateText(t, engine, radar, text)
	if !result.Passed {
		t.Fatalf("DIY message rejected: reason=%q score=%.1f breakdown=%+v", result.RejectReason, result.Score, result.Breakdown)
	}
	if result.Breakdown.DIYFatigueScore <= 0 {
		t.Errorf("DIYFatigueScore = %v, want positive", result.Breakdown.DIYFatigueScore)
	}
	if len(result.MatchedTags) != 1 || result.MatchedTags[0] != "website" {
		t.Errorf("MatchedTags = %#v", result.MatchedTags)
	}
}

func TestHRVacancyToggle(t *testing.T) {
	text := "We are hiring a developer for our website team. Salary is negotiable, send your CV. We need a website specialist."
	tags := []string{"website", "developer"}

	engine := NewEngine()
	blocked := evaluateText(t, engine, testRadar(tags...), text)
	if blocked.Passed || blocked.RejectReason != RejectHRVacancy {
		t.Fatalf("HR should be rejected by default: passed=%v reason=%q", blocked.Passed, blocked.RejectReason)
	}

	allowedRadar := testRadar(tags...)
	allowedRadar.AllowHRVacancies = true
	allowedEngine := NewEngine()
	allowed := evaluateText(t, allowedEngine, allowedRadar, text)
	if allowed.RejectReason == RejectHRVacancy {
		t.Fatalf("HR still rejected with AllowHRVacancies: %+v", allowed)
	}
	if !allowed.Passed {
		t.Fatalf("HR allowed message should pass: reason=%q score=%.1f", allowed.RejectReason, allowed.Score)
	}
}

func TestBroadTagGuard(t *testing.T) {
	engine := NewEngine()
	radar := testRadar("website")

	text := "The website looks really nice today and loads fast on mobile."
	result := evaluateText(t, engine, radar, text)
	if result.Passed || result.RejectReason != RejectBroadTagNoProof {
		t.Fatalf("broad tag spam not rejected: passed=%v reason=%q", result.Passed, result.RejectReason)
	}

	corroborated := evaluateText(t, engine, radar, "I need a website for my business, who can build it?")
	if !corroborated.Passed {
		t.Fatalf("corroborated single tag rejected: reason=%q score=%.1f", corroborated.RejectReason, corroborated.Score)
	}
}

func TestTagThrottling(t *testing.T) {
	radar := testRadar("website", "developer")
	future := time.Now().Add(time.Hour)

	both := map[string]time.Time{"website": future, "developer": future}
	result := evaluateTextWithThrottles(t, NewEngine(), radar, "I need a website developer for my project with budget.", both)
	if result.Passed || result.RejectReason != RejectTagThrottled {
		t.Fatalf("all tags throttled: passed=%v reason=%q", result.Passed, result.RejectReason)
	}
	if len(result.ThrottledTags) != 2 {
		t.Errorf("ThrottledTags = %#v", result.ThrottledTags)
	}

	one := map[string]time.Time{"website": future}
	result = evaluateTextWithThrottles(t, NewEngine(), radar, "I need a website developer for my project with budget.", one)
	if !result.Passed {
		t.Fatalf("unthrottled tag should pass: reason=%q score=%.1f", result.RejectReason, result.Score)
	}
	if len(result.ThrottledTags) != 1 || result.ThrottledTags[0] != "website" {
		t.Errorf("ThrottledTags = %#v", result.ThrottledTags)
	}

	past := map[string]time.Time{"website": time.Now().Add(-time.Hour), "developer": time.Now().Add(-time.Hour)}
	result = evaluateTextWithThrottles(t, NewEngine(), radar, "I need a website developer for my project with budget.", past)
	if !result.Passed {
		t.Fatalf("expired throttles still blocking: reason=%q", result.RejectReason)
	}
}

func evaluateTextWithThrottles(t *testing.T, engine *Tier0Engine, radar domain.RadarProfile, text string, throttles map[string]time.Time) EvalResult {
	t.Helper()
	return engine.Evaluate(CandidateInput{ChatID: 1, MsgID: 1, AuthorID: 7, Text: text}, radar, throttles)
}

func TestEarlyRejects(t *testing.T) {
	engine := NewEngine()
	radar := testRadar("website", "developer")

	tests := []struct {
		name  string
		input CandidateInput
		want  RejectReason
	}{
		{
			name:  "bot",
			input: CandidateInput{Text: "I need a website developer", IsBot: true},
			want:  RejectServiceBot,
		},
		{
			name:  "service flag",
			input: CandidateInput{Text: "I need a website developer", IsService: true},
			want:  RejectServiceBot,
		},
		{
			name:  "service text",
			input: CandidateInput{Text: "Please verify you are human, captcha verification required to post."},
			want:  RejectServiceBot,
		},
		{
			name:  "short",
			input: CandidateInput{Text: "hi"},
			want:  RejectEmptyOrShort,
		},
		{
			name:  "marketplace spam",
			input: CandidateInput{Text: "Guaranteed profit crypto investment USDT airdrop, passive income now!"},
			want:  RejectMarketplaceSpam,
		},
		{
			name:  "stop phrase",
			input: CandidateInput{Text: "I need a website developer but this is a giveaway post."},
			want:  RejectStopPhrase,
		},
		{
			name:  "no tag match",
			input: CandidateInput{Text: "I need a kubernetes operator for my cluster with budget."},
			want:  RejectNoTagMatch,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tc.input.ChatID = 1
			tc.input.MsgID = 1
			tc.input.AuthorID = 9
			r := radar
			r.StopPhrases = []string{"giveaway"}
			result := engine.Evaluate(tc.input, r, nil)
			if result.RejectReason != tc.want {
				t.Fatalf("reason = %q, want %q (score=%.1f matched=%#v)", result.RejectReason, tc.want, result.Score, result.MatchedTags)
			}
		})
	}

	learned := radar
	learned.LearnedStopWords = []string{"scam"}
	result := engine.Evaluate(CandidateInput{ChatID: 1, MsgID: 2, AuthorID: 9, Text: "I need a website developer for my scam project with budget."}, learned, nil)
	if result.RejectReason != RejectStopPhrase {
		t.Fatalf("learned stop word not applied: %q", result.RejectReason)
	}

	singleSpam := engine.Evaluate(CandidateInput{ChatID: 1, MsgID: 3, AuthorID: 10, Text: "I need a website developer, crypto payments accepted, budget 3000."}, radar, nil)
	if singleSpam.RejectReason == RejectMarketplaceSpam {
		t.Fatal("single incidental spam marker must not reject a buyer")
	}
	if singleSpam.Breakdown.SpamPenalty >= 0 {
		t.Errorf("SpamPenalty = %v, want negative", singleSpam.Breakdown.SpamPenalty)
	}
}

func TestDedupExactAndDryRun(t *testing.T) {
	radar := testRadar("website", "developer")
	text := "I need a website developer for my business project, budget 3000 dollars."

	engine := NewEngine()
	first := engine.Evaluate(CandidateInput{ChatID: 1, MsgID: 1, AuthorID: 1, Text: text}, radar, nil)
	if first.RejectReason != RejectNone {
		t.Fatalf("first evaluation rejected: %q", first.RejectReason)
	}
	second := engine.Evaluate(CandidateInput{ChatID: 1, MsgID: 2, AuthorID: 1, Text: text}, radar, nil)
	if second.RejectReason != RejectDuplicate {
		t.Fatalf("exact duplicate not detected: %q", second.RejectReason)
	}

	dryEngine := NewEngine()
	for i := 0; i < 2; i++ {
		dry := dryEngine.Evaluate(CandidateInput{ChatID: 1, MsgID: i + 1, AuthorID: 2, Text: text, DryRun: true}, radar, nil)
		if dry.RejectReason == RejectDuplicate {
			t.Fatalf("dry-run mutated dedup state on iteration %d", i)
		}
	}
	real := dryEngine.Evaluate(CandidateInput{ChatID: 1, MsgID: 3, AuthorID: 2, Text: text}, radar, nil)
	if real.RejectReason == RejectDuplicate {
		t.Fatal("dry-run polluted dedup cache")
	}
	replay := dryEngine.Evaluate(CandidateInput{ChatID: 1, MsgID: 4, AuthorID: 2, Text: text}, radar, nil)
	if replay.RejectReason != RejectDuplicate {
		t.Fatalf("real duplicate not detected: %q", replay.RejectReason)
	}
}

func TestDedupFuzzyJaccard(t *testing.T) {
	radar := testRadar("website", "developer")
	engine := NewEngine()

	original := "I need a website developer for my small business project because we are launching a new product line next month and our current site cannot handle the traffic or the checkout flow we need help with Shopify integration and landing pages with a budget of 3000 dollars."
	nearDuplicate := "I need a website developer for my small business project because we are launching a new product line next month and our current site cannot handle the traffic or the checkout flow we need help with Shopify integration and landing pages with a budget of 5000 dollars."
	sameAuthor := CandidateInput{ChatID: 1, MsgID: 1, AuthorID: 77, Text: original}
	engine.Evaluate(sameAuthor, radar, nil)

	result := engine.Evaluate(CandidateInput{ChatID: 1, MsgID: 2, AuthorID: 77, Text: nearDuplicate}, radar, nil)
	if result.RejectReason != RejectDuplicate {
		t.Fatalf("per-author fuzzy duplicate not detected: %q", result.RejectReason)
	}

	global := engine.Evaluate(CandidateInput{ChatID: 1, MsgID: 3, AuthorID: 88, Text: nearDuplicate}, radar, nil)
	if global.RejectReason != RejectDuplicate {
		t.Fatalf("global fuzzy duplicate not detected: %q", global.RejectReason)
	}
}

func TestDedupBounded(t *testing.T) {
	engine := NewEngine(WithDedupCapacity(10))
	for i := 0; i < 60; i++ {
		tokens := []string{
			fmt.Sprintf("word%d", i),
			fmt.Sprintf("term%d", i),
			fmt.Sprintf("item%d", i),
			fmt.Sprintf("node%d", i),
		}
		engine.dedup.Check(fmt.Sprintf("fp-%d", i), tokens, int64(i), false)
	}
	if got := engine.dedup.Len(); got > 10 {
		t.Fatalf("dedup cache size = %d, want <= 10", got)
	}
	if got := len(engine.dedup.byAuthor); got > 10 {
		t.Fatalf("dedup author buckets = %d, want <= 10", got)
	}
}

func TestDedupTTLExpiry(t *testing.T) {
	now := time.Now()
	deduper := newDeduper(8, time.Minute, func() time.Time { return now })
	tokens := Tokenize("i need a website developer")
	fingerprint := Fingerprint("i need a website developer")

	if deduper.Check(fingerprint, tokens, 1, false) {
		t.Fatal("first observation reported as duplicate")
	}
	if !deduper.Check(fingerprint, tokens, 1, false) {
		t.Fatal("exact duplicate not detected")
	}
	now = now.Add(2 * time.Minute)
	if deduper.Check(fingerprint, tokens, 1, false) {
		t.Fatal("expired fingerprint still treated as duplicate")
	}
	if deduper.Len() != 1 {
		t.Fatalf("expired entry was not replaced, Len=%d", deduper.Len())
	}
	if len(deduper.byAuthor) != 1 {
		t.Fatalf("stale author buckets = %d, want 1", len(deduper.byAuthor))
	}
}

func TestEvaluateConcurrentRace(t *testing.T) {
	engine := NewEngine(WithDedupCapacity(128))
	radar := testRadar("website", "developer")

	const workers = 16
	const perWorker = 100
	var wg sync.WaitGroup
	wg.Add(workers)
	for w := 0; w < workers; w++ {
		go func(worker int) {
			defer wg.Done()
			for i := 0; i < perWorker; i++ {
				text := fmt.Sprintf("Worker %d needs a website developer for project %d with budget 3000.", worker, i)
				result := engine.Evaluate(CandidateInput{
					ChatID:   int64(worker),
					MsgID:    i,
					AuthorID: int64(worker),
					Text:     text,
					DryRun:   i%2 == 0,
				}, radar, nil)
				_ = result
			}
		}(w)
	}
	wg.Wait()
	if got := engine.dedup.Len(); got > 128 {
		t.Fatalf("dedup cache exceeded capacity: %d", got)
	}
}

func TestRootVersusLiveWeighting(t *testing.T) {
	norm := Normalize("i need a website")
	padded := padText(norm)
	docStems := Tokenize(norm)
	for i, token := range docStems {
		docStems[i] = stemToken(token, "en")
	}

	rootLive := domain.RadarProfile{Tags: []domain.RadarTag{
		{Phrase: "website", Kind: domain.RadarTagRoot, Weight: 1, Enabled: true},
		{Phrase: "website", Kind: domain.RadarTagLive, Weight: 1, Enabled: true},
	}}
	tags, df := compileRadarTags(rootLive, "en")
	matched, _ := matchTags(norm, padded, docStems, tags, df, nil, time.Now())
	if len(matched) != 2 {
		t.Fatalf("matched = %d, want 2", len(matched))
	}
	if matched[0].score <= matched[1].score {
		t.Fatalf("root tag score %.4f must exceed live %.4f", matched[0].score, matched[1].score)
	}

	weighted := domain.RadarProfile{Tags: []domain.RadarTag{
		{Phrase: "website", Kind: domain.RadarTagRoot, Weight: 1, Enabled: true},
		{Phrase: "website", Kind: domain.RadarTagRoot, Weight: 2, Enabled: true},
	}}
	tags, df = compileRadarTags(weighted, "en")
	matched, _ = matchTags(norm, padded, docStems, tags, df, nil, time.Now())
	if len(matched) != 2 {
		t.Fatalf("weighted matched = %d, want 2", len(matched))
	}
	if matched[0].score <= matched[1].score {
		t.Fatalf("weight 2 tag score %.4f must exceed weight 1 %.4f", matched[0].score, matched[1].score)
	}
}

func TestEvaluatePopulatesBreakdownAndFingerprint(t *testing.T) {
	engine := NewEngine()
	radar := testRadar("website", "developer")
	result := evaluateText(t, engine, radar, "I am looking for a website developer, budget 3000, but I already tried everything and I am frustrated.")
	if result.Fingerprint == "" {
		t.Fatal("missing fingerprint")
	}
	if len(result.Breakdown.MatchedIntentHits) == 0 {
		t.Errorf("missing intent hits: %+v", result.Breakdown)
	}
	if len(result.Breakdown.MatchedIntentHits) == 0 || result.Breakdown.IntentScore <= 0 {
		t.Errorf("breakdown intent not populated: %+v", result.Breakdown)
	}
	if result.Breakdown.DIYFatigueScore <= 0 {
		t.Errorf("DIY score not populated: %+v", result.Breakdown)
	}
	if result.LatencyMicros < 0 {
		t.Errorf("negative latency")
	}
}
