package thread

import (
	"strings"
	"testing"
)

func TestValidateNameRequired(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "valid name", input: "General Discussion", want: "General Discussion"},
		{name: "trims whitespace", input: "  trimmed  ", want: "trimmed"},
		{name: "empty string", input: "", wantErr: true},
		{name: "whitespace only", input: "   ", wantErr: true},
		{name: "exactly 100 runes", input: strings.Repeat("a", 100), want: strings.Repeat("a", 100)},
		{name: "101 runes", input: strings.Repeat("a", 101), wantErr: true},
		{name: "single character", input: "x", want: "x"},
		{name: "unicode characters", input: "日本語スレッド", want: "日本語スレッド"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := ValidateNameRequired(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestValidateName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		input   *string
		want    *string
		wantErr bool
	}{
		{name: "nil means no change", input: nil, want: nil},
		{name: "valid name", input: ptr("Discussion"), want: ptr("Discussion")},
		{name: "trims whitespace", input: ptr("  trimmed  "), want: ptr("trimmed")},
		{name: "empty string", input: ptr(""), wantErr: true},
		{name: "whitespace only", input: ptr("   "), wantErr: true},
		{name: "101 runes", input: ptr(strings.Repeat("a", 101)), wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			// Copy the pointer so parallel tests do not share the same string.
			var input *string
			if tt.input != nil {
				cp := *tt.input
				input = &cp
			}
			err := ValidateName(input)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.want == nil {
				if input != nil {
					t.Errorf("expected nil, got %q", *input)
				}
				return
			}
			if input == nil || *input != *tt.want {
				t.Errorf("got %v, want %q", input, *tt.want)
			}
		})
	}
}

func ptr(s string) *string { return &s }
