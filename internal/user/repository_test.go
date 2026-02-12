package user

import (
	"errors"
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

// wrappedPgError returns a PgError wrapped with fmt.Errorf to test errors.As unwrapping.
func wrappedPgError(code string) error {
	pgErr := &pgconn.PgError{Code: code}
	return errors.Join(errors.New("context"), pgErr)
}
