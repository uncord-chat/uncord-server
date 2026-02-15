package attachment

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

// Sentinel errors for the attachment package.
var ErrNotFound = errors.New("one or more attachments not found or not available for linking")

// Attachment holds the fields read from the database for a message attachment.
type Attachment struct {
	ID           uuid.UUID
	MessageID    *uuid.UUID
	ChannelID    uuid.UUID
	UploaderID   uuid.UUID
	Filename     string
	ContentType  string
	SizeBytes    int64
	StorageKey   string
	Width        *int
	Height       *int
	ThumbnailKey *string
	CreatedAt    time.Time
}

// CreateParams groups the inputs for inserting a new pending attachment record.
type CreateParams struct {
	ChannelID   uuid.UUID
	UploaderID  uuid.UUID
	Filename    string
	ContentType string
	SizeBytes   int64
	StorageKey  string
	Width       *int
	Height      *int
}

// Repository defines the data-access contract for attachment operations.
type Repository interface {
	// Create inserts a new pending attachment (message_id is NULL).
	Create(ctx context.Context, params CreateParams) (*Attachment, error)

	// GetByID returns a single attachment by ID.
	GetByID(ctx context.Context, id uuid.UUID) (*Attachment, error)

	// LinkToMessage atomically assigns the given attachment IDs to a message. Only pending attachments (message_id IS
	// NULL) owned by uploaderID are linked. Returns ErrNotFound if any ID is missing, already linked, or belongs to a
	// different user.
	LinkToMessage(ctx context.Context, attachmentIDs []uuid.UUID, messageID uuid.UUID, uploaderID uuid.UUID) ([]Attachment, error)

	// ListByMessage returns all attachments linked to the given message, ordered by creation time.
	ListByMessage(ctx context.Context, messageID uuid.UUID) ([]Attachment, error)

	// ListByMessages returns attachments for multiple messages in a single query, keyed by message ID.
	ListByMessages(ctx context.Context, messageIDs []uuid.UUID) (map[uuid.UUID][]Attachment, error)

	// SetThumbnailKey records the storage key of a generated thumbnail.
	SetThumbnailKey(ctx context.Context, id uuid.UUID, thumbnailKey string) error

	// PurgeOrphans deletes pending attachments older than the given threshold and returns their storage keys (including
	// thumbnail keys) so the caller can remove the files.
	PurgeOrphans(ctx context.Context, olderThan time.Time) ([]string, error)
}
