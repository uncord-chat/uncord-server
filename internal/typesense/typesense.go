package typesense

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// EnsureMessagesCollection creates the messages collection in Typesense if it does not already exist. Returns true if
// the collection was created, false if it already existed. A 409 (conflict) response is treated as success.
func EnsureMessagesCollection(ctx context.Context, baseURL, apiKey string) (bool, error) {
	schema := map[string]any{
		"name": "messages",
		"fields": []map[string]any{
			{"name": "id", "type": "string"},
			{"name": "content", "type": "string"},
			{"name": "author_id", "type": "string", "facet": true},
			{"name": "channel_id", "type": "string", "facet": true},
			{"name": "created_at", "type": "int64", "sort": true},
		},
		"default_sorting_field": "created_at",
	}

	body, err := json.Marshal(schema)
	if err != nil {
		return false, fmt.Errorf("marshal typesense schema: %w", err)
	}

	client := &http.Client{Timeout: 10 * time.Second}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/collections", bytes.NewReader(body))
	if err != nil {
		return false, fmt.Errorf("create typesense request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-TYPESENSE-API-KEY", apiKey)

	resp, err := client.Do(req)
	if err != nil {
		return false, fmt.Errorf("typesense request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusConflict {
		return false, nil
	}

	if resp.StatusCode >= 400 {
		return false, fmt.Errorf("typesense returned status %d", resp.StatusCode)
	}

	return true, nil
}
