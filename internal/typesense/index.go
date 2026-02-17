package typesense

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Indexer performs document-level CRUD operations against a Typesense messages collection.
type Indexer struct {
	baseURL string
	apiKey  string
	client  *http.Client
}

// NewIndexer creates a new Typesense document indexer.
func NewIndexer(baseURL, apiKey string, timeout time.Duration) *Indexer {
	return &Indexer{
		baseURL: baseURL,
		apiKey:  apiKey,
		client:  &http.Client{Timeout: timeout},
	}
}

// retryableStatusMin is the lower bound (inclusive) of HTTP status codes that are considered transient and worth
// retrying. All 5xx responses indicate a server-side problem that may resolve on a subsequent attempt.
const retryableStatusMin = 500

// retryDelay is the fixed pause between the initial attempt and the single retry. Kept short because index operations
// are best-effort and run in background goroutines.
const retryDelay = 500 * time.Millisecond

// do executes an HTTP request with a single retry on transient failures (network errors or 5xx responses). Index
// operations are best-effort, so one retry is enough to ride out brief Typesense restarts without adding latency to the
// happy path.
func (idx *Indexer) do(req *http.Request) (*http.Response, error) {
	resp, firstErr := idx.client.Do(req)
	if firstErr == nil && resp.StatusCode < retryableStatusMin {
		return resp, nil
	}

	// Close the failed response body before retrying.
	if resp != nil {
		_ = resp.Body.Close()
	}

	// Reset the request body for the retry. http.NewRequestWithContext sets GetBody automatically when the body is a
	// *bytes.Reader, so this is a no-op for DELETE requests (nil body).
	if req.GetBody != nil {
		body, bodyErr := req.GetBody()
		if bodyErr != nil {
			if firstErr != nil {
				return nil, firstErr
			}
			return nil, fmt.Errorf("reset request body for retry: %w", bodyErr)
		}
		req.Body = body
	}

	select {
	case <-req.Context().Done():
		if firstErr != nil {
			return nil, firstErr
		}
		return nil, req.Context().Err()
	case <-time.After(retryDelay):
	}

	return idx.client.Do(req)
}

// messageDoc is the JSON structure indexed in Typesense.
type messageDoc struct {
	ID        string `json:"id"`
	Content   string `json:"content"`
	AuthorID  string `json:"author_id"`
	ChannelID string `json:"channel_id"`
	CreatedAt int64  `json:"created_at"`
}

// messageDocUpdate is the subset of messageDoc fields sent during a content update. Kept as a named type so it stays
// visually coupled to messageDoc and is easy to extend if new updatable fields are added.
type messageDocUpdate struct {
	ID      string `json:"id"`
	Content string `json:"content"`
}

// IndexMessage adds a message document to the Typesense messages collection.
func (idx *Indexer) IndexMessage(ctx context.Context, id, content, authorID, channelID string, createdAt int64) error {
	doc := messageDoc{
		ID:        id,
		Content:   content,
		AuthorID:  authorID,
		ChannelID: channelID,
		CreatedAt: createdAt,
	}

	body, err := json.Marshal(doc)
	if err != nil {
		return fmt.Errorf("marshal message doc: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		idx.baseURL+"/collections/"+messagesCollection+"/documents", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build index request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-TYPESENSE-API-KEY", idx.apiKey)

	resp, err := idx.do(req)
	if err != nil {
		return fmt.Errorf("index request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		detail, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("typesense returned status %d on index: %s", resp.StatusCode, detail)
	}

	return nil
}

// UpdateMessage upserts a message document in the Typesense messages collection, updating its content.
func (idx *Indexer) UpdateMessage(ctx context.Context, id, content string) error {
	doc := messageDocUpdate{ID: id, Content: content}

	body, err := json.Marshal(doc)
	if err != nil {
		return fmt.Errorf("marshal update doc: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		idx.baseURL+"/collections/"+messagesCollection+"/documents?action=upsert", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build upsert request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-TYPESENSE-API-KEY", idx.apiKey)

	resp, err := idx.do(req)
	if err != nil {
		return fmt.Errorf("upsert request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		detail, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("typesense returned status %d on upsert: %s", resp.StatusCode, detail)
	}

	return nil
}

// DeleteMessage removes a message document from the Typesense messages collection.
func (idx *Indexer) DeleteMessage(ctx context.Context, id string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete,
		idx.baseURL+"/collections/"+messagesCollection+"/documents/"+id, nil)
	if err != nil {
		return fmt.Errorf("build delete request: %w", err)
	}
	req.Header.Set("X-TYPESENSE-API-KEY", idx.apiKey)

	resp, err := idx.do(req)
	if err != nil {
		return fmt.Errorf("delete request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// 404 is acceptable when the document was never indexed or was already removed.
	if resp.StatusCode >= 400 && resp.StatusCode != http.StatusNotFound {
		detail, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("typesense returned status %d on delete: %s", resp.StatusCode, detail)
	}

	return nil
}
