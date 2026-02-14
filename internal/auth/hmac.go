package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// HMACIdentifier computes an HMAC-SHA256 of the given identifier using the provided hex-encoded key and returns the
// hex-encoded digest. The caller is responsible for any case folding or whitespace trimming before calling.
func HMACIdentifier(identifier, hexKey string) (string, error) {
	key, err := hex.DecodeString(hexKey)
	if err != nil {
		return "", fmt.Errorf("decode HMAC key: %w", err)
	}
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(identifier))
	return hex.EncodeToString(mac.Sum(nil)), nil
}
