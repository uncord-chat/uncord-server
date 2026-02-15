package role

import (
	"errors"
	"strings"
	"testing"

	"github.com/uncord-chat/uncord-protocol/permissions"
)

func TestSentinelErrors(t *testing.T) {
	t.Parallel()

	// Verify sentinel errors are distinct and usable with errors.Is.
	sentinels := []struct {
		name string
		err  error
	}{
		{"ErrNotFound", ErrNotFound},
		{"ErrAlreadyExists", ErrAlreadyExists},
		{"ErrNameLength", ErrNameLength},
		{"ErrInvalidPosition", ErrInvalidPosition},
		{"ErrInvalidPermissions", ErrInvalidPermissions},
		{"ErrInvalidColour", ErrInvalidColour},
		{"ErrMaxRolesReached", ErrMaxRolesReached},
		{"ErrEveryoneImmutable", ErrEveryoneImmutable},
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

func TestValidateNameRequired(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{"valid name", "Moderator", "Moderator", false},
		{"trims whitespace", "  Admin  ", "Admin", false},
		{"single char", "X", "X", false},
		{"100 chars", strings.Repeat("a", 100), strings.Repeat("a", 100), false},
		{"101 chars", strings.Repeat("a", 101), "", true},
		{"empty string", "", "", true},
		{"whitespace only", "   ", "", true},
		{"100 multibyte runes", strings.Repeat("中", 100), strings.Repeat("中", 100), false},
		{"101 multibyte runes", strings.Repeat("中", 101), "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := ValidateNameRequired(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateNameRequired(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if err != nil && !errors.Is(err, ErrNameLength) {
				t.Errorf("ValidateNameRequired(%q) error = %v, want ErrNameLength", tt.input, err)
			}
			if got != tt.want {
				t.Errorf("ValidateNameRequired(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestValidateName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   *string
		want    string
		wantErr bool
	}{
		{"nil is valid", nil, "", false},
		{"valid name", ptr("Moderator"), "Moderator", false},
		{"trims whitespace", ptr("  Admin  "), "Admin", false},
		{"single char", ptr("X"), "X", false},
		{"100 chars", ptr(strings.Repeat("a", 100)), strings.Repeat("a", 100), false},
		{"101 chars", ptr(strings.Repeat("a", 101)), "", true},
		{"empty string", ptr(""), "", true},
		{"whitespace only", ptr("   "), "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			// Copy the pointer so parallel subtests do not share state.
			var input *string
			if tt.input != nil {
				cp := *tt.input
				input = &cp
			}
			err := ValidateName(input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateName() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil && !errors.Is(err, ErrNameLength) {
				t.Errorf("ValidateName() error = %v, want ErrNameLength", err)
			}
			if err == nil && input != nil && *input != tt.want {
				t.Errorf("ValidateName() mutated value = %q, want %q", *input, tt.want)
			}
		})
	}
}

func TestValidatePosition(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   *int
		wantErr bool
	}{
		{"nil is valid", nil, false},
		{"zero", intPtr(0), false},
		{"positive", intPtr(42), false},
		{"large positive", intPtr(999999), false},
		{"negative one", intPtr(-1), true},
		{"large negative", intPtr(-100), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidatePosition(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidatePosition() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil && !errors.Is(err, ErrInvalidPosition) {
				t.Errorf("ValidatePosition() error = %v, want ErrInvalidPosition", err)
			}
		})
	}
}

func TestValidatePermissions(t *testing.T) {
	t.Parallel()

	all := int64(permissions.AllPermissions)

	tests := []struct {
		name    string
		input   *int64
		wantErr bool
	}{
		{"nil is valid", nil, false},
		{"zero", int64Ptr(0), false},
		{"all permissions", int64Ptr(all), false},
		{"single valid bit", int64Ptr(int64(permissions.ViewChannels)), false},
		{"combined valid bits", int64Ptr(int64(permissions.ViewChannels | permissions.SendMessages)), false},
		{"bit above all permissions", int64Ptr(all + 1), true},
		{"high invalid bit", int64Ptr(1 << 50), true},
		{"negative", int64Ptr(-1), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidatePermissions(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidatePermissions() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil && !errors.Is(err, ErrInvalidPermissions) {
				t.Errorf("ValidatePermissions() error = %v, want ErrInvalidPermissions", err)
			}
		})
	}
}

func TestValidateColour(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   *int
		wantErr bool
	}{
		{"nil is valid", nil, false},
		{"zero", intPtr(0), false},
		{"max RGB", intPtr(0xFFFFFF), false},
		{"mid range", intPtr(0x7F7F7F), false},
		{"one over max", intPtr(0xFFFFFF + 1), true},
		{"negative", intPtr(-1), true},
		{"large negative", intPtr(-999999), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateColour(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateColour() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil && !errors.Is(err, ErrInvalidColour) {
				t.Errorf("ValidateColour() error = %v, want ErrInvalidColour", err)
			}
		})
	}
}

func ptr(s string) *string    { return &s }
func intPtr(i int) *int       { return &i }
func int64Ptr(i int64) *int64 { return &i }
