package message

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateContent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		input     string
		maxLength int
		want      string
		wantErr   error
	}{
		{"valid simple", "hello world", 2000, "hello world", nil},
		{"trims whitespace", "  hello  ", 2000, "hello", nil},
		{"exact max length", strings.Repeat("a", 100), 100, strings.Repeat("a", 100), nil},
		{"multibyte at limit", strings.Repeat("日", 50), 50, strings.Repeat("日", 50), nil},
		{"empty after trim", "   ", 2000, "", ErrEmptyContent},
		{"empty string", "", 2000, "", ErrEmptyContent},
		{"exceeds max length", strings.Repeat("a", 101), 100, "", ErrContentTooLong},
		{"multibyte exceeds max", strings.Repeat("日", 51), 50, "", ErrContentTooLong},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := ValidateContent(tt.input, tt.maxLength)
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("ValidateContent(%q, %d) error = %v, wantErr %v", tt.input, tt.maxLength, err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("ValidateContent(%q, %d) = %q, want %q", tt.input, tt.maxLength, got, tt.want)
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
		{"negative defaults", -1, DefaultLimit},
		{"within range", 25, 25},
		{"at minimum boundary", 1, 1},
		{"at maximum boundary", MaxLimit, MaxLimit},
		{"exceeds maximum", MaxLimit + 1, MaxLimit},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := ClampLimit(tt.input); got != tt.want {
				t.Errorf("ClampLimit(%d) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}
