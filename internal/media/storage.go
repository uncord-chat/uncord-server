package media

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"strings"
)

// Sentinel errors for storage operations.
var (
	ErrUnsupportedContentType = errors.New("content type is not allowed")
	ErrFileTooLarge           = errors.New("file exceeds the maximum upload size")
	ErrStorageKeyNotFound     = errors.New("storage key not found")
)

// StorageProvider abstracts file storage so the server can swap between local disk, S3, or other backends without
// changing business logic.
type StorageProvider interface {
	// Put writes the contents of r to the given key, creating parent directories as needed. The caller is responsible
	// for closing r.
	Put(ctx context.Context, key string, r io.Reader) error

	// Get opens the file at key for reading. The caller must close the returned ReadCloser. Returns
	// ErrStorageKeyNotFound when the key does not exist.
	Get(ctx context.Context, key string) (io.ReadCloser, error)

	// Delete removes the file at key. Missing keys are not treated as errors.
	Delete(ctx context.Context, key string) error

	// URL returns the public URL for the given storage key.
	URL(key string) string
}

// AllowedContentTypes maps MIME types that are accepted for upload. Executables are intentionally excluded.
var AllowedContentTypes = map[string]bool{
	// Images
	"image/jpeg":    true,
	"image/png":     true,
	"image/gif":     true,
	"image/webp":    true,
	"image/svg+xml": true,
	"image/bmp":     true,
	"image/tiff":    true,
	"image/avif":    true,

	// Video
	"video/mp4":       true,
	"video/webm":      true,
	"video/ogg":       true,
	"video/quicktime": true,

	// Audio
	"audio/mpeg":  true,
	"audio/ogg":   true,
	"audio/wav":   true,
	"audio/webm":  true,
	"audio/flac":  true,
	"audio/aac":   true,
	"audio/x-m4a": true,

	// Documents
	"application/pdf":  true,
	"text/plain":       true,
	"text/csv":         true,
	"application/json": true,
	"application/xml":  true,
	"application/vnd.openxmlformats-officedocument.wordprocessingml.document":   true,
	"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet":         true,
	"application/vnd.openxmlformats-officedocument.presentationml.presentation": true,
	"application/vnd.oasis.opendocument.text":                                   true,
	"application/vnd.oasis.opendocument.spreadsheet":                            true,
	"application/vnd.oasis.opendocument.presentation":                           true,
	"application/rtf": true,

	// Archives
	"application/zip":              true,
	"application/gzip":             true,
	"application/x-tar":            true,
	"application/x-7z-compressed":  true,
	"application/x-rar-compressed": true,
}

// ImageContentTypes maps MIME types eligible for thumbnail generation. SVG is excluded because it is a vector format
// that does not benefit from raster resizing.
var ImageContentTypes = map[string]bool{
	"image/jpeg": true,
	"image/png":  true,
	"image/gif":  true,
	"image/webp": true,
	"image/bmp":  true,
	"image/tiff": true,
}

// IsAllowedContentType reports whether the given MIME type is accepted for upload.
func IsAllowedContentType(contentType string) bool {
	return AllowedContentTypes[normaliseContentType(contentType)]
}

// IsImageContentType reports whether the given MIME type is eligible for thumbnail generation.
func IsImageContentType(contentType string) bool {
	return ImageContentTypes[normaliseContentType(contentType)]
}

// ExtensionFromFilename extracts the file extension from a filename, including the leading dot (e.g. ".jpg"). Returns
// an empty string when the filename has no extension.
func ExtensionFromFilename(filename string) string {
	return strings.ToLower(filepath.Ext(filename))
}

// normaliseContentType strips any parameters (e.g. charset) from a MIME type and lowercases it.
func normaliseContentType(ct string) string {
	if i := strings.IndexByte(ct, ';'); i != -1 {
		ct = ct[:i]
	}
	return strings.TrimSpace(strings.ToLower(ct))
}
