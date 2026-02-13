package typesense

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"time"
)

// field defines a single field in a Typesense collection schema. Only the properties that matter for structural
// comparison are included; Typesense returns additional metadata that we intentionally ignore.
type field struct {
	Name  string `json:"name"`
	Type  string `json:"type"`
	Facet bool   `json:"facet,omitempty"`
	Sort  bool   `json:"sort,omitempty"`
}

// messagesFields is the canonical field list for the messages collection. Update this slice when the schema changes and
// the collection will be recreated on the next startup.
var messagesFields = []field{
	{Name: "content", Type: "string"},
	{Name: "author_id", Type: "string", Facet: true},
	{Name: "channel_id", Type: "string", Facet: true},
	{Name: "created_at", Type: "int64", Sort: true},
}

const (
	messagesCollection   = "messages"
	messagesSortingField = "created_at"
)

// collectionSchema is the JSON structure sent to Typesense when creating a collection.
type collectionSchema struct {
	Name                string  `json:"name"`
	Fields              []field `json:"fields"`
	DefaultSortingField string  `json:"default_sorting_field"`
}

// remoteCollection represents the subset of a Typesense collection response needed for schema comparison.
type remoteCollection struct {
	Fields              []field `json:"fields"`
	DefaultSortingField string  `json:"default_sorting_field"`
}

// Result describes what EnsureMessagesCollection did on startup.
type Result int

const (
	ResultCreated   Result = iota // Collection was created fresh.
	ResultUnchanged               // Collection already existed with the correct schema.
	ResultRecreated               // Collection was dropped and recreated due to a schema change.
)

// EnsureMessagesCollection ensures the messages collection exists in Typesense with the correct schema. If the
// collection exists but the schema differs, it is dropped and recreated. The returned Result indicates what action was
// taken.
func EnsureMessagesCollection(ctx context.Context, baseURL, apiKey string) (Result, error) {
	client := &http.Client{Timeout: 10 * time.Second}

	// Check whether the collection already exists.
	existing, err := getCollection(ctx, client, baseURL, apiKey)
	if err != nil {
		return 0, fmt.Errorf("fetch existing collection: %w", err)
	}

	if existing != nil {
		if schemasMatch(existing) {
			return ResultUnchanged, nil
		}

		// Schema differs; drop the old collection so we can recreate it.
		if err := deleteCollection(ctx, client, baseURL, apiKey); err != nil {
			return 0, fmt.Errorf("delete outdated collection: %w", err)
		}

		if err := createCollection(ctx, client, baseURL, apiKey); err != nil {
			return 0, fmt.Errorf("recreate collection: %w", err)
		}

		return ResultRecreated, nil
	}

	// Collection does not exist yet; create it.
	if err := createCollection(ctx, client, baseURL, apiKey); err != nil {
		return 0, fmt.Errorf("create collection: %w", err)
	}

	return ResultCreated, nil
}

// schemasMatch returns true when the remote collection's fields and sorting field match the desired schema.
func schemasMatch(remote *remoteCollection) bool {
	if remote.DefaultSortingField != messagesSortingField {
		return false
	}

	if len(remote.Fields) != len(messagesFields) {
		return false
	}

	for _, want := range messagesFields {
		if !slices.ContainsFunc(remote.Fields, func(got field) bool {
			return got == want
		}) {
			return false
		}
	}

	return true
}

// getCollection fetches the messages collection from Typesense. Returns nil (without error) if the collection does not
// exist (404).
func getCollection(ctx context.Context, client *http.Client, baseURL, apiKey string) (*remoteCollection, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/collections/"+messagesCollection, nil)
	if err != nil {
		return nil, fmt.Errorf("build get request: %w", err)
	}
	req.Header.Set("X-TYPESENSE-API-KEY", apiKey)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("get request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("typesense returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}

	var col remoteCollection
	if err := json.Unmarshal(body, &col); err != nil {
		return nil, fmt.Errorf("unmarshal collection: %w", err)
	}

	return &col, nil
}

// deleteCollection drops the messages collection.
func deleteCollection(ctx context.Context, client *http.Client, baseURL, apiKey string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, baseURL+"/collections/"+messagesCollection, nil)
	if err != nil {
		return fmt.Errorf("build delete request: %w", err)
	}
	req.Header.Set("X-TYPESENSE-API-KEY", apiKey)

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("delete request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("typesense returned status %d on delete", resp.StatusCode)
	}

	return nil
}

// createCollection creates the messages collection with the canonical schema.
func createCollection(ctx context.Context, client *http.Client, baseURL, apiKey string) error {
	schema := collectionSchema{
		Name:                messagesCollection,
		Fields:              messagesFields,
		DefaultSortingField: messagesSortingField,
	}

	body, err := json.Marshal(schema)
	if err != nil {
		return fmt.Errorf("marshal schema: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/collections", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-TYPESENSE-API-KEY", apiKey)

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("create request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("typesense returned status %d on create", resp.StatusCode)
	}

	return nil
}
