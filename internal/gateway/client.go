package gateway

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fasthttp/websocket"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/uncord-chat/uncord-protocol/events"
	"github.com/uncord-chat/uncord-protocol/models"

	"github.com/uncord-chat/uncord-server/internal/presence"
)

const (
	// maxMessageSize is the maximum size in bytes of a single inbound WebSocket message.
	maxMessageSize = 4096

	// writeWait is the time allowed to write a message to the peer.
	writeWait = 10 * time.Second

	// identifyTimeout is how long a client has to send Identify or Resume after connecting.
	identifyTimeout = 30 * time.Second
)

// Client represents a single WebSocket connection. Each client runs two goroutines (readPump and writePump) and
// communicates with the Hub via its send channel and callback methods.
type Client struct {
	hub  *Hub
	conn *websocket.Conn
	send chan []byte
	log  zerolog.Logger

	// done is closed to signal that the client is shutting down. The send channel is never closed directly; writePump
	// and enqueue both select on done to detect termination, avoiding send-on-closed-channel panics that would
	// otherwise occur when unregister races with dispatch.
	done      chan struct{}
	closeOnce sync.Once

	// Session state, protected by mu. Fields are written during Identify/Resume and read by the Hub during dispatch.
	mu         sync.RWMutex
	userID     uuid.UUID
	sessionID  string
	seq        atomic.Int64
	identified bool

	// Rate limiting state (only accessed from readPump, no mutex needed).
	eventCount  int
	windowStart time.Time
}

func newClient(hub *Hub, conn *websocket.Conn, logger zerolog.Logger) *Client {
	return &Client{
		hub:  hub,
		conn: conn,
		send: make(chan []byte, 256),
		done: make(chan struct{}),
		log:  logger,
	}
}

// closeSend signals the client's write loop to stop. It is safe to call from multiple goroutines; only the first call
// has any effect.
func (c *Client) closeSend() {
	c.closeOnce.Do(func() { close(c.done) })
}

// UserID returns the authenticated user ID. The caller must hold at least a read lock or call this after the client
// is fully identified.
func (c *Client) UserID() uuid.UUID {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.userID
}

// SessionID returns the session identifier.
func (c *Client) SessionID() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.sessionID
}

// IsIdentified returns whether the client has completed authentication.
func (c *Client) IsIdentified() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.identified
}

// nextSeq increments and returns the next sequence number for a dispatch event.
func (c *Client) nextSeq() int64 {
	return c.seq.Add(1)
}

// currentSeq returns the current sequence number without incrementing.
func (c *Client) currentSeq() int64 {
	return c.seq.Load()
}

// readPump reads messages from the WebSocket connection and routes them by opcode. It runs in its own goroutine and
// is responsible for closing the connection when the read loop exits.
func (c *Client) readPump() {
	defer func() {
		c.hub.unregister(c)
		_ = c.conn.Close()
	}()

	heartbeatInterval := time.Duration(c.hub.cfg.GatewayHeartbeatIntervalMS) * time.Millisecond
	c.conn.SetReadLimit(maxMessageSize)
	// Allow slightly more than one heartbeat interval before timing out, so a single missed heartbeat does not
	// immediately sever the connection.
	_ = c.conn.SetReadDeadline(time.Now().Add(heartbeatInterval + heartbeatInterval/2))

	// Identify timeout: close the connection if the client does not authenticate within the deadline.
	identifyTimer := time.AfterFunc(identifyTimeout, func() {
		if !c.IsIdentified() {
			c.log.Debug().Msg("Client did not identify in time")
			c.closeWithCode(CloseNotAuthenticated, "identify timeout")
		}
	})
	defer identifyTimer.Stop()

	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				c.log.Debug().Err(err).Msg("WebSocket read error")
			}
			return
		}

		if c.rateLimited() {
			c.closeWithCode(CloseRateLimited, "rate limit exceeded")
			return
		}

		var frame events.Frame
		if err := json.Unmarshal(message, &frame); err != nil {
			c.closeWithCode(CloseDecodeError, "invalid JSON")
			return
		}

		switch frame.Op {
		case events.OpcodeHeartbeat:
			c.handleHeartbeat(heartbeatInterval)
		case events.OpcodeIdentify:
			identifyTimer.Stop()
			c.handleIdentify(frame.Data)
		case events.OpcodePresenceUpdate:
			c.handlePresenceUpdate(frame.Data)
		case events.OpcodeResume:
			identifyTimer.Stop()
			c.handleResume(frame.Data)
		default:
			c.closeWithCode(CloseUnknownOpcode, "unknown opcode")
			return
		}
	}
}

// writePump writes messages from the send channel to the WebSocket connection. It runs in its own goroutine and exits
// when done is closed. Any messages remaining in the send buffer are drained before returning.
func (c *Client) writePump() {
	defer func() { _ = c.conn.Close() }()

	for {
		select {
		case msg := <-c.send:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				c.log.Debug().Err(err).Msg("WebSocket write error")
				return
			}
		case <-c.done:
			// Drain any messages already buffered so the client receives them before the connection closes.
			for {
				select {
				case msg := <-c.send:
					_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
					if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
						return
					}
				default:
					return
				}
			}
		}
	}
}

// handleHeartbeat responds with a HeartbeatACK and resets the read deadline. For identified clients, the heartbeat
// also refreshes the presence TTL so the key does not expire while the connection is alive.
func (c *Client) handleHeartbeat(heartbeatInterval time.Duration) {
	_ = c.conn.SetReadDeadline(time.Now().Add(heartbeatInterval + heartbeatInterval/2))

	ack, err := NewHeartbeatACKFrame()
	if err != nil {
		c.log.Error().Err(err).Msg("Failed to build heartbeat ACK")
		return
	}
	c.enqueue(ack)

	if c.IsIdentified() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		c.hub.refreshPresence(ctx, c.UserID())
	}
}

// handleIdentify processes an op 2 Identify payload.
func (c *Client) handleIdentify(data json.RawMessage) {
	if c.IsIdentified() {
		c.closeWithCode(CloseAlreadyAuthenticated, "already identified")
		return
	}

	var id models.IdentifyData
	if err := json.Unmarshal(data, &id); err != nil {
		c.closeWithCode(CloseDecodeError, "invalid identify payload")
		return
	}

	if id.Token == "" {
		c.closeWithCode(CloseAuthFailed, "token required")
		return
	}

	c.hub.handleIdentify(c, id.Token)
}

// handleResume processes an op 6 Resume payload.
func (c *Client) handleResume(data json.RawMessage) {
	if c.IsIdentified() {
		c.closeWithCode(CloseAlreadyAuthenticated, "already identified")
		return
	}

	var r models.ResumeData
	if err := json.Unmarshal(data, &r); err != nil {
		c.closeWithCode(CloseDecodeError, "invalid resume payload")
		return
	}

	if r.Token == "" || r.SessionID == "" {
		c.closeWithCode(CloseAuthFailed, "token and session_id required")
		return
	}

	c.hub.handleResume(c, r)
}

// handlePresenceUpdate processes an op 3 PresenceUpdate payload.
func (c *Client) handlePresenceUpdate(data json.RawMessage) {
	if !c.IsIdentified() {
		c.closeWithCode(CloseNotAuthenticated, "not identified")
		return
	}

	var req models.PresenceUpdateRequest
	if err := json.Unmarshal(data, &req); err != nil {
		c.closeWithCode(CloseDecodeError, "invalid presence payload")
		return
	}

	if !presence.ValidStatus(req.Status) {
		c.closeWithCode(CloseDecodeError, "invalid status value")
		return
	}

	c.hub.handlePresenceUpdate(c, req.Status)
}

// enqueue sends a message to the client's write channel. If the client has already been shut down the message is
// silently dropped. If the channel is full, the message is dropped and the connection is closed to prevent backpressure
// from stalling the Hub.
func (c *Client) enqueue(msg []byte) {
	select {
	case <-c.done:
		return
	default:
	}

	select {
	case c.send <- msg:
	case <-c.done:
	default:
		c.log.Warn().Msg("Client send buffer full, closing connection")
		c.closeSend()
		_ = c.conn.Close()
	}
}

// closeWithCode sends a WebSocket close frame with the given code and reason, then closes the underlying connection.
func (c *Client) closeWithCode(code int, reason string) {
	msg := websocket.FormatCloseMessage(code, reason)
	_ = c.conn.WriteControl(websocket.CloseMessage, msg, time.Now().Add(writeWait))
	_ = c.conn.Close()
}

// rateLimited returns true if the client has exceeded the configured message rate limit.
func (c *Client) rateLimited() bool {
	now := time.Now()
	window := time.Duration(c.hub.cfg.RateLimitWSWindowSeconds) * time.Second
	if now.Sub(c.windowStart) > window {
		c.eventCount = 0
		c.windowStart = now
	}
	c.eventCount++
	return c.eventCount > c.hub.cfg.RateLimitWSCount
}
