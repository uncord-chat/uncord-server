package member

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/uncord-chat/uncord-protocol/models"
)

// Sentinel errors for the member package.
var (
	ErrNotFound       = errors.New("member not found")
	ErrBanNotFound    = errors.New("ban not found")
	ErrNicknameLength = errors.New("nickname must be between 1 and 32 characters")
	ErrAlreadyMember  = errors.New("user is already a member")
	ErrAlreadyBanned  = errors.New("user is already banned")
	ErrEveryoneRole   = errors.New("the @everyone role cannot be manually assigned or removed")
	ErrTimeoutInPast  = errors.New("timeout must be in the future")
	ErrNotPending     = errors.New("member is not in pending status")
)

// Pagination defaults.
const (
	DefaultLimit = 50
	MaxLimit     = 100
)

// Member holds the fields read from the members table.
type Member struct {
	UserID       uuid.UUID
	Nickname     *string
	Status       string
	TimeoutUntil *time.Time
	JoinedAt     time.Time
	OnboardedAt  *time.Time
	UpdatedAt    time.Time
}

// MemberWithProfile combines membership fields with public user data and role assignments. Produced by queries that
// join across the members, users, and member_roles tables.
type MemberWithProfile struct {
	UserID       uuid.UUID
	Username     string
	DisplayName  *string
	AvatarKey    *string
	Nickname     *string
	Status       string
	TimeoutUntil *time.Time
	JoinedAt     time.Time
	RoleIDs      []uuid.UUID
}

// ToModel converts the internal member type to the protocol response type.
func (m *MemberWithProfile) ToModel() models.Member {
	roleIDs := make([]string, len(m.RoleIDs))
	for i, id := range m.RoleIDs {
		roleIDs[i] = id.String()
	}
	result := models.Member{
		User: models.MemberUser{
			ID:          m.UserID.String(),
			Username:    m.Username,
			DisplayName: m.DisplayName,
			AvatarKey:   m.AvatarKey,
		},
		Nickname: m.Nickname,
		JoinedAt: m.JoinedAt.Format(time.RFC3339),
		Roles:    roleIDs,
		Status:   m.Status,
	}
	if m.TimeoutUntil != nil {
		s := m.TimeoutUntil.Format(time.RFC3339)
		result.TimeoutUntil = &s
	}
	return result
}

// BanRecord holds a ban row joined with the banned user's public profile.
type BanRecord struct {
	UserID      uuid.UUID
	Username    string
	DisplayName *string
	AvatarKey   *string
	Reason      *string
	BannedBy    *uuid.UUID
	ExpiresAt   *time.Time
	CreatedAt   time.Time
}

// ValidateNickname checks that a non-nil nickname is between 1 and 32 runes after trimming whitespace. A nil pointer
// means "clear the nickname." On success the pointed-to value is replaced with the trimmed result.
func ValidateNickname(nickname *string) error {
	if nickname == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*nickname)
	if utf8.RuneCountInString(trimmed) < 1 || utf8.RuneCountInString(trimmed) > 32 {
		return ErrNicknameLength
	}
	*nickname = trimmed
	return nil
}

// ClampLimit constrains a requested page size to [1, MaxLimit], defaulting to DefaultLimit when the input is zero or
// negative.
func ClampLimit(limit int) int {
	if limit <= 0 {
		return DefaultLimit
	}
	if limit > MaxLimit {
		return MaxLimit
	}
	return limit
}

// Repository defines the data-access contract for member operations.
type Repository interface {
	// Listing
	List(ctx context.Context, after *uuid.UUID, limit int) ([]MemberWithProfile, error)
	GetByUserID(ctx context.Context, userID uuid.UUID) (*MemberWithProfile, error)
	GetByUserIDAnyStatus(ctx context.Context, userID uuid.UUID) (*MemberWithProfile, error)
	GetStatus(ctx context.Context, userID uuid.UUID) (string, error)

	// Mutation
	UpdateNickname(ctx context.Context, userID uuid.UUID, nickname *string) (*MemberWithProfile, error)
	Delete(ctx context.Context, userID uuid.UUID) error

	// Timeout
	SetTimeout(ctx context.Context, userID uuid.UUID, until time.Time) (*MemberWithProfile, error)
	ClearTimeout(ctx context.Context, userID uuid.UUID) (*MemberWithProfile, error)

	// Bans
	Ban(ctx context.Context, userID, bannedBy uuid.UUID, reason *string, expiresAt *time.Time) error
	Unban(ctx context.Context, userID uuid.UUID) error
	ListBans(ctx context.Context, after *uuid.UUID, limit int) ([]BanRecord, error)
	IsBanned(ctx context.Context, userID uuid.UUID) (bool, error)

	// Roles
	AssignRole(ctx context.Context, userID, roleID uuid.UUID) error
	RemoveRole(ctx context.Context, userID, roleID uuid.UUID) error

	// Onboarding
	CreatePending(ctx context.Context, userID uuid.UUID) (*MemberWithProfile, error)
	Activate(ctx context.Context, userID uuid.UUID, autoRoles []uuid.UUID) (*MemberWithProfile, error)
}
