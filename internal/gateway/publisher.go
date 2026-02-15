package gateway

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
	"github.com/uncord-chat/uncord-protocol/events"
)

const eventsChannel = "uncord.gateway.events"

// envelope is the JSON structure published to the gateway events channel.
type envelope struct {
	Type string `json:"t"`
	Data any    `json:"d"`
}

// Publisher serialises dispatch events and publishes them to a Valkey pub/sub channel for consumption by the gateway.
type Publisher struct {
	rdb *redis.Client
	log zerolog.Logger
}

// NewPublisher creates a new gateway event publisher.
func NewPublisher(rdb *redis.Client, logger zerolog.Logger) *Publisher {
	return &Publisher{rdb: rdb, log: logger}
}

// Publish serialises the event as JSON and publishes it to the gateway events channel.
func (p *Publisher) Publish(ctx context.Context, eventType events.DispatchEvent, data any) error {
	payload, err := json.Marshal(envelope{Type: string(eventType), Data: data})
	if err != nil {
		return fmt.Errorf("marshal gateway event: %w", err)
	}
	if err := p.rdb.Publish(ctx, eventsChannel, payload).Err(); err != nil {
		return fmt.Errorf("publish gateway event: %w", err)
	}
	return nil
}
