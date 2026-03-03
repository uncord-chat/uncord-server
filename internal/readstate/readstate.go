package readstate

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/uncord-chat/uncord-protocol/models"
)

// Sentinel errors for the readstate package.
var (
	ErrNotFound            = errors.New("read state not found")
	ErrMessageNotInChannel = errors.New("message does not exist in this channel")
)

// ReadState holds the database representation of a user's read position in a channel.
type ReadState struct {
	UserID        uuid.UUID
	ChannelID     uuid.UUID
	LastMessageID *uuid.UUID
	MentionCount  int
	UpdatedAt     time.Time
}

// ToModel converts the internal read state to the protocol response type.
func (rs *ReadState) ToModel() models.ReadState {
	var lastMsgID *string
	if rs.LastMessageID != nil {
		s := rs.LastMessageID.String()
		lastMsgID = &s
	}
	return models.ReadState{
		ChannelID:     rs.ChannelID.String(),
		LastMessageID: lastMsgID,
		MentionCount:  rs.MentionCount,
	}
}

// Repository defines the data-access contract for channel read state operations.
type Repository interface {
	ListByUser(ctx context.Context, userID uuid.UUID) ([]ReadState, error)
	Ack(ctx context.Context, userID, channelID, messageID uuid.UUID) (*ReadState, error)
	DeleteByChannel(ctx context.Context, channelID uuid.UUID) error
}

var _ Repository = (*PGRepository)(nil)
