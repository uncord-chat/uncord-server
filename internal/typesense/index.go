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

// messageDoc is the JSON structure indexed in Typesense.
type messageDoc struct {
	ID        string `json:"id"`
	Content   string `json:"content"`
	AuthorID  string `json:"author_id"`
	ChannelID string `json:"channel_id"`
	CreatedAt int64  `json:"created_at"`
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

	resp, err := idx.client.Do(req)
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
	doc := struct {
		ID      string `json:"id"`
		Content string `json:"content"`
	}{ID: id, Content: content}

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

	resp, err := idx.client.Do(req)
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

	resp, err := idx.client.Do(req)
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
