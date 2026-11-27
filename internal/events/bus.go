// Package events provides a non-blocking pub/sub event bus and bounded history
// for the Live AI Terminal and WebSocket subscribers.
package events

import (
	"context"

	"github.com/demonidze1337/mia-lead-radar/internal/domain"
)

// Bus broadcasts real-time events without blocking producers on slow consumers.
type Bus interface {
	// Publish appends the event to the ring buffer and dispatches non-blockingly to subscribers.
	Publish(ctx context.Context, ev domain.Event)
	// Subscribe returns a buffered channel of events and an unsubscribe cleanup function.
	Subscribe(buffer int) (<-chan domain.Event, func())
	// Recent returns up to limit most recent events in chronological order.
	Recent(limit int) []domain.Event
	// Close closes all active subscriber channels and prevents further publishes.
	Close()
}
