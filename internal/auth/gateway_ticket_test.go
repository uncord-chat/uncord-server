package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestCreateAndConsumeGatewayTicket(t *testing.T) {
	t.Parallel()
	_, rdb := setupMiniredis(t)
	ctx := context.Background()
	userID := uuid.New()

	ticket, err := CreateGatewayTicket(ctx, rdb, userID)
	if err != nil {
		t.Fatalf("CreateGatewayTicket() error = %v", err)
	}
	if ticket == "" {
		t.Fatal("CreateGatewayTicket() returned empty ticket")
	}

	got, err := ConsumeGatewayTicket(ctx, rdb, ticket)
	if err != nil {
		t.Fatalf("ConsumeGatewayTicket() error = %v", err)
	}
	if got != userID {
		t.Errorf("ConsumeGatewayTicket() = %v, want %v", got, userID)
	}
}

func TestConsumeGatewayTicketNotFound(t *testing.T) {
	t.Parallel()
	_, rdb := setupMiniredis(t)
	ctx := context.Background()

	_, err := ConsumeGatewayTicket(ctx, rdb, "nonexistent-ticket")
	if !errors.Is(err, ErrInvalidToken) {
		t.Errorf("ConsumeGatewayTicket() error = %v, want ErrInvalidToken", err)
	}
}

func TestConsumeGatewayTicketDoubleUse(t *testing.T) {
	t.Parallel()
	_, rdb := setupMiniredis(t)
	ctx := context.Background()
	userID := uuid.New()

	ticket, err := CreateGatewayTicket(ctx, rdb, userID)
	if err != nil {
		t.Fatalf("CreateGatewayTicket() error = %v", err)
	}

	_, err = ConsumeGatewayTicket(ctx, rdb, ticket)
	if err != nil {
		t.Fatalf("first ConsumeGatewayTicket() error = %v", err)
	}

	_, err = ConsumeGatewayTicket(ctx, rdb, ticket)
	if !errors.Is(err, ErrInvalidToken) {
		t.Errorf("second ConsumeGatewayTicket() error = %v, want ErrInvalidToken", err)
	}
}

func TestConsumeGatewayTicketExpired(t *testing.T) {
	t.Parallel()
	mr, rdb := setupMiniredis(t)
	ctx := context.Background()
	userID := uuid.New()

	ticket, err := CreateGatewayTicket(ctx, rdb, userID)
	if err != nil {
		t.Fatalf("CreateGatewayTicket() error = %v", err)
	}

	mr.FastForward(GatewayTicketTTL + time.Second)

	_, err = ConsumeGatewayTicket(ctx, rdb, ticket)
	if !errors.Is(err, ErrInvalidToken) {
		t.Errorf("ConsumeGatewayTicket() after expiry error = %v, want ErrInvalidToken", err)
	}
}
