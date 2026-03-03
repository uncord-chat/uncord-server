package media

import "testing"

func TestIsAllowedContentType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		contentType string
		want        bool
	}{
		{"image/jpeg allowed", "image/jpeg", true},
		{"image/png allowed", "image/png", true},
		{"image/gif allowed", "image/gif", true},
		{"image/webp allowed", "image/webp", true},
		{"image/svg+xml allowed", "image/svg+xml", true},
		{"video/mp4 allowed", "video/mp4", true},
		{"audio/mpeg allowed", "audio/mpeg", true},
		{"application/pdf allowed", "application/pdf", true},
		{"application/zip allowed", "application/zip", true},
		{"text/plain allowed", "text/plain", true},

		// With charset parameter
		{"text/plain with charset", "text/plain; charset=utf-8", true},
		{"application/json with charset", "application/json; charset=utf-8", true},

		// Blocked
		{"msdownload blocked", "application/x-msdownload", false},
		{"executable blocked", "application/x-executable", false},
		{"octet-stream blocked", "application/octet-stream", false},
		{"empty string blocked", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsAllowedContentType(tt.contentType); got != tt.want {
				t.Errorf("IsAllowedContentType(%q) = %v, want %v", tt.contentType, got, tt.want)
			}
		})
	}
}

func TestIsImageContentType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		contentType string
		want        bool
	}{
		{"jpeg is image", "image/jpeg", true},
		{"png is image", "image/png", true},
		{"gif is image", "image/gif", true},
		{"webp is image", "image/webp", true},
		{"bmp is image", "image/bmp", true},
		{"tiff is image", "image/tiff", true},

		// SVG excluded from thumbnails
		{"svg excluded", "image/svg+xml", false},
		{"avif excluded", "image/avif", false},

		// Non-images
		{"video not image", "video/mp4", false},
		{"pdf not image", "application/pdf", false},
		{"empty string", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsImageContentType(tt.contentType); got != tt.want {
				t.Errorf("IsImageContentType(%q) = %v, want %v", tt.contentType, got, tt.want)
			}
		})
	}
}

func TestExtensionFromFilename(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		filename string
		want     string
	}{
		{"lowercase extension", "photo.jpg", ".jpg"},
		{"uppercase extension", "photo.JPG", ".jpg"},
		{"uppercase PDF", "document.PDF", ".pdf"},
		{"double extension takes last", "archive.tar.gz", ".gz"},
		{"no extension", "noextension", ""},
		{"hidden file", ".hidden", ".hidden"},
		{"empty string", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ExtensionFromFilename(tt.filename); got != tt.want {
				t.Errorf("ExtensionFromFilename(%q) = %q, want %q", tt.filename, got, tt.want)
			}
		})
	}
}

func TestNormaliseContentType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"already normalised", "image/jpeg", "image/jpeg"},
		{"uppercase normalised", "IMAGE/JPEG", "image/jpeg"},
		{"strips charset parameter", "text/plain; charset=utf-8", "text/plain"},
		{"trims whitespace and strips params", "  Application/JSON ; charset=utf-8 ", "application/json"},
		{"empty string", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normaliseContentType(tt.input); got != tt.want {
				t.Errorf("normaliseContentType(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
