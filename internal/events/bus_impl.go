package events

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/demonidze1337/mia-lead-radar/internal/domain"
)

// bus is the default Bus implementation: a mutex-guarded subscriber registry,
// a bounded ring buffer, and non-blocking fan-out.
type bus struct {
	mu         sync.RWMutex
	history    []domain.Event
	historyCap int
	historyLen int
	historyPos int
	nextID     atomic.Int64
	subs       map[uint64]chan domain.Event
	nextSubID  uint64
	closed     bool
}

// NewBus creates an event bus retaining up to historyCap recent events.
// Non-positive capacities disable history retention.
func NewBus(historyCap int) Bus {
	if historyCap < 0 {
		historyCap = 0
	}
	return &bus{
		history:    make([]domain.Event, historyCap),
		historyCap: historyCap,
		subs:       make(map[uint64]chan domain.Event),
	}
}

func (b *bus) Publish(ctx context.Context, ev domain.Event) {
	if ctx != nil {
		select {
		case <-ctx.Done():
			return
		default:
		}
	}
	ev.ID = 0
	if ev.Timestamp.IsZero() {
		ev.Timestamp = time.Now()
	}
	ev.Timestamp = ev.Timestamp.UTC()

	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return
	}
	// Assign the ID under the same lock that appends history, so Recent order
	// always matches monotonically increasing event IDs.
	ev.ID = b.nextID.Add(1)
	b.appendHistory(ev)
	for _, ch := range b.subs {
		select {
		case ch <- ev:
		default:
			// Slow consumer: drop this event instead of blocking producers.
		}
	}
}

func (b *bus) appendHistory(ev domain.Event) {
	if b.historyCap == 0 {
		return
	}
	if b.historyLen < b.historyCap {
		pos := (b.historyPos + b.historyLen) % b.historyCap
		b.history[pos] = ev
		b.historyLen++
		return
	}
	b.history[b.historyPos] = ev
	b.historyPos = (b.historyPos + 1) % b.historyCap
}

func (b *bus) Subscribe(buffer int) (<-chan domain.Event, func()) {
	if buffer < 0 {
		buffer = 0
	}
	ch := make(chan domain.Event, buffer)

	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		close(ch)
		return ch, func() {}
	}
	b.nextSubID++
	id := b.nextSubID
	b.subs[id] = ch
	b.mu.Unlock()

	var once sync.Once
	unsubscribe := func() {
		once.Do(func() {
			b.mu.Lock()
			if existing, ok := b.subs[id]; ok {
				delete(b.subs, id)
				close(existing)
			}
			b.mu.Unlock()
		})
	}
	return ch, unsubscribe
}

func (b *bus) Recent(limit int) []domain.Event {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if b.historyLen == 0 {
		return nil
	}
	count := b.historyLen
	if limit > 0 && limit < count {
		count = limit
	}
	start := (b.historyPos + b.historyLen - count) % b.historyCap
	out := make([]domain.Event, count)
	for i := 0; i < count; i++ {
		out[i] = b.history[(start+i)%b.historyCap]
	}
	return out
}

func (b *bus) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return
	}
	b.closed = true
	for id, ch := range b.subs {
		delete(b.subs, id)
		close(ch)
	}
}
