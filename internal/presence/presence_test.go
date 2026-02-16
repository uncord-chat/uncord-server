package presence

import (
	"context"
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

func TestSetAndGet(t *testing.T) {
	t.Parallel()
	_, rdb := newTestRedis(t)
	store := NewStore(rdb)
	ctx := context.Background()
	userID := uuid.New()

	if err := store.Set(ctx, userID, StatusOnline); err != nil {
		t.Fatalf("Set() error = %v", err)
	}

	got, err := store.Get(ctx, userID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got != StatusOnline {
		t.Errorf("Get() = %q, want %q", got, StatusOnline)
	}
}

func TestGetReturnsOfflineWhenMissing(t *testing.T) {
	t.Parallel()
	_, rdb := newTestRedis(t)
	store := NewStore(rdb)
	ctx := context.Background()

	got, err := store.Get(ctx, uuid.New())
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got != StatusOffline {
		t.Errorf("Get() = %q, want %q", got, StatusOffline)
	}
}

func TestGetManyFiltersInvisible(t *testing.T) {
	t.Parallel()
	_, rdb := newTestRedis(t)
	store := NewStore(rdb)
	ctx := context.Background()

	onlineUser := uuid.New()
	invisibleUser := uuid.New()
	offlineUser := uuid.New()

	if err := store.Set(ctx, onlineUser, StatusOnline); err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	if err := store.Set(ctx, invisibleUser, StatusInvisible); err != nil {
		t.Fatalf("Set() error = %v", err)
	}

	result, err := store.GetMany(ctx, []uuid.UUID{onlineUser, invisibleUser, offlineUser})
	if err != nil {
		t.Fatalf("GetMany() error = %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("GetMany() returned %d results, want 1", len(result))
	}
	if result[0].UserID != onlineUser.String() {
		t.Errorf("result[0].UserID = %q, want %q", result[0].UserID, onlineUser.String())
	}
	if result[0].Status != StatusOnline {
		t.Errorf("result[0].Status = %q, want %q", result[0].Status, StatusOnline)
	}
}

func TestGetManyEmptyInput(t *testing.T) {
	t.Parallel()
	_, rdb := newTestRedis(t)
	store := NewStore(rdb)

	result, err := store.GetMany(context.Background(), nil)
	if err != nil {
		t.Fatalf("GetMany() error = %v", err)
	}
	if result != nil {
		t.Errorf("GetMany(nil) = %v, want nil", result)
	}
}

func TestRefreshExtendsTTL(t *testing.T) {
	t.Parallel()
	mr, rdb := newTestRedis(t)
	store := NewStore(rdb)
	ctx := context.Background()
	userID := uuid.New()

	if err := store.Set(ctx, userID, StatusIdle); err != nil {
		t.Fatalf("Set() error = %v", err)
	}

	// Advance time so the key is near expiry.
	mr.FastForward(100 * time.Second)

	if err := store.Refresh(ctx, userID); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}

	// After refresh, the key should survive another full TTL.
	mr.FastForward(100 * time.Second)

	got, err := store.Get(ctx, userID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got != StatusIdle {
		t.Errorf("Get() = %q after Refresh, want %q", got, StatusIdle)
	}
}

func TestDelete(t *testing.T) {
	t.Parallel()
	_, rdb := newTestRedis(t)
	store := NewStore(rdb)
	ctx := context.Background()
	userID := uuid.New()

	if err := store.Set(ctx, userID, StatusOnline); err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	if err := store.Delete(ctx, userID); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	got, err := store.Get(ctx, userID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got != StatusOffline {
		t.Errorf("Get() = %q after Delete, want %q", got, StatusOffline)
	}
}

func TestSetTypingDedup(t *testing.T) {
	t.Parallel()
	_, rdb := newTestRedis(t)
	store := NewStore(rdb)
	ctx := context.Background()

	channelID := uuid.New()
	userID := uuid.New()

	created, err := store.SetTyping(ctx, channelID, userID)
	if err != nil {
		t.Fatalf("SetTyping() error = %v", err)
	}
	if !created {
		t.Error("SetTyping() first call returned false, want true")
	}

	created, err = store.SetTyping(ctx, channelID, userID)
	if err != nil {
		t.Fatalf("SetTyping() error = %v", err)
	}
	if created {
		t.Error("SetTyping() second call returned true, want false (dedup)")
	}
}

func TestSetTypingExpires(t *testing.T) {
	t.Parallel()
	mr, rdb := newTestRedis(t)
	store := NewStore(rdb)
	ctx := context.Background()

	channelID := uuid.New()
	userID := uuid.New()

	created, err := store.SetTyping(ctx, channelID, userID)
	if err != nil {
		t.Fatalf("SetTyping() error = %v", err)
	}
	if !created {
		t.Fatal("SetTyping() first call returned false, want true")
	}

	mr.FastForward(11 * time.Second)

	created, err = store.SetTyping(ctx, channelID, userID)
	if err != nil {
		t.Fatalf("SetTyping() error = %v", err)
	}
	if !created {
		t.Error("SetTyping() after expiry returned false, want true")
	}
}

func TestValidStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		status string
		want   bool
	}{
		{StatusOnline, true},
		{StatusIdle, true},
		{StatusDND, true},
		{StatusInvisible, true},
		{StatusOffline, false},
		{"", false},
		{"away", false},
	}
	for _, tt := range tests {
		if got := ValidStatus(tt.status); got != tt.want {
			t.Errorf("ValidStatus(%q) = %v, want %v", tt.status, got, tt.want)
		}
	}
}
