package media

import "testing"

func TestIsAllowedContentType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		contentType string
		want        bool
	}{
		{"image/jpeg", true},
		{"image/png", true},
		{"image/gif", true},
		{"image/webp", true},
		{"image/svg+xml", true},
		{"video/mp4", true},
		{"audio/mpeg", true},
		{"application/pdf", true},
		{"application/zip", true},
		{"text/plain", true},

		// With charset parameter
		{"text/plain; charset=utf-8", true},
		{"application/json; charset=utf-8", true},

		// Blocked
		{"application/x-msdownload", false},
		{"application/x-executable", false},
		{"application/octet-stream", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := IsAllowedContentType(tt.contentType); got != tt.want {
			t.Errorf("IsAllowedContentType(%q) = %v, want %v", tt.contentType, got, tt.want)
		}
	}
}

func TestIsImageContentType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		contentType string
		want        bool
	}{
		{"image/jpeg", true},
		{"image/png", true},
		{"image/gif", true},
		{"image/webp", true},
		{"image/bmp", true},
		{"image/tiff", true},

		// SVG excluded from thumbnails
		{"image/svg+xml", false},
		{"image/avif", false},

		// Non-images
		{"video/mp4", false},
		{"application/pdf", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := IsImageContentType(tt.contentType); got != tt.want {
			t.Errorf("IsImageContentType(%q) = %v, want %v", tt.contentType, got, tt.want)
		}
	}
}

func TestExtensionFromFilename(t *testing.T) {
	t.Parallel()

	tests := []struct {
		filename string
		want     string
	}{
		{"photo.jpg", ".jpg"},
		{"photo.JPG", ".jpg"},
		{"document.PDF", ".pdf"},
		{"archive.tar.gz", ".gz"},
		{"noextension", ""},
		{".hidden", ".hidden"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := ExtensionFromFilename(tt.filename); got != tt.want {
			t.Errorf("ExtensionFromFilename(%q) = %q, want %q", tt.filename, got, tt.want)
		}
	}
}

func TestNormaliseContentType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  string
	}{
		{"image/jpeg", "image/jpeg"},
		{"IMAGE/JPEG", "image/jpeg"},
		{"text/plain; charset=utf-8", "text/plain"},
		{"  Application/JSON ; charset=utf-8 ", "application/json"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := normaliseContentType(tt.input); got != tt.want {
			t.Errorf("normaliseContentType(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
