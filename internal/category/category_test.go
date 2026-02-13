package category

import (
	"strings"
	"testing"
)

func TestValidateName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   *string
		wantErr bool
	}{
		{"nil", nil, false},
		{"empty after trim", ptr("   "), true},
		{"one char", ptr("A"), false},
		{"100 chars", ptr(strings.Repeat("a", 100)), false},
		{"101 chars", ptr(strings.Repeat("a", 101)), true},
		{"whitespace padded valid", ptr("  hello  "), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateName(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateName(%v) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if err != nil && tt.wantErr && err != ErrNameLength {
				t.Errorf("ValidateName(%v) error = %v, want ErrNameLength", tt.input, err)
			}
		})
	}

	t.Run("trims whitespace in place", func(t *testing.T) {
		t.Parallel()
		name := ptr("  hello  ")
		if err := ValidateName(name); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if *name != "hello" {
			t.Errorf("expected trimmed value %q, got %q", "hello", *name)
		}
	})
}

func TestValidateNameRequired(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{"empty", "", "", true},
		{"whitespace only", "   ", "", true},
		{"valid", "General", "General", false},
		{"padded", "  General  ", "General", false},
		{"100 chars", strings.Repeat("a", 100), strings.Repeat("a", 100), false},
		{"101 chars", strings.Repeat("a", 101), "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := ValidateNameRequired(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateNameRequired(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("ValidateNameRequired(%q) = %q, want %q", tt.input, got, tt.want)
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
		{"nil", nil, false},
		{"zero", intPtr(0), false},
		{"positive", intPtr(5), false},
		{"negative", intPtr(-1), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidatePosition(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidatePosition(%v) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if err != nil && tt.wantErr && err != ErrInvalidPosition {
				t.Errorf("ValidatePosition(%v) error = %v, want ErrInvalidPosition", tt.input, err)
			}
		})
	}
}

func ptr(s string) *string { return &s }
func intPtr(n int) *int    { return &n }
