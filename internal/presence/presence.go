// Package presence provides ephemeral presence and typing state backed by Valkey. Presence keys expire after 120
// seconds and are refreshed by each gateway heartbeat. Typing indicators use a 10-second TTL with SET NX to deduplicate
// rapid keystrokes.
package presence

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/uncord-chat/uncord-protocol/models"
)

const (
	// presenceTTL is the lifetime of a presence key. Heartbeats refresh this TTL so keys expire only when the client
	// stops sending heartbeats.
	presenceTTL = 120 * time.Second

	// typingTTL is the lifetime of a typing indicator key. Clients may re-trigger the typing endpoint, but SET NX
	// suppresses duplicate dispatches until the key expires.
	typingTTL = 10 * time.Second

	// StatusOnline indicates the user is actively connected.
	StatusOnline = "online"
	// StatusIdle indicates the user is connected but inactive.
	StatusIdle = "idle"
	// StatusDND indicates the user does not want to be disturbed.
	StatusDND = "dnd"
	// StatusInvisible makes the user appear offline to others while remaining connected.
	StatusInvisible = "invisible"
	// StatusOffline is the implicit status when no presence key exists. It is never stored in Valkey.
	StatusOffline = "offline"
)

// Store reads and writes ephemeral presence and typing state in Valkey.
type Store struct {
	rdb *redis.Client
}

// NewStore creates a new presence store backed by the given Valkey client.
func NewStore(rdb *redis.Client) *Store {
	return &Store{rdb: rdb}
}

// Set stores the user's presence status with the standard TTL.
func (s *Store) Set(ctx context.Context, userID uuid.UUID, status string) error {
	if err := s.rdb.Set(ctx, presenceKey(userID), status, presenceTTL).Err(); err != nil {
		return fmt.Errorf("set presence for %s: %w", userID, err)
	}
	return nil
}

// Get returns the user's current presence status. If the key does not exist the user is considered offline.
func (s *Store) Get(ctx context.Context, userID uuid.UUID) (string, error) {
	val, err := s.rdb.Get(ctx, presenceKey(userID)).Result()
	if errors.Is(err, redis.Nil) {
		return StatusOffline, nil
	}
	if err != nil {
		return "", fmt.Errorf("get presence for %s: %w", userID, err)
	}
	return val, nil
}

// GetMany returns the visible presence state for each user. Invisible users are excluded from the result so they
// appear offline to other clients. The returned slice may be shorter than the input when users are offline or
// invisible.
func (s *Store) GetMany(ctx context.Context, userIDs []uuid.UUID) ([]models.PresenceState, error) {
	if len(userIDs) == 0 {
		return nil, nil
	}

	keys := make([]string, len(userIDs))
	for i, id := range userIDs {
		keys[i] = presenceKey(id)
	}

	vals, err := s.rdb.MGet(ctx, keys...).Result()
	if err != nil {
		return nil, fmt.Errorf("mget presence: %w", err)
	}

	result := make([]models.PresenceState, 0, len(userIDs))
	for i, v := range vals {
		if v == nil {
			continue
		}
		// MGet returns []interface{} where each element is nil or a string. The comma-ok assertion guards against
		// unexpected types from future Redis driver changes or pipeline corruption.
		status, ok := v.(string)
		if !ok || status == StatusInvisible {
			continue
		}
		result = append(result, models.PresenceState{
			UserID: userIDs[i].String(),
			Status: status,
		})
	}
	return result, nil
}

// Refresh extends the TTL of an existing presence key without changing the stored status.
func (s *Store) Refresh(ctx context.Context, userID uuid.UUID) error {
	if err := s.rdb.Expire(ctx, presenceKey(userID), presenceTTL).Err(); err != nil {
		return fmt.Errorf("refresh presence for %s: %w", userID, err)
	}
	return nil
}

// Delete removes the user's presence key. After deletion the user is considered offline.
func (s *Store) Delete(ctx context.Context, userID uuid.UUID) error {
	if err := s.rdb.Del(ctx, presenceKey(userID)).Err(); err != nil {
		return fmt.Errorf("delete presence for %s: %w", userID, err)
	}
	return nil
}

// SetTyping records that the user started typing in the given channel. The key uses SET NX so repeated calls within
// the TTL window are no-ops. Returns true when the key was newly created (meaning a TYPING_START dispatch should be
// sent), and false when the key already existed (duplicate suppressed).
func (s *Store) SetTyping(ctx context.Context, channelID, userID uuid.UUID) (bool, error) {
	ok, err := s.rdb.SetNX(ctx, typingKey(channelID, userID), 1, typingTTL).Result()
	if err != nil {
		return false, fmt.Errorf("set typing for %s in %s: %w", userID, channelID, err)
	}
	return ok, nil
}

// ClearTyping removes the typing indicator for the given user in the given channel. It returns true when the key existed
// and was deleted (meaning a TYPING_STOP dispatch should be sent), and false when the key did not exist.
func (s *Store) ClearTyping(ctx context.Context, channelID, userID uuid.UUID) (bool, error) {
	n, err := s.rdb.Del(ctx, typingKey(channelID, userID)).Result()
	if err != nil {
		return false, fmt.Errorf("clear typing for %s in %s: %w", userID, channelID, err)
	}
	return n > 0, nil
}

// ValidStatus returns true for statuses that a client may set via opcode 3. StatusOffline is not valid because clients
// go offline by disconnecting (or set StatusInvisible to appear offline while staying connected).
func ValidStatus(status string) bool {
	switch status {
	case StatusOnline, StatusIdle, StatusDND, StatusInvisible:
		return true
	default:
		return false
	}
}

func presenceKey(userID uuid.UUID) string {
	return "presence:" + userID.String()
}

func typingKey(channelID, userID uuid.UUID) string {
	return "typing:" + channelID.String() + ":" + userID.String()
}
