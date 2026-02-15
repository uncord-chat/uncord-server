package media

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// LocalStorage stores files on the local filesystem.
type LocalStorage struct {
	basePath string
	baseURL  string
}

// NewLocalStorage creates a storage provider that reads and writes files under basePath. Public URLs are constructed by
// joining baseURL with the storage key.
func NewLocalStorage(basePath, baseURL string) *LocalStorage {
	return &LocalStorage{
		basePath: basePath,
		baseURL:  strings.TrimRight(baseURL, "/"),
	}
}

// Put writes the contents of r to the file identified by key. Parent directories are created automatically. If the
// write fails partway through, the partially written file is removed.
func (s *LocalStorage) Put(_ context.Context, key string, r io.Reader) error {
	fullPath := filepath.Join(s.basePath, key)

	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		return fmt.Errorf("create storage directory: %w", err)
	}

	f, err := os.Create(fullPath)
	if err != nil {
		return fmt.Errorf("create storage file: %w", err)
	}

	if _, err := io.Copy(f, r); err != nil {
		_ = f.Close()
		_ = os.Remove(fullPath)
		return fmt.Errorf("write storage file: %w", err)
	}

	if err := f.Close(); err != nil {
		_ = os.Remove(fullPath)
		return fmt.Errorf("close storage file: %w", err)
	}
	return nil
}

// Get opens the file identified by key for reading. Returns ErrStorageKeyNotFound when the file does not exist.
func (s *LocalStorage) Get(_ context.Context, key string) (io.ReadCloser, error) {
	fullPath := filepath.Join(s.basePath, key)
	f, err := os.Open(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrStorageKeyNotFound
		}
		return nil, fmt.Errorf("open storage file: %w", err)
	}
	return f, nil
}

// Delete removes the file at key. If the file does not exist, no error is returned.
func (s *LocalStorage) Delete(_ context.Context, key string) error {
	fullPath := filepath.Join(s.basePath, key)
	if err := os.Remove(fullPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("delete storage file: %w", err)
	}
	return nil
}

// URL returns the public URL for the given storage key.
func (s *LocalStorage) URL(key string) string {
	return s.baseURL + "/media/" + key
}
