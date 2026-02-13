package auth

import "testing"

func TestHashAndVerifyPassword(t *testing.T) {
	t.Parallel()
	password := "testPassword123!"

	hash, err := HashPassword(password, 65536, 1, 1, 16, 32)
	if err != nil {
		t.Fatalf("HashPassword() error = %v", err)
	}

	if hash == "" {
		t.Fatal("HashPassword() returned empty hash")
	}

	match, err := VerifyPassword(password, hash)
	if err != nil {
		t.Fatalf("VerifyPassword() error = %v", err)
	}
	if !match {
		t.Error("VerifyPassword() = false, want true for correct password")
	}
}

func TestVerifyPasswordWrong(t *testing.T) {
	t.Parallel()
	hash, err := HashPassword("correctPassword", 65536, 1, 1, 16, 32)
	if err != nil {
		t.Fatalf("HashPassword() error = %v", err)
	}

	match, err := VerifyPassword("wrongPassword!", hash)
	if err != nil {
		t.Fatalf("VerifyPassword() error = %v", err)
	}
	if match {
		t.Error("VerifyPassword() = true, want false for wrong password")
	}
}

func TestNeedsRehash(t *testing.T) {
	t.Parallel()

	// Generate a reference hash with known parameters.
	var (
		memory      uint32 = 65536
		iterations  uint32 = 1
		parallelism uint8  = 1
		saltLen     uint32 = 16
		keyLen      uint32 = 32
	)

	hash, err := HashPassword("testPassword", memory, iterations, parallelism, saltLen, keyLen)
	if err != nil {
		t.Fatalf("HashPassword() error = %v", err)
	}

	tests := []struct {
		name        string
		memory      uint32
		iterations  uint32
		parallelism uint8
		saltLen     uint32
		keyLen      uint32
		want        bool
	}{
		{"matching params", memory, iterations, parallelism, saltLen, keyLen, false},
		{"different memory", memory * 2, iterations, parallelism, saltLen, keyLen, true},
		{"different iterations", memory, iterations + 1, parallelism, saltLen, keyLen, true},
		{"different parallelism", memory, iterations, parallelism + 1, saltLen, keyLen, true},
		{"different salt length", memory, iterations, parallelism, saltLen + 8, keyLen, true},
		{"different key length", memory, iterations, parallelism, saltLen, keyLen + 8, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := NeedsRehash(hash, tt.memory, tt.iterations, tt.parallelism, tt.saltLen, tt.keyLen)
			if got != tt.want {
				t.Errorf("NeedsRehash() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNeedsRehashInvalidHash(t *testing.T) {
	t.Parallel()
	// An unparseable hash should return false (not crashable, not rehash-worthy).
	got := NeedsRehash("not-a-valid-argon2id-hash", 65536, 1, 1, 16, 32)
	if got {
		t.Error("NeedsRehash() with invalid hash = true, want false")
	}
}
