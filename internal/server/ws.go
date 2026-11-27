package server

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/demonidze1337/mia-lead-radar/internal/events"
)

const (
	defaultWSQueue   = 128
	defaultWSTimeout = 3 * time.Second
	defaultWSBacklog = 50
)

// WSHub streams Live Terminal events to authenticated WebSocket clients without
// ever blocking the event bus or other subscribers.
type WSHub struct {
	bus          events.Bus
	auth         *Authenticator
	writeTimeout time.Duration
	queueSize    int

	mu      sync.RWMutex
	clients map[*wsClient]struct{}
	closed  bool
	done    chan struct{}
}

// WSOption customizes the WebSocket hub.
type WSOption func(*WSHub)

// WithWSQueue sets the per-client bounded queue size.
func WithWSQueue(size int) WSOption {
	return func(h *WSHub) {
		if size > 0 {
			h.queueSize = size
		}
	}
}

// WithWSWriteTimeout sets the per-message write deadline.
func WithWSWriteTimeout(timeout time.Duration) WSOption {
	return func(h *WSHub) {
		if timeout > 0 {
			h.writeTimeout = timeout
		}
	}
}

// NewWSHub creates a non-blocking WebSocket hub.
func NewWSHub(bus events.Bus, auth *Authenticator, opts ...WSOption) *WSHub {
	hub := &WSHub{
		bus:          bus,
		auth:         auth,
		writeTimeout: defaultWSTimeout,
		queueSize:    defaultWSQueue,
		clients:      make(map[*wsClient]struct{}),
		done:         make(chan struct{}),
	}
	for _, opt := range opts {
		opt(hub)
	}
	if bus != nil {
		go hub.fanout()
	}
	return hub
}

func (h *WSHub) fanout() {
	ch, unsubscribe := h.bus.Subscribe(h.queueSize)
	defer unsubscribe()
	for {
		select {
		case <-h.done:
			return
		case ev, ok := <-ch:
			if !ok {
				return
			}
			data, err := json.Marshal(ev)
			if err != nil {
				continue
			}
			h.broadcast(data)
		}
	}
}

func (h *WSHub) broadcast(data []byte) {
	h.mu.RLock()
	clients := make([]*wsClient, 0, len(h.clients))
	for client := range h.clients {
		clients = append(clients, client)
	}
	h.mu.RUnlock()
	for _, client := range clients {
		select {
		case client.send <- data:
		default:
			// A slow/stalled browser must never backpressure the hub.
			client.close()
		}
	}
}

// HandleWS authenticates the upgrade, sends a bounded backlog, then streams live events.
func (h *WSHub) HandleWS(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.auth == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if _, err := h.auth.AuthenticateRequest(r); err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: false,
		OriginPatterns:     []string{"*"},
	})
	if err != nil {
		return
	}
	client := &wsClient{
		conn: conn,
		send: make(chan []byte, h.queueSize),
		done: make(chan struct{}),
	}
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		_ = conn.Close(websocket.StatusGoingAway, "server shutting down")
		return
	}
	h.clients[client] = struct{}{}
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		delete(h.clients, client)
		h.mu.Unlock()
	}()

	if h.bus != nil {
		for _, ev := range h.bus.Recent(defaultWSBacklog) {
			data, marshalErr := json.Marshal(ev)
			if marshalErr != nil {
				continue
			}
			select {
			case client.send <- data:
			default:
			}
		}
	}
	go client.writeLoop(h.writeTimeout)
	client.readLoop()
	<-client.done
}

// Close stops the fanout goroutine and closes every active socket.
func (h *WSHub) Close() {
	if h == nil {
		return
	}
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return
	}
	h.closed = true
	close(h.done)
	clients := make([]*wsClient, 0, len(h.clients))
	for client := range h.clients {
		clients = append(clients, client)
	}
	h.clients = make(map[*wsClient]struct{})
	h.mu.Unlock()
	for _, client := range clients {
		client.close()
	}
}

type wsConn interface {
	Write(ctx context.Context, typ websocket.MessageType, p []byte) error
	Read(ctx context.Context) (websocket.MessageType, []byte, error)
	Close(code websocket.StatusCode, reason string) error
}

type wsClient struct {
	conn wsConn
	send chan []byte
	done chan struct{}
	once sync.Once
}

func (c *wsClient) close() {
	c.once.Do(func() {
		close(c.done)
		_ = c.conn.Close(websocket.StatusNormalClosure, "closed")
	})
}

func (c *wsClient) writeLoop(timeout time.Duration) {
	for {
		select {
		case <-c.done:
			return
		case data := <-c.send:
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			err := c.conn.Write(ctx, websocket.MessageText, data)
			cancel()
			if err != nil {
				c.close()
				return
			}
		}
	}
}

func (c *wsClient) readLoop() {
	defer c.close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	for {
		if _, _, err := c.conn.Read(ctx); err != nil {
			return
		}
	}
}
