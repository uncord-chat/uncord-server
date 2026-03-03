package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
	"github.com/uncord-chat/uncord-protocol/events"
)

func TestPublish_Success(t *testing.T) {
	t.Parallel()

	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = rdb.Close() }()

	pub := NewPublisher(rdb, zerolog.Nop(), 1, 16, 5*time.Second)

	sub := rdb.Subscribe(context.Background(), eventsChannel)
	defer func() { _ = sub.Close() }()

	_, err := sub.Receive(context.Background())
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	data := map[string]string{"id": "msg-1", "content": "hello"}
	if err := pub.Publish(context.Background(), events.MessageCreate, data); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}

	msg, err := sub.ReceiveMessage(context.Background())
	if err != nil {
		t.Fatalf("receive message: %v", err)
	}

	if msg.Channel != eventsChannel {
		t.Errorf("channel = %q, want %q", msg.Channel, eventsChannel)
	}

	var env envelope
	if err := json.Unmarshal([]byte(msg.Payload), &env); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}

	if env.Type != string(events.MessageCreate) {
		t.Errorf("type = %q, want %q", env.Type, events.MessageCreate)
	}
}

func TestPublish_EventType(t *testing.T) {
	t.Parallel()

	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = rdb.Close() }()

	pub := NewPublisher(rdb, zerolog.Nop(), 1, 16, 5*time.Second)

	sub := rdb.Subscribe(context.Background(), eventsChannel)
	defer func() { _ = sub.Close() }()
	_, err := sub.Receive(context.Background())
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	if err := pub.Publish(context.Background(), events.MessageDelete, map[string]string{"id": "msg-2"}); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}

	msg, err := sub.ReceiveMessage(context.Background())
	if err != nil {
		t.Fatalf("receive message: %v", err)
	}

	var env envelope
	if err := json.Unmarshal([]byte(msg.Payload), &env); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}

	if env.Type != string(events.MessageDelete) {
		t.Errorf("type = %q, want %q", env.Type, events.MessageDelete)
	}
}

func TestEnqueue_ProcessedByWorker(t *testing.T) {
	t.Parallel()

	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = rdb.Close() }()

	pub := NewPublisher(rdb, zerolog.Nop(), 1, 16, 5*time.Second)

	sub := rdb.Subscribe(context.Background(), eventsChannel)
	defer func() { _ = sub.Close() }()
	_, err := sub.Receive(context.Background())
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		_ = pub.Run(ctx)
		close(done)
	}()

	pub.Enqueue(events.MessageCreate, map[string]string{"id": "msg-1"})

	msg, err := sub.ReceiveMessage(context.Background())
	if err != nil {
		t.Fatalf("receive message: %v", err)
	}

	var env envelope
	if err := json.Unmarshal([]byte(msg.Payload), &env); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if env.Type != string(events.MessageCreate) {
		t.Errorf("type = %q, want %q", env.Type, events.MessageCreate)
	}

	cancel()
	<-done
}

func TestEnqueue_DropsWhenFull(t *testing.T) {
	t.Parallel()

	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = rdb.Close() }()

	// Queue size 1, no workers started so nothing drains.
	pub := NewPublisher(rdb, zerolog.Nop(), 1, 1, 5*time.Second)

	// Fill the queue.
	pub.Enqueue(events.MessageCreate, map[string]string{"id": "msg-1"})

	// This should be dropped.
	pub.Enqueue(events.MessageCreate, map[string]string{"id": "msg-2"})

	if dropped := pub.dropped.Load(); dropped != 1 {
		t.Errorf("dropped = %d, want 1", dropped)
	}
}

func TestRun_DrainsOnShutdown(t *testing.T) {
	t.Parallel()

	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = rdb.Close() }()

	pub := NewPublisher(rdb, zerolog.Nop(), 2, 64, 5*time.Second)

	sub := rdb.Subscribe(context.Background(), eventsChannel)
	defer func() { _ = sub.Close() }()
	_, err := sub.Receive(context.Background())
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	const count = 10
	for i := range count {
		pub.Enqueue(events.ChannelCreate, map[string]int{"n": i})
	}

	ctx, cancel := context.WithCancel(context.Background())
	// Cancel immediately so Run drains the buffered items and returns.
	cancel()

	if err := pub.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() error = %v", err)
	}

	// All 10 events should have been published before Run returned.
	for range count {
		msg, err := sub.ReceiveMessage(context.Background())
		if err != nil {
			t.Fatalf("receive message: %v", err)
		}
		var env envelope
		if err := json.Unmarshal([]byte(msg.Payload), &env); err != nil {
			t.Fatalf("unmarshal payload: %v", err)
		}
		if env.Type != string(events.ChannelCreate) {
			t.Errorf("type = %q, want %q", env.Type, events.ChannelCreate)
		}
	}
}

func TestRun_PublishTimeout(t *testing.T) {
	t.Parallel()

	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = rdb.Close() }()

	// Very short timeout to exercise the timeout path.
	pub := NewPublisher(rdb, zerolog.Nop(), 1, 16, 10*time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		_ = pub.Run(ctx)
		close(done)
	}()

	// Close miniredis so publishes fail (simulates Valkey being unreachable).
	mr.Close()

	pub.Enqueue(events.MessageCreate, map[string]string{"id": "msg-1"})

	// No observable counter for failed publishes; a short sleep is the only option to let the worker process the item.
	time.Sleep(100 * time.Millisecond)

	cancel()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run() did not return after context cancellation")
	}
}
