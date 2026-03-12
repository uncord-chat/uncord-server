package settingsync

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

// Sentinel errors for the settings sync package.
var (
	ErrNotFound        = errors.New("synced settings not found")
	ErrVersionConflict = errors.New("blob version must be higher than the current stored version")
	ErrSaltLength      = errors.New("salt must be exactly 16 bytes")
	ErrNonceLength     = errors.New("nonce must be exactly 12 bytes")
	ErrBlobTooLarge    = errors.New("encrypted blob must not exceed 65536 bytes")
	ErrBlobEmpty       = errors.New("encrypted blob must not be empty")
	ErrVersionInvalid  = errors.New("blob version must be at least 1")
)

// MaxBlobSize is the maximum size of the encrypted blob in bytes.
const MaxBlobSize = 65536

// Blob holds the encrypted synced settings stored on behalf of a user.
type Blob struct {
	UserID        uuid.UUID
	EncryptedBlob []byte
	Salt          []byte
	Nonce         []byte
	BlobVersion   int
	UpdatedAt     time.Time
}

// UpsertParams groups the inputs for creating or updating a synced settings blob.
type UpsertParams struct {
	EncryptedBlob []byte
	Salt          []byte
	Nonce         []byte
	BlobVersion   int
}

// Repository defines the data-access contract for synced settings operations.
type Repository interface {
	Get(ctx context.Context, userID uuid.UUID) (*Blob, error)
	Upsert(ctx context.Context, userID uuid.UUID, params UpsertParams) (*Blob, error)
	Delete(ctx context.Context, userID uuid.UUID) error
}

var _ Repository = (*PGRepository)(nil)
