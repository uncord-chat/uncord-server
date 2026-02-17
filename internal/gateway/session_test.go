package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

func newTestRedis(t *testing.T) (*miniredis.Miniredis, *redis.Client) {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	return mr, rdb
}

func TestSessionSaveAndLoad(t *testing.T) {
	t.Parallel()
	_, rdb := newTestRedis(t)
	store := NewSessionStore(rdb, 5*time.Minute, 100)
	ctx := context.Background()

	userID := uuid.New()
	sid := "test-session-1"

	if err := store.Save(ctx, sid, userID, 42); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	loaded, err := store.Load(ctx, sid)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if loaded.UserID != userID {
		t.Errorf("UserID = %v, want %v", loaded.UserID, userID)
	}
	if loaded.LastSeq != 42 {
		t.Errorf("LastSeq = %d, want 42", loaded.LastSeq)
	}
}

func TestSessionLoadNotFound(t *testing.T) {
	t.Parallel()
	_, rdb := newTestRedis(t)
	store := NewSessionStore(rdb, 5*time.Minute, 100)

	_, err := store.Load(context.Background(), "nonexistent")
	if !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("Load() error = %v, want ErrSessionNotFound", err)
	}
}

func TestSessionLoadExpired(t *testing.T) {
	t.Parallel()
	mr, rdb := newTestRedis(t)
	store := NewSessionStore(rdb, 5*time.Minute, 100)
	ctx := context.Background()

	sid := "expiring-session"
	if err := store.Save(ctx, sid, uuid.New(), 1); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	mr.FastForward(6 * time.Minute)

	_, err := store.Load(ctx, sid)
	if !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("Load() after expiry error = %v, want ErrSessionNotFound", err)
	}
}

func TestSessionDelete(t *testing.T) {
	t.Parallel()
	_, rdb := newTestRedis(t)
	store := NewSessionStore(rdb, 5*time.Minute, 100)
	ctx := context.Background()

	sid := "delete-me"
	if err := store.Save(ctx, sid, uuid.New(), 1); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	if err := store.Delete(ctx, sid); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	_, err := store.Load(ctx, sid)
	if !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("Load() after delete error = %v, want ErrSessionNotFound", err)
	}
}

func TestSessionReplayAppendAndRetrieve(t *testing.T) {
	t.Parallel()
	_, rdb := newTestRedis(t)
	store := NewSessionStore(rdb, 5*time.Minute, 100)
	ctx := context.Background()

	sid := "replay-session"

	for i := int64(1); i <= 5; i++ {
		payload := json.RawMessage(`{"seq":` + json.Number(json.RawMessage([]byte{byte('0' + i)})).String() + `}`)
		if err := store.AppendReplay(ctx, sid, i, payload); err != nil {
			t.Fatalf("AppendReplay(seq=%d) error = %v", i, err)
		}
	}

	// Replay after seq 3 should return events 4 and 5.
	events, err := store.Replay(ctx, sid, 3)
	if err != nil {
		t.Fatalf("Replay() error = %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("Replay() returned %d events, want 2", len(events))
	}
}

func TestSessionReplayAfterZero(t *testing.T) {
	t.Parallel()
	_, rdb := newTestRedis(t)
	store := NewSessionStore(rdb, 5*time.Minute, 100)
	ctx := context.Background()

	sid := "replay-all"
	for i := int64(1); i <= 3; i++ {
		if err := store.AppendReplay(ctx, sid, i, json.RawMessage(`{}`)); err != nil {
			t.Fatalf("AppendReplay(seq=%d) error = %v", i, err)
		}
	}

	events, err := store.Replay(ctx, sid, 0)
	if err != nil {
		t.Fatalf("Replay() error = %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("Replay() returned %d events, want 3", len(events))
	}
}

func TestSessionReplayBufferCap(t *testing.T) {
	t.Parallel()
	_, rdb := newTestRedis(t)
	store := NewSessionStore(rdb, 5*time.Minute, 3)
	ctx := context.Background()

	sid := "capped-replay"
	for i := int64(1); i <= 10; i++ {
		if err := store.AppendReplay(ctx, sid, i, json.RawMessage(`{}`)); err != nil {
			t.Fatalf("AppendReplay(seq=%d) error = %v", i, err)
		}
	}

	// Only the last 3 events (8, 9, 10) should remain in the buffer.
	events, err := store.Replay(ctx, sid, 0)
	if err != nil {
		t.Fatalf("Replay() error = %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("Replay() returned %d events, want 3", len(events))
	}
}

func TestSessionReplayEmpty(t *testing.T) {
	t.Parallel()
	_, rdb := newTestRedis(t)
	store := NewSessionStore(rdb, 5*time.Minute, 100)

	events, err := store.Replay(context.Background(), "no-such-session", 0)
	if err != nil {
		t.Fatalf("Replay() error = %v", err)
	}
	if len(events) != 0 {
		t.Errorf("Replay() returned %d events, want 0", len(events))
	}
}

func TestSessionDeleteCleansReplayBuffer(t *testing.T) {
	t.Parallel()
	_, rdb := newTestRedis(t)
	store := NewSessionStore(rdb, 5*time.Minute, 100)
	ctx := context.Background()

	sid := "delete-with-replay"
	if err := store.Save(ctx, sid, uuid.New(), 5); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	for i := int64(1); i <= 3; i++ {
		if err := store.AppendReplay(ctx, sid, i, json.RawMessage(`{}`)); err != nil {
			t.Fatalf("AppendReplay() error = %v", err)
		}
	}

	if err := store.Delete(ctx, sid); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	events, err := store.Replay(ctx, sid, 0)
	if err != nil {
		t.Fatalf("Replay() error = %v", err)
	}
	if len(events) != 0 {
		t.Errorf("Replay() after delete returned %d events, want 0", len(events))
	}
}

func TestNewSessionID(t *testing.T) {
	t.Parallel()
	id1 := NewSessionID()
	id2 := NewSessionID()
	if id1 == "" {
		t.Error("NewSessionID() returned empty string")
	}
	if id1 == id2 {
		t.Errorf("NewSessionID() returned duplicate: %q", id1)
	}
}
