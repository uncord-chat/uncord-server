package emoji

import (
	"errors"
	"testing"
)

func TestValidateName(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"valid lowercase", "thumbsup", false},
		{"valid uppercase", "ThumbsUp", false},
		{"valid with underscores", "my_cool_emoji", false},
		{"valid with numbers", "emoji123", false},
		{"valid minimum length", "ab", false},
		{"valid 32 chars", "abcdefghijklmnopqrstuvwxyz012345", false},
		{"too short single char", "a", true},
		{"empty string", "", true},
		{"too long 33 chars", "abcdefghijklmnopqrstuvwxyz0123456", true},
		{"invalid spaces", "my emoji", true},
		{"invalid dashes", "my-emoji", true},
		{"invalid dots", "my.emoji", true},
		{"invalid special chars", "emoji@#!", true},
		{"invalid unicode", "émoji", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateName(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateName(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if err != nil && !errors.Is(err, ErrInvalidName) {
				t.Errorf("ValidateName(%q) returned %v, want ErrInvalidName", tt.input, err)
			}
		})
	}
}
