package config

// Secret wraps a sensitive string value and redacts it during logging and serialisation. Call Expose to retrieve the
// underlying plaintext when the raw value is genuinely needed (e.g., passing to a cryptographic function).
type Secret struct {
	value string
}

// NewSecret creates a Secret from a plaintext string.
func NewSecret(value string) Secret { return Secret{value: value} }

// Expose returns the underlying plaintext. Use only when the raw value is required.
func (s Secret) Expose() string { return s.value }

// IsSet returns true when the secret holds a non-empty value.
func (s Secret) IsSet() bool { return s.value != "" }

const redacted = "[REDACTED]"

func (s Secret) String() string               { return redacted }
func (s Secret) GoString() string             { return redacted }
func (s Secret) MarshalText() ([]byte, error) { return []byte(redacted), nil }
func (s Secret) MarshalJSON() ([]byte, error) { return []byte(`"` + redacted + `"`), nil }
