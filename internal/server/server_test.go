package server

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
}

func TestValidateDescription(t *testing.T) {
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
			err := ValidateDescription(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateDescription(%v) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if err != nil && tt.wantErr && err != ErrDescriptionLength {
				t.Errorf("ValidateDescription(%v) error = %v, want ErrDescriptionLength", tt.input, err)
			}
		})
	}
}

func ptr(s string) *string { return &s }
