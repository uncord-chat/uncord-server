package disposable

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// Blocklist checks email domains against a list of known disposable email providers. The domain list is fetched lazily
// on first use and cached for the lifetime of the process. If the initial fetch fails, subsequent calls will retry
// until the list is loaded successfully.
type Blocklist struct {
	url     string
	enabled bool

	mu      sync.RWMutex
	domains map[string]struct{}
	loaded  bool
}

// NewBlocklist creates a new disposable email blocklist. If enabled is false, IsBlocked always returns false without
// fetching the list.
func NewBlocklist(url string, enabled bool) *Blocklist {
	return &Blocklist{
		url:     url,
		enabled: enabled,
	}
}

// Prefetch loads the blocklist in the background so the first call to IsBlocked does not block on a network request.
// Errors are logged but not returned; IsBlocked will retry lazily if prefetch fails.
func (b *Blocklist) Prefetch(ctx context.Context) {
	if !b.enabled {
		return
	}

	domains, err := fetchDomains(ctx, b.url)
	if err != nil {
		log.Warn().Err(err).Msg("Failed to prefetch disposable email blocklist")
		return
	}

	b.mu.Lock()
	b.domains = domains
	b.loaded = true
	b.mu.Unlock()

	log.Info().Int("domains", len(domains)).Msg("Disposable email blocklist loaded")
}

// IsBlocked returns true if the given domain appears in the disposable email blocklist. Returns false immediately if
// the blocklist is disabled.
func (b *Blocklist) IsBlocked(ctx context.Context, domain string) (bool, error) {
	if !b.enabled {
		return false, nil
	}

	// Fast path: already loaded.
	b.mu.RLock()
	if b.loaded {
		_, blocked := b.domains[strings.ToLower(domain)]
		b.mu.RUnlock()
		return blocked, nil
	}
	b.mu.RUnlock()

	// Slow path: fetch the list (retries on failure each call).
	b.mu.Lock()
	defer b.mu.Unlock()

	// Double-check after acquiring write lock.
	if b.loaded {
		_, blocked := b.domains[strings.ToLower(domain)]
		return blocked, nil
	}

	domains, err := fetchDomains(ctx, b.url)
	if err != nil {
		return false, fmt.Errorf("load disposable email blocklist: %w", err)
	}

	b.domains = domains
	b.loaded = true

	_, blocked := domains[strings.ToLower(domain)]
	return blocked, nil
}

func fetchDomains(ctx context.Context, url string) (map[string]struct{}, error) {
	client := &http.Client{Timeout: 10 * time.Second}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create blocklist request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch blocklist: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("blocklist returned status %d", resp.StatusCode)
	}

	domains := make(map[string]struct{})
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		domains[strings.ToLower(line)] = struct{}{}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read blocklist: %w", err)
	}

	return domains, nil
}
