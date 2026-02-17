package media

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// LocalStorage stores files on the local filesystem. All file operations are scoped to a root directory using os.Root
// (Go 1.24+), which guarantees that no key can escape the base directory via traversal sequences, symbolic links, or
// OS-specific tricks.
type LocalStorage struct {
	root    *os.Root
	baseURL string
}

// NewLocalStorage creates a storage provider that reads and writes files under basePath. The base directory must exist.
// Public URLs are constructed by joining baseURL with the storage key.
func NewLocalStorage(basePath, baseURL string) (*LocalStorage, error) {
	root, err := os.OpenRoot(basePath)
	if err != nil {
		return nil, fmt.Errorf("open storage root %s: %w", basePath, err)
	}
	return &LocalStorage{
		root:    root,
		baseURL: strings.TrimRight(baseURL, "/"),
	}, nil
}

// Close releases the underlying root directory handle.
func (s *LocalStorage) Close() error {
	return s.root.Close()
}

// Put writes the contents of r to the file identified by key. Parent directories are created automatically. If the
// write fails partway through, the partially written file is removed.
func (s *LocalStorage) Put(_ context.Context, key string, r io.Reader) error {
	dir := filepath.Dir(key)
	if dir != "." {
		if err := s.root.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create storage directory: %w", err)
		}
	}

	f, err := s.root.Create(key)
	if err != nil {
		return fmt.Errorf("create storage file: %w", err)
	}

	if _, err := io.Copy(f, r); err != nil {
		_ = f.Close()
		_ = s.root.Remove(key)
		return fmt.Errorf("write storage file: %w", err)
	}

	if err := f.Close(); err != nil {
		_ = s.root.Remove(key)
		return fmt.Errorf("close storage file: %w", err)
	}
	return nil
}

// Get opens the file identified by key for reading. Returns ErrStorageKeyNotFound when the file does not exist.
func (s *LocalStorage) Get(_ context.Context, key string) (io.ReadCloser, error) {
	f, err := s.root.Open(key)
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
	if err := s.root.Remove(key); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("delete storage file: %w", err)
	}
	return nil
}

// URL returns the public URL for the given storage key.
func (s *LocalStorage) URL(key string) string {
	return s.baseURL + "/media/" + key
}
