package config

import (
	"encoding/json"
	"fmt"
	"testing"
)

func TestSecretExpose(t *testing.T) {
	t.Parallel()
	s := NewSecret("hunter2")
	if got := s.Expose(); got != "hunter2" {
		t.Errorf("Expose() = %q, want %q", got, "hunter2")
	}
}

func TestSecretIsSet(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		value string
		want  bool
	}{
		{"non-empty", "abc", true},
		{"empty string", "", false},
		{"zero value", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := NewSecret(tt.value)
			if got := s.IsSet(); got != tt.want {
				t.Errorf("IsSet() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSecretZeroValueIsSet(t *testing.T) {
	t.Parallel()
	var s Secret
	if s.IsSet() {
		t.Error("IsSet() on zero-value Secret = true, want false")
	}
}

func TestSecretStringRedaction(t *testing.T) {
	t.Parallel()
	s := NewSecret("supersecret")

	tests := []struct {
		name   string
		format string
		want   string
	}{
		{"String()", "%s", "[REDACTED]"},
		{"%v", "%v", "[REDACTED]"},
		{"%+v", "%+v", "[REDACTED]"},
		{"GoString()", "%#v", "[REDACTED]"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := fmt.Sprintf(tt.format, s); got != tt.want {
				t.Errorf("Sprintf(%q) = %q, want %q", tt.format, got, tt.want)
			}
		})
	}
}

func TestSecretMarshalText(t *testing.T) {
	t.Parallel()
	s := NewSecret("supersecret")
	b, err := s.MarshalText()
	if err != nil {
		t.Fatalf("MarshalText() error = %v", err)
	}
	if got := string(b); got != "[REDACTED]" {
		t.Errorf("MarshalText() = %q, want %q", got, "[REDACTED]")
	}
}

func TestSecretMarshalJSON(t *testing.T) {
	t.Parallel()
	s := NewSecret("supersecret")
	b, err := s.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON() error = %v", err)
	}
	if got := string(b); got != `"[REDACTED]"` {
		t.Errorf("MarshalJSON() = %q, want %q", got, `"[REDACTED]"`)
	}
}

func TestSecretJSONMarshalInStruct(t *testing.T) {
	t.Parallel()
	type wrapper struct {
		Key Secret `json:"key"`
	}
	w := wrapper{Key: NewSecret("supersecret")}
	b, err := json.Marshal(w)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	want := `{"key":"[REDACTED]"}`
	if got := string(b); got != want {
		t.Errorf("json.Marshal() = %s, want %s", got, want)
	}
}
