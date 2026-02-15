package permission

import (
	"context"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/uncord-chat/uncord-protocol/permissions"
)

func setupMiniRedis(t *testing.T) (*miniredis.Miniredis, *ValkeyCache) {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	return mr, NewValkeyCache(rdb)
}

func TestCacheSetAndGet(t *testing.T) {
	t.Parallel()
	_, cache := setupMiniRedis(t)
	ctx := context.Background()
	userID := uuid.New()
	channelID := uuid.New()
	perm := permissions.ViewChannels | permissions.SendMessages

	if err := cache.Set(ctx, userID, channelID, perm); err != nil {
		t.Fatalf("Set() error = %v", err)
	}

	got, ok, err := cache.Get(ctx, userID, channelID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if !ok {
		t.Fatal("Get() returned ok=false, want true")
	}
	if got != perm {
		t.Errorf("Get() = %d, want %d", got, perm)
	}
}

func TestCacheGetMiss(t *testing.T) {
	t.Parallel()
	_, cache := setupMiniRedis(t)
	ctx := context.Background()

	_, ok, err := cache.Get(ctx, uuid.New(), uuid.New())
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if ok {
		t.Error("Get() returned ok=true for missing key")
	}
}

func TestCacheDeleteByUser(t *testing.T) {
	t.Parallel()
	_, cache := setupMiniRedis(t)
	ctx := context.Background()
	userID := uuid.New()
	ch1 := uuid.New()
	ch2 := uuid.New()
	otherUser := uuid.New()

	// Set entries for our user and another user
	_ = cache.Set(ctx, userID, ch1, permissions.ViewChannels)
	_ = cache.Set(ctx, userID, ch2, permissions.SendMessages)
	_ = cache.Set(ctx, otherUser, ch1, permissions.ViewChannels)

	if err := cache.DeleteByUser(ctx, userID); err != nil {
		t.Fatalf("DeleteByUser() error = %v", err)
	}

	// Our user's entries should be gone
	_, ok, _ := cache.Get(ctx, userID, ch1)
	if ok {
		t.Error("user entry 1 should be deleted")
	}
	_, ok, _ = cache.Get(ctx, userID, ch2)
	if ok {
		t.Error("user entry 2 should be deleted")
	}

	// Other user's entry should remain
	_, ok, _ = cache.Get(ctx, otherUser, ch1)
	if !ok {
		t.Error("other user's entry should not be deleted")
	}
}

func TestCacheDeleteByChannel(t *testing.T) {
	t.Parallel()
	_, cache := setupMiniRedis(t)
	ctx := context.Background()
	channelID := uuid.New()
	u1 := uuid.New()
	u2 := uuid.New()
	otherChannel := uuid.New()

	_ = cache.Set(ctx, u1, channelID, permissions.ViewChannels)
	_ = cache.Set(ctx, u2, channelID, permissions.SendMessages)
	_ = cache.Set(ctx, u1, otherChannel, permissions.ViewChannels)

	if err := cache.DeleteByChannel(ctx, channelID); err != nil {
		t.Fatalf("DeleteByChannel() error = %v", err)
	}

	_, ok, _ := cache.Get(ctx, u1, channelID)
	if ok {
		t.Error("channel entry 1 should be deleted")
	}
	_, ok, _ = cache.Get(ctx, u2, channelID)
	if ok {
		t.Error("channel entry 2 should be deleted")
	}

	_, ok, _ = cache.Get(ctx, u1, otherChannel)
	if !ok {
		t.Error("other channel entry should not be deleted")
	}
}

func TestCacheDeleteExact(t *testing.T) {
	t.Parallel()
	_, cache := setupMiniRedis(t)
	ctx := context.Background()
	userID := uuid.New()
	ch1 := uuid.New()
	ch2 := uuid.New()

	_ = cache.Set(ctx, userID, ch1, permissions.ViewChannels)
	_ = cache.Set(ctx, userID, ch2, permissions.SendMessages)

	if err := cache.DeleteExact(ctx, userID, ch1); err != nil {
		t.Fatalf("DeleteExact() error = %v", err)
	}

	_, ok, _ := cache.Get(ctx, userID, ch1)
	if ok {
		t.Error("exact entry should be deleted")
	}

	_, ok, _ = cache.Get(ctx, userID, ch2)
	if !ok {
		t.Error("other entry should not be deleted")
	}
}

func TestCacheTTLApplied(t *testing.T) {
	t.Parallel()
	mr, cache := setupMiniRedis(t)
	ctx := context.Background()
	userID := uuid.New()
	channelID := uuid.New()

	if err := cache.Set(ctx, userID, channelID, permissions.ViewChannels); err != nil {
		t.Fatalf("Set() error = %v", err)
	}

	key := cacheKey(userID, channelID)
	ttl := mr.TTL(key)
	if ttl <= 0 {
		t.Errorf("key TTL = %v, want positive", ttl)
	}
	if ttl > CacheTTL {
		t.Errorf("key TTL = %v, want <= %v", ttl, CacheTTL)
	}
}

func TestGetManyAllHits(t *testing.T) {
	t.Parallel()
	_, cache := setupMiniRedis(t)
	ctx := context.Background()
	userID := uuid.New()
	ch1, ch2 := uuid.New(), uuid.New()
	perm1 := permissions.ViewChannels
	perm2 := permissions.SendMessages

	_ = cache.Set(ctx, userID, ch1, perm1)
	_ = cache.Set(ctx, userID, ch2, perm2)

	got, err := cache.GetMany(ctx, userID, []uuid.UUID{ch1, ch2})
	if err != nil {
		t.Fatalf("GetMany() error = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("GetMany() returned %d entries, want 2", len(got))
	}
	if got[ch1] != perm1 {
		t.Errorf("GetMany()[ch1] = %d, want %d", got[ch1], perm1)
	}
	if got[ch2] != perm2 {
		t.Errorf("GetMany()[ch2] = %d, want %d", got[ch2], perm2)
	}
}

func TestGetManyPartialHits(t *testing.T) {
	t.Parallel()
	_, cache := setupMiniRedis(t)
	ctx := context.Background()
	userID := uuid.New()
	ch1, ch2 := uuid.New(), uuid.New()

	_ = cache.Set(ctx, userID, ch1, permissions.ViewChannels)

	got, err := cache.GetMany(ctx, userID, []uuid.UUID{ch1, ch2})
	if err != nil {
		t.Fatalf("GetMany() error = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("GetMany() returned %d entries, want 1", len(got))
	}
	if _, ok := got[ch2]; ok {
		t.Error("GetMany() should not contain missing channel")
	}
}

func TestGetManyEmpty(t *testing.T) {
	t.Parallel()
	_, cache := setupMiniRedis(t)
	ctx := context.Background()

	got, err := cache.GetMany(ctx, uuid.New(), nil)
	if err != nil {
		t.Fatalf("GetMany() error = %v", err)
	}
	if got != nil {
		t.Errorf("GetMany(nil) = %v, want nil", got)
	}
}

func TestSetManyWritesAll(t *testing.T) {
	t.Parallel()
	_, cache := setupMiniRedis(t)
	ctx := context.Background()
	userID := uuid.New()
	ch1, ch2 := uuid.New(), uuid.New()
	perm1 := permissions.ViewChannels
	perm2 := permissions.SendMessages | permissions.EmbedLinks

	err := cache.SetMany(ctx, userID, map[uuid.UUID]permissions.Permission{ch1: perm1, ch2: perm2})
	if err != nil {
		t.Fatalf("SetMany() error = %v", err)
	}

	got1, ok1, _ := cache.Get(ctx, userID, ch1)
	got2, ok2, _ := cache.Get(ctx, userID, ch2)
	if !ok1 || got1 != perm1 {
		t.Errorf("after SetMany: ch1 = %d (ok=%v), want %d", got1, ok1, perm1)
	}
	if !ok2 || got2 != perm2 {
		t.Errorf("after SetMany: ch2 = %d (ok=%v), want %d", got2, ok2, perm2)
	}
}

func TestSetManyEmpty(t *testing.T) {
	t.Parallel()
	_, cache := setupMiniRedis(t)
	ctx := context.Background()

	err := cache.SetMany(ctx, uuid.New(), nil)
	if err != nil {
		t.Fatalf("SetMany(nil) error = %v", err)
	}
}

func TestSetManyTTLApplied(t *testing.T) {
	t.Parallel()
	mr, cache := setupMiniRedis(t)
	ctx := context.Background()
	userID := uuid.New()
	ch := uuid.New()

	_ = cache.SetMany(ctx, userID, map[uuid.UUID]permissions.Permission{ch: permissions.ViewChannels})

	key := cacheKey(userID, ch)
	ttl := mr.TTL(key)
	if ttl <= 0 {
		t.Errorf("SetMany key TTL = %v, want positive", ttl)
	}
	if ttl > CacheTTL {
		t.Errorf("SetMany key TTL = %v, want <= %v", ttl, CacheTTL)
	}
}
