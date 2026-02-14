package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// Valkey key pattern for MFA tickets:
//
//	mfa_ticket:{uuid} → user_id (STRING with TTL)

func mfaTicketKey(ticket string) string {
	return "mfa_ticket:" + ticket
}

// CreateMFATicket generates a single-use MFA ticket, stores it in Valkey with the given TTL, and returns the ticket
// string. The ticket maps to the user ID so the MFA verify endpoint can identify the user after password verification.
func CreateMFATicket(ctx context.Context, rdb *redis.Client, userID uuid.UUID, ttl time.Duration) (string, error) {
	ticket := uuid.New().String()

	err := rdb.Set(ctx, mfaTicketKey(ticket), userID.String(), ttl).Err()
	if err != nil {
		return "", fmt.Errorf("store MFA ticket: %w", err)
	}

	return ticket, nil
}

// ConsumeMFATicket atomically reads and deletes an MFA ticket from Valkey, returning the associated user ID. Returns
// ErrInvalidToken if the ticket does not exist or has already been consumed. GETDEL is atomic, guaranteeing that each
// ticket can only be consumed once without requiring a Lua script.
func ConsumeMFATicket(ctx context.Context, rdb *redis.Client, ticket string) (uuid.UUID, error) {
	val, err := rdb.GetDel(ctx, mfaTicketKey(ticket)).Result()
	if errors.Is(err, redis.Nil) {
		return uuid.Nil, ErrInvalidToken
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("consume MFA ticket: %w", err)
	}

	userID, err := uuid.Parse(val)
	if err != nil {
		return uuid.Nil, fmt.Errorf("parse user ID from MFA ticket: %w", err)
	}

	return userID, nil
}
