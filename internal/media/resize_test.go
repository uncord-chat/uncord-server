package media

import (
	"bytes"
	"image"
	"image/color" //nolint:misspell // stdlib package name
	"image/png"
	"testing"
)

// createTestPNG generates a solid-colour PNG with the given dimensions.
func createTestPNG(t *testing.T, width, height int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := range height {
		for x := range width {
			img.Set(x, y, color.RGBA{R: 255, G: 0, B: 0, A: 255}) //nolint:misspell // stdlib type
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode test PNG: %v", err)
	}
	return buf.Bytes()
}

func TestResizeImage_OversizedImage(t *testing.T) {
	t.Parallel()
	data := createTestPNG(t, 2000, 2000)

	buf, err := ResizeImage(bytes.NewReader(data), 500, 500)
	if err != nil {
		t.Fatalf("ResizeImage() error = %v", err)
	}
	if buf.Len() == 0 {
		t.Fatal("ResizeImage() returned empty buffer")
	}

	// Decode the output to verify dimensions were constrained.
	cfg, _, err := image.DecodeConfig(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("decode output config: %v", err)
	}
	if cfg.Width > 500 || cfg.Height > 500 {
		t.Errorf("output dimensions %dx%d exceed 500x500", cfg.Width, cfg.Height)
	}
}

func TestResizeImage_WithinBounds(t *testing.T) {
	t.Parallel()
	data := createTestPNG(t, 100, 100)

	buf, err := ResizeImage(bytes.NewReader(data), 500, 500)
	if err != nil {
		t.Fatalf("ResizeImage() error = %v", err)
	}
	if buf.Len() == 0 {
		t.Fatal("ResizeImage() returned empty buffer")
	}

	// Image should be re-encoded at original dimensions.
	cfg, _, err := image.DecodeConfig(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("decode output config: %v", err)
	}
	if cfg.Width != 100 || cfg.Height != 100 {
		t.Errorf("output dimensions %dx%d, want 100x100", cfg.Width, cfg.Height)
	}
}

func TestResizeImage_PreservesAspectRatio(t *testing.T) {
	t.Parallel()
	data := createTestPNG(t, 1000, 500)

	buf, err := ResizeImage(bytes.NewReader(data), 400, 400)
	if err != nil {
		t.Fatalf("ResizeImage() error = %v", err)
	}

	cfg, _, err := image.DecodeConfig(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("decode output config: %v", err)
	}
	// The 2:1 aspect ratio should be preserved: width=400, height=200.
	if cfg.Width != 400 {
		t.Errorf("output width = %d, want 400", cfg.Width)
	}
	if cfg.Height != 200 {
		t.Errorf("output height = %d, want 200", cfg.Height)
	}
}

func TestResizeImage_InvalidData(t *testing.T) {
	t.Parallel()

	_, err := ResizeImage(bytes.NewReader([]byte("not an image")), 500, 500)
	if err == nil {
		t.Fatal("ResizeImage() returned nil error for invalid data")
	}
}

func TestIsAvatarContentType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		ct   string
		want bool
	}{
		{"image/jpeg", true},
		{"image/png", true},
		{"image/gif", true},
		{"image/webp", true},
		{"image/svg+xml", false},
		{"image/bmp", false},
		{"image/tiff", false},
		{"application/pdf", false},
		{"image/jpeg; charset=utf-8", true},
	}
	for _, tt := range tests {
		if got := IsAvatarContentType(tt.ct); got != tt.want {
			t.Errorf("IsAvatarContentType(%q) = %v, want %v", tt.ct, got, tt.want)
		}
	}
}
