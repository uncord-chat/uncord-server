package disposable

import (
	"bufio"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Blocklist checks email domains against a list of known disposable email
// providers. The domain list is fetched lazily on first use and cached for
// the lifetime of the process. If the initial fetch fails, subsequent calls
// will retry until the list is loaded successfully.
type Blocklist struct {
	url     string
	enabled bool

	mu      sync.RWMutex
	domains map[string]struct{}
	loaded  bool
}

// NewBlocklist creates a new disposable email blocklist. If enabled is false,
// IsBlocked always returns false without fetching the list.
func NewBlocklist(url string, enabled bool) *Blocklist {
	return &Blocklist{
		url:     url,
		enabled: enabled,
	}
}

// IsBlocked returns true if the given domain appears in the disposable email
// blocklist. Returns false immediately if the blocklist is disabled.
func (b *Blocklist) IsBlocked(domain string) (bool, error) {
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

	domains, err := fetchDomains(b.url)
	if err != nil {
		return false, fmt.Errorf("load disposable email blocklist: %w", err)
	}

	b.domains = domains
	b.loaded = true

	_, blocked := domains[strings.ToLower(domain)]
	return blocked, nil
}

func fetchDomains(url string) (map[string]struct{}, error) {
	client := &http.Client{Timeout: 10 * time.Second}

	resp, err := client.Get(url)
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
