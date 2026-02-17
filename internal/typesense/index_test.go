package typesense

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestIndexMessage_Success(t *testing.T) {
	t.Parallel()

	var received messageDoc
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/collections/messages/documents" {
			t.Errorf("path = %s, want /collections/messages/documents", r.URL.Path)
		}
		if r.Header.Get("X-TYPESENSE-API-KEY") != "test-key" {
			t.Errorf("api key = %q, want %q", r.Header.Get("X-TYPESENSE-API-KEY"), "test-key")
		}
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	idx := NewIndexer(srv.URL, "test-key", 5*time.Second)
	err := idx.IndexMessage(context.Background(), "msg-1", "hello world", "author-1", "chan-1", 1700000000)
	if err != nil {
		t.Fatalf("IndexMessage() error = %v", err)
	}

	if received.ID != "msg-1" {
		t.Errorf("id = %q, want %q", received.ID, "msg-1")
	}
	if received.Content != "hello world" {
		t.Errorf("content = %q, want %q", received.Content, "hello world")
	}
	if received.AuthorID != "author-1" {
		t.Errorf("author_id = %q, want %q", received.AuthorID, "author-1")
	}
	if received.ChannelID != "chan-1" {
		t.Errorf("channel_id = %q, want %q", received.ChannelID, "chan-1")
	}
	if received.CreatedAt != 1700000000 {
		t.Errorf("created_at = %d, want %d", received.CreatedAt, 1700000000)
	}
}

func TestIndexMessage_ServerError(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("internal error"))
	}))
	defer srv.Close()

	idx := NewIndexer(srv.URL, "test-key", 5*time.Second)
	err := idx.IndexMessage(context.Background(), "msg-1", "hello", "a", "c", 0)
	if err == nil {
		t.Fatal("IndexMessage() expected error for 500 response")
	}
}

func TestUpdateMessage_Success(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.URL.Query().Get("action") != "upsert" {
			t.Errorf("action = %q, want %q", r.URL.Query().Get("action"), "upsert")
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	idx := NewIndexer(srv.URL, "test-key", 5*time.Second)
	if err := idx.UpdateMessage(context.Background(), "msg-1", "updated content"); err != nil {
		t.Fatalf("UpdateMessage() error = %v", err)
	}
}

func TestDeleteMessage_Success(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("method = %s, want DELETE", r.Method)
		}
		if r.URL.Path != "/collections/messages/documents/msg-1" {
			t.Errorf("path = %s, want /collections/messages/documents/msg-1", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	idx := NewIndexer(srv.URL, "test-key", 5*time.Second)
	if err := idx.DeleteMessage(context.Background(), "msg-1"); err != nil {
		t.Fatalf("DeleteMessage() error = %v", err)
	}
}

func TestIndexMessage_RetriesOnTransient500(t *testing.T) {
	t.Parallel()

	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if attempts.Add(1) == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("transient"))
			return
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	idx := NewIndexer(srv.URL, "test-key", 5*time.Second)
	if err := idx.IndexMessage(context.Background(), "msg-1", "hello", "a", "c", 0); err != nil {
		t.Fatalf("IndexMessage() error = %v, want success after retry", err)
	}
	if got := attempts.Load(); got != 2 {
		t.Errorf("attempts = %d, want 2", got)
	}
}

func TestIndexMessage_Persistent500ReturnsError(t *testing.T) {
	t.Parallel()

	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("permanent failure"))
	}))
	defer srv.Close()

	idx := NewIndexer(srv.URL, "test-key", 5*time.Second)
	if err := idx.IndexMessage(context.Background(), "msg-1", "hello", "a", "c", 0); err == nil {
		t.Fatal("IndexMessage() expected error for persistent 500")
	}
	if got := attempts.Load(); got != 2 {
		t.Errorf("attempts = %d, want 2 (initial + 1 retry)", got)
	}
}

func TestDeleteMessage_NotFoundIsAccepted(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	idx := NewIndexer(srv.URL, "test-key", 5*time.Second)
	if err := idx.DeleteMessage(context.Background(), "msg-1"); err != nil {
		t.Fatalf("DeleteMessage() should accept 404, got error = %v", err)
	}
}
