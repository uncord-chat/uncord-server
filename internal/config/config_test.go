package config

import (
	"strings"
	"testing"
)

// TestLoadDefaults is not t.Parallel because it mutates process-wide environment variables.
func TestLoadDefaults(t *testing.T) {
	// Clear any env vars that would override defaults
	keys := []string{
		"SERVER_NAME", "SERVER_DESCRIPTION", "SERVER_URL", "SERVER_PORT", "SERVER_ENV",
		"DATABASE_URL", "DATABASE_MAX_CONNS", "DATABASE_MIN_CONNS",
		"VALKEY_URL",
		"ARGON2_MEMORY", "ARGON2_ITERATIONS", "ARGON2_PARALLELISM", "ARGON2_SALT_LENGTH", "ARGON2_KEY_LENGTH",
		"JWT_SECRET", "JWT_ACCESS_TTL", "JWT_REFRESH_TTL",
		"ABUSE_DISPOSABLE_EMAIL_BLOCKLIST_ENABLED", "ABUSE_DISPOSABLE_EMAIL_BLOCKLIST_URL",
		"TYPESENSE_URL", "TYPESENSE_API_KEY",
		"INIT_OWNER_EMAIL", "INIT_OWNER_PASSWORD",
		"ONBOARDING_REQUIRE_RULES", "ONBOARDING_REQUIRE_EMAIL_VERIFICATION",
		"ONBOARDING_MIN_ACCOUNT_AGE", "ONBOARDING_REQUIRE_PHONE", "ONBOARDING_REQUIRE_CAPTCHA",
	}
	for _, k := range keys {
		t.Setenv(k, "")
	}

	// JWT_SECRET is required by validation
	t.Setenv("JWT_SECRET", "test-secret-for-defaults")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned unexpected error: %v", err)
	}

	// Core defaults
	if cfg.ServerName != "My Community" {
		t.Errorf("ServerName = %q, want %q", cfg.ServerName, "My Community")
	}
	if cfg.ServerPort != 8080 {
		t.Errorf("ServerPort = %d, want 8080", cfg.ServerPort)
	}
	if cfg.ServerEnv != "production" {
		t.Errorf("ServerEnv = %q, want %q", cfg.ServerEnv, "production")
	}

	// Database defaults
	if cfg.DatabaseMaxConn != 25 {
		t.Errorf("DatabaseMaxConn = %d, want 25", cfg.DatabaseMaxConn)
	}
	if cfg.DatabaseMinConn != 5 {
		t.Errorf("DatabaseMinConn = %d, want 5", cfg.DatabaseMinConn)
	}

	// Argon2 defaults
	if cfg.Argon2Memory != 65536 {
		t.Errorf("Argon2Memory = %d, want 65536", cfg.Argon2Memory)
	}
	if cfg.Argon2Iterations != 3 {
		t.Errorf("Argon2Iterations = %d, want 3", cfg.Argon2Iterations)
	}
	if cfg.Argon2Parallelism != 2 {
		t.Errorf("Argon2Parallelism = %d, want 2", cfg.Argon2Parallelism)
	}
	if cfg.Argon2SaltLength != 16 {
		t.Errorf("Argon2SaltLength = %d, want 16", cfg.Argon2SaltLength)
	}
	if cfg.Argon2KeyLength != 32 {
		t.Errorf("Argon2KeyLength = %d, want 32", cfg.Argon2KeyLength)
	}

	// JWT defaults
	if cfg.JWTAccessTTL != 900 {
		t.Errorf("JWTAccessTTL = %d, want 900", cfg.JWTAccessTTL)
	}
	if cfg.JWTRefreshTTL != 604800 {
		t.Errorf("JWTRefreshTTL = %d, want 604800", cfg.JWTRefreshTTL)
	}

	// Abuse defaults
	if !cfg.DisposableEmailBlocklistEnabled {
		t.Error("DisposableEmailBlocklistEnabled = false, want true")
	}
	if cfg.DisposableEmailBlocklistURL == "" {
		t.Error("DisposableEmailBlocklistURL is empty, want default URL")
	}

	// Onboarding defaults
	if !cfg.OnboardingRequireRules {
		t.Error("OnboardingRequireRules = false, want true")
	}
	if !cfg.OnboardingRequireEmailVerification {
		t.Error("OnboardingRequireEmailVerification = false, want true")
	}
	if cfg.OnboardingMinAccountAge != 0 {
		t.Errorf("OnboardingMinAccountAge = %d, want 0", cfg.OnboardingMinAccountAge)
	}
	if cfg.OnboardingRequirePhone {
		t.Error("OnboardingRequirePhone = true, want false")
	}
	if cfg.OnboardingRequireCaptcha {
		t.Error("OnboardingRequireCaptcha = true, want false")
	}

	// Rate limit defaults
	if cfg.RateLimitAPIRequests != 60 {
		t.Errorf("RateLimitAPIRequests = %d, want 60", cfg.RateLimitAPIRequests)
	}
	if cfg.RateLimitAuthCount != 5 {
		t.Errorf("RateLimitAuthCount = %d, want 5", cfg.RateLimitAuthCount)
	}
}

func TestLoadValidationRequiresJWTSecret(t *testing.T) {
	t.Setenv("JWT_SECRET", "")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() returned nil error, want validation error for missing JWT_SECRET")
	}
	if !strings.Contains(err.Error(), "JWT_SECRET") {
		t.Errorf("error %q does not mention JWT_SECRET", err.Error())
	}
}

func TestLoadOverrides(t *testing.T) {
	t.Setenv("SERVER_NAME", "Test Server")
	t.Setenv("SERVER_PORT", "9090")
	t.Setenv("SERVER_ENV", "development")
	t.Setenv("DATABASE_MAX_CONNS", "50")
	t.Setenv("ARGON2_MEMORY", "131072")
	t.Setenv("ONBOARDING_REQUIRE_RULES", "false")
	t.Setenv("INIT_OWNER_EMAIL", "test@example.com")
	t.Setenv("JWT_SECRET", "test-secret-key")
	t.Setenv("JWT_ACCESS_TTL", "1800")
	t.Setenv("JWT_REFRESH_TTL", "86400")
	t.Setenv("ABUSE_DISPOSABLE_EMAIL_BLOCKLIST_ENABLED", "false")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned unexpected error: %v", err)
	}

	if cfg.ServerName != "Test Server" {
		t.Errorf("ServerName = %q, want %q", cfg.ServerName, "Test Server")
	}
	if cfg.ServerPort != 9090 {
		t.Errorf("ServerPort = %d, want 9090", cfg.ServerPort)
	}
	if cfg.ServerEnv != "development" {
		t.Errorf("ServerEnv = %q, want %q", cfg.ServerEnv, "development")
	}
	if cfg.DatabaseMaxConn != 50 {
		t.Errorf("DatabaseMaxConn = %d, want 50", cfg.DatabaseMaxConn)
	}
	if cfg.Argon2Memory != 131072 {
		t.Errorf("Argon2Memory = %d, want 131072", cfg.Argon2Memory)
	}
	if cfg.OnboardingRequireRules {
		t.Error("OnboardingRequireRules = true, want false")
	}
	if cfg.InitOwnerEmail != "test@example.com" {
		t.Errorf("InitOwnerEmail = %q, want %q", cfg.InitOwnerEmail, "test@example.com")
	}
	if cfg.JWTSecret != "test-secret-key" {
		t.Errorf("JWTSecret = %q, want %q", cfg.JWTSecret, "test-secret-key")
	}
	if cfg.JWTAccessTTL != 1800 {
		t.Errorf("JWTAccessTTL = %d, want 1800", cfg.JWTAccessTTL)
	}
	if cfg.JWTRefreshTTL != 86400 {
		t.Errorf("JWTRefreshTTL = %d, want 86400", cfg.JWTRefreshTTL)
	}
	if cfg.DisposableEmailBlocklistEnabled {
		t.Error("DisposableEmailBlocklistEnabled = true, want false")
	}
}

func TestLoadInvalidInt(t *testing.T) {
	t.Setenv("SERVER_PORT", "not-a-number")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() returned nil error, want parse error")
	}
	if !strings.Contains(err.Error(), "SERVER_PORT") {
		t.Errorf("error %q does not mention SERVER_PORT", err.Error())
	}
	if !strings.Contains(err.Error(), "not-a-number") {
		t.Errorf("error %q does not include the invalid value", err.Error())
	}
}

func TestLoadInvalidBool(t *testing.T) {
	t.Setenv("ONBOARDING_REQUIRE_RULES", "maybe")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() returned nil error, want parse error")
	}
	if !strings.Contains(err.Error(), "ONBOARDING_REQUIRE_RULES") {
		t.Errorf("error %q does not mention ONBOARDING_REQUIRE_RULES", err.Error())
	}
}

func TestLoadMultipleErrors(t *testing.T) {
	t.Setenv("SERVER_PORT", "abc")
	t.Setenv("DATABASE_MAX_CONNS", "xyz")
	t.Setenv("ONBOARDING_REQUIRE_RULES", "nope")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() returned nil error, want multiple parse errors")
	}

	errStr := err.Error()
	if !strings.Contains(errStr, "SERVER_PORT") {
		t.Errorf("error missing SERVER_PORT, got: %s", errStr)
	}
	if !strings.Contains(errStr, "DATABASE_MAX_CONNS") {
		t.Errorf("error missing DATABASE_MAX_CONNS, got: %s", errStr)
	}
	if !strings.Contains(errStr, "ONBOARDING_REQUIRE_RULES") {
		t.Errorf("error missing ONBOARDING_REQUIRE_RULES, got: %s", errStr)
	}
}

func TestIsDevelopment(t *testing.T) {
	tests := []struct {
		env  string
		want bool
	}{
		{"development", true},
		{"production", false},
		{"", false},
		{"staging", false},
	}
	for _, tt := range tests {
		cfg := &Config{ServerEnv: tt.env}
		if got := cfg.IsDevelopment(); got != tt.want {
			t.Errorf("IsDevelopment() with env=%q = %v, want %v", tt.env, got, tt.want)
		}
	}
}
