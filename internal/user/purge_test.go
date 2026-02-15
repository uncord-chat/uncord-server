package user

import (
	"context"
	"testing"
	"time"
)

func TestPurgeLoginAttemptsNilPool(t *testing.T) {
	t.Parallel()

	repo := &PGRepository{}
	_, err := repo.PurgeLoginAttempts(context.Background(), time.Now())
	if err == nil {
		t.Fatal("expected error for nil pool, got nil")
	}
}

func TestPurgeTombstonesNilPool(t *testing.T) {
	t.Parallel()

	repo := &PGRepository{}
	_, err := repo.PurgeTombstones(context.Background(), time.Now())
	if err == nil {
		t.Fatal("expected error for nil pool, got nil")
	}
}
