package config

import (
	"strings"
	"testing"
	"time"
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
		"ABUSE_DISPOSABLE_EMAIL_BLOCKLIST_REFRESH_INTERVAL",
		"TYPESENSE_URL", "TYPESENSE_API_KEY",
		"INIT_OWNER_EMAIL", "INIT_OWNER_PASSWORD",
		"ONBOARDING_REQUIRE_RULES", "ONBOARDING_REQUIRE_EMAIL_VERIFICATION",
		"ONBOARDING_MIN_ACCOUNT_AGE", "ONBOARDING_REQUIRE_PHONE", "ONBOARDING_REQUIRE_CAPTCHA",
		"MAX_UPLOAD_SIZE_MB",
		"MAX_CHANNELS", "MAX_CATEGORIES",
		"SMTP_HOST", "SMTP_PORT", "SMTP_USERNAME", "SMTP_PASSWORD", "SMTP_FROM",
	}
	for _, k := range keys {
		t.Setenv(k, "")
	}

	// JWT_SECRET is required by validation
	t.Setenv("JWT_SECRET", "test-secret-for-defaults-minimum-32")

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
	if cfg.JWTAccessTTL != 15*time.Minute {
		t.Errorf("JWTAccessTTL = %v, want 15m", cfg.JWTAccessTTL)
	}
	if cfg.JWTRefreshTTL != 7*24*time.Hour {
		t.Errorf("JWTRefreshTTL = %v, want 168h", cfg.JWTRefreshTTL)
	}

	// Abuse defaults
	if !cfg.DisposableEmailBlocklistEnabled {
		t.Error("DisposableEmailBlocklistEnabled = false, want true")
	}
	if cfg.DisposableEmailBlocklistURL == "" {
		t.Error("DisposableEmailBlocklistURL is empty, want default URL")
	}
	if cfg.DisposableEmailBlocklistRefreshInterval != 24*time.Hour {
		t.Errorf("DisposableEmailBlocklistRefreshInterval = %v, want 24h", cfg.DisposableEmailBlocklistRefreshInterval)
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

	// Upload limit defaults
	if cfg.MaxUploadSizeMB != 100 {
		t.Errorf("MaxUploadSizeMB = %d, want 100", cfg.MaxUploadSizeMB)
	}

	// Entity limit defaults
	if cfg.MaxChannels != 500 {
		t.Errorf("MaxChannels = %d, want 500", cfg.MaxChannels)
	}
	if cfg.MaxCategories != 50 {
		t.Errorf("MaxCategories = %d, want 50", cfg.MaxCategories)
	}

	// SMTP defaults
	if cfg.SMTPHost != "" {
		t.Errorf("SMTPHost = %q, want empty", cfg.SMTPHost)
	}
	if cfg.SMTPPort != 587 {
		t.Errorf("SMTPPort = %d, want 587", cfg.SMTPPort)
	}
	if cfg.SMTPFrom != "noreply@chat.example.com" {
		t.Errorf("SMTPFrom = %q, want %q", cfg.SMTPFrom, "noreply@chat.example.com")
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

func TestLoadValidationJWTSecretTooShort(t *testing.T) {
	t.Setenv("JWT_SECRET", "short")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() returned nil error, want validation error for short JWT_SECRET")
	}
	if !strings.Contains(err.Error(), "JWT_SECRET must be at least 32 characters") {
		t.Errorf("error %q does not mention minimum length", err.Error())
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
	t.Setenv("JWT_SECRET", "test-secret-key-that-is-32-chars!")
	t.Setenv("JWT_ACCESS_TTL", "30m")
	t.Setenv("JWT_REFRESH_TTL", "24h")
	t.Setenv("ABUSE_DISPOSABLE_EMAIL_BLOCKLIST_ENABLED", "false")
	t.Setenv("ABUSE_DISPOSABLE_EMAIL_BLOCKLIST_REFRESH_INTERVAL", "12h")
	t.Setenv("MAX_UPLOAD_SIZE_MB", "50")
	t.Setenv("MAX_CHANNELS", "100")
	t.Setenv("MAX_CATEGORIES", "25")

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
	if cfg.JWTSecret != "test-secret-key-that-is-32-chars!" {
		t.Errorf("JWTSecret = %q, want %q", cfg.JWTSecret, "test-secret-key-that-is-32-chars!")
	}
	if cfg.JWTAccessTTL != 30*time.Minute {
		t.Errorf("JWTAccessTTL = %v, want 30m", cfg.JWTAccessTTL)
	}
	if cfg.JWTRefreshTTL != 24*time.Hour {
		t.Errorf("JWTRefreshTTL = %v, want 24h", cfg.JWTRefreshTTL)
	}
	if cfg.DisposableEmailBlocklistEnabled {
		t.Error("DisposableEmailBlocklistEnabled = true, want false")
	}
	if cfg.DisposableEmailBlocklistRefreshInterval != 12*time.Hour {
		t.Errorf("DisposableEmailBlocklistRefreshInterval = %v, want 12h", cfg.DisposableEmailBlocklistRefreshInterval)
	}
	if cfg.MaxUploadSizeMB != 50 {
		t.Errorf("MaxUploadSizeMB = %d, want 50", cfg.MaxUploadSizeMB)
	}
	if cfg.MaxChannels != 100 {
		t.Errorf("MaxChannels = %d, want 100", cfg.MaxChannels)
	}
	if cfg.MaxCategories != 25 {
		t.Errorf("MaxCategories = %d, want 25", cfg.MaxCategories)
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

func TestLoadInvalidDuration(t *testing.T) {
	t.Setenv("ABUSE_DISPOSABLE_EMAIL_BLOCKLIST_REFRESH_INTERVAL", "not-a-duration")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() returned nil error, want parse error")
	}
	if !strings.Contains(err.Error(), "ABUSE_DISPOSABLE_EMAIL_BLOCKLIST_REFRESH_INTERVAL") {
		t.Errorf("error %q does not mention ABUSE_DISPOSABLE_EMAIL_BLOCKLIST_REFRESH_INTERVAL", err.Error())
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

func TestBodyLimitBytes(t *testing.T) {
	cfg := &Config{MaxUploadSizeMB: 100}
	want := 101 * 1024 * 1024 // 100 MB + 1 MB overhead
	if got := cfg.BodyLimitBytes(); got != want {
		t.Errorf("BodyLimitBytes() = %d, want %d", got, want)
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

func TestLoadSMTPOverrides(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-for-defaults-minimum-32")
	t.Setenv("SMTP_HOST", "mail.example.com")
	t.Setenv("SMTP_PORT", "465")
	t.Setenv("SMTP_USERNAME", "user@example.com")
	t.Setenv("SMTP_PASSWORD", "secret")
	t.Setenv("SMTP_FROM", "hello@example.com")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned unexpected error: %v", err)
	}
	if cfg.SMTPHost != "mail.example.com" {
		t.Errorf("SMTPHost = %q, want %q", cfg.SMTPHost, "mail.example.com")
	}
	if cfg.SMTPPort != 465 {
		t.Errorf("SMTPPort = %d, want 465", cfg.SMTPPort)
	}
	if cfg.SMTPUsername != "user@example.com" {
		t.Errorf("SMTPUsername = %q, want %q", cfg.SMTPUsername, "user@example.com")
	}
	if cfg.SMTPPassword != "secret" {
		t.Errorf("SMTPPassword = %q, want %q", cfg.SMTPPassword, "secret")
	}
	if cfg.SMTPFrom != "hello@example.com" {
		t.Errorf("SMTPFrom = %q, want %q", cfg.SMTPFrom, "hello@example.com")
	}
}

func TestLoadSMTPValidation(t *testing.T) {
	tests := []struct {
		name    string
		host    string
		port    string
		from    string
		wantErr string
	}{
		{
			name:    "invalid port",
			host:    "mail.example.com",
			port:    "99999",
			from:    "noreply@example.com",
			wantErr: "SMTP_PORT",
		},
		{
			name:    "invalid from address",
			host:    "mail.example.com",
			port:    "587",
			from:    "not-an-email",
			wantErr: "SMTP_FROM",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("JWT_SECRET", "test-secret-for-defaults-minimum-32")
			t.Setenv("SMTP_HOST", tt.host)
			t.Setenv("SMTP_PORT", tt.port)
			t.Setenv("SMTP_FROM", tt.from)

			_, err := Load()
			if err == nil {
				t.Fatal("Load() returned nil error, want validation error")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error %q does not mention %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestLoadDevelopmentOverrides(t *testing.T) {
	tests := []struct {
		name          string
		serverEnv     string
		serverPort    string
		smtpHost      string
		wantHost      string
		wantPort      int
		wantUsername  string
		wantPassword  string
		wantServerURL string
	}{
		{
			name:          "development mode overrides SMTP and ServerURL",
			serverEnv:     "development",
			serverPort:    "",
			smtpHost:      "",
			wantHost:      "mailpit",
			wantPort:      1025,
			wantUsername:  "",
			wantPassword:  "",
			wantServerURL: "http://localhost:8080",
		},
		{
			name:          "development mode uses configured port in ServerURL",
			serverEnv:     "development",
			serverPort:    "9090",
			smtpHost:      "",
			wantHost:      "mailpit",
			wantPort:      1025,
			wantUsername:  "",
			wantPassword:  "",
			wantServerURL: "http://localhost:9090",
		},
		{
			name:          "production mode leaves SMTP and ServerURL unchanged",
			serverEnv:     "production",
			serverPort:    "",
			smtpHost:      "mail.example.com",
			wantHost:      "mail.example.com",
			wantPort:      587,
			wantUsername:  "user@example.com",
			wantPassword:  "secret",
			wantServerURL: "https://chat.example.com",
		},
		{
			name:          "development mode overrides explicit SMTP settings",
			serverEnv:     "development",
			serverPort:    "",
			smtpHost:      "mail.example.com",
			wantHost:      "mailpit",
			wantPort:      1025,
			wantUsername:  "",
			wantPassword:  "",
			wantServerURL: "http://localhost:8080",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("JWT_SECRET", "test-secret-for-defaults-minimum-32")
			t.Setenv("SERVER_ENV", tt.serverEnv)
			t.Setenv("SERVER_PORT", tt.serverPort)
			t.Setenv("SMTP_HOST", tt.smtpHost)
			t.Setenv("SMTP_PORT", "587")
			t.Setenv("SMTP_USERNAME", "user@example.com")
			t.Setenv("SMTP_PASSWORD", "secret")
			t.Setenv("SMTP_FROM", "noreply@example.com")

			cfg, err := Load()
			if err != nil {
				t.Fatalf("Load() returned unexpected error: %v", err)
			}

			if cfg.SMTPHost != tt.wantHost {
				t.Errorf("SMTPHost = %q, want %q", cfg.SMTPHost, tt.wantHost)
			}
			if cfg.SMTPPort != tt.wantPort {
				t.Errorf("SMTPPort = %d, want %d", cfg.SMTPPort, tt.wantPort)
			}
			if cfg.SMTPUsername != tt.wantUsername {
				t.Errorf("SMTPUsername = %q, want %q", cfg.SMTPUsername, tt.wantUsername)
			}
			if cfg.SMTPPassword != tt.wantPassword {
				t.Errorf("SMTPPassword = %q, want %q", cfg.SMTPPassword, tt.wantPassword)
			}
			if cfg.ServerURL != tt.wantServerURL {
				t.Errorf("ServerURL = %q, want %q", cfg.ServerURL, tt.wantServerURL)
			}
		})
	}
}

func TestSMTPConfigured(t *testing.T) {
	tests := []struct {
		host string
		want bool
	}{
		{"", false},
		{"mail.example.com", true},
	}
	for _, tt := range tests {
		cfg := &Config{SMTPHost: tt.host}
		if got := cfg.SMTPConfigured(); got != tt.want {
			t.Errorf("SMTPConfigured() with host=%q = %v, want %v", tt.host, got, tt.want)
		}
	}
}
