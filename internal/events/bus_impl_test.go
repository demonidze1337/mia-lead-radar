package events

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/demonidze1337/mia-lead-radar/internal/domain"
)

func TestPublishFansOutNonBlocking(t *testing.T) {
	b := NewBus(4)
	defer b.Close()

	ch, unsubscribe := b.Subscribe(2)
	defer unsubscribe()

	ev := domain.Event{Type: domain.EventTerminalLog, Message: "hello"}
	b.Publish(context.Background(), ev)

	select {
	case got := <-ch:
		if got.ID != 1 {
			t.Errorf("ID = %d, want 1", got.ID)
		}
		if got.Message != "hello" {
			t.Errorf("Message = %q", got.Message)
		}
		if got.Timestamp.IsZero() {
			t.Error("Timestamp was not defaulted")
		}
		if got.Timestamp.Location() != time.UTC {
			t.Errorf("Timestamp location = %v", got.Timestamp.Location())
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
	}
}

func TestPublishDoesNotBlockOnSlowSubscriber(t *testing.T) {
	b := NewBus(8)
	defer b.Close()

	_, unsubscribe := b.Subscribe(1)
	defer unsubscribe()

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 5000; i++ {
			b.Publish(context.Background(), domain.Event{Type: domain.EventTier0Passed, Message: fmt.Sprintf("event-%d", i)})
		}
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("publisher blocked on a slow subscriber")
	}
}

func TestCanceledContextSkipsPublish(t *testing.T) {
	b := NewBus(2)
	defer b.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	b.Publish(ctx, domain.Event{Type: domain.EventTerminalLog, Message: "ignored"})
	if got := b.Recent(0); len(got) != 0 {
		t.Fatalf("expected no history, got %d events", len(got))
	}
}

func TestHistoryRingBufferOrderAndLimit(t *testing.T) {
	b := NewBus(3)
	defer b.Close()

	for i := 1; i <= 5; i++ {
		b.Publish(context.Background(), domain.Event{Message: fmt.Sprintf("m%d", i)})
	}

	recent := b.Recent(0)
	if len(recent) != 3 {
		t.Fatalf("len(Recent(0)) = %d, want 3", len(recent))
	}
	for i, want := range []string{"m3", "m4", "m5"} {
		if recent[i].Message != want {
			t.Errorf("Recent(0)[%d] = %q, want %q", i, recent[i].Message, want)
		}
	}

	last := b.Recent(2)
	if len(last) != 2 || last[0].Message != "m4" || last[1].Message != "m5" {
		t.Fatalf("Recent(2) = %#v", last)
	}

	// Non-positive limits return all retained history.
	if all := b.Recent(-3); len(all) != 3 {
		t.Fatalf("len(Recent(-3)) = %d, want 3", len(all))
	}
}

func TestHistoryDisabled(t *testing.T) {
	b := NewBus(0)
	defer b.Close()
	b.Publish(context.Background(), domain.Event{Message: "dropped"})
	if got := b.Recent(10); got != nil {
		t.Fatalf("expected nil history, got %#v", got)
	}
}

func TestCloseClosesSubscribersAndIsIdempotent(t *testing.T) {
	b := NewBus(4)
	ch, _ := b.Subscribe(4)
	b.Publish(context.Background(), domain.Event{Message: "before"})
	if got := <-ch; got.Message != "before" {
		t.Fatalf("Message = %q", got.Message)
	}

	b.Close()
	b.Close()

	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("subscriber channel should be closed")
		}
	case <-time.After(time.Second):
		t.Fatal("subscriber channel was not closed")
	}

	historyLen := len(b.Recent(0))
	b.Publish(context.Background(), domain.Event{Message: "after"})
	if after := len(b.Recent(0)); after != historyLen {
		t.Fatalf("publish after close mutated history: %d -> %d", historyLen, after)
	}
}

func TestUnsubscribeIsIdempotent(t *testing.T) {
	b := NewBus(1)
	defer b.Close()
	ch, unsubscribe := b.Subscribe(1)
	unsubscribe()
	unsubscribe()
	if _, ok := <-ch; ok {
		t.Fatal("channel should already be closed")
	}
}

func TestSubscribeAfterCloseReturnsClosedChannel(t *testing.T) {
	b := NewBus(1)
	b.Close()
	ch, unsubscribe := b.Subscribe(1)
	defer unsubscribe()
	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("expected closed channel")
		}
	default:
		t.Fatal("expected ready closed channel")
	}
}

func TestConcurrentPublishSubscribeUnsubscribeClose(t *testing.T) {
	b := NewBus(64)

	const publishers = 8
	const eventsPerPublisher = 200

	var wg sync.WaitGroup
	wg.Add(publishers)
	for p := 0; p < publishers; p++ {
		go func(id int) {
			defer wg.Done()
			for i := 0; i < eventsPerPublisher; i++ {
				b.Publish(context.Background(), domain.Event{
					Type:    domain.EventTerminalLog,
					Message: fmt.Sprintf("p%d-%d", id, i),
				})
			}
		}(p)
	}

	var readers sync.WaitGroup
	for r := 0; r < 4; r++ {
		readers.Add(1)
		go func() {
			defer readers.Done()
			for i := 0; i < 100; i++ {
				ch, unsubscribe := b.Subscribe(2)
				select {
				case <-ch:
				default:
				}
				unsubscribe()
				_ = b.Recent(10)
			}
		}()
	}

	wg.Wait()
	b.Close()
	readers.Wait()

	recent := b.Recent(0)
	if len(recent) != 64 {
		t.Fatalf("history length = %d, want 64", len(recent))
	}
	for i := 1; i < len(recent); i++ {
		if recent[i].ID <= recent[i-1].ID {
			t.Fatalf("history not chronological at %d: %d <= %d", i, recent[i].ID, recent[i-1].ID)
		}
	}
}
