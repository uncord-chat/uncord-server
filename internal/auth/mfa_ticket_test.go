package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestCreateAndConsumeMFATicket(t *testing.T) {
	t.Parallel()
	_, rdb := setupMiniredis(t)
	ctx := context.Background()
	userID := uuid.New()

	ticket, err := CreateMFATicket(ctx, rdb, userID, 5*time.Minute)
	if err != nil {
		t.Fatalf("CreateMFATicket() error = %v", err)
	}
	if ticket == "" {
		t.Fatal("CreateMFATicket() returned empty ticket")
	}

	got, err := ConsumeMFATicket(ctx, rdb, ticket)
	if err != nil {
		t.Fatalf("ConsumeMFATicket() error = %v", err)
	}
	if got != userID {
		t.Errorf("ConsumeMFATicket() = %v, want %v", got, userID)
	}
}

func TestConsumeMFATicketNotFound(t *testing.T) {
	t.Parallel()
	_, rdb := setupMiniredis(t)
	ctx := context.Background()

	_, err := ConsumeMFATicket(ctx, rdb, "nonexistent-ticket")
	if !errors.Is(err, ErrInvalidToken) {
		t.Errorf("ConsumeMFATicket() error = %v, want ErrInvalidToken", err)
	}
}

func TestConsumeMFATicketDoubleUse(t *testing.T) {
	t.Parallel()
	_, rdb := setupMiniredis(t)
	ctx := context.Background()
	userID := uuid.New()

	ticket, err := CreateMFATicket(ctx, rdb, userID, 5*time.Minute)
	if err != nil {
		t.Fatalf("CreateMFATicket() error = %v", err)
	}

	_, err = ConsumeMFATicket(ctx, rdb, ticket)
	if err != nil {
		t.Fatalf("first ConsumeMFATicket() error = %v", err)
	}

	_, err = ConsumeMFATicket(ctx, rdb, ticket)
	if !errors.Is(err, ErrInvalidToken) {
		t.Errorf("second ConsumeMFATicket() error = %v, want ErrInvalidToken", err)
	}
}

func TestConsumeMFATicketExpired(t *testing.T) {
	t.Parallel()
	mr, rdb := setupMiniredis(t)
	ctx := context.Background()
	userID := uuid.New()

	ticket, err := CreateMFATicket(ctx, rdb, userID, 1*time.Second)
	if err != nil {
		t.Fatalf("CreateMFATicket() error = %v", err)
	}

	mr.FastForward(2 * time.Second)

	_, err = ConsumeMFATicket(ctx, rdb, ticket)
	if !errors.Is(err, ErrInvalidToken) {
		t.Errorf("ConsumeMFATicket() after expiry error = %v, want ErrInvalidToken", err)
	}
}
