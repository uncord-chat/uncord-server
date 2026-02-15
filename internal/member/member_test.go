package member

import (
	"strings"
	"testing"
)

func TestValidateNickname(t *testing.T) {
	t.Parallel()

	ptr := func(s string) *string { return &s }

	tests := []struct {
		name    string
		input   *string
		wantErr bool
		want    string
	}{
		{"nil clears nickname", nil, false, ""},
		{"valid nickname", ptr("alice"), false, "alice"},
		{"single character", ptr("a"), false, "a"},
		{"max 32 characters", ptr(strings.Repeat("a", 32)), false, strings.Repeat("a", 32)},
		{"exceeds 32 characters", ptr(strings.Repeat("a", 33)), true, ""},
		{"empty string", ptr(""), true, ""},
		{"whitespace only", ptr("   "), true, ""},
		{"trims whitespace", ptr("  bob  "), false, "bob"},
		{"multibyte runes within limit", ptr(strings.Repeat("\u00e9", 32)), false, strings.Repeat("\u00e9", 32)},
		{"multibyte runes exceeding limit", ptr(strings.Repeat("\u00e9", 33)), true, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			// Copy the pointer so parallel subtests do not share state.
			var input *string
			if tt.input != nil {
				s := *tt.input
				input = &s
			}

			err := ValidateNickname(input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateNickname() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && input != nil && *input != tt.want {
				t.Errorf("ValidateNickname() trimmed = %q, want %q", *input, tt.want)
			}
		})
	}
}

func TestClampLimit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input int
		want  int
	}{
		{"zero defaults", 0, DefaultLimit},
		{"negative defaults", -5, DefaultLimit},
		{"within range", 25, 25},
		{"at max", MaxLimit, MaxLimit},
		{"exceeds max", MaxLimit + 1, MaxLimit},
		{"one", 1, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := ClampLimit(tt.input)
			if got != tt.want {
				t.Errorf("ClampLimit(%d) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}
