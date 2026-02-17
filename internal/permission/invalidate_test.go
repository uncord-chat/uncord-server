package permission

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
	"github.com/uncord-chat/uncord-protocol/permissions"
)

// --- Spy Cache for invalidation tests ---

type spyCache struct {
	deleteByUserCalled    bool
	deleteByChannelCalled bool
	deleteExactCalled     bool
	deleteAllCalled       bool
	lastUserID            uuid.UUID
	lastChannelID         uuid.UUID
}

func (c *spyCache) Get(_ context.Context, _, _ uuid.UUID) (permissions.Permission, bool, error) {
	return 0, false, nil
}
func (c *spyCache) Set(_ context.Context, _, _ uuid.UUID, _ permissions.Permission) error {
	return nil
}
func (c *spyCache) GetMany(_ context.Context, _ uuid.UUID, _ []uuid.UUID) (map[uuid.UUID]permissions.Permission, error) {
	return nil, nil
}
func (c *spyCache) SetMany(_ context.Context, _ uuid.UUID, _ map[uuid.UUID]permissions.Permission) error {
	return nil
}
func (c *spyCache) DeleteByUser(_ context.Context, userID uuid.UUID) error {
	c.deleteByUserCalled = true
	c.lastUserID = userID
	return nil
}
func (c *spyCache) DeleteByChannel(_ context.Context, channelID uuid.UUID) error {
	c.deleteByChannelCalled = true
	c.lastChannelID = channelID
	return nil
}
func (c *spyCache) DeleteExact(_ context.Context, userID, channelID uuid.UUID) error {
	c.deleteExactCalled = true
	c.lastUserID = userID
	c.lastChannelID = channelID
	return nil
}
func (c *spyCache) DeleteAll(_ context.Context) error {
	c.deleteAllCalled = true
	return nil
}

func TestHandleMessageUserOnly(t *testing.T) {
	t.Parallel()
	cache := &spyCache{}
	sub := &Subscriber{cache: cache, log: zerolog.Nop()}
	userID := uuid.New()

	payload := `{"user_id":"` + userID.String() + `"}`
	sub.handleMessage(context.Background(), payload)

	if !cache.deleteByUserCalled {
		t.Error("DeleteByUser should be called")
	}
	if cache.lastUserID != userID {
		t.Errorf("userID = %v, want %v", cache.lastUserID, userID)
	}
}

func TestHandleMessageChannelOnly(t *testing.T) {
	t.Parallel()
	cache := &spyCache{}
	sub := &Subscriber{cache: cache, log: zerolog.Nop()}
	channelID := uuid.New()

	payload := `{"channel_id":"` + channelID.String() + `"}`
	sub.handleMessage(context.Background(), payload)

	if !cache.deleteByChannelCalled {
		t.Error("DeleteByChannel should be called")
	}
	if cache.lastChannelID != channelID {
		t.Errorf("channelID = %v, want %v", cache.lastChannelID, channelID)
	}
}

func TestHandleMessageBoth(t *testing.T) {
	t.Parallel()
	cache := &spyCache{}
	sub := &Subscriber{cache: cache, log: zerolog.Nop()}
	userID := uuid.New()
	channelID := uuid.New()

	payload := `{"user_id":"` + userID.String() + `","channel_id":"` + channelID.String() + `"}`
	sub.handleMessage(context.Background(), payload)

	if !cache.deleteExactCalled {
		t.Error("DeleteExact should be called")
	}
	if cache.lastUserID != userID {
		t.Errorf("userID = %v, want %v", cache.lastUserID, userID)
	}
	if cache.lastChannelID != channelID {
		t.Errorf("channelID = %v, want %v", cache.lastChannelID, channelID)
	}
}

func TestHandleMessageMalformedJSON(t *testing.T) {
	t.Parallel()
	cache := &spyCache{}
	sub := &Subscriber{cache: cache, log: zerolog.Nop()}

	// Should not panic or call any cache method
	sub.handleMessage(context.Background(), "not valid json")

	if cache.deleteByUserCalled || cache.deleteByChannelCalled || cache.deleteExactCalled {
		t.Error("no cache method should be called on malformed JSON")
	}
}

func TestHandleMessageAll(t *testing.T) {
	t.Parallel()
	cache := &spyCache{}
	sub := &Subscriber{cache: cache, log: zerolog.Nop()}

	sub.handleMessage(context.Background(), `{"all":true}`)

	if !cache.deleteAllCalled {
		t.Error("DeleteAll should be called")
	}
	if cache.deleteByUserCalled || cache.deleteByChannelCalled || cache.deleteExactCalled {
		t.Error("only DeleteAll should be called")
	}
}

func TestHandleMessageEmptyJSON(t *testing.T) {
	t.Parallel()
	cache := &spyCache{}
	sub := &Subscriber{cache: cache, log: zerolog.Nop()}

	sub.handleMessage(context.Background(), "{}")

	if cache.deleteByUserCalled || cache.deleteByChannelCalled || cache.deleteExactCalled || cache.deleteAllCalled {
		t.Error("no cache method should be called on empty JSON")
	}
}

// --- Thread-safe spy cache for concurrent tests ---

type syncSpyCache struct {
	mu                    sync.Mutex
	deleteByUserCalled    bool
	deleteByChannelCalled bool
	deleteExactCalled     bool
	deleteAllCalled       bool
	lastUserID            uuid.UUID
	lastChannelID         uuid.UUID
}

func (c *syncSpyCache) Get(_ context.Context, _, _ uuid.UUID) (permissions.Permission, bool, error) {
	return 0, false, nil
}
func (c *syncSpyCache) Set(_ context.Context, _, _ uuid.UUID, _ permissions.Permission) error {
	return nil
}
func (c *syncSpyCache) GetMany(_ context.Context, _ uuid.UUID, _ []uuid.UUID) (map[uuid.UUID]permissions.Permission, error) {
	return nil, nil
}
func (c *syncSpyCache) SetMany(_ context.Context, _ uuid.UUID, _ map[uuid.UUID]permissions.Permission) error {
	return nil
}
func (c *syncSpyCache) DeleteByUser(_ context.Context, userID uuid.UUID) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.deleteByUserCalled = true
	c.lastUserID = userID
	return nil
}
func (c *syncSpyCache) DeleteByChannel(_ context.Context, channelID uuid.UUID) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.deleteByChannelCalled = true
	c.lastChannelID = channelID
	return nil
}
func (c *syncSpyCache) DeleteExact(_ context.Context, userID, channelID uuid.UUID) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.deleteExactCalled = true
	c.lastUserID = userID
	c.lastChannelID = channelID
	return nil
}
func (c *syncSpyCache) DeleteAll(_ context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.deleteAllCalled = true
	return nil
}

// --- Publisher tests with miniredis ---

func setupPubSub(t *testing.T) *redis.Client {
	t.Helper()
	mr := miniredis.RunT(t)
	return redis.NewClient(&redis.Options{Addr: mr.Addr()})
}

func TestPublisherInvalidateUser(t *testing.T) {
	t.Parallel()
	rdb := setupPubSub(t)
	ctx := context.Background()
	pub := NewPublisher(rdb)
	userID := uuid.New()

	// Subscribe first
	sub := rdb.Subscribe(ctx, InvalidateChannel)
	defer func() { _ = sub.Close() }()
	ch := sub.Channel()

	err := pub.InvalidateUser(ctx, userID)
	if err != nil {
		t.Fatalf("InvalidateUser() error = %v", err)
	}

	select {
	case msg := <-ch:
		var im InvalidationMessage
		_ = json.Unmarshal([]byte(msg.Payload), &im)
		if im.UserID == nil || *im.UserID != userID {
			t.Errorf("published user_id = %v, want %v", im.UserID, userID)
		}
		if im.ChannelID != nil {
			t.Errorf("channel_id should be nil, got %v", im.ChannelID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for published message")
	}
}

func TestPublisherInvalidateChannel(t *testing.T) {
	t.Parallel()
	rdb := setupPubSub(t)
	ctx := context.Background()
	pub := NewPublisher(rdb)
	channelID := uuid.New()

	sub := rdb.Subscribe(ctx, InvalidateChannel)
	defer func() { _ = sub.Close() }()
	ch := sub.Channel()

	err := pub.InvalidateChannel(ctx, channelID)
	if err != nil {
		t.Fatalf("InvalidateChannel() error = %v", err)
	}

	select {
	case msg := <-ch:
		var im InvalidationMessage
		_ = json.Unmarshal([]byte(msg.Payload), &im)
		if im.ChannelID == nil || *im.ChannelID != channelID {
			t.Errorf("published channel_id = %v, want %v", im.ChannelID, channelID)
		}
		if im.UserID != nil {
			t.Errorf("user_id should be nil, got %v", im.UserID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for published message")
	}
}

func TestPublisherInvalidateUserChannel(t *testing.T) {
	t.Parallel()
	rdb := setupPubSub(t)
	ctx := context.Background()
	pub := NewPublisher(rdb)
	userID := uuid.New()
	channelID := uuid.New()

	sub := rdb.Subscribe(ctx, InvalidateChannel)
	defer func() { _ = sub.Close() }()
	ch := sub.Channel()

	err := pub.InvalidateUserChannel(ctx, userID, channelID)
	if err != nil {
		t.Fatalf("InvalidateUserChannel() error = %v", err)
	}

	select {
	case msg := <-ch:
		var im InvalidationMessage
		_ = json.Unmarshal([]byte(msg.Payload), &im)
		if im.UserID == nil || *im.UserID != userID {
			t.Errorf("published user_id = %v, want %v", im.UserID, userID)
		}
		if im.ChannelID == nil || *im.ChannelID != channelID {
			t.Errorf("published channel_id = %v, want %v", im.ChannelID, channelID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for published message")
	}
}

func TestSubscriberRunContextCancel(t *testing.T) {
	t.Parallel()
	rdb := setupPubSub(t)
	cache := &spyCache{}
	sub := NewSubscriber(cache, rdb, zerolog.Nop())

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		done <- sub.Run(ctx)
	}()

	// Give subscriber time to connect
	time.Sleep(100 * time.Millisecond)

	cancel()

	select {
	case err := <-done:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Errorf("Run() error = %v, want nil or context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for Run to return")
	}
}

func TestSubscriberRunReceivesAndInvalidates(t *testing.T) {
	t.Parallel()
	rdb := setupPubSub(t)
	cache := &syncSpyCache{}
	sub := NewSubscriber(cache, rdb, zerolog.Nop())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- sub.Run(ctx)
	}()

	// Give subscriber time to connect
	time.Sleep(100 * time.Millisecond)

	// Publish a message
	userID := uuid.New()
	msg := InvalidationMessage{UserID: &userID}
	data, _ := json.Marshal(msg)
	rdb.Publish(ctx, InvalidateChannel, data)

	// Wait for processing
	time.Sleep(200 * time.Millisecond)

	cache.mu.Lock()
	called := cache.deleteByUserCalled
	gotID := cache.lastUserID
	cache.mu.Unlock()

	if !called {
		t.Error("subscriber should have called DeleteByUser")
	}
	if gotID != userID {
		t.Errorf("subscriber userID = %v, want %v", gotID, userID)
	}

	cancel()
}
