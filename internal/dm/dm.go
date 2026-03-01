package dm

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

// Channel type constants.
const (
	TypeDM      = "dm"
	TypeGroupDM = "group_dm"
)

// MaxGroupDMParticipants is the maximum number of participants allowed in a group DM.
const MaxGroupDMParticipants = 10

// Sentinel errors for the dm package.
var (
	ErrDMNotFound         = errors.New("DM channel not found")
	ErrAlreadyParticipant = errors.New("user is already a participant")
	ErrNotParticipant     = errors.New("user is not a participant of this DM channel")
	ErrGroupDMFull        = errors.New("group DM has reached the maximum number of participants")
	ErrNotOwner           = errors.New("only the group DM owner can perform this action")
	ErrCannotRemoveSelf   = errors.New("cannot remove yourself from a DM channel")
	ErrCannotRemoveFromDM = errors.New("cannot remove participants from a 1:1 DM")
)

// Channel represents a DM channel (1:1 or group).
type Channel struct {
	ID        uuid.UUID
	Type      string
	Name      *string
	OwnerID   *uuid.UUID
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Participant represents a user's membership in a DM channel.
type Participant struct {
	DMChannelID uuid.UUID
	UserID      uuid.UUID
	JoinedAt    time.Time
}

// CreateDMParams groups the inputs for creating a 1:1 DM channel.
type CreateDMParams struct {
	CreatorID   uuid.UUID
	RecipientID uuid.UUID
}

// CreateGroupDMParams groups the inputs for creating a group DM channel.
type CreateGroupDMParams struct {
	OwnerID        uuid.UUID
	Name           string
	ParticipantIDs []uuid.UUID
}

// Repository defines the data-access contract for DM channel operations.
type Repository interface {
	// CreateDM creates or retrieves an existing 1:1 DM channel between two users. If a channel already exists between
	// the two users, the existing channel is returned (idempotent).
	CreateDM(ctx context.Context, params CreateDMParams) (*Channel, error)
	// CreateGroupDM creates a new group DM channel with the specified participants.
	CreateGroupDM(ctx context.Context, params CreateGroupDMParams) (*Channel, error)
	// GetByID returns a DM channel by its ID.
	GetByID(ctx context.Context, id uuid.UUID) (*Channel, error)
	// ListForUser returns all DM channels the user participates in, ordered by most recent activity.
	ListForUser(ctx context.Context, userID uuid.UUID) ([]Channel, error)
	// AddParticipant adds a user to a group DM channel.
	AddParticipant(ctx context.Context, channelID, userID uuid.UUID) error
	// RemoveParticipant removes a user from a group DM channel.
	RemoveParticipant(ctx context.Context, channelID, userID uuid.UUID) error
	// ListParticipants returns all participants of a DM channel.
	ListParticipants(ctx context.Context, channelID uuid.UUID) ([]Participant, error)
	// IsParticipant checks whether a user is a participant of a DM channel.
	IsParticipant(ctx context.Context, channelID, userID uuid.UUID) (bool, error)
	// ListDMPeers returns the distinct user IDs of all users who share any DM channel with the given user.
	ListDMPeers(ctx context.Context, userID uuid.UUID) ([]uuid.UUID, error)
}
