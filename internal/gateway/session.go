package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// sessionData is the JSON structure persisted in Valkey for a disconnected session.
type sessionData struct {
	UserID         string `json:"user_id"`
	LastSeq        int64  `json:"last_seq"`
	DisconnectedAt int64  `json:"disconnected_at"`
}

// SessionStore manages gateway session persistence and replay buffers in Valkey. Sessions are saved when a client
// disconnects and loaded when the client resumes. Each session has a configurable TTL after which it expires and can
// no longer be resumed.
type SessionStore struct {
	rdb       *redis.Client
	ttl       time.Duration
	maxReplay int
}

// NewSessionStore creates a new session store backed by the given Valkey client.
func NewSessionStore(rdb *redis.Client, ttl time.Duration, maxReplay int) *SessionStore {
	return &SessionStore{rdb: rdb, ttl: ttl, maxReplay: maxReplay}
}

func sessionKey(sessionID string) string { return "gwsession:" + sessionID }
func replayKey(sessionID string) string  { return "gwreplay:" + sessionID }

// Save persists a session when a client disconnects. The session and replay buffer share the same TTL so they expire
// together.
func (s *SessionStore) Save(ctx context.Context, sessionID string, userID uuid.UUID, lastSeq int64) error {
	data, err := json.Marshal(sessionData{
		UserID:         userID.String(),
		LastSeq:        lastSeq,
		DisconnectedAt: time.Now().Unix(),
	})
	if err != nil {
		return fmt.Errorf("marshal session: %w", err)
	}

	pipe := s.rdb.Pipeline()
	pipe.Set(ctx, sessionKey(sessionID), data, s.ttl)
	pipe.Expire(ctx, replayKey(sessionID), s.ttl)
	_, err = pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("save session: %w", err)
	}
	return nil
}

// LoadedSession contains the restored state for a resumed session.
type LoadedSession struct {
	UserID  uuid.UUID
	LastSeq int64
}

// Load retrieves a saved session. Returns ErrSessionNotFound if the session does not exist or has expired.
func (s *SessionStore) Load(ctx context.Context, sessionID string) (*LoadedSession, error) {
	raw, err := s.rdb.Get(ctx, sessionKey(sessionID)).Bytes()
	if err != nil {
		if err == redis.Nil {
			return nil, ErrSessionNotFound
		}
		return nil, fmt.Errorf("load session: %w", err)
	}

	var sd sessionData
	if err := json.Unmarshal(raw, &sd); err != nil {
		return nil, fmt.Errorf("unmarshal session: %w", err)
	}

	userID, err := uuid.Parse(sd.UserID)
	if err != nil {
		return nil, fmt.Errorf("parse session user ID: %w", err)
	}

	return &LoadedSession{UserID: userID, LastSeq: sd.LastSeq}, nil
}

// Delete removes a session and its replay buffer. This is called after a successful resume.
func (s *SessionStore) Delete(ctx context.Context, sessionID string) error {
	if err := s.rdb.Del(ctx, sessionKey(sessionID), replayKey(sessionID)).Err(); err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

// replayEntry stores a serialised dispatch frame alongside its sequence number for efficient filtering during replay.
type replayEntry struct {
	Seq     int64           `json:"s"`
	Payload json.RawMessage `json:"p"`
}

// AppendReplay adds a serialised dispatch frame to the session's replay buffer. The buffer is capped at the configured
// maximum size using LTRIM and the TTL is refreshed on each append.
func (s *SessionStore) AppendReplay(ctx context.Context, sessionID string, seq int64, payload json.RawMessage) error {
	entry, err := json.Marshal(replayEntry{Seq: seq, Payload: payload})
	if err != nil {
		return fmt.Errorf("marshal replay entry: %w", err)
	}

	key := replayKey(sessionID)
	pipe := s.rdb.Pipeline()
	pipe.RPush(ctx, key, entry)
	pipe.LTrim(ctx, key, int64(-s.maxReplay), -1)
	pipe.Expire(ctx, key, s.ttl)
	_, err = pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("append replay: %w", err)
	}
	return nil
}

// Replay returns all buffered dispatch frame payloads with sequence numbers strictly greater than afterSeq. Each
// element is a fully serialised dispatch frame ready to send over the WebSocket.
func (s *SessionStore) Replay(ctx context.Context, sessionID string, afterSeq int64) ([]json.RawMessage, error) {
	raw, err := s.rdb.LRange(ctx, replayKey(sessionID), 0, -1).Result()
	if err != nil {
		return nil, fmt.Errorf("read replay buffer: %w", err)
	}

	var result []json.RawMessage
	for _, item := range raw {
		var entry replayEntry
		if err := json.Unmarshal([]byte(item), &entry); err != nil {
			continue
		}
		if entry.Seq > afterSeq {
			result = append(result, entry.Payload)
		}
	}
	return result, nil
}

// NewSessionID generates a unique session identifier.
func NewSessionID() string {
	return strconv.FormatInt(time.Now().UnixNano(), 36) + "-" + uuid.New().String()[:8]
}
