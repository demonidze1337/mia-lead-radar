package filter

import (
	"math"
	"sort"
	"strings"
	"time"

	"github.com/demonidze1337/mia-lead-radar/internal/domain"
)

const (
	defaultMinScore = 31.0
	bm25K1          = 1.2
	bm25B           = 0.6
	avgDocLen       = 24.0
	maxDocLen       = 240
)

type phraseMatcher struct {
	display string
	norm    string
	padded  string
	cjk     bool
}

func compilePhrase(raw string) (phraseMatcher, bool) {
	norm := Normalize(raw)
	if norm == "" {
		return phraseMatcher{}, false
	}
	return phraseMatcher{
		display: strings.TrimSpace(raw),
		norm:    norm,
		padded:  padText(norm),
		cjk:     hasCJK(norm),
	}, true
}

func (p phraseMatcher) matches(norm, padded string) bool {
	if p.norm == "" {
		return false
	}
	if p.cjk {
		return strings.Contains(norm, p.norm)
	}
	return strings.Contains(padded, p.padded)
}

type compiledPack struct {
	detectWords map[string]struct{}
	intent      []phraseMatcher
	diy         []phraseMatcher
	seller      []phraseMatcher
	hr          []phraseMatcher
	spam        []phraseMatcher
	service     []phraseMatcher
}

var (
	compiledPacks  []*compiledPack
	compiledByCode map[string]*compiledPack
)

func init() {
	compiledPacks = make([]*compiledPack, 0, len(languagePacks))
	compiledByCode = make(map[string]*compiledPack, len(languagePacks))
	for _, pack := range languagePacks {
		compiled := &compiledPack{detectWords: make(map[string]struct{}, len(pack.DetectWords))}
		for _, word := range pack.DetectWords {
			for _, token := range Tokenize(Normalize(word)) {
				compiled.detectWords[token] = struct{}{}
			}
		}
		compiled.intent = compilePhrases(pack.IntentSignals)
		compiled.diy = compilePhrases(pack.DIYSignals)
		compiled.seller = compilePhrases(pack.SellerSignals)
		compiled.hr = compilePhrases(pack.HRSignals)
		compiled.spam = compilePhrases(pack.SpamSignals)
		compiled.service = compilePhrases(pack.ServiceSignals)
		compiledPacks = append(compiledPacks, compiled)
		compiledByCode[pack.Code] = compiled
	}
}

func compilePhrases(raw []string) []phraseMatcher {
	out := make([]phraseMatcher, 0, len(raw))
	seen := make(map[string]struct{}, len(raw))
	for _, phrase := range raw {
		matcher, ok := compilePhrase(phrase)
		if !ok {
			continue
		}
		if _, exists := seen[matcher.norm]; exists {
			continue
		}
		seen[matcher.norm] = struct{}{}
		out = append(out, matcher)
	}
	return out
}

// Tier0Engine is the zero-network prefilter. It is safe for concurrent use.
type Tier0Engine struct {
	dedup    *Deduper
	minScore float64
	now      func() time.Time
	byCode   map[string]*compiledPack
	allPacks []*compiledPack
}

type engineConfig struct {
	dedupCapacity int
	dedupTTL      time.Duration
	minScore      float64
	now           func() time.Time
}

// Option customizes Tier0Engine construction.
type Option func(*engineConfig)

// WithDedupCapacity bounds the in-memory dedup cache.
func WithDedupCapacity(capacity int) Option {
	return func(cfg *engineConfig) {
		if capacity > 0 {
			cfg.dedupCapacity = capacity
		}
	}
}

// WithDedupTTL sets how long fingerprints remain valid.
func WithDedupTTL(ttl time.Duration) Option {
	return func(cfg *engineConfig) {
		if ttl > 0 {
			cfg.dedupTTL = ttl
		}
	}
}

// WithMinScore overrides the pass threshold (default 31, matching Doubtful).
func WithMinScore(score float64) Option {
	return func(cfg *engineConfig) {
		cfg.minScore = score
	}
}

// WithClock injects a deterministic clock for tests.
func WithClock(now func() time.Time) Option {
	return func(cfg *engineConfig) {
		if now != nil {
			cfg.now = now
		}
	}
}

// NewEngine builds a Tier-0 engine with the shared multilingual pack table.
func NewEngine(opts ...Option) *Tier0Engine {
	cfg := engineConfig{
		dedupCapacity: defaultDedupCapacity,
		dedupTTL:      defaultDedupTTL,
		minScore:      defaultMinScore,
		now:           time.Now,
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	if math.IsNaN(cfg.minScore) {
		cfg.minScore = defaultMinScore
	}
	if cfg.minScore < 0 {
		cfg.minScore = 0
	}
	if cfg.minScore > 100 {
		cfg.minScore = 100
	}
	return &Tier0Engine{
		dedup:    newDeduper(cfg.dedupCapacity, cfg.dedupTTL, cfg.now),
		minScore: cfg.minScore,
		now:      cfg.now,
		byCode:   compiledByCode,
		allPacks: compiledPacks,
	}
}

// DetectLanguage returns the detected language code and confidence.
func (e *Tier0Engine) DetectLanguage(text string) (string, float64) {
	return detectLanguage(Normalize(text))
}

var _ Engine = (*Tier0Engine)(nil)

type packHits struct {
	intent  []string
	diy     []string
	seller  []string
	hr      []string
	spam    []string
	service []string
}

func (e *Tier0Engine) packsFor(lang string) []*compiledPack {
	if pack, ok := e.byCode[lang]; ok {
		if lang == "en" {
			return []*compiledPack{pack}
		}
		if en, exists := e.byCode["en"]; exists {
			return []*compiledPack{pack, en}
		}
		return []*compiledPack{pack}
	}
	return e.allPacks
}

func collectHits(packs []*compiledPack, norm, padded string) packHits {
	hits := packHits{}
	seen := map[string]map[string]struct{}{
		"intent": {}, "diy": {}, "seller": {}, "hr": {}, "spam": {}, "service": {},
	}
	for _, pack := range packs {
		collectPhraseHits(&hits.intent, seen["intent"], pack.intent, norm, padded)
		collectPhraseHits(&hits.diy, seen["diy"], pack.diy, norm, padded)
		collectPhraseHits(&hits.seller, seen["seller"], pack.seller, norm, padded)
		collectPhraseHits(&hits.hr, seen["hr"], pack.hr, norm, padded)
		collectPhraseHits(&hits.spam, seen["spam"], pack.spam, norm, padded)
		collectPhraseHits(&hits.service, seen["service"], pack.service, norm, padded)
	}
	return hits
}

func collectPhraseHits(dst *[]string, seen map[string]struct{}, phrases []phraseMatcher, norm, padded string) {
	for _, phrase := range phrases {
		if _, exists := seen[phrase.norm]; exists {
			continue
		}
		if phrase.matches(norm, padded) {
			seen[phrase.norm] = struct{}{}
			*dst = append(*dst, phrase.display)
		}
	}
}

func isShortNoise(norm string, tokens []string) bool {
	if norm == "" || len(tokens) == 0 {
		return true
	}
	if runeLen(norm) < 3 {
		return true
	}
	if !hasCJK(norm) && runeLen(norm) < 4 {
		return true
	}
	if !hasCJK(norm) && len(tokens) < 2 && runeLen(norm) < 8 {
		return true
	}
	return false
}

func hasServiceSignal(packs []*compiledPack, norm, padded string) bool {
	for _, pack := range packs {
		for _, phrase := range pack.service {
			if phrase.matches(norm, padded) {
				return true
			}
		}
	}
	return false
}

func (e *Tier0Engine) evaluate(input CandidateInput, radar domain.RadarProfile, throttledTags map[string]time.Time) EvalResult {
	norm := Normalize(input.Text)
	padded := padText(norm)
	tokens := Tokenize(norm)
	lang, confidence := detectLanguage(norm)
	result := EvalResult{
		DetectedLang:  lang,
		LangConf:      confidence,
		Fingerprint:   e.dedup.Fingerprint(norm),
		MatchedTags:   []string{},
		ThrottledTags: []string{},
	}

	if input.IsBot || input.IsService {
		result.RejectReason = RejectServiceBot
		return result
	}
	packs := e.packsFor(lang)
	if hasServiceSignal(packs, norm, padded) {
		result.RejectReason = RejectServiceBot
		return result
	}
	if isShortNoise(norm, tokens) {
		result.RejectReason = RejectEmptyOrShort
		return result
	}
	if e.dedup.Check(result.Fingerprint, tokens, input.AuthorID, input.DryRun) {
		result.RejectReason = RejectDuplicate
		return result
	}

	hits := collectHits(packs, norm, padded)
	result.Breakdown = buildBreakdown(result.Breakdown, hits, radar.AllowHRVacancies)

	if len(hits.spam) >= 2 && len(hits.intent) == 0 && len(hits.diy) == 0 {
		result.RejectReason = RejectMarketplaceSpam
		return result
	}
	if len(hits.spam) >= 1 && len(hits.seller) >= 1 && len(hits.intent) == 0 {
		result.RejectReason = RejectMarketplaceSpam
		return result
	}
	if matchedStops := matchRadarStops(norm, padded, radar); len(matchedStops) > 0 {
		result.Breakdown.StopWordPenalty = -100
		result.RejectReason = RejectStopPhrase
		return result
	}
	if !radar.AllowHRVacancies && len(hits.hr) > 0 {
		result.Breakdown.HRPenalty = -float64(min(len(hits.hr), 3) * 20)
		result.RejectReason = RejectHRVacancy
		return result
	}
	if len(hits.seller) > 0 && len(hits.intent) == 0 && len(hits.diy) == 0 {
		result.Breakdown.SellerPenalty = -float64(min(len(hits.seller), 3) * 10)
		result.RejectReason = RejectSellerPromo
		return result
	}

	docStems := make([]string, len(tokens))
	for i, token := range tokens {
		docStems[i] = stemToken(token, lang)
	}
	tags, df := compileRadarTags(radar, lang)
	matched, throttledMatches := matchTags(norm, padded, docStems, tags, df, throttledTags, e.now().UTC())
	result.ThrottledTags = throttledMatches
	if len(matched) == 0 {
		if len(throttledMatches) > 0 {
			result.RejectReason = RejectTagThrottled
		} else {
			result.RejectReason = RejectNoTagMatch
		}
		return result
	}

	matchedTags := make([]string, 0, len(matched))
	for _, tag := range matched {
		matchedTags = append(matchedTags, tag.display)
	}
	result.MatchedTags = matchedTags

	if len(matched) == 1 && !matched[0].strong && len(hits.intent) == 0 && len(hits.diy) == 0 {
		result.RejectReason = RejectBroadTagNoProof
		return result
	}

	bm25Raw := 0.0
	for _, tag := range matched {
		bm25Raw += tag.score
	}
	bm25Score := 45 * (1 - math.Exp(-bm25Raw/1.2))
	intentScore := 0.0
	if len(hits.intent) > 0 {
		intentScore = 10 + float64(min(len(hits.intent), 3))*12
	}
	diyScore := 0.0
	if len(hits.diy) > 0 {
		diyScore = 10 + float64(min(len(hits.diy), 2))*10
	}
	extraTags := float64(min(len(matched)-1, 3)) * 5
	strongBonus := 0.0
	for _, tag := range matched {
		if tag.strong {
			strongBonus = 10
			break
		}
	}
	synergy := 0.0
	if len(hits.intent) > 0 && len(hits.diy) > 0 {
		synergy = 6
	}
	sellerPenalty := -float64(min(len(hits.seller), 3) * 10)
	spamPerHit := 15.0
	if len(hits.intent) > 0 || len(hits.diy) > 0 {
		// Topic words such as "crypto" are weak evidence when the author is
		// clearly the buyer; keep them as a diagnostic penalty only.
		spamPerHit = 5
	}
	spamPenalty := -float64(min(len(hits.spam), 3)) * spamPerHit
	hrPenalty := 0.0
	if !radar.AllowHRVacancies && len(hits.hr) > 0 {
		hrPenalty = -float64(min(len(hits.hr), 3) * 20)
	}

	score := bm25Score + intentScore + diyScore + extraTags + strongBonus + synergy + sellerPenalty + spamPenalty + hrPenalty
	score = math.Max(0, math.Min(100, score))

	result.Breakdown.BM25Score = bm25Score
	result.Breakdown.IntentScore = intentScore
	result.Breakdown.DIYFatigueScore = diyScore
	result.Breakdown.SellerPenalty = sellerPenalty
	result.Breakdown.SpamPenalty = spamPenalty
	result.Breakdown.HRPenalty = hrPenalty
	result.Score = score

	if score < e.minScore {
		result.RejectReason = RejectLowScore
		return result
	}
	result.Passed = true
	result.RejectReason = RejectNone
	return result
}

// Evaluate scores a candidate against a radar profile and tag throttles.
func (e *Tier0Engine) Evaluate(input CandidateInput, radar domain.RadarProfile, throttledTags map[string]time.Time) EvalResult {
	start := time.Now()
	result := e.evaluate(input, radar, throttledTags)
	result.LatencyMicros = time.Since(start).Microseconds()
	return result
}

func buildBreakdown(base SignalBreakdown, hits packHits, allowHR bool) SignalBreakdown {
	base.MatchedIntentHits = append([]string{}, hits.intent...)
	base.MatchedSellerHits = append([]string{}, hits.seller...)
	base.MatchedHRHits = append([]string{}, hits.hr...)
	base.SellerPenalty = -float64(min(len(hits.seller), 3) * 10)
	base.SpamPenalty = -float64(min(len(hits.spam), 3) * 15)
	if !allowHR && len(hits.hr) > 0 {
		base.HRPenalty = -float64(min(len(hits.hr), 3) * 20)
	}
	return base
}

func matchRadarStops(norm, padded string, radar domain.RadarProfile) []string {
	phrases := make([]string, 0, len(radar.StopPhrases)+len(radar.LearnedStopWords))
	phrases = append(phrases, radar.StopPhrases...)
	phrases = append(phrases, radar.LearnedStopWords...)
	seen := make(map[string]struct{}, len(phrases))
	out := make([]string, 0, 2)
	for _, raw := range phrases {
		matcher, ok := compilePhrase(raw)
		if !ok {
			continue
		}
		if _, exists := seen[matcher.norm]; exists {
			continue
		}
		seen[matcher.norm] = struct{}{}
		if matcher.matches(norm, padded) {
			out = append(out, matcher.display)
		}
	}
	return out
}

type compiledTag struct {
	display string
	norm    string
	padded  string
	cjk     bool
	weight  float64
	unique  []string
}

func compileRadarTags(radar domain.RadarProfile, lang string) ([]compiledTag, map[string]int) {
	tags := make([]compiledTag, 0, len(radar.Tags))
	df := make(map[string]int)
	for _, raw := range radar.Tags {
		if !raw.Enabled {
			continue
		}
		phrase := Normalize(raw.Phrase)
		if phrase == "" {
			continue
		}
		tokens := Tokenize(phrase)
		if len(tokens) == 0 {
			continue
		}
		unique := make([]string, 0, len(tokens))
		seen := make(map[string]struct{}, len(tokens))
		for _, token := range tokens {
			stemmed := stemToken(token, lang)
			if _, exists := seen[stemmed]; exists {
				continue
			}
			seen[stemmed] = struct{}{}
			unique = append(unique, stemmed)
		}
		weight := raw.Weight
		if weight <= 0 {
			weight = 1
		}
		if weight > 5 {
			weight = 5
		}
		kindFactor := 1.0
		if raw.Kind == domain.RadarTagLive {
			kindFactor = 0.95
		}
		tag := compiledTag{
			display: raw.Phrase,
			norm:    phrase,
			padded:  padText(phrase),
			cjk:     hasCJK(phrase),
			weight:  weight * kindFactor,
			unique:  unique,
		}
		tags = append(tags, tag)
		for _, token := range unique {
			df[token]++
		}
	}
	return tags, df
}

type matchedTag struct {
	display string
	score   float64
	strong  bool
}

func matchTags(norm, padded string, docStems []string, tags []compiledTag, df map[string]int, throttledTags map[string]time.Time, now time.Time) ([]matchedTag, []string) {
	if len(tags) == 0 {
		return nil, nil
	}
	counts := make(map[string]int, len(docStems))
	for _, token := range docStems {
		counts[token]++
	}
	docLen := len(docStems)
	if docLen > maxDocLen {
		docLen = maxDocLen
	}
	normFactor := bm25K1 * (1 - bm25B + bm25B*float64(docLen)/avgDocLen)
	tagCount := float64(len(tags))

	matched := make([]matchedTag, 0, len(tags))
	throttled := make([]string, 0, 2)
	for _, tag := range tags {
		phraseHit := false
		if tag.cjk {
			phraseHit = strings.Contains(norm, tag.norm)
		} else {
			phraseHit = strings.Contains(padded, tag.padded)
		}
		present := 0
		termScore := 0.0
		for _, token := range tag.unique {
			tf := counts[token]
			if tf == 0 {
				continue
			}
			present++
			if tf > 3 {
				tf = 3
			}
			dfv := df[token]
			if dfv < 1 {
				dfv = 1
			}
			idf := math.Log(1 + (tagCount-float64(dfv)+0.5)/(float64(dfv)+0.5))
			termScore += idf * (float64(tf) * (bm25K1 + 1)) / (float64(tf) + normFactor)
		}
		coverage := float64(present) / float64(len(tag.unique))
		if phraseHit {
			coverage = 1
			termScore += 0.5
		}
		if coverage < 0.6 {
			continue
		}
		coverageFactor := 0.6 + 0.4*coverage
		score := tag.weight * coverageFactor * termScore
		isStrong := coverage >= 0.999 && len(tag.unique) >= 2
		display := tag.display
		if isThrottled(throttledTags, display, tag.norm, now) {
			throttled = append(throttled, display)
			continue
		}
		matched = append(matched, matchedTag{
			display: display,
			score:   score,
			strong:  isStrong,
		})
	}
	sort.SliceStable(matched, func(i, j int) bool {
		return matched[i].score > matched[j].score
	})
	return matched, throttled
}

func isThrottled(throttledTags map[string]time.Time, display, norm string, now time.Time) bool {
	if len(throttledTags) == 0 {
		return false
	}
	for _, key := range []string{strings.TrimSpace(display), norm} {
		if until, ok := throttledTags[key]; ok && until.After(now) {
			return true
		}
	}
	return false
}
