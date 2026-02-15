package media

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color" //nolint:misspell // Go standard library uses American English
	"image/png"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

// fakeUpdater records SetThumbnailKey calls for test assertions.
type fakeUpdater struct {
	calls map[uuid.UUID]string
}

func newFakeUpdater() *fakeUpdater {
	return &fakeUpdater{calls: make(map[uuid.UUID]string)}
}

func (f *fakeUpdater) SetThumbnailKey(_ context.Context, id uuid.UUID, key string) error {
	f.calls[id] = key
	return nil
}

func TestEnqueueThumbnail(t *testing.T) {
	t.Parallel()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = rdb.Close() }()

	ctx := context.Background()
	job := ThumbnailJob{
		AttachmentID: uuid.New().String(),
		StorageKey:   "attachments/test.png",
		ContentType:  "image/png",
	}
	if err := EnqueueThumbnail(ctx, rdb, job); err != nil {
		t.Fatalf("EnqueueThumbnail() error: %v", err)
	}

	msgs, err := rdb.XRange(ctx, thumbnailStream, "-", "+").Result()
	if err != nil {
		t.Fatalf("XRange() error: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("got %d messages, want 1", len(msgs))
	}

	raw := msgs[0].Values["job"].(string)
	var decoded ThumbnailJob
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		t.Fatalf("unmarshal job: %v", err)
	}
	if decoded.AttachmentID != job.AttachmentID {
		t.Errorf("attachment_id = %q, want %q", decoded.AttachmentID, job.AttachmentID)
	}
}

func TestThumbnailWorker_GenerateThumbnail(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// Create a small test PNG image.
	img := image.NewRGBA(image.Rect(0, 0, 800, 600))
	for y := range 600 {
		for x := range 800 {
			img.Set(x, y, color.RGBA{R: 255, G: 0, B: 0, A: 255}) //nolint:misspell // Go standard library uses American English
		}
	}
	var imgBuf bytes.Buffer
	if err := png.Encode(&imgBuf, img); err != nil {
		t.Fatalf("encode test PNG: %v", err)
	}

	dir := t.TempDir()
	store := NewLocalStorage(dir, "http://localhost:8080")

	storageKey := "attachments/test.png"
	if err := store.Put(ctx, storageKey, bytes.NewReader(imgBuf.Bytes())); err != nil {
		t.Fatalf("store.Put() error: %v", err)
	}

	attachmentID := uuid.New()
	updater := newFakeUpdater()

	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = rdb.Close() }()

	worker := NewThumbnailWorker(rdb, store, updater, zerolog.Nop())

	job := ThumbnailJob{
		AttachmentID: attachmentID.String(),
		StorageKey:   storageKey,
		ContentType:  "image/png",
	}
	if err := worker.generateThumbnail(ctx, job); err != nil {
		t.Fatalf("generateThumbnail() error: %v", err)
	}

	expectedKey := "thumbnails/" + attachmentID.String() + ".jpg"
	if updater.calls[attachmentID] != expectedKey {
		t.Errorf("thumbnail key = %q, want %q", updater.calls[attachmentID], expectedKey)
	}

	// Verify the thumbnail file was created and is a valid JPEG.
	rc, err := store.Get(ctx, expectedKey)
	if err != nil {
		t.Fatalf("store.Get() thumbnail error: %v", err)
	}
	defer func() { _ = rc.Close() }()

	thumbImg, format, err := image.Decode(rc)
	if err != nil {
		t.Fatalf("decode thumbnail: %v", err)
	}
	if format != "jpeg" {
		t.Errorf("thumbnail format = %q, want %q", format, "jpeg")
	}

	bounds := thumbImg.Bounds()
	if bounds.Dx() != thumbnailWidth {
		t.Errorf("thumbnail width = %d, want %d", bounds.Dx(), thumbnailWidth)
	}
}
