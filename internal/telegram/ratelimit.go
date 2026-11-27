package telegram

import (
	"context"
	"fmt"
	"math/rand"
	"sync"
	"time"

	"github.com/demonidze1337/mia-lead-radar/internal/domain"
	"github.com/demonidze1337/mia-lead-radar/internal/storage"
)

// OpKind identifies rate-limited MTProto action classes.
type OpKind string

const (
	OpJoin          OpKind = "join"
	OpSearch        OpKind = "search"
	OpReadHistory   OpKind = "read_history"
	OpFetchMessages OpKind = "fetch_messages"
	OpCaptcha       OpKind = "captcha"
	OpAuth          OpKind = "auth"
)

// OpPolicy defines token-bucket capacity, refill interval, and anti-detection jitter.
type OpPolicy struct {
	Burst    float64
	Interval time.Duration
	Jitter   time.Duration
}

// DefaultOpPolicies provides conservative anti-ban MTProto pacing per account.
func DefaultOpPolicies() map[OpKind]OpPolicy {
	return map[OpKind]OpPolicy{
		OpJoin:          {Burst: 1, Interval: 45 * time.Second, Jitter: 10 * time.Second},
		OpSearch:        {Burst: 2, Interval: 5 * time.Second, Jitter: 1500 * time.Millisecond},
		OpReadHistory:   {Burst: 3, Interval: 2 * time.Second, Jitter: 500 * time.Millisecond},
		OpFetchMessages: {Burst: 5, Interval: 1 * time.Second, Jitter: 250 * time.Millisecond},
		OpCaptcha:       {Burst: 2, Interval: 2 * time.Second, Jitter: 400 * time.Millisecond},
		OpAuth:          {Burst: 3, Interval: 2 * time.Second, Jitter: 0},
	}
}

type bucketKey struct {
	accountID int64
	op        OpKind
}

type tokenBucket struct {
	tokens     float64
	lastRefill time.Time
}

// RateGovernor enforces per-account, per-operation token-bucket pacing with
// context-cancellable timer waits (never time.Sleep).
type RateGovernor struct {
	mu       sync.Mutex
	policies map[OpKind]OpPolicy
	buckets  map[bucketKey]*tokenBucket
	now      func() time.Time
	jitter   func(time.Duration) time.Duration
}

// NewRateGovernor constructs a token-bucket governor with optional custom policies/clock.
func NewRateGovernor(policies map[OpKind]OpPolicy, nowFn func() time.Time) *RateGovernor {
	if len(policies) == 0 {
		policies = DefaultOpPolicies()
	} else {
		copied := make(map[OpKind]OpPolicy, len(policies))
		for k, v := range policies {
			copied[k] = v
		}
		policies = copied
	}
	if nowFn == nil {
		nowFn = time.Now
	}
	return &RateGovernor{
		policies: policies,
		buckets:  make(map[bucketKey]*tokenBucket),
		now:      nowFn,
		jitter:   defaultGovernorJitter,
	}
}

// Wait consumes one token for (accountID, op), sleeping only via context-cancellable timer.
func (g *RateGovernor) Wait(ctx context.Context, accountID int64, op OpKind) error {
	if ctx == nil {
		ctx = context.Background()
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		waitDur := g.reserveOrDelay(accountID, op)
		if waitDur <= 0 {
			return nil
		}
		timer := time.NewTimer(waitDur)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func (g *RateGovernor) reserveOrDelay(accountID int64, op OpKind) time.Duration {
	g.mu.Lock()
	defer g.mu.Unlock()

	policy, ok := g.policies[op]
	if !ok || policy.Interval <= 0 {
		return 0
	}
	burst := policy.Burst
	if burst < 1 {
		burst = 1
	}

	now := g.now().UTC()
	key := bucketKey{accountID: accountID, op: op}
	b, exists := g.buckets[key]
	if !exists {
		b = &tokenBucket{tokens: burst, lastRefill: now}
		g.buckets[key] = b
	} else {
		elapsed := now.Sub(b.lastRefill)
		if elapsed > 0 {
			refill := float64(elapsed) / float64(policy.Interval)
			b.tokens += refill
			if b.tokens > burst {
				b.tokens = burst
			}
			b.lastRefill = now
		}
	}

	if b.tokens >= 1 {
		b.tokens -= 1
		return 0
	}

	deficit := 1 - b.tokens
	wait := time.Duration(deficit * float64(policy.Interval))
	if wait <= 0 {
		wait = time.Millisecond
	}
	if policy.Jitter > 0 && g.jitter != nil {
		wait += g.jitter(policy.Jitter)
	}
	return wait
}

func defaultGovernorJitter(max time.Duration) time.Duration {
	if max <= 0 {
		return 0
	}
	return time.Duration(rand.Int63n(int64(max) + 1))
}

// FloodGate maintains persistent per-account FLOOD_WAIT circuit-breaker state
// backed by SQLite so cooldowns survive process restarts.
type FloodGate struct {
	mu           sync.RWMutex
	store        storage.Store
	blockedUntil map[int64]time.Time
	safetyMargin time.Duration
	now          func() time.Time
	rrCursor     uint64
}

// NewFloodGate creates a persistent FloodGate circuit breaker.
func NewFloodGate(store storage.Store, safetyMargin time.Duration, nowFn func() time.Time) *FloodGate {
	if safetyMargin < 0 {
		safetyMargin = 0
	}
	if nowFn == nil {
		nowFn = time.Now
	}
	return &FloodGate{
		store:        store,
		blockedUntil: make(map[int64]time.Time),
		safetyMargin: safetyMargin,
		now:          nowFn,
	}
}

// SyncFromStore clears expired floods and loads active flood_until deadlines into memory.
func (f *FloodGate) SyncFromStore(ctx context.Context) error {
	if f.store == nil {
		return nil
	}
	now := f.now().UTC()
	if _, err := f.store.ClearExpiredFloods(ctx, now); err != nil {
		return err
	}
	accounts, err := f.store.ListAccounts(ctx)
	if err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.blockedUntil = make(map[int64]time.Time, len(accounts))
	for _, acc := range accounts {
		if acc.FloodUntil != nil && acc.FloodUntil.After(now) {
			f.blockedUntil[acc.ID] = acc.FloodUntil.UTC()
		}
	}
	return nil
}

// RegisterFlood persists a FLOOD_WAIT cooldown (with safetyMargin) in DB and memory.
func (f *FloodGate) RegisterFlood(ctx context.Context, accountID int64, wait time.Duration, reason string) (time.Time, error) {
	if wait < time.Second {
		wait = time.Second
	}
	until := f.now().UTC().Add(wait + f.safetyMargin)
	if reason == "" {
		reason = fmt.Sprintf("FLOOD_WAIT_%d", int(wait.Seconds()))
	}
	if f.store != nil && accountID > 0 {
		if err := f.store.SetAccountFloodUntil(ctx, accountID, until, reason); err != nil {
			return time.Time{}, err
		}
	}
	f.mu.Lock()
	if prev, ok := f.blockedUntil[accountID]; !ok || until.After(prev) {
		f.blockedUntil[accountID] = until
	} else {
		until = prev
	}
	f.mu.Unlock()
	return until, nil
}

// IsBlocked reports whether an account is currently inside a FloodWait cooldown.
func (f *FloodGate) IsBlocked(accountID int64) (bool, time.Time) {
	now := f.now().UTC()
	f.mu.RLock()
	until, ok := f.blockedUntil[accountID]
	f.mu.RUnlock()
	if !ok || !until.After(now) {
		if ok {
			f.mu.Lock()
			if cur, exists := f.blockedUntil[accountID]; exists && !cur.After(now) {
				delete(f.blockedUntil, accountID)
			}
			f.mu.Unlock()
		}
		return false, time.Time{}
	}
	return true, until
}

// CheckErr returns wrapped domain.ErrFloodWait if the account is currently blocked.
func (f *FloodGate) CheckErr(accountID int64) error {
	if blocked, until := f.IsBlocked(accountID); blocked {
		return fmt.Errorf("%w: account %d blocked until %s", domain.ErrFloodWait, accountID, until.Format(time.RFC3339))
	}
	return nil
}

// PickAvailableAccount selects the next active non-flooded account in round-robin order.
func (f *FloodGate) PickAvailableAccount(accounts []domain.TelegramAccount) (domain.TelegramAccount, bool) {
	candidates := make([]domain.TelegramAccount, 0, len(accounts))
	for _, acc := range accounts {
		if acc.Status != domain.AccountStatusActive {
			continue
		}
		if blocked, _ := f.IsBlocked(acc.ID); blocked {
			continue
		}
		candidates = append(candidates, acc)
	}
	if len(candidates) == 0 {
		return domain.TelegramAccount{}, false
	}
	f.mu.Lock()
	idx := int(f.rrCursor % uint64(len(candidates)))
	f.rrCursor++
	f.mu.Unlock()
	return candidates[idx], true
}
