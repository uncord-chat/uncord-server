package user

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/uncord-chat/uncord-protocol/models"
)

// Sentinel errors for the user package.
var (
	ErrNotFound          = errors.New("user not found")
	ErrAlreadyExists     = errors.New("email or username already taken")
	ErrInvalidToken      = errors.New("invalid or expired verification token")
	ErrTombstoned        = errors.New("email or username was previously used by a deleted account")
	ErrDisplayNameLength = errors.New("display name must be between 1 and 32 characters")
	ErrPronounsLength    = errors.New("pronouns must be between 1 and 40 characters")
	ErrAboutLength       = errors.New("about must be between 1 and 190 characters")
	ErrThemeColourRange  = errors.New("theme colour must be between 0 and 16777215")
)

// User holds the core identity fields read from the database.
type User struct {
	ID                   uuid.UUID
	Email                string
	Username             string
	DisplayName          *string
	AvatarKey            *string
	Pronouns             *string
	BannerKey            *string
	About                *string
	ThemeColourPrimary   *int
	ThemeColourSecondary *int
	MFAEnabled           bool
	EmailVerified        bool
	CreatedAt            time.Time
}

// ToModel converts the internal user struct to the protocol response type. This is the single source of truth for the
// conversion; HTTP handlers and the gateway both call this method rather than maintaining their own copies.
func (u *User) ToModel() models.User {
	return models.User{
		ID:                   u.ID.String(),
		Email:                u.Email,
		Username:             u.Username,
		DisplayName:          u.DisplayName,
		AvatarKey:            u.AvatarKey,
		Pronouns:             u.Pronouns,
		BannerKey:            u.BannerKey,
		About:                u.About,
		ThemeColourPrimary:   u.ThemeColourPrimary,
		ThemeColourSecondary: u.ThemeColourSecondary,
		MFAEnabled:           u.MFAEnabled,
		EmailVerified:        u.EmailVerified,
	}
}

// Credentials extends User with the password hash and optional MFA secret. Only repository methods that serve the
// authentication path return this type; all other read methods return *User to prevent credential leakage at the type
// level.
type Credentials struct {
	User
	PasswordHash string
	MFASecret    *string
}

// MFARecoveryCode represents a single unused recovery code stored in the database.
type MFARecoveryCode struct {
	ID       uuid.UUID
	CodeHash string
}

// CreateParams groups the inputs for creating a new user. When VerifyToken is non-empty, an email_verifications row is
// inserted in the same transaction.
type CreateParams struct {
	Email        string
	Username     string
	PasswordHash string
	VerifyToken  string
	VerifyExpiry time.Time
}

// UpdateParams groups the optional fields for updating a user profile.
type UpdateParams struct {
	DisplayName          *string
	AvatarKey            *string
	Pronouns             *string
	BannerKey            *string
	About                *string
	ThemeColourPrimary   *int
	ThemeColourSecondary *int
}

// TombstoneType identifies the kind of identifier stored in a deletion tombstone.
type TombstoneType string

const (
	TombstoneEmail    TombstoneType = "email"
	TombstoneUsername TombstoneType = "username"
)

// Tombstone represents an HMAC hash of an identifier that belonged to a deleted account, used to prevent
// re-registration with the same email or username.
type Tombstone struct {
	IdentifierType TombstoneType
	HMACHash       string
}

// NormalizeDisplayName trims surrounding whitespace from the pointed-to value. Nil values are left untouched.
func NormalizeDisplayName(name *string) {
	if name == nil {
		return
	}
	*name = strings.TrimSpace(*name)
}

// ValidateDisplayName checks that a non-nil display name is between 1 and 32 Unicode characters.
func ValidateDisplayName(name *string) error {
	if name == nil {
		return nil
	}
	if n := utf8.RuneCountInString(*name); n < 1 || n > 32 {
		return ErrDisplayNameLength
	}
	return nil
}

// NormalizePronouns trims surrounding whitespace from the pointed-to value. Nil values are left untouched.
func NormalizePronouns(p *string) {
	if p == nil {
		return
	}
	*p = strings.TrimSpace(*p)
}

// ValidatePronouns checks that a non-nil pronouns string is between 1 and 40 Unicode characters.
func ValidatePronouns(p *string) error {
	if p == nil {
		return nil
	}
	if n := utf8.RuneCountInString(*p); n < 1 || n > 40 {
		return ErrPronounsLength
	}
	return nil
}

// NormalizeAbout trims surrounding whitespace from the pointed-to value. Nil values are left untouched.
func NormalizeAbout(a *string) {
	if a == nil {
		return
	}
	*a = strings.TrimSpace(*a)
}

// ValidateAbout checks that a non-nil about string is between 1 and 190 Unicode characters.
func ValidateAbout(a *string) error {
	if a == nil {
		return nil
	}
	if n := utf8.RuneCountInString(*a); n < 1 || n > 190 {
		return ErrAboutLength
	}
	return nil
}

// ValidateThemeColour checks that a non-nil theme colour is within the 24-bit RGB range (0 to 16777215).
func ValidateThemeColour(colour *int) error {
	if colour == nil {
		return nil
	}
	if *colour < 0 || *colour > 0xFFFFFF {
		return ErrThemeColourRange
	}
	return nil
}

// Repository defines the data-access contract for user operations.
type Repository interface {
	Create(ctx context.Context, params CreateParams) (uuid.UUID, error)
	GetByID(ctx context.Context, id uuid.UUID) (*User, error)
	GetByEmail(ctx context.Context, email string) (*Credentials, error)
	GetCredentialsByID(ctx context.Context, id uuid.UUID) (*Credentials, error)
	VerifyEmail(ctx context.Context, token string) (uuid.UUID, error)
	RecordLoginAttempt(ctx context.Context, email, ipAddress string, success bool) error
	UpdatePasswordHash(ctx context.Context, userID uuid.UUID, hash string) error
	Update(ctx context.Context, id uuid.UUID, params UpdateParams) (*User, error)
	EnableMFA(ctx context.Context, userID uuid.UUID, encryptedSecret string, codeHashes []string) error
	DisableMFA(ctx context.Context, userID uuid.UUID) error
	GetUnusedRecoveryCodes(ctx context.Context, userID uuid.UUID) ([]MFARecoveryCode, error)
	UseRecoveryCode(ctx context.Context, codeID uuid.UUID) error
	ReplaceRecoveryCodes(ctx context.Context, userID uuid.UUID, codeHashes []string) error
	DeleteWithTombstones(ctx context.Context, id uuid.UUID, tombstones []Tombstone) error
	CheckTombstone(ctx context.Context, identifierType TombstoneType, hmacHash string) (bool, error)
}
