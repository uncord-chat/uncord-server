package valkey

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// Connect parses the Valkey URL, connects, and pings to verify the connection. The valkey:// scheme is replaced with
// redis:// for go-redis compatibility. The dialTimeout parameter controls how long the client waits when establishing
// new connections.
func Connect(ctx context.Context, rawURL string, dialTimeout time.Duration) (*redis.Client, error) {
	// go-redis only understands the redis:// scheme, so replace valkey:// (case-insensitive) before parsing.
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("parse valkey URL: %w", err)
	}
	if strings.EqualFold(parsed.Scheme, "valkey") {
		parsed.Scheme = "redis"
	}

	opts, err := redis.ParseURL(parsed.String())
	if err != nil {
		return nil, fmt.Errorf("parse valkey URL: %w", err)
	}
	opts.DialTimeout = dialTimeout

	client := redis.NewClient(opts)

	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("ping valkey: %w", err)
	}

	return client, nil
}
