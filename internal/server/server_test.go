package server

import (
	"errors"
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
		{"empty after trim", new("   "), true},
		{"one char", new("A"), false},
		{"100 chars", new(strings.Repeat("a", 100)), false},
		{"101 chars", new(strings.Repeat("a", 101)), true},
		{"whitespace padded valid", new("  hello  "), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateName(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateName(%v) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if err != nil && tt.wantErr && !errors.Is(err, ErrNameLength) {
				t.Errorf("ValidateName(%v) error = %v, want ErrNameLength", tt.input, err)
			}
		})
	}

	t.Run("trims whitespace in place", func(t *testing.T) {
		t.Parallel()
		name := new("  hello  ")
		if err := ValidateName(name); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if *name != "hello" {
			t.Errorf("expected trimmed value %q, got %q", "hello", *name)
		}
	})
}

func TestValidateDescription(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   *string
		wantErr bool
	}{
		{"nil", nil, false},
		{"empty", new(""), false},
		{"1024 chars", new(strings.Repeat("a", 1024)), false},
		{"1025 chars", new(strings.Repeat("a", 1025)), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateDescription(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateDescription(%v) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if err != nil && tt.wantErr && !errors.Is(err, ErrDescriptionLength) {
				t.Errorf("ValidateDescription(%v) error = %v, want ErrDescriptionLength", tt.input, err)
			}
		})
	}
}
