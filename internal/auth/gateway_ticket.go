package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// GatewayTicketTTL is the lifetime of a gateway ticket. Tickets are intended to be consumed within seconds of creation,
// so a 30-second TTL provides ample margin while limiting the window for replay.
const GatewayTicketTTL = 30 * time.Second

func gatewayTicketKey(ticket string) string {
	return "gateway_ticket:" + ticket
}

// CreateGatewayTicket generates a single-use gateway ticket, stores it in Valkey with a 30-second TTL, and returns the
// ticket string. Web clients present this ticket in the WebSocket Identify frame instead of a JWT access token.
func CreateGatewayTicket(ctx context.Context, rdb *redis.Client, userID uuid.UUID) (string, error) {
	ticket := uuid.New().String()

	err := rdb.Set(ctx, gatewayTicketKey(ticket), userID.String(), GatewayTicketTTL).Err()
	if err != nil {
		return "", fmt.Errorf("store gateway ticket: %w", err)
	}

	return ticket, nil
}

// ConsumeGatewayTicket atomically reads and deletes a gateway ticket from Valkey, returning the associated user ID.
// Returns ErrInvalidToken if the ticket does not exist, has expired, or has already been consumed.
func ConsumeGatewayTicket(ctx context.Context, rdb *redis.Client, ticket string) (uuid.UUID, error) {
	val, err := rdb.GetDel(ctx, gatewayTicketKey(ticket)).Result()
	if errors.Is(err, redis.Nil) {
		return uuid.Nil, ErrInvalidToken
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("consume gateway ticket: %w", err)
	}

	userID, err := uuid.Parse(val)
	if err != nil {
		return uuid.Nil, fmt.Errorf("parse user ID from gateway ticket: %w", err)
	}

	return userID, nil
}
