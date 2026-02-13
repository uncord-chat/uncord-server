package valkey

import (
	"context"
	"fmt"
	"strings"

	"github.com/redis/go-redis/v9"
)

// Connect parses the Valkey URL, connects, and pings to verify the connection. The valkey:// scheme is replaced with
// redis:// for go-redis compatibility.
func Connect(ctx context.Context, url string) (*redis.Client, error) {
	// go-redis only understands redis:// scheme
	url = strings.Replace(url, "valkey://", "redis://", 1)

	opts, err := redis.ParseURL(url)
	if err != nil {
		return nil, fmt.Errorf("parse valkey URL: %w", err)
	}

	client := redis.NewClient(opts)

	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("ping valkey: %w", err)
	}

	return client, nil
}
