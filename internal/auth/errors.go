package auth

import "errors"

// Sentinel errors for the auth package.
var (
	// ErrRefreshTokenReused is returned when a consumed refresh token is presented again, indicating potential token
	// theft.
	ErrRefreshTokenReused   = errors.New("refresh token reused")
	ErrInvalidEmail         = errors.New("invalid email format")
	ErrUsernameLength       = errors.New("username must be between 2 and 32 characters")
	ErrUsernameInvalidChars = errors.New("username may only contain letters, digits, underscores, and periods")
	ErrPasswordTooShort     = errors.New("password must be at least 8 characters")
	ErrPasswordTooLong      = errors.New("password must be at most 128 characters")
	ErrInvalidCredentials   = errors.New("invalid email or password")
	ErrDisposableEmail      = errors.New("disposable email addresses are not allowed")
	ErrMFARequired          = errors.New("multi-factor authentication is required")
	ErrEmailAlreadyTaken    = errors.New("email or username already taken")
	ErrInvalidToken         = errors.New("invalid or expired token")
	ErrRefreshTokenNotFound = errors.New("refresh token not found")
	ErrInvalidMFACode       = errors.New("invalid MFA code")
	ErrMFANotEnabled        = errors.New("MFA is not enabled on this account")
	ErrMFAAlreadyEnabled    = errors.New("MFA is already enabled on this account")
	ErrMFANotConfigured     = errors.New("MFA is not configured on this server")
	ErrServerOwner          = errors.New("the server owner cannot delete their account")
	ErrAccountTombstoned    = errors.New("email or username was previously used by a deleted account")
)
