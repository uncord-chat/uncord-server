package auth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
)

// EncryptTOTPSecret encrypts a TOTP secret using AES-256-GCM. The hexKey must be exactly 64 hex characters (32 bytes).
// The returned string is base64(nonce || ciphertext || tag).
func EncryptTOTPSecret(secret, hexKey string) (string, error) {
	key, err := hex.DecodeString(hexKey)
	if err != nil {
		return "", fmt.Errorf("decode encryption key: %w", err)
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("create GCM: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize())
	_, _ = rand.Read(nonce)

	ciphertext := gcm.Seal(nonce, nonce, []byte(secret), nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// DecryptTOTPSecret decrypts a TOTP secret that was encrypted by EncryptTOTPSecret.
func DecryptTOTPSecret(encoded, hexKey string) (string, error) {
	key, err := hex.DecodeString(hexKey)
	if err != nil {
		return "", fmt.Errorf("decode encryption key: %w", err)
	}

	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("decode ciphertext: %w", err)
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("create GCM: %w", err)
	}

	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return "", fmt.Errorf("ciphertext too short")
	}

	nonce, ciphertext := data[:nonceSize], data[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("decrypt: %w", err)
	}

	return string(plaintext), nil
}

const recoveryCodeCount = 10

// GenerateRecoveryCodes generates a set of recovery codes in the format "xxxx-xxxx-xxxx-xxxx-xxxx" where each group is
// 4 hex characters. Each code represents 10 random bytes (80 bits of entropy).
func GenerateRecoveryCodes() []string {
	codes := make([]string, recoveryCodeCount)
	for i := range codes {
		b := make([]byte, 10)
		_, _ = rand.Read(b)
		h := hex.EncodeToString(b)
		codes[i] = h[:4] + "-" + h[4:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:]
	}
	return codes
}

// HashRecoveryCode hashes a recovery code using the same Argon2id parameters as passwords. The hyphen is stripped
// before hashing so that codes entered with or without the separator produce the same hash.
func HashRecoveryCode(code string, memory, iterations uint32, parallelism uint8, saltLen, keyLen uint32) (string, error) {
	stripped := strings.ReplaceAll(code, "-", "")
	return HashPassword(stripped, memory, iterations, parallelism, saltLen, keyLen)
}

// VerifyRecoveryCode checks whether a plaintext recovery code matches the given Argon2id hash. The hyphen is stripped
// before verification.
func VerifyRecoveryCode(code, hash string) (bool, error) {
	stripped := strings.ReplaceAll(code, "-", "")
	return VerifyPassword(stripped, hash)
}
