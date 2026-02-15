package typesense

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// --- schemasMatch tests ---

func TestSchemasMatch(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		remote *remoteCollection
		want   bool
	}{
		{
			name: "exact match",
			remote: &remoteCollection{
				Fields:              messagesFields,
				DefaultSortingField: messagesSortingField,
			},
			want: true,
		},
		{
			name: "fields in different order",
			remote: &remoteCollection{
				Fields: []field{
					{Name: "created_at", Type: "int64", Sort: true},
					{Name: "channel_id", Type: "string", Facet: true},
					{Name: "content", Type: "string"},
					{Name: "author_id", Type: "string", Facet: true},
				},
				DefaultSortingField: messagesSortingField,
			},
			want: true,
		},
		{
			name: "wrong sorting field",
			remote: &remoteCollection{
				Fields:              messagesFields,
				DefaultSortingField: "content",
			},
			want: false,
		},
		{
			name: "empty sorting field",
			remote: &remoteCollection{
				Fields:              messagesFields,
				DefaultSortingField: "",
			},
			want: false,
		},
		{
			name: "fewer fields",
			remote: &remoteCollection{
				Fields: []field{
					{Name: "content", Type: "string"},
					{Name: "author_id", Type: "string", Facet: true},
				},
				DefaultSortingField: messagesSortingField,
			},
			want: false,
		},
		{
			name: "extra field",
			remote: &remoteCollection{
				Fields: []field{
					{Name: "content", Type: "string"},
					{Name: "author_id", Type: "string", Facet: true},
					{Name: "channel_id", Type: "string", Facet: true},
					{Name: "created_at", Type: "int64", Sort: true},
					{Name: "extra", Type: "string"},
				},
				DefaultSortingField: messagesSortingField,
			},
			want: false,
		},
		{
			name: "field type mismatch",
			remote: &remoteCollection{
				Fields: []field{
					{Name: "content", Type: "int32"},
					{Name: "author_id", Type: "string", Facet: true},
					{Name: "channel_id", Type: "string", Facet: true},
					{Name: "created_at", Type: "int64", Sort: true},
				},
				DefaultSortingField: messagesSortingField,
			},
			want: false,
		},
		{
			name: "field facet mismatch",
			remote: &remoteCollection{
				Fields: []field{
					{Name: "content", Type: "string"},
					{Name: "author_id", Type: "string"},
					{Name: "channel_id", Type: "string", Facet: true},
					{Name: "created_at", Type: "int64", Sort: true},
				},
				DefaultSortingField: messagesSortingField,
			},
			want: false,
		},
		{
			name: "field sort mismatch",
			remote: &remoteCollection{
				Fields: []field{
					{Name: "content", Type: "string"},
					{Name: "author_id", Type: "string", Facet: true},
					{Name: "channel_id", Type: "string", Facet: true},
					{Name: "created_at", Type: "int64"},
				},
				DefaultSortingField: messagesSortingField,
			},
			want: false,
		},
		{
			name: "no fields",
			remote: &remoteCollection{
				DefaultSortingField: messagesSortingField,
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := schemasMatch(tt.remote); got != tt.want {
				t.Errorf("schemasMatch() = %v, want %v", got, tt.want)
			}
		})
	}
}

// --- getCollection tests ---

func TestGetCollection(t *testing.T) {
	t.Parallel()

	t.Run("parses collection on 200", func(t *testing.T) {
		t.Parallel()
		col := remoteCollection{
			Fields:              messagesFields,
			DefaultSortingField: messagesSortingField,
		}
		body, _ := json.Marshal(col)
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write(body)
		}))
		defer srv.Close()

		got, err := getCollection(context.Background(), srv.Client(), srv.URL, "key")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got == nil {
			t.Fatal("expected non-nil collection")
		}
		if !schemasMatch(got) {
			t.Error("returned collection does not match expected schema")
		}
	})

	t.Run("returns nil on 404", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		defer srv.Close()

		got, err := getCollection(context.Background(), srv.Client(), srv.URL, "key")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != nil {
			t.Errorf("expected nil collection, got %+v", got)
		}
	})

	t.Run("returns error on 500", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer srv.Close()

		_, err := getCollection(context.Background(), srv.Client(), srv.URL, "key")
		if err == nil {
			t.Fatal("expected error for 500 response")
		}
	})

	t.Run("returns error on invalid JSON", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("{invalid"))
		}))
		defer srv.Close()

		_, err := getCollection(context.Background(), srv.Client(), srv.URL, "key")
		if err == nil {
			t.Fatal("expected error for invalid JSON response")
		}
	})

	t.Run("sends API key header", func(t *testing.T) {
		t.Parallel()
		const apiKey = "secret-key-42"
		var gotKey string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotKey = r.Header.Get("X-TYPESENSE-API-KEY")
			w.WriteHeader(http.StatusNotFound)
		}))
		defer srv.Close()

		_, _ = getCollection(context.Background(), srv.Client(), srv.URL, apiKey)
		if gotKey != apiKey {
			t.Errorf("API key header = %q, want %q", gotKey, apiKey)
		}
	})
}

// --- deleteCollection tests ---

func TestDeleteCollection(t *testing.T) {
	t.Parallel()

	t.Run("succeeds on 200", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()

		if err := deleteCollection(context.Background(), srv.Client(), srv.URL, "key"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("returns error on 500", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer srv.Close()

		if err := deleteCollection(context.Background(), srv.Client(), srv.URL, "key"); err == nil {
			t.Fatal("expected error for 500 response")
		}
	})

	t.Run("sends API key header", func(t *testing.T) {
		t.Parallel()
		const apiKey = "delete-key-99"
		var gotKey string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotKey = r.Header.Get("X-TYPESENSE-API-KEY")
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()

		_ = deleteCollection(context.Background(), srv.Client(), srv.URL, apiKey)
		if gotKey != apiKey {
			t.Errorf("API key header = %q, want %q", gotKey, apiKey)
		}
	})
}

// --- createCollection tests ---

func TestCreateCollection(t *testing.T) {
	t.Parallel()

	t.Run("succeeds on 200", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()

		if err := createCollection(context.Background(), srv.Client(), srv.URL, "key"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("returns error on 500", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer srv.Close()

		if err := createCollection(context.Background(), srv.Client(), srv.URL, "key"); err == nil {
			t.Fatal("expected error for 500 response")
		}
	})

	t.Run("sends correct headers", func(t *testing.T) {
		t.Parallel()
		const apiKey = "create-key-77"
		var gotKey, gotContentType string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotKey = r.Header.Get("X-TYPESENSE-API-KEY")
			gotContentType = r.Header.Get("Content-Type")
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()

		if err := createCollection(context.Background(), srv.Client(), srv.URL, apiKey); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if gotKey != apiKey {
			t.Errorf("API key header = %q, want %q", gotKey, apiKey)
		}
		if gotContentType != "application/json" {
			t.Errorf("Content-Type = %q, want %q", gotContentType, "application/json")
		}
	})

	t.Run("sends canonical schema as body", func(t *testing.T) {
		t.Parallel()
		var gotBody []byte
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotBody, _ = io.ReadAll(r.Body)
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()

		if err := createCollection(context.Background(), srv.Client(), srv.URL, "key"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		var schema collectionSchema
		if err := json.Unmarshal(gotBody, &schema); err != nil {
			t.Fatalf("unmarshal request body: %v", err)
		}
		if schema.Name != messagesCollection {
			t.Errorf("schema.Name = %q, want %q", schema.Name, messagesCollection)
		}
		if schema.DefaultSortingField != messagesSortingField {
			t.Errorf("schema.DefaultSortingField = %q, want %q", schema.DefaultSortingField, messagesSortingField)
		}
		if len(schema.Fields) != len(messagesFields) {
			t.Errorf("schema field count = %d, want %d", len(schema.Fields), len(messagesFields))
		}
	})
}

// --- EnsureMessagesCollection integration tests ---

func TestEnsureMessagesCollection(t *testing.T) {
	t.Parallel()

	matchingBody, _ := json.Marshal(remoteCollection{
		Fields:              messagesFields,
		DefaultSortingField: messagesSortingField,
	})
	staleBody, _ := json.Marshal(remoteCollection{
		Fields:              []field{{Name: "content", Type: "string"}},
		DefaultSortingField: messagesSortingField,
	})

	t.Run("creates collection when none exists", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodGet:
				w.WriteHeader(http.StatusNotFound)
			case http.MethodPost:
				w.WriteHeader(http.StatusCreated)
			}
		}))
		defer srv.Close()

		result, err := EnsureMessagesCollection(context.Background(), srv.URL, "key", 30*time.Second)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result != ResultCreated {
			t.Errorf("result = %d, want ResultCreated (%d)", result, ResultCreated)
		}
	})

	t.Run("returns unchanged when schema matches", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write(matchingBody)
		}))
		defer srv.Close()

		result, err := EnsureMessagesCollection(context.Background(), srv.URL, "key", 30*time.Second)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result != ResultUnchanged {
			t.Errorf("result = %d, want ResultUnchanged (%d)", result, ResultUnchanged)
		}
	})

	t.Run("recreates collection when schema differs", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodGet:
				_, _ = w.Write(staleBody)
			case http.MethodDelete:
				w.WriteHeader(http.StatusOK)
			case http.MethodPost:
				w.WriteHeader(http.StatusCreated)
			}
		}))
		defer srv.Close()

		result, err := EnsureMessagesCollection(context.Background(), srv.URL, "key", 30*time.Second)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result != ResultRecreated {
			t.Errorf("result = %d, want ResultRecreated (%d)", result, ResultRecreated)
		}
	})

	t.Run("returns error when get fails", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer srv.Close()

		_, err := EnsureMessagesCollection(context.Background(), srv.URL, "key", 30*time.Second)
		if err == nil {
			t.Fatal("expected error when get fails")
		}
		if !strings.Contains(err.Error(), "fetch existing collection") {
			t.Errorf("error = %q, want to contain %q", err.Error(), "fetch existing collection")
		}
	})

	t.Run("returns error when delete fails", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodGet:
				_, _ = w.Write(staleBody)
			case http.MethodDelete:
				w.WriteHeader(http.StatusInternalServerError)
			}
		}))
		defer srv.Close()

		_, err := EnsureMessagesCollection(context.Background(), srv.URL, "key", 30*time.Second)
		if err == nil {
			t.Fatal("expected error when delete fails")
		}
		if !strings.Contains(err.Error(), "delete outdated collection") {
			t.Errorf("error = %q, want to contain %q", err.Error(), "delete outdated collection")
		}
	})

	t.Run("returns error when create fails for new collection", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodGet:
				w.WriteHeader(http.StatusNotFound)
			case http.MethodPost:
				w.WriteHeader(http.StatusInternalServerError)
			}
		}))
		defer srv.Close()

		_, err := EnsureMessagesCollection(context.Background(), srv.URL, "key", 30*time.Second)
		if err == nil {
			t.Fatal("expected error when create fails")
		}
		if !strings.Contains(err.Error(), "create collection") {
			t.Errorf("error = %q, want to contain %q", err.Error(), "create collection")
		}
	})

	t.Run("returns error when recreate fails", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodGet:
				_, _ = w.Write(staleBody)
			case http.MethodDelete:
				w.WriteHeader(http.StatusOK)
			case http.MethodPost:
				w.WriteHeader(http.StatusInternalServerError)
			}
		}))
		defer srv.Close()

		_, err := EnsureMessagesCollection(context.Background(), srv.URL, "key", 30*time.Second)
		if err == nil {
			t.Fatal("expected error when recreate fails")
		}
		if !strings.Contains(err.Error(), "recreate collection") {
			t.Errorf("error = %q, want to contain %q", err.Error(), "recreate collection")
		}
	})
}
