package media

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/gif" // Register GIF decoder for image.Decode
	"image/jpeg"
	_ "image/png" // Register PNG decoder for image.Decode
	"strings"
	"time"

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

	// retryMinIdle is the minimum time a message must sit unacknowledged before it becomes eligible for reclaim.
	retryMinIdle = 30 * time.Second

	// maxRetries is the maximum number of delivery attempts for a single job. After this many failures the job is
	// acknowledged and discarded to prevent infinite retry loops.
	maxRetries = 3
)

// errPermanent wraps an error to indicate that retrying will not help (e.g. corrupt image, invalid UUID).
var errPermanent = errors.New("permanent")

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

// Run reads and processes thumbnail jobs until the context is cancelled. Transient failures leave the message
// unacknowledged so it can be reclaimed on the next iteration. Permanent failures and messages that exceed the maximum
// retry count are acknowledged and discarded.
func (w *ThumbnailWorker) Run(ctx context.Context) error {
	consumerName := "worker-" + uuid.New().String()[:8]

	for {
		w.reclaimStale(ctx, consumerName)

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

// reclaimStale uses XAUTOCLAIM to take ownership of messages that have been pending longer than retryMinIdle. This
// handles jobs that failed with a transient error on a previous attempt.
func (w *ThumbnailWorker) reclaimStale(ctx context.Context, consumerName string) {
	msgs, _, err := w.rdb.XAutoClaim(ctx, &redis.XAutoClaimArgs{
		Stream:   thumbnailStream,
		Group:    consumerGroup,
		Consumer: consumerName,
		MinIdle:  retryMinIdle,
		Start:    "0-0",
		Count:    10,
	}).Result()
	if err != nil {
		if ctx.Err() == nil {
			w.log.Warn().Err(err).Msg("Failed to reclaim stale thumbnail jobs")
		}
		return
	}

	for _, msg := range msgs {
		w.processJob(ctx, msg)
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
		if errors.Is(err, errPermanent) || w.deliveryCount(ctx, msg.ID) >= maxRetries {
			w.log.Warn().Err(err).Str("attachment_id", job.AttachmentID).Msg("Thumbnail generation failed permanently")
			w.ack(ctx, msg.ID)
			return
		}
		w.log.Warn().Err(err).Str("attachment_id", job.AttachmentID).Msg("Thumbnail generation failed, will retry")
		return
	}
	w.ack(ctx, msg.ID)
}

func (w *ThumbnailWorker) generateThumbnail(ctx context.Context, job ThumbnailJob) error {
	rc, err := w.storage.Get(ctx, job.StorageKey)
	if err != nil {
		if errors.Is(err, ErrStorageKeyNotFound) {
			return fmt.Errorf("read original: %w", errors.Join(err, errPermanent))
		}
		return fmt.Errorf("read original: %w", err)
	}
	defer func() { _ = rc.Close() }()

	img, _, err := image.Decode(rc)
	if err != nil {
		return fmt.Errorf("decode image: %w", errors.Join(err, errPermanent))
	}

	thumb := imaging.Resize(img, thumbnailWidth, 0, imaging.Lanczos)

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, thumb, &jpeg.Options{Quality: thumbnailQuality}); err != nil {
		return fmt.Errorf("encode thumbnail: %w", errors.Join(err, errPermanent))
	}

	thumbnailKey := "thumbnails/" + job.AttachmentID + ".jpg"
	if err := w.storage.Put(ctx, thumbnailKey, &buf); err != nil {
		return fmt.Errorf("write thumbnail: %w", err)
	}

	attachmentID, err := uuid.Parse(job.AttachmentID)
	if err != nil {
		return fmt.Errorf("parse attachment id: %w", errors.Join(err, errPermanent))
	}

	if err := w.updater.SetThumbnailKey(ctx, attachmentID, thumbnailKey); err != nil {
		return fmt.Errorf("update thumbnail key: %w", err)
	}

	w.log.Debug().Str("attachment_id", job.AttachmentID).Msg("Thumbnail generated")
	return nil
}

// deliveryCount returns how many times the given message has been delivered to a consumer. Returns maxRetries on error
// so the caller treats it as exhausted rather than retrying indefinitely.
func (w *ThumbnailWorker) deliveryCount(ctx context.Context, messageID string) int64 {
	pending, err := w.rdb.XPendingExt(ctx, &redis.XPendingExtArgs{
		Stream: thumbnailStream,
		Group:  consumerGroup,
		Start:  messageID,
		End:    messageID,
		Count:  1,
	}).Result()
	if err != nil || len(pending) == 0 {
		return maxRetries
	}
	return pending[0].RetryCount
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
