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
