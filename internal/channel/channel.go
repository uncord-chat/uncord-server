package channel

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

// Channel type constants matching the database CHECK constraint.
const (
	TypeText         = "text"
	TypeVoice        = "voice"
	TypeAnnouncement = "announcement"
	TypeForum        = "forum"
	TypeStage        = "stage"
)

// validTypes is the set of allowed channel types.
var validTypes = map[string]bool{
	TypeText:         true,
	TypeVoice:        true,
	TypeAnnouncement: true,
	TypeForum:        true,
	TypeStage:        true,
}

// Sentinel errors for the channel package.
var (
	ErrNotFound           = errors.New("channel not found")
	ErrMaxChannelsReached = errors.New("maximum number of channels reached")
	ErrNameLength         = errors.New("channel name must be between 1 and 100 characters")
	ErrInvalidType        = errors.New("invalid channel type")
	ErrTopicLength        = errors.New("channel topic must be 1024 characters or fewer")
	ErrInvalidSlowmode    = errors.New("slowmode seconds must be between 0 and 21600")
	ErrInvalidPosition    = errors.New("position must be non-negative")
	ErrCategoryNotFound   = errors.New("category not found")
)

// Channel holds the fields read from the database.
type Channel struct {
	ID              uuid.UUID
	CategoryID      *uuid.UUID
	Name            string
	Type            string
	Topic           string
	Position        int
	SlowmodeSeconds int
	NSFW            bool
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// CreateParams groups the inputs for creating a new channel.
type CreateParams struct {
	Name            string
	Type            string
	CategoryID      *uuid.UUID
	Topic           string
	SlowmodeSeconds int
	NSFW            bool
}

// UpdateParams groups the optional fields for updating a channel. SetCategoryNull distinguishes "no change" (nil
// CategoryID with SetCategoryNull false) from "remove from category" (nil CategoryID with SetCategoryNull true).
type UpdateParams struct {
	Name            *string
	CategoryID      *uuid.UUID
	SetCategoryNull bool
	Topic           *string
	Position        *int
	SlowmodeSeconds *int
	NSFW            *bool
}

// ValidateName checks that a non-nil name is between 1 and 100 characters (runes) after trimming whitespace. A nil
// pointer means "no change" (useful for PATCH semantics); a non-nil pointer is always validated. On success the
// pointed-to value is replaced with the trimmed result.
func ValidateName(name *string) error {
	if name == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*name)
	if utf8.RuneCountInString(trimmed) < 1 || utf8.RuneCountInString(trimmed) > 100 {
		return ErrNameLength
	}
	*name = trimmed
	return nil
}

// ValidateNameRequired validates and trims a name that must be present. It returns the trimmed result on success.
func ValidateNameRequired(name string) (string, error) {
	trimmed := strings.TrimSpace(name)
	if utf8.RuneCountInString(trimmed) < 1 || utf8.RuneCountInString(trimmed) > 100 {
		return "", ErrNameLength
	}
	return trimmed, nil
}

// ValidateType checks that the channel type is one of the allowed values.
func ValidateType(t string) error {
	if !validTypes[t] {
		return ErrInvalidType
	}
	return nil
}

// ValidateTopic checks that a non-nil topic is 1024 characters (runes) or fewer. A nil pointer means "no change."
func ValidateTopic(topic *string) error {
	if topic == nil {
		return nil
	}
	if utf8.RuneCountInString(*topic) > 1024 {
		return ErrTopicLength
	}
	return nil
}

// ValidateSlowmode checks that a non-nil slowmode value is between 0 and 21600 (6 hours). A nil pointer means
// "no change."
func ValidateSlowmode(seconds *int) error {
	if seconds == nil {
		return nil
	}
	if *seconds < 0 || *seconds > 21600 {
		return ErrInvalidSlowmode
	}
	return nil
}

// ValidatePosition checks that a non-nil position is non-negative. A nil pointer means "no change."
func ValidatePosition(pos *int) error {
	if pos == nil {
		return nil
	}
	if *pos < 0 {
		return ErrInvalidPosition
	}
	return nil
}

// Repository defines the data-access contract for channel operations.
type Repository interface {
	List(ctx context.Context) ([]Channel, error)
	GetByID(ctx context.Context, id uuid.UUID) (*Channel, error)
	Create(ctx context.Context, params CreateParams, maxChannels int) (*Channel, error)
	Update(ctx context.Context, id uuid.UUID, params UpdateParams) (*Channel, error)
	Delete(ctx context.Context, id uuid.UUID) error
}
