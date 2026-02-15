package valkey

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
)

func TestConnect_ValkeyScheme(t *testing.T) {
	t.Parallel()
	mr := miniredis.RunT(t)

	client, err := Connect(context.Background(), "valkey://"+mr.Addr(), 5*time.Second)
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	_ = client.Close()
}

func TestConnect_ValkeySchemeUpperCase(t *testing.T) {
	t.Parallel()
	mr := miniredis.RunT(t)

	client, err := Connect(context.Background(), "VALKEY://"+mr.Addr(), 5*time.Second)
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	_ = client.Close()
}

func TestConnect_RedisScheme(t *testing.T) {
	t.Parallel()
	mr := miniredis.RunT(t)

	client, err := Connect(context.Background(), "redis://"+mr.Addr(), 5*time.Second)
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	_ = client.Close()
}

func TestConnect_InvalidURL(t *testing.T) {
	t.Parallel()

	_, err := Connect(context.Background(), "://missing-scheme", 5*time.Second)
	if err == nil {
		t.Fatal("Connect() expected error for invalid URL, got nil")
	}
}

func TestConnect_UnreachableHost(t *testing.T) {
	t.Parallel()

	_, err := Connect(context.Background(), "redis://localhost:1", 100*time.Millisecond)
	if err == nil {
		t.Fatal("Connect() expected error for unreachable host, got nil")
	}
}
