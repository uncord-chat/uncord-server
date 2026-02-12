package auth

import (
	"net/mail"
	"regexp"
	"strings"
)

var usernameRegex = regexp.MustCompile(`^[a-zA-Z0-9_.]+$`)

const maxEmailLength = 254

// ValidateEmail parses and normalizes an email address, returning the normalized form and domain. Returns
// ErrInvalidEmail if the format is invalid or the address exceeds the RFC 5321 maximum of 254 characters.
func ValidateEmail(email string) (normalized, domain string, err error) {
	addr, parseErr := mail.ParseAddress(email)
	if parseErr != nil {
		return "", "", ErrInvalidEmail
	}

	normalized = strings.ToLower(addr.Address)

	if len(normalized) > maxEmailLength {
		return "", "", ErrInvalidEmail
	}

	parts := strings.SplitN(normalized, "@", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", ErrInvalidEmail
	}

	return normalized, parts[1], nil
}

// ValidateUsername checks a username is 2-32 characters and only contains letters, digits, underscores, and periods.
// len() is used intentionally because usernameRegex restricts to ASCII, where byte count equals rune count.
func ValidateUsername(username string) error {
	if len(username) < 2 || len(username) > 32 {
		return ErrUsernameLength
	}
	if !usernameRegex.MatchString(username) {
		return ErrUsernameInvalidChars
	}
	return nil
}

// ValidatePassword checks that a password is between 8 and 128 characters.
func ValidatePassword(password string) error {
	if len(password) < 8 {
		return ErrPasswordTooShort
	}
	if len(password) > 128 {
		return ErrPasswordTooLong
	}
	return nil
}
