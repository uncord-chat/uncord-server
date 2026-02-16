package media

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	_ "image/gif" // Register GIF decoder for image.Decode
	"image/jpeg"
	_ "image/png" // Register PNG decoder for image.Decode
	"strings"

	"github.com/disintegration/imaging"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

const (
	thumbnailStream  = "uncord.jobs.thumbnails"
	consumerGroup    = "uncord-workers"
	thumbnailWidth   = 400
	thumbnailQuality = 85
)

// ThumbnailJob describes a pending thumbnail generation task.
type ThumbnailJob struct {
	AttachmentID string `json:"attachment_id"`
	StorageKey   string `json:"storage_key"`
	ContentType  string `json:"content_type"`
}

// ThumbnailKeyUpdater records generated thumbnail keys. Satisfied by attachment.Repository.
type ThumbnailKeyUpdater interface {
	SetThumbnailKey(ctx context.Context, id uuid.UUID, thumbnailKey string) error
}

// ThumbnailWorker consumes thumbnail generation jobs from a Valkey stream and produces JPEG thumbnails.
type ThumbnailWorker struct {
	rdb     *redis.Client
	storage StorageProvider
	updater ThumbnailKeyUpdater
	log     zerolog.Logger
}

// NewThumbnailWorker creates a worker that processes thumbnail jobs.
func NewThumbnailWorker(rdb *redis.Client, storage StorageProvider, updater ThumbnailKeyUpdater, logger zerolog.Logger) *ThumbnailWorker {
	return &ThumbnailWorker{
		rdb:     rdb,
		storage: storage,
		updater: updater,
		log:     logger,
	}
}

// EnsureStream creates the consumer group for the thumbnail stream, ignoring errors if the group already exists.
func (w *ThumbnailWorker) EnsureStream(ctx context.Context) {
	err := w.rdb.XGroupCreateMkStream(ctx, thumbnailStream, consumerGroup, "0").Err()
	if err != nil && !strings.HasPrefix(err.Error(), "BUSYGROUP") {
		w.log.Warn().Err(err).Msg("Failed to create thumbnail consumer group")
	}
}

// Run reads and processes thumbnail jobs until the context is cancelled. Each job failure is logged but does not stop
// the worker.
func (w *ThumbnailWorker) Run(ctx context.Context) error {
	consumerName := "worker-" + uuid.New().String()[:8]

	for {
		streams, err := w.rdb.XReadGroup(ctx, &redis.XReadGroupArgs{
			Group:    consumerGroup,
			Consumer: consumerName,
			Streams:  []string{thumbnailStream, ">"},
			Count:    1,
			Block:    0,
		}).Result()
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("xreadgroup: %w", err)
		}

		for _, stream := range streams {
			for _, msg := range stream.Messages {
				w.processJob(ctx, msg)
			}
		}
	}
}

func (w *ThumbnailWorker) processJob(ctx context.Context, msg redis.XMessage) {
	raw, ok := msg.Values["job"]
	if !ok {
		w.log.Warn().Str("message_id", msg.ID).Msg("Thumbnail job missing 'job' field")
		w.ack(ctx, msg.ID)
		return
	}

	var job ThumbnailJob
	if err := json.Unmarshal([]byte(raw.(string)), &job); err != nil {
		w.log.Warn().Err(err).Str("message_id", msg.ID).Msg("Failed to unmarshal thumbnail job")
		w.ack(ctx, msg.ID)
		return
	}

	if err := w.generateThumbnail(ctx, job); err != nil {
		w.log.Warn().Err(err).Str("attachment_id", job.AttachmentID).Msg("Thumbnail generation failed")
	}
	w.ack(ctx, msg.ID)
}

func (w *ThumbnailWorker) generateThumbnail(ctx context.Context, job ThumbnailJob) error {
	rc, err := w.storage.Get(ctx, job.StorageKey)
	if err != nil {
		return fmt.Errorf("read original: %w", err)
	}
	defer func() { _ = rc.Close() }()

	img, _, err := image.Decode(rc)
	if err != nil {
		return fmt.Errorf("decode image: %w", err)
	}

	thumb := imaging.Resize(img, thumbnailWidth, 0, imaging.Lanczos)

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, thumb, &jpeg.Options{Quality: thumbnailQuality}); err != nil {
		return fmt.Errorf("encode thumbnail: %w", err)
	}

	thumbnailKey := "thumbnails/" + job.AttachmentID + ".jpg"
	if err := w.storage.Put(ctx, thumbnailKey, &buf); err != nil {
		return fmt.Errorf("write thumbnail: %w", err)
	}

	attachmentID, err := uuid.Parse(job.AttachmentID)
	if err != nil {
		return fmt.Errorf("parse attachment id: %w", err)
	}

	if err := w.updater.SetThumbnailKey(ctx, attachmentID, thumbnailKey); err != nil {
		return fmt.Errorf("update thumbnail key: %w", err)
	}

	w.log.Debug().Str("attachment_id", job.AttachmentID).Msg("Thumbnail generated")
	return nil
}

func (w *ThumbnailWorker) ack(ctx context.Context, messageID string) {
	if err := w.rdb.XAck(ctx, thumbnailStream, consumerGroup, messageID).Err(); err != nil {
		w.log.Warn().Err(err).Str("message_id", messageID).Msg("Failed to ACK thumbnail job")
	}
}

// EnqueueThumbnail adds a thumbnail generation job to the stream.
func EnqueueThumbnail(ctx context.Context, rdb *redis.Client, job ThumbnailJob) error {
	data, err := json.Marshal(job)
	if err != nil {
		return fmt.Errorf("marshal thumbnail job: %w", err)
	}
	return rdb.XAdd(ctx, &redis.XAddArgs{
		Stream: thumbnailStream,
		Values: map[string]any{"job": string(data)},
	}).Err()
}
