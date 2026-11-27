package telegram

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/demonidze1337/mia-lead-radar/internal/domain"
	"github.com/demonidze1337/mia-lead-radar/internal/events"
	tier0filter "github.com/demonidze1337/mia-lead-radar/internal/filter"
	"github.com/demonidze1337/mia-lead-radar/internal/insights"
	"github.com/demonidze1337/mia-lead-radar/internal/llm"
	"github.com/demonidze1337/mia-lead-radar/internal/prompts"
	"github.com/demonidze1337/mia-lead-radar/internal/storage"
)

const (
	defaultScanInterval = 30 * time.Second
	defaultJoinInterval = 2 * time.Second
	defaultReadInterval = 5 * time.Minute
	keepTrashSamples    = 200
	maxTrashExcerpt     = 500
)

// LeadNotifier is implemented by bot.Service without creating an import cycle.
type LeadNotifier interface {
	NotifyLead(ctx context.Context, lead domain.Lead, radarName string) error
}

// PipelineDeps wires the MTProto pipeline collaborators.
type PipelineDeps struct {
	Store      storage.Store
	Bus        events.Bus
	Filter     tier0filter.Engine
	LLM        llm.Provider
	Insights   insights.Service
	Governor   *RateGovernor
	FloodGate  *FloodGate
	Scout      *Scout
	Joiner     *Joiner
	Captcha    *CaptchaSolver
	Notifier   LeadNotifier
	Transports []AccountTransport

	ScanInterval time.Duration
	JoinInterval time.Duration
	ReadInterval time.Duration
}

// Pipeline coordinates join workers, chat scanning, captcha solving, Tier-0,
// LLM qualification, persistence and Live Terminal events.
type Pipeline struct {
	deps       PipelineDeps
	transports map[int64]AccountTransport
	order      []AccountTransport

	clientsMu sync.Mutex
	scouts    map[int64]*Scout
	joiners   map[int64]*Joiner
	captchas  map[int64]*CaptchaSolver

	running atomic.Bool
	paused  atomic.Bool
	statsMu sync.RWMutex
	stats   PipelineStats
	lastErr string

	processed atomic.Int64
	qualified atomic.Int64
}

// NewPipeline normalizes dependencies and returns a ready coordinator.
func NewPipeline(deps PipelineDeps) *Pipeline {
	if deps.ScanInterval <= 0 {
		deps.ScanInterval = defaultScanInterval
	}
	if deps.JoinInterval <= 0 {
		deps.JoinInterval = defaultJoinInterval
	}
	if deps.ReadInterval <= 0 {
		deps.ReadInterval = defaultReadInterval
	}
	p := &Pipeline{
		deps:       deps,
		transports: make(map[int64]AccountTransport, len(deps.Transports)),
		order:      append([]AccountTransport{}, deps.Transports...),
		scouts:     make(map[int64]*Scout, len(deps.Transports)),
		joiners:    make(map[int64]*Joiner, len(deps.Transports)),
		captchas:   make(map[int64]*CaptchaSolver, len(deps.Transports)),
	}
	for _, transport := range deps.Transports {
		if transport != nil {
			p.transports[transport.AccountID()] = transport
		}
	}
	return p
}

// Pause temporarily suspends join/scan/read work.
func (p *Pipeline) Pause() { p.paused.Store(true) }

// Resume restarts suspended work.
func (p *Pipeline) Resume() { p.paused.Store(false) }

// Stats returns a consistent snapshot for API and Owner Bot status views.
func (p *Pipeline) Stats() PipelineStats {
	p.statsMu.RLock()
	defer p.statsMu.RUnlock()
	stats := p.stats
	stats.Running = p.running.Load()
	stats.Paused = p.paused.Load()
	stats.ProcessedMsgs = p.processed.Load()
	stats.QualifiedLeads = p.qualified.Load()
	stats.LastError = p.lastErr
	return stats
}

// Run starts worker loops and blocks until ctx is cancelled, then drains them.
func (p *Pipeline) Run(ctx context.Context) error {
	if p == nil || p.deps.Store == nil {
		return fmt.Errorf("%w: pipeline store is not configured", domain.ErrInvalidInput)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if !p.running.CompareAndSwap(false, true) {
		return fmt.Errorf("%w: pipeline is already running", domain.ErrInvalidInput)
	}
	defer p.running.Store(false)
	p.refreshAccountStats(ctx)

	var wg sync.WaitGroup
	wg.Add(3)
	go func() { defer wg.Done(); p.loop(ctx, p.deps.JoinInterval, p.joinOnce) }()
	go func() { defer wg.Done(); p.loop(ctx, p.deps.ScanInterval, p.scanOnce) }()
	go func() { defer wg.Done(); p.loop(ctx, p.deps.ReadInterval, p.readOnce) }()

	<-ctx.Done()
	p.running.Store(false)
	wg.Wait()
	return nil
}

func (p *Pipeline) loop(ctx context.Context, interval time.Duration, fn func(context.Context)) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if p.paused.Load() {
				continue
			}
			fn(ctx)
		}
	}
}

func (p *Pipeline) setError(err error) {
	if err == nil {
		return
	}
	p.statsMu.Lock()
	p.lastErr = err.Error()
	p.statsMu.Unlock()
}

func (p *Pipeline) accessors() (tier0filter.Engine, llm.Provider, insights.Service) {
	return p.deps.Filter, p.deps.LLM, p.deps.Insights
}

func (p *Pipeline) joinOnce(ctx context.Context) {
	item, err := p.deps.Store.ClaimNextJoinJob(ctx, time.Now().UTC())
	if err != nil {
		p.setError(err)
		return
	}
	if item == nil {
		return
	}
	transport, ok := p.pickTransport(ctx)
	if !ok {
		at := time.Now().UTC().Add(30 * time.Second)
		_ = p.deps.Store.CompleteJoinJob(ctx, item.ID, domain.JoinJobPending, &at, "no available account")
		return
	}
	joiner := p.joinerFor(transport)
	outcome, err := joiner.Join(ctx, *item)
	if err != nil {
		p.setError(err)
		return
	}
	if outcome.ErrorClass == TGErrorFloodWait {
		p.publishFlood(ctx, transport.AccountID(), outcome.FloodWait)
	}
	p.refreshAccountStats(ctx)
}

func (p *Pipeline) scanOnce(ctx context.Context) {
	p.refreshAccountStats(ctx)
	radar, err := p.deps.Store.GetActiveRadar(ctx)
	if err != nil {
		if !errors.Is(err, domain.ErrNotFound) {
			p.setError(err)
		}
		return
	}
	settings, err := p.deps.Store.GetSettings(ctx)
	if err != nil {
		p.setError(err)
		return
	}
	throttles := p.throttledTags(ctx, radar.ID)
	chats, err := p.deps.Store.ListChats(ctx, storage.ChatFilter{
		Statuses: []domain.ChatStatus{domain.ChatStatusJoined},
		Limit:    100,
	})
	if err != nil {
		p.setError(err)
		return
	}
	for _, chat := range chats {
		if ctx.Err() != nil {
			return
		}
		transport, ok := p.transportForChat(ctx, chat)
		if !ok {
			continue
		}
		if p.deps.Governor != nil {
			if err := p.deps.Governor.Wait(ctx, transport.AccountID(), OpFetchMessages); err != nil {
				return
			}
		}
		messages, err := transport.GetRecentMessages(ctx, chat.TGChatID, chat.AccessHash, chat.LastMessageID, 20)
		if err != nil {
			p.handleTransportError(ctx, transport.AccountID(), err)
			continue
		}
		lastID := chat.LastMessageID
		for _, message := range messages {
			if message.MsgID > lastID {
				lastID = message.MsgID
			}
			p.processMessage(ctx, transport, radar, settings, throttles, chat, message)
		}
		now := time.Now().UTC()
		if err := p.deps.Store.UpdateChatProgress(ctx, chat.ID, lastID, nil, &now); err != nil {
			p.setError(err)
		}
	}
}

func (p *Pipeline) processMessage(ctx context.Context, transport AccountTransport, radar domain.RadarProfile, settings domain.RuntimeSettings, throttles map[string]time.Time, chat domain.Chat, message IncomingMessage) {
	message.ChatID = chat.ID
	message.TGChatID = chat.TGChatID
	message.AccessHash = chat.AccessHash
	message.ChatTitle = chat.Title
	message.ChatUsername = chat.Username
	message.ChatType = chat.ChatType

	captcha := p.captchaFor(transport)
	if captcha != nil {
		solved, err := captcha.Solve(ctx, message)
		if err != nil {
			p.setError(err)
		}
		if solved {
			return
		}
	}
	if scout := p.scoutFor(transport); scout != nil {
		if _, err := scout.ExtractAndEnqueueLinks(ctx, []IncomingMessage{message}); err != nil {
			p.setError(err)
		}
	}

	filterEngine, provider, insightService := p.accessors()
	if filterEngine == nil {
		p.setError(errors.New("pipeline: Tier-0 engine is not configured"))
		return
	}
	result := filterEngine.Evaluate(tier0filter.CandidateInput{
		ChatID:     chat.ID,
		MsgID:      message.MsgID,
		AuthorID:   message.AuthorID,
		AuthorName: message.AuthorName,
		IsBot:      message.IsBot,
		IsService:  message.IsService,
		Text:       message.Text,
		Timestamp:  message.Timestamp,
	}, radar, throttles)
	p.processed.Add(1)

	if insightService != nil {
		if err := insightService.RecordTier0Outcome(ctx, radar.ID, result.Passed, result.MatchedTags); err != nil {
			p.setError(err)
		}
	}

	if !result.Passed {
		p.publish(ctx, domain.EventTier0Rejected, "info", "Tier-0 rejected message", map[string]any{
			"chat_id":        chat.ID,
			"msg_id":         message.MsgID,
			"reason":         string(result.RejectReason),
			"score":          result.Score,
			"lang":           result.DetectedLang,
			"matched_tags":   result.MatchedTags,
			"latency_micros": result.LatencyMicros,
		})
		p.saveTrash(ctx, radar, chat, message, "tier0", string(result.RejectReason), result.MatchedTags, int(result.Score), result.DetectedLang)
		return
	}
	p.publish(ctx, domain.EventTier0Passed, "info", "Tier-0 passed message", map[string]any{
		"chat_id":      chat.ID,
		"msg_id":       message.MsgID,
		"score":        result.Score,
		"lang":         result.DetectedLang,
		"matched_tags": result.MatchedTags,
	})

	if provider == nil {
		p.setError(errors.New("pipeline: LLM provider is not configured"))
		p.saveTrash(ctx, radar, chat, message, "llm", "llm_unavailable", result.MatchedTags, 0, result.DetectedLang)
		return
	}
	var qualified prompts.QualifyResult
	response, err := provider.CompleteJSON(ctx, llm.ChatRequest{
		Tier:        llm.TierFast,
		Messages:    prompts.BuildQualifierMessages(radar, settings.UILanguage, chat.Title, result.DetectedLang, message.Text, result.MatchedTags),
		Temperature: 0.1,
		Schema:      prompts.QualifySchema(),
	}, &qualified)
	if err != nil {
		p.setError(err)
		return
	}
	score := qualified.Score
	if score < 0 {
		score = 0
	}
	if score > 100 {
		score = 100
	}
	category := domain.CategoryFromScore(score)
	if !qualified.IsLead || !category.IsLead() {
		category = domain.LeadCategoryTrash
	}
	if insightService != nil {
		if err := insightService.RecordLLMOutcome(ctx, radar.ID, category, result.MatchedTags); err != nil {
			p.setError(err)
		}
	}
	p.publish(ctx, domain.EventLLMQualified, "info", "LLM qualification completed", map[string]any{
		"chat_id":  chat.ID,
		"msg_id":   message.MsgID,
		"score":    score,
		"category": string(category),
		"lead":     category.IsLead(),
	})
	if !category.IsLead() || score < settings.MinLeadScore {
		p.saveTrash(ctx, radar, chat, message, "llm", string(category), result.MatchedTags, score, result.DetectedLang)
		return
	}
	lead := domain.Lead{
		RadarID:          radar.ID,
		ChatID:           chat.ID,
		TGChatID:         chat.TGChatID,
		ChatTitle:        chat.Title,
		ChatUsername:     chat.Username,
		MsgID:            message.MsgID,
		MsgLink:          BuildMessageLink(chat.Username, chat.TGChatID, message.MsgID),
		AuthorID:         message.AuthorID,
		AuthorUsername:   message.AuthorUsername,
		AuthorName:       message.AuthorName,
		Text:             message.Text,
		Lang:             firstNonEmpty(qualified.Lang, result.DetectedLang, "und"),
		Score:            score,
		Category:         category,
		Status:           domain.LeadStatusNew,
		Reason:           qualified.Reason,
		MatchedTags:      result.MatchedTags,
		Tier0Score:       result.Score,
		LLMModel:         response.Model,
		SavedTimeSeconds: 45,
	}
	saved, inserted, err := p.deps.Store.SaveLead(ctx, lead)
	if err != nil {
		p.setError(err)
		return
	}
	if !inserted {
		return
	}
	p.qualified.Add(1)
	p.publish(ctx, domain.EventLeadNew, "success", "Qualified lead saved", map[string]any{
		"lead_id":  saved.ID,
		"score":    saved.Score,
		"category": string(saved.Category),
		"chat_id":  chat.ID,
		"msg_id":   message.MsgID,
	})
	if settings.WebhookURL != "" && insightService != nil {
		if err := insightService.DispatchLeadWebhook(ctx, settings.WebhookURL, "", radar.Name, saved); err != nil {
			p.setError(err)
		}
	}
	if p.deps.Notifier != nil {
		if err := p.deps.Notifier.NotifyLead(ctx, saved, radar.Name); err != nil {
			p.setError(err)
		}
	}
}

func (p *Pipeline) readOnce(ctx context.Context) {
	settings, err := p.deps.Store.GetSettings(ctx)
	if err != nil {
		p.setError(err)
		return
	}
	if !settings.AutoReadEnabled {
		return
	}
	chats, err := p.deps.Store.ListChats(ctx, storage.ChatFilter{
		Statuses: []domain.ChatStatus{domain.ChatStatusJoined},
		Limit:    100,
	})
	if err != nil {
		p.setError(err)
		return
	}
	for _, chat := range chats {
		if chat.LastMessageID <= 0 {
			continue
		}
		transport, ok := p.transportForChat(ctx, chat)
		if !ok {
			continue
		}
		if p.deps.Governor != nil {
			if err := p.deps.Governor.Wait(ctx, transport.AccountID(), OpReadHistory); err != nil {
				return
			}
		}
		if err := transport.ReadHistory(ctx, chat.TGChatID, chat.AccessHash, chat.LastMessageID); err != nil {
			p.handleTransportError(ctx, transport.AccountID(), err)
			continue
		}
		now := time.Now().UTC()
		if err := p.deps.Store.UpdateChatProgress(ctx, chat.ID, chat.LastMessageID, &now, nil); err != nil {
			p.setError(err)
		}
	}
}

func (p *Pipeline) saveTrash(ctx context.Context, radar domain.RadarProfile, chat domain.Chat, message IncomingMessage, source, reason string, tags []string, score int, lang string) {
	excerpt := message.Text
	if len([]rune(excerpt)) > maxTrashExcerpt {
		excerpt = string([]rune(excerpt)[:maxTrashExcerpt])
	}
	sample := domain.TrashSample{
		RadarID:     radar.ID,
		Source:      source,
		Reason:      reason,
		MatchedTags: tags,
		Score:       score,
		ChatTitle:   chat.Title,
		AuthorID:    message.AuthorID,
		TextExcerpt: excerpt,
		Lang:        firstNonEmpty(lang, "und"),
	}
	if err := p.deps.Store.SaveTrashSample(ctx, sample, keepTrashSamples); err != nil {
		p.setError(err)
	}
}

func (p *Pipeline) publish(ctx context.Context, eventType domain.EventType, level, message string, data map[string]any) {
	if p.deps.Bus == nil {
		return
	}
	p.deps.Bus.Publish(ctx, domain.Event{
		Type:      eventType,
		Level:     level,
		Message:   message,
		Data:      data,
		Timestamp: time.Now().UTC(),
	})
}

func (p *Pipeline) publishFlood(ctx context.Context, accountID int64, wait time.Duration) {
	p.publish(ctx, domain.EventMTProtoFlood, "warn", fmt.Sprintf("FloodWait for account %d", accountID), map[string]any{
		"account_id": accountID,
		"wait":       wait.String(),
	})
}

func (p *Pipeline) throttledTags(ctx context.Context, radarID int64) map[string]time.Time {
	stats, err := p.deps.Store.ListTagStats(ctx, radarID)
	if err != nil {
		p.setError(err)
		return nil
	}
	now := time.Now().UTC()
	out := make(map[string]time.Time)
	for _, stat := range stats {
		if stat.ThrottledUntil != nil && stat.ThrottledUntil.After(now) {
			out[stat.Tag] = *stat.ThrottledUntil
		}
	}
	return out
}

func (p *Pipeline) transportForChat(ctx context.Context, chat domain.Chat) (AccountTransport, bool) {
	if transport, ok := p.transports[chat.AssignedAccountID]; ok {
		if p.deps.FloodGate == nil {
			return transport, true
		}
		if blocked, _ := p.deps.FloodGate.IsBlocked(transport.AccountID()); !blocked {
			return transport, true
		}
	}
	return p.pickTransport(ctx)
}

func (p *Pipeline) pickTransport(ctx context.Context) (AccountTransport, bool) {
	if p.deps.FloodGate == nil {
		for _, transport := range p.order {
			if transport != nil {
				return transport, true
			}
		}
		return nil, false
	}
	accounts, err := p.deps.Store.ListAccounts(ctx)
	if err != nil {
		p.setError(err)
		return nil, false
	}
	account, ok := p.deps.FloodGate.PickAvailableAccount(accounts)
	if !ok {
		return nil, false
	}
	transport, exists := p.transports[account.ID]
	return transport, exists
}

func (p *Pipeline) handleTransportError(ctx context.Context, accountID int64, err error) {
	class, wait := ClassifyTGError(err)
	switch class {
	case TGErrorFloodWait:
		if p.deps.FloodGate != nil {
			if _, floodErr := p.deps.FloodGate.RegisterFlood(ctx, accountID, wait, err.Error()); floodErr != nil {
				p.setError(floodErr)
			}
		}
		p.publishFlood(ctx, accountID, wait)
	case TGErrorBanned:
		if statusErr := p.deps.Store.SetAccountStatus(ctx, accountID, domain.AccountStatusBanned, err.Error()); statusErr != nil {
			p.setError(statusErr)
		}
	case TGErrorChannelsTooMuch:
		if statusErr := p.deps.Store.SetAccountStatus(ctx, accountID, domain.AccountStatusChannelsFull, err.Error()); statusErr != nil {
			p.setError(statusErr)
		}
	}
	p.setError(err)
	p.refreshAccountStats(ctx)
}

func (p *Pipeline) refreshAccountStats(ctx context.Context) {
	if p.deps.Store == nil {
		return
	}
	accounts, err := p.deps.Store.ListAccounts(ctx)
	if err != nil {
		p.setError(err)
		return
	}
	active, flooded := 0, 0
	for _, acc := range accounts {
		if acc.Status == domain.AccountStatusActive {
			active++
		}
		if p.deps.FloodGate != nil {
			if blocked, _ := p.deps.FloodGate.IsBlocked(acc.ID); blocked {
				flooded++
			}
		}
	}
	p.statsMu.Lock()
	p.stats.ActiveAccounts = active
	p.stats.FloodedAccount = flooded
	p.statsMu.Unlock()
}

func (p *Pipeline) joinerFor(transport AccountTransport) *Joiner {
	if p.deps.Joiner != nil && len(p.order) == 1 {
		return p.deps.Joiner
	}
	p.clientsMu.Lock()
	defer p.clientsMu.Unlock()
	if existing := p.joiners[transport.AccountID()]; existing != nil {
		return existing
	}
	created := NewJoiner(p.deps.Store, p.deps.FloodGate, p.deps.Governor, transport)
	p.joiners[transport.AccountID()] = created
	return created
}

func (p *Pipeline) scoutFor(transport AccountTransport) *Scout {
	if p.deps.Scout != nil && len(p.order) == 1 {
		return p.deps.Scout
	}
	p.clientsMu.Lock()
	defer p.clientsMu.Unlock()
	if existing := p.scouts[transport.AccountID()]; existing != nil {
		return existing
	}
	created := NewScout(p.deps.Store, p.deps.Governor, transport)
	p.scouts[transport.AccountID()] = created
	return created
}

func (p *Pipeline) captchaFor(transport AccountTransport) *CaptchaSolver {
	if p.deps.Captcha != nil && len(p.order) == 1 {
		return p.deps.Captcha
	}
	p.clientsMu.Lock()
	defer p.clientsMu.Unlock()
	if existing := p.captchas[transport.AccountID()]; existing != nil {
		return existing
	}
	created := NewCaptchaSolver(transport, p.deps.Governor, p.deps.LLM)
	p.captchas[transport.AccountID()] = created
	return created
}
