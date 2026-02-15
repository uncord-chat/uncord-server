package gateway

import (
	"context"
	"encoding/json"
	"testing"

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

	pub := NewPublisher(rdb, zerolog.Nop())

	// Subscribe before publishing so we can verify the message.
	sub := rdb.Subscribe(context.Background(), eventsChannel)
	defer func() { _ = sub.Close() }()

	// Wait for subscription to be active.
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

	pub := NewPublisher(rdb, zerolog.Nop())

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
