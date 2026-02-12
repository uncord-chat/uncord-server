package auth

import (
	"strings"
	"testing"
)

func TestValidateEmail(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		input      string
		wantNorm   string
		wantDomain string
		wantErr    bool
	}{
		{"valid simple", "user@example.com", "user@example.com", "example.com", false},
		{"valid mixed case", "User@Example.COM", "user@example.com", "example.com", false},
		{"valid with display name", "Test <test@example.com>", "test@example.com", "example.com", false},
		{"valid with plus", "user+tag@example.com", "user+tag@example.com", "example.com", false},
		{"valid subdomain", "user@mail.example.com", "user@mail.example.com", "mail.example.com", false},
		{"invalid empty", "", "", "", true},
		{"invalid no at", "userexample.com", "", "", true},
		{"invalid no domain", "user@", "", "", true},
		{"invalid no local", "@example.com", "", "", true},
		{"invalid spaces", "user @example.com", "", "", true},
		{"too long", strings.Repeat("a", 243) + "@example.com", "", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			norm, domain, err := ValidateEmail(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateEmail(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if norm != tt.wantNorm {
				t.Errorf("ValidateEmail(%q) normalized = %q, want %q", tt.input, norm, tt.wantNorm)
			}
			if domain != tt.wantDomain {
				t.Errorf("ValidateEmail(%q) domain = %q, want %q", tt.input, domain, tt.wantDomain)
			}
		})
	}
}

func TestValidateUsername(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		wantErr bool
		errMsg  string
	}{
		{"valid simple", "alice", false, ""},
		{"valid with underscore", "alice_bob", false, ""},
		{"valid with period", "alice.bob", false, ""},
		{"valid with digits", "alice123", false, ""},
		{"valid min length", "ab", false, ""},
		{"valid 32 chars", strings.Repeat("a", 32), false, ""},
		{"too short", "a", true, "between 2 and 32"},
		{"too long", strings.Repeat("a", 33), true, "between 2 and 32"},
		{"invalid space", "alice bob", true, "letters, digits"},
		{"invalid special", "alice@bob", true, "letters, digits"},
		{"invalid dash", "alice-bob", true, "letters, digits"},
		{"empty", "", true, "between 2 and 32"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := ValidateUsername(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateUsername(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if tt.wantErr && err != nil && tt.errMsg != "" {
				if !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("ValidateUsername(%q) error = %q, want to contain %q", tt.input, err.Error(), tt.errMsg)
				}
			}
		})
	}
}

func TestValidatePassword(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		wantErr bool
		errMsg  string
	}{
		{"valid 8 chars", "12345678", false, ""},
		{"valid 128 chars", strings.Repeat("a", 128), false, ""},
		{"valid normal", "mySecurePassword123!", false, ""},
		{"too short", "1234567", true, "at least 8"},
		{"too long", strings.Repeat("a", 129), true, "at most 128"},
		{"empty", "", true, "at least 8"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := ValidatePassword(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidatePassword(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if tt.wantErr && err != nil && tt.errMsg != "" {
				if !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("ValidatePassword(%q) error = %q, want to contain %q", tt.input, err.Error(), tt.errMsg)
				}
			}
		})
	}
}
