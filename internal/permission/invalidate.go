package permission

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog/log"
)

// InvalidationMessage is published to trigger cache invalidation.
type InvalidationMessage struct {
	UserID    *uuid.UUID `json:"user_id,omitempty"`
	ChannelID *uuid.UUID `json:"channel_id,omitempty"`
}

// Publisher sends cache invalidation messages via Valkey pub/sub.
type Publisher struct {
	Client *redis.Client
}

// NewPublisher creates a new invalidation publisher.
func NewPublisher(client *redis.Client) *Publisher {
	return &Publisher{Client: client}
}

// InvalidateUser publishes an invalidation for all cached permissions of a user.
func (p *Publisher) InvalidateUser(ctx context.Context, userID uuid.UUID) error {
	return p.publish(ctx, InvalidationMessage{UserID: &userID})
}

// InvalidateChannel publishes an invalidation for all cached permissions of a channel.
func (p *Publisher) InvalidateChannel(ctx context.Context, channelID uuid.UUID) error {
	return p.publish(ctx, InvalidationMessage{ChannelID: &channelID})
}

// InvalidateUserChannel publishes an invalidation for a specific user+channel pair.
func (p *Publisher) InvalidateUserChannel(ctx context.Context, userID, channelID uuid.UUID) error {
	return p.publish(ctx, InvalidationMessage{UserID: &userID, ChannelID: &channelID})
}

func (p *Publisher) publish(ctx context.Context, msg InvalidationMessage) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal invalidation: %w", err)
	}
	return p.Client.Publish(ctx, InvalidateChannel, data).Err()
}

// Subscriber listens for cache invalidation messages and removes cached entries.
type Subscriber struct {
	Cache  Cache
	Client *redis.Client
}

// NewSubscriber creates a new invalidation subscriber.
func NewSubscriber(cache Cache, client *redis.Client) *Subscriber {
	return &Subscriber{Cache: cache, Client: client}
}

// Run subscribes to the invalidation channel and processes messages until the
// context is cancelled. This method blocks and should be called in a goroutine.
func (s *Subscriber) Run(ctx context.Context) error {
	sub := s.Client.Subscribe(ctx, InvalidateChannel)
	defer sub.Close()

	ch := sub.Channel()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case msg, ok := <-ch:
			if !ok {
				return nil
			}
			s.handleMessage(ctx, msg.Payload)
		}
	}
}

func (s *Subscriber) handleMessage(ctx context.Context, payload string) {
	var msg InvalidationMessage
	if err := json.Unmarshal([]byte(payload), &msg); err != nil {
		log.Warn().Err(err).Str("payload", payload).Msg("Invalid invalidation message")
		return
	}

	var err error
	switch {
	case msg.UserID != nil && msg.ChannelID != nil:
		err = s.Cache.DeleteExact(ctx, *msg.UserID, *msg.ChannelID)
	case msg.UserID != nil:
		err = s.Cache.DeleteByUser(ctx, *msg.UserID)
	case msg.ChannelID != nil:
		err = s.Cache.DeleteByChannel(ctx, *msg.ChannelID)
	default:
		return
	}

	if err != nil {
		log.Warn().Err(err).Msg("Cache invalidation failed")
	}
}
