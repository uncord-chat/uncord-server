package channel

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
		{"empty after trim", ptr("   "), true},
		{"one char", ptr("A"), false},
		{"100 chars", ptr(strings.Repeat("a", 100)), false},
		{"101 chars", ptr(strings.Repeat("a", 101)), true},
		{"whitespace padded valid", ptr("  general  "), false},
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
		name := ptr("  general  ")
		if err := ValidateName(name); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if *name != "general" {
			t.Errorf("expected trimmed value %q, got %q", "general", *name)
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
		{"valid", "general", "general", false},
		{"padded", "  general  ", "general", false},
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

func TestValidateType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"text", "text", false},
		{"voice", "voice", false},
		{"announcement", "announcement", false},
		{"forum", "forum", false},
		{"stage", "stage", false},
		{"invalid", "video", true},
		{"empty", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateType(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateType(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if err != nil && tt.wantErr && !errors.Is(err, ErrInvalidType) {
				t.Errorf("ValidateType(%q) error = %v, want ErrInvalidType", tt.input, err)
			}
		})
	}
}

func TestValidateTopic(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   *string
		wantErr bool
	}{
		{"nil", nil, false},
		{"empty", ptr(""), false},
		{"1024 chars", ptr(strings.Repeat("a", 1024)), false},
		{"1025 chars", ptr(strings.Repeat("a", 1025)), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateTopic(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateTopic(%v) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if err != nil && tt.wantErr && !errors.Is(err, ErrTopicLength) {
				t.Errorf("ValidateTopic(%v) error = %v, want ErrTopicLength", tt.input, err)
			}
		})
	}
}

func TestValidateSlowmode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   *int
		wantErr bool
	}{
		{"nil", nil, false},
		{"zero", intPtr(0), false},
		{"max", intPtr(21600), false},
		{"over max", intPtr(21601), true},
		{"negative", intPtr(-1), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateSlowmode(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateSlowmode(%v) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if err != nil && tt.wantErr && !errors.Is(err, ErrInvalidSlowmode) {
				t.Errorf("ValidateSlowmode(%v) error = %v, want ErrInvalidSlowmode", tt.input, err)
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
			if err != nil && tt.wantErr && !errors.Is(err, ErrInvalidPosition) {
				t.Errorf("ValidatePosition(%v) error = %v, want ErrInvalidPosition", tt.input, err)
			}
		})
	}
}

func ptr(s string) *string { return &s }
func intPtr(n int) *int    { return &n }
