package auth

import "errors"

// Token errors.
var (
	// ErrRefreshTokenReused is returned when a consumed refresh token is
	// presented again, indicating potential token theft.
	ErrRefreshTokenReused = errors.New("refresh token reused")
)

// Validation errors returned by ValidateEmail, ValidateUsername, and
// ValidatePassword. These are sentinel values so callers can match with
// errors.Is and the handler can map them to HTTP responses independently.
var (
	ErrInvalidEmail         = errors.New("invalid email format")
	ErrUsernameLength       = errors.New("username must be between 2 and 32 characters")
	ErrUsernameInvalidChars = errors.New("username may only contain letters, digits, underscores, and periods")
	ErrPasswordTooShort     = errors.New("password must be at least 8 characters")
	ErrPasswordTooLong      = errors.New("password must be at most 128 characters")
)

// Auth flow errors returned by the Service layer.
var (
	ErrInvalidCredentials   = errors.New("invalid email or password")
	ErrDisposableEmail      = errors.New("disposable email addresses are not allowed")
	ErrMFARequired          = errors.New("multi-factor authentication is required")
	ErrEmailAlreadyTaken    = errors.New("email or username already taken")
	ErrInvalidToken         = errors.New("invalid or expired token")
	ErrRefreshTokenNotFound = errors.New("refresh token not found")
)
