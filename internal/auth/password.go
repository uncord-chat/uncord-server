package auth

import (
	"fmt"

	"github.com/alexedwards/argon2id"
)

// HashPassword hashes a password using argon2id with the given parameters.
func HashPassword(password string, memory, iterations uint32, parallelism uint8, saltLen, keyLen uint32) (string, error) {
	params := &argon2id.Params{
		Memory:      memory,
		Iterations:  iterations,
		Parallelism: parallelism,
		SaltLength:  saltLen,
		KeyLength:   keyLen,
	}
	hash, err := argon2id.CreateHash(password, params)
	if err != nil {
		return "", fmt.Errorf("hash password: %w", err)
	}
	return hash, nil
}

// VerifyPassword checks whether a plaintext password matches the given argon2id hash.
func VerifyPassword(password, hash string) (bool, error) {
	match, err := argon2id.ComparePasswordAndHash(password, hash)
	if err != nil {
		return false, fmt.Errorf("verify password: %w", err)
	}
	return match, nil
}

// NeedsRehash returns true if the given Argon2id hash was generated with parameters that differ from the provided
// configuration values, indicating that the hash should be regenerated on next successful login.
func NeedsRehash(hash string, memory, iterations uint32, parallelism uint8, saltLen, keyLen uint32) bool {
	params, salt, key, err := argon2id.DecodeHash(hash)
	if err != nil {
		return false
	}
	return params.Memory != memory ||
		params.Iterations != iterations ||
		params.Parallelism != parallelism ||
		uint32(len(salt)) != saltLen ||
		uint32(len(key)) != keyLen
}
