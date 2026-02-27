package media

import (
	"bytes"
	"fmt"
	"image"
	// Register standard image decoders so image.Decode handles JPEG, PNG, GIF, and WebP input.
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"

	"github.com/HugoSmits86/nativewebp"
	"github.com/disintegration/imaging"
	_ "golang.org/x/image/webp" // Register WebP decoder for image.Decode.
)

// AvatarContentTypes maps MIME types accepted for avatar and banner uploads. SVG is excluded to prevent XSS from
// embedded scripts, and BMP/TIFF are excluded as legacy formats with no benefit for profile images.
var AvatarContentTypes = map[string]bool{
	"image/jpeg": true,
	"image/png":  true,
	"image/gif":  true,
	"image/webp": true,
}

// IsAvatarContentType reports whether the given MIME type is accepted for avatar and banner uploads.
func IsAvatarContentType(contentType string) bool {
	return AvatarContentTypes[normaliseContentType(contentType)]
}

// ResizeImage decodes an image from r, constrains it to fit within maxWidth x maxHeight while preserving aspect ratio,
// and re-encodes the result as lossless WebP. Images already within bounds are re-encoded without scaling.
func ResizeImage(r io.Reader, maxWidth, maxHeight int) (*bytes.Buffer, error) {
	img, _, err := image.Decode(r)
	if err != nil {
		return nil, fmt.Errorf("decode image: %w", err)
	}

	bounds := img.Bounds()
	if bounds.Dx() > maxWidth || bounds.Dy() > maxHeight {
		img = imaging.Fit(img, maxWidth, maxHeight, imaging.Lanczos)
	}

	var buf bytes.Buffer
	if err := nativewebp.Encode(&buf, img, nil); err != nil {
		return nil, fmt.Errorf("encode webp: %w", err)
	}

	return &buf, nil
}
