package user

import (
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestIsUniqueViolation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "unique violation",
			err:  &pgconn.PgError{Code: "23505"},
			want: true,
		},
		{
			name: "other pg error",
			err:  &pgconn.PgError{Code: "23503"},
			want: false,
		},
		{
			name: "non-pg error",
			err:  errors.New("generic error"),
			want: false,
		},
		{
			name: "nil error",
			err:  nil,
			want: false,
		},
		{
			name: "wrapped unique violation",
			err:  wrappedPgError("23505"),
			want: true,
		},
		{
			name: "wrapped other pg error",
			err:  wrappedPgError("42601"),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := isUniqueViolation(tt.err); got != tt.want {
				t.Errorf("isUniqueViolation() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSentinelErrors(t *testing.T) {
	t.Parallel()

	// Verify sentinel errors are distinct and usable with errors.Is.
	sentinels := []struct {
		name string
		err  error
	}{
		{"ErrNotFound", ErrNotFound},
		{"ErrAlreadyExists", ErrAlreadyExists},
		{"ErrInvalidToken", ErrInvalidToken},
		{"ErrDisplayNameLength", ErrDisplayNameLength},
	}

	for i, a := range sentinels {
		for j, b := range sentinels {
			if i == j {
				if !errors.Is(a.err, b.err) {
					t.Errorf("errors.Is(%s, %s) = false, want true", a.name, b.name)
				}
			} else {
				if errors.Is(a.err, b.err) {
					t.Errorf("errors.Is(%s, %s) = true, want false", a.name, b.name)
				}
			}
		}
	}
}

func TestCreateParamsZeroValue(t *testing.T) {
	t.Parallel()

	var p CreateParams
	if p.Email != "" || p.Username != "" || p.PasswordHash != "" || p.VerifyToken != "" {
		t.Error("CreateParams zero value should have empty strings")
	}
	if !p.VerifyExpiry.IsZero() {
		t.Error("CreateParams zero value should have zero time")
	}
}

func TestNormalizeDisplayName(t *testing.T) {
	t.Parallel()

	t.Run("nil is a no-op", func(t *testing.T) {
		t.Parallel()
		NormalizeDisplayName(nil) // must not panic
	})

	t.Run("trims surrounding whitespace", func(t *testing.T) {
		t.Parallel()
		name := ptr("  Bob  ")
		NormalizeDisplayName(name)
		if *name != "Bob" {
			t.Errorf("expected trimmed value %q, got %q", "Bob", *name)
		}
	})

	t.Run("leaves clean value unchanged", func(t *testing.T) {
		t.Parallel()
		name := ptr("Alice")
		NormalizeDisplayName(name)
		if *name != "Alice" {
			t.Errorf("expected %q, got %q", "Alice", *name)
		}
	})
}

func TestValidateDisplayName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   *string
		wantErr bool
	}{
		{"nil is valid", nil, false},
		{"single char", ptr("A"), false},
		{"32 chars", ptr(strings.Repeat("a", 32)), false},
		{"33 chars", ptr(strings.Repeat("a", 33)), true},
		{"empty string", ptr(""), true},
		{"32 multibyte runes", ptr(strings.Repeat("🎮", 32)), false},
		{"33 multibyte runes", ptr(strings.Repeat("🎮", 33)), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateDisplayName(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateDisplayName() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil && !errors.Is(err, ErrDisplayNameLength) {
				t.Errorf("ValidateDisplayName() error = %v, want ErrDisplayNameLength", err)
			}
		})
	}
}

func TestNormalizeAndValidateDisplayName(t *testing.T) {
	t.Parallel()

	t.Run("whitespace only rejects after trim", func(t *testing.T) {
		t.Parallel()
		name := ptr("   ")
		NormalizeDisplayName(name)
		if err := ValidateDisplayName(name); !errors.Is(err, ErrDisplayNameLength) {
			t.Errorf("expected ErrDisplayNameLength after trimming whitespace-only input, got %v", err)
		}
	})

	t.Run("padded value passes after trim", func(t *testing.T) {
		t.Parallel()
		name := ptr("  Bob  ")
		NormalizeDisplayName(name)
		if err := ValidateDisplayName(name); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if *name != "Bob" {
			t.Errorf("expected %q, got %q", "Bob", *name)
		}
	})
}

func ptr(s string) *string { return &s }

// wrappedPgError returns a PgError wrapped with fmt.Errorf to test errors.As unwrapping.
func wrappedPgError(code string) error {
	pgErr := &pgconn.PgError{Code: code}
	return errors.Join(errors.New("context"), pgErr)
}
