package auth

import "testing"

// testHMACKey is a valid 64 hex character (32 byte) key used across HMAC tests.
const testHMACKey = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func TestHMACIdentifierDeterministic(t *testing.T) {
	t.Parallel()
	h1, err := HMACIdentifier("alice@example.com", testHMACKey)
	if err != nil {
		t.Fatalf("HMACIdentifier() error = %v", err)
	}
	h2, err := HMACIdentifier("alice@example.com", testHMACKey)
	if err != nil {
		t.Fatalf("HMACIdentifier() error = %v", err)
	}
	if h1 != h2 {
		t.Errorf("HMACIdentifier() not deterministic: %q != %q", h1, h2)
	}
	if h1 == "" {
		t.Error("HMACIdentifier() returned empty string")
	}
}

func TestHMACIdentifierDistinctInputs(t *testing.T) {
	t.Parallel()
	h1, err := HMACIdentifier("alice@example.com", testHMACKey)
	if err != nil {
		t.Fatalf("HMACIdentifier() error = %v", err)
	}
	h2, err := HMACIdentifier("bob@example.com", testHMACKey)
	if err != nil {
		t.Fatalf("HMACIdentifier() error = %v", err)
	}
	if h1 == h2 {
		t.Error("HMACIdentifier() produced identical hashes for distinct inputs")
	}
}

func TestHMACIdentifierDistinctKeys(t *testing.T) {
	t.Parallel()
	key2 := "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"

	h1, err := HMACIdentifier("alice@example.com", testHMACKey)
	if err != nil {
		t.Fatalf("HMACIdentifier() error = %v", err)
	}
	h2, err := HMACIdentifier("alice@example.com", key2)
	if err != nil {
		t.Fatalf("HMACIdentifier() error = %v", err)
	}
	if h1 == h2 {
		t.Error("HMACIdentifier() produced identical hashes for distinct keys")
	}
}

func TestHMACIdentifierInvalidKey(t *testing.T) {
	t.Parallel()
	_, err := HMACIdentifier("alice@example.com", "not-hex")
	if err == nil {
		t.Error("HMACIdentifier() with invalid hex key should return error")
	}
}
