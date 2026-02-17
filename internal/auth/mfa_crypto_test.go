package auth

import (
	"strings"
	"testing"
)

const testEncryptionKey = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func TestEncryptDecryptTOTPSecret(t *testing.T) {
	t.Parallel()
	secret := "JBSWY3DPEHPK3PXP"

	encrypted, err := EncryptTOTPSecret(secret, testEncryptionKey)
	if err != nil {
		t.Fatalf("EncryptTOTPSecret() error = %v", err)
	}
	if encrypted == secret {
		t.Error("EncryptTOTPSecret() returned plaintext")
	}

	decrypted, err := DecryptTOTPSecret(encrypted, testEncryptionKey)
	if err != nil {
		t.Fatalf("DecryptTOTPSecret() error = %v", err)
	}
	if decrypted != secret {
		t.Errorf("DecryptTOTPSecret() = %q, want %q", decrypted, secret)
	}
}

func TestDecryptTOTPSecretWrongKey(t *testing.T) {
	t.Parallel()
	secret := "JBSWY3DPEHPK3PXP"
	wrongKey := "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"

	encrypted, err := EncryptTOTPSecret(secret, testEncryptionKey)
	if err != nil {
		t.Fatalf("EncryptTOTPSecret() error = %v", err)
	}

	_, err = DecryptTOTPSecret(encrypted, wrongKey)
	if err == nil {
		t.Error("DecryptTOTPSecret() with wrong key should fail")
	}
}

func TestDecryptTOTPSecretCorruptedData(t *testing.T) {
	t.Parallel()

	_, err := DecryptTOTPSecret("not-valid-base64!!!", testEncryptionKey)
	if err == nil {
		t.Error("DecryptTOTPSecret() with corrupted data should fail")
	}
}

func TestEncryptTOTPSecretInvalidKey(t *testing.T) {
	t.Parallel()

	_, err := EncryptTOTPSecret("secret", "not-hex")
	if err == nil {
		t.Error("EncryptTOTPSecret() with invalid hex key should fail")
	}
}

func TestGenerateRecoveryCodes(t *testing.T) {
	t.Parallel()

	codes, err := GenerateRecoveryCodes()
	if err != nil {
		t.Fatalf("GenerateRecoveryCodes() error = %v", err)
	}
	if len(codes) != recoveryCodeCount {
		t.Fatalf("GenerateRecoveryCodes() returned %d codes, want %d", len(codes), recoveryCodeCount)
	}

	seen := make(map[string]bool)
	for _, code := range codes {
		// Verify format: xxxx-xxxx-xxxx-xxxx-xxxx (20 hex chars with 4 hyphens)
		if len(code) != 24 {
			t.Errorf("code %q has length %d, want 24", code, len(code))
			continue
		}
		for _, pos := range []int{4, 9, 14, 19} {
			if code[pos] != '-' {
				t.Errorf("code %q missing hyphen at position %d", code, pos)
			}
		}

		// Verify all characters are hex (except the hyphens)
		stripped := strings.ReplaceAll(code, "-", "")
		if len(stripped) != 20 {
			t.Errorf("code %q has %d hex characters, want 20", code, len(stripped))
		}
		for _, c := range stripped {
			if !strings.ContainsRune("0123456789abcdef", c) {
				t.Errorf("code %q contains non-hex character %q", code, string(c))
			}
		}

		if seen[code] {
			t.Errorf("duplicate code %q", code)
		}
		seen[code] = true
	}
}

func TestHashVerifyRecoveryCode(t *testing.T) {
	t.Parallel()
	code := "abcd-ef01-2345-6789-abcd"

	hash, err := HashRecoveryCode(code, 64*1024, 1, 1, 16, 32)
	if err != nil {
		t.Fatalf("HashRecoveryCode() error = %v", err)
	}

	match, err := VerifyRecoveryCode(code, hash)
	if err != nil {
		t.Fatalf("VerifyRecoveryCode() error = %v", err)
	}
	if !match {
		t.Error("VerifyRecoveryCode() = false, want true")
	}
}

func TestVerifyRecoveryCodeHyphenStripping(t *testing.T) {
	t.Parallel()
	// Hash with hyphens, verify without; should still match because both strip hyphens before hashing.
	code := "abcd-ef01-2345-6789-abcd"

	hash, err := HashRecoveryCode(code, 64*1024, 1, 1, 16, 32)
	if err != nil {
		t.Fatalf("HashRecoveryCode() error = %v", err)
	}

	match, err := VerifyRecoveryCode("abcdef0123456789abcd", hash)
	if err != nil {
		t.Fatalf("VerifyRecoveryCode() error = %v", err)
	}
	if !match {
		t.Error("VerifyRecoveryCode() without hyphens = false, want true")
	}
}

func TestVerifyRecoveryCodeWrongCode(t *testing.T) {
	t.Parallel()
	code := "abcd-ef01-2345-6789-abcd"

	hash, err := HashRecoveryCode(code, 64*1024, 1, 1, 16, 32)
	if err != nil {
		t.Fatalf("HashRecoveryCode() error = %v", err)
	}

	match, err := VerifyRecoveryCode("0000-0000-0000-0000-0000", hash)
	if err != nil {
		t.Fatalf("VerifyRecoveryCode() error = %v", err)
	}
	if match {
		t.Error("VerifyRecoveryCode() with wrong code = true, want false")
	}
}
