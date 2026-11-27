package server

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/demonidze1337/mia-lead-radar/internal/domain"
)

func wsURL(serverURL, token string) string {
	base := strings.Replace(serverURL, "http://", "ws://", 1)
	base = strings.Replace(base, "https://", "wss://", 1)
	return base + "/api/ws/terminal?token=" + url.QueryEscape(token)
}

func TestWSHubBacklogLiveAndAuth(t *testing.T) {
	env := newServerTest(t)
	httpServer := httptest.NewServer(env.server.Handler())
	defer httpServer.Close()

	env.bus.Publish(context.Background(), domain.Event{Type: domain.EventTerminalLog, Message: "backlog"})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, wsURL(httpServer.URL, env.token), nil)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "done")

	_, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read backlog: %v", err)
	}
	var event domain.Event
	if err := json.Unmarshal(data, &event); err != nil || event.Message != "backlog" {
		t.Fatalf("backlog event = %s (%v)", data, err)
	}

	env.bus.Publish(context.Background(), domain.Event{Type: domain.EventLeadNew, Message: "live"})
	_, data, err = conn.Read(ctx)
	if err != nil {
		t.Fatalf("read live: %v", err)
	}
	if err := json.Unmarshal(data, &event); err != nil || event.Message != "live" {
		t.Fatalf("live event = %s (%v)", data, err)
	}
}

func TestWSHubRejectsUnauthenticated(t *testing.T) {
	env := newServerTest(t)
	httpServer := httptest.NewServer(env.server.Handler())
	defer httpServer.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if conn, _, err := websocket.Dial(ctx, wsURL(httpServer.URL, "bad-token"), nil); err == nil {
		_ = conn.Close(websocket.StatusNormalClosure, "")
		t.Fatal("unauthenticated websocket was accepted")
	}
}

func TestWSHubCloseDisconnectsClients(t *testing.T) {
	env := newServerTest(t)
	httpServer := httptest.NewServer(env.server.Handler())
	defer httpServer.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, wsURL(httpServer.URL, env.token), nil)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	env.server.Close()
	if _, _, err := conn.Read(ctx); err == nil {
		t.Fatal("read after hub close unexpectedly succeeded")
	}
}
