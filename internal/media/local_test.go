package media

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestLocalStorage_PutAndGet(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dir := t.TempDir()
	store := NewLocalStorage(dir, "http://localhost:8080")

	content := []byte("hello world")
	if err := store.Put(ctx, "test/file.txt", bytes.NewReader(content)); err != nil {
		t.Fatalf("Put() error: %v", err)
	}

	rc, err := store.Get(ctx, "test/file.txt")
	if err != nil {
		t.Fatalf("Get() error: %v", err)
	}
	defer func() { _ = rc.Close() }()

	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("ReadAll() error: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Errorf("Get() content = %q, want %q", got, content)
	}
}

func TestLocalStorage_GetNotFound(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dir := t.TempDir()
	store := NewLocalStorage(dir, "http://localhost:8080")

	_, err := store.Get(ctx, "nonexistent.txt")
	if !errors.Is(err, ErrStorageKeyNotFound) {
		t.Errorf("Get() error = %v, want ErrStorageKeyNotFound", err)
	}
}

func TestLocalStorage_Delete(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dir := t.TempDir()
	store := NewLocalStorage(dir, "http://localhost:8080")

	content := []byte("to be deleted")
	if err := store.Put(ctx, "delete-me.txt", bytes.NewReader(content)); err != nil {
		t.Fatalf("Put() error: %v", err)
	}

	if err := store.Delete(ctx, "delete-me.txt"); err != nil {
		t.Fatalf("Delete() error: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "delete-me.txt")); !os.IsNotExist(err) {
		t.Error("file still exists after Delete()")
	}
}

func TestLocalStorage_DeleteNonexistent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dir := t.TempDir()
	store := NewLocalStorage(dir, "http://localhost:8080")

	if err := store.Delete(ctx, "nonexistent.txt"); err != nil {
		t.Errorf("Delete() error = %v, want nil for missing file", err)
	}
}

func TestLocalStorage_URL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		baseURL string
		key     string
		want    string
	}{
		{"http://localhost:8080", "attachments/abc.jpg", "http://localhost:8080/media/attachments/abc.jpg"},
		{"http://localhost:8080/", "attachments/abc.jpg", "http://localhost:8080/media/attachments/abc.jpg"},
		{"https://cdn.example.com", "thumbnails/def.jpg", "https://cdn.example.com/media/thumbnails/def.jpg"},
	}
	for _, tt := range tests {
		store := NewLocalStorage(t.TempDir(), tt.baseURL)
		if got := store.URL(tt.key); got != tt.want {
			t.Errorf("URL(%q) with base %q = %q, want %q", tt.key, tt.baseURL, got, tt.want)
		}
	}
}

func TestLocalStorage_PutCreatesNestedDirs(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dir := t.TempDir()
	store := NewLocalStorage(dir, "http://localhost:8080")

	key := "a/b/c/deep.txt"
	if err := store.Put(ctx, key, bytes.NewReader([]byte("deep"))); err != nil {
		t.Fatalf("Put() error: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, key)); err != nil {
		t.Errorf("nested file not found: %v", err)
	}
}
