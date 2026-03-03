package gateway

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/fasthttp/websocket"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"

	"github.com/uncord-chat/uncord-server/internal/config"
)

// TestIdentifyTimeout verifies that a client that connects but never sends an Identify or Resume frame is
// disconnected with CloseNotAuthenticated after the configured identify timeout expires.
func TestIdentifyTimeout(t *testing.T) {
	t.Parallel()

	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })

	cfg := &config.Config{
		GatewayHeartbeatIntervalMS: 20000,
		GatewayIdentifyTimeout:     200 * time.Millisecond,
		GatewaySessionTTL:          5 * time.Minute,
		GatewayReplayBufferSize:    10,
		GatewayMaxConnections:      10,
		RateLimitWSCount:           120,
		RateLimitWSWindowSeconds:   60,
	}
	sessions := NewSessionStore(rdb, zerolog.Nop(), cfg.GatewaySessionTTL, cfg.GatewayReplayBufferSize)
	hub := NewHub(HubDeps{
		RDB:      rdb,
		Cfg:      cfg,
		Sessions: sessions,
		Logger:   zerolog.Nop(),
	})

	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		hub.ServeWebSocket(conn)
	}))
	t.Cleanup(srv.Close)

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()

	// Read the Hello frame that the server sends immediately after upgrade.
	_, _, err = conn.ReadMessage()
	if err != nil {
		t.Fatalf("reading Hello frame: %v", err)
	}

	// Do NOT send Identify. Wait for the server to close the connection due to the identify timeout.
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, _, err = conn.ReadMessage()
	if err == nil {
		t.Fatal("expected connection to be closed by identify timeout, but read succeeded")
	}

	var closeErr *websocket.CloseError
	if !errors.As(err, &closeErr) {
		t.Fatalf("expected CloseError, got %T: %v", err, err)
	}
	if closeErr.Code != CloseNotAuthenticated {
		t.Errorf("close code = %d, want %d (CloseNotAuthenticated)", closeErr.Code, CloseNotAuthenticated)
	}
}
