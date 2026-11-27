package server

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

type blockingWSConn struct {
	closed chan struct{}
	once   sync.Once
}

func (b *blockingWSConn) Write(ctx context.Context, _ websocket.MessageType, _ []byte) error {
	<-ctx.Done()
	return ctx.Err()
}

func (b *blockingWSConn) Read(ctx context.Context) (websocket.MessageType, []byte, error) {
	<-ctx.Done()
	return 0, nil, ctx.Err()
}

func (b *blockingWSConn) Close(websocket.StatusCode, string) error {
	b.once.Do(func() { close(b.closed) })
	return nil
}

func TestWSHubSlowClientDropped(t *testing.T) {
	conn := &blockingWSConn{closed: make(chan struct{})}
	client := &wsClient{
		conn: conn,
		send: make(chan []byte, 1),
		done: make(chan struct{}),
	}
	hub := &WSHub{
		writeTimeout: 50 * time.Millisecond,
		queueSize:    1,
		clients:      map[*wsClient]struct{}{client: {}},
		done:         make(chan struct{}),
	}
	defer hub.Close()
	go client.writeLoop(hub.writeTimeout)

	hub.broadcast([]byte(`{"event":1}`))
	hub.broadcast([]byte(`{"event":2}`))
	hub.broadcast([]byte(`{"event":3}`))

	select {
	case <-client.done:
	case <-time.After(time.Second):
		t.Fatal("slow client was not dropped")
	}
	select {
	case <-conn.closed:
	case <-time.After(time.Second):
		t.Fatal("slow client connection was not closed")
	}
}
