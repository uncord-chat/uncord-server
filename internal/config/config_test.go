package config

import (
	"strings"
	"testing"
	"time"
)

// testServerSecret is a valid 64 hex character (32 byte) key used across config tests.
const testServerSecret = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

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
		"ABUSE_DISPOSABLE_EMAIL_BLOCKLIST_REFRESH_INTERVAL", "ABUSE_DISPOSABLE_EMAIL_BLOCKLIST_TIMEOUT",
		"VALKEY_DIAL_TIMEOUT",
		"TYPESENSE_URL", "TYPESENSE_API_KEY", "TYPESENSE_TIMEOUT",
		"INIT_OWNER_EMAIL", "INIT_OWNER_PASSWORD",
		"ONBOARDING_REQUIRE_RULES", "ONBOARDING_REQUIRE_EMAIL_VERIFICATION",
		"ONBOARDING_MIN_ACCOUNT_AGE", "ONBOARDING_REQUIRE_PHONE", "ONBOARDING_REQUIRE_CAPTCHA",
		"MAX_UPLOAD_SIZE_MB", "MAX_AVATAR_SIZE_MB", "MAX_AVATAR_DIMENSION",
		"STORAGE_BACKEND", "STORAGE_LOCAL_PATH",
		"MAX_ATTACHMENTS_PER_MESSAGE", "ATTACHMENT_ORPHAN_TTL",
		"RATE_LIMIT_UPLOAD_COUNT", "RATE_LIMIT_UPLOAD_WINDOW_SECONDS",
		"MAX_CHANNELS", "MAX_CATEGORIES",
		"SMTP_HOST", "SMTP_PORT", "SMTP_USERNAME", "SMTP_PASSWORD", "SMTP_FROM",
		"SERVER_SECRET", "DELETION_TOMBSTONE_USERNAMES", "DELETION_TOMBSTONE_RETENTION",
		"LOGIN_ATTEMPT_RETENTION", "DATA_CLEANUP_INTERVAL",
	}
	for _, k := range keys {
		t.Setenv(k, "")
	}

	// JWT_SECRET and SERVER_SECRET are required by validation
	t.Setenv("JWT_SECRET", "test-secret-for-defaults-minimum-32")
	t.Setenv("SERVER_SECRET", testServerSecret)

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
	if cfg.DisposableEmailBlocklistTimeout != 10*time.Second {
		t.Errorf("DisposableEmailBlocklistTimeout = %v, want 10s", cfg.DisposableEmailBlocklistTimeout)
	}

	// Valkey defaults
	if cfg.ValkeyDialTimeout != 5*time.Second {
		t.Errorf("ValkeyDialTimeout = %v, want 5s", cfg.ValkeyDialTimeout)
	}

	// Typesense defaults
	if cfg.TypesenseTimeout != 30*time.Second {
		t.Errorf("TypesenseTimeout = %v, want 30s", cfg.TypesenseTimeout)
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
	if cfg.MaxAvatarSizeMB != 8 {
		t.Errorf("MaxAvatarSizeMB = %d, want 8", cfg.MaxAvatarSizeMB)
	}
	if cfg.MaxAvatarDimension != 1080 {
		t.Errorf("MaxAvatarDimension = %d, want 1080", cfg.MaxAvatarDimension)
	}

	// Storage defaults
	if cfg.StorageBackend != "local" {
		t.Errorf("StorageBackend = %q, want %q", cfg.StorageBackend, "local")
	}
	if cfg.StorageLocalPath != "/data/uncord/media" {
		t.Errorf("StorageLocalPath = %q, want %q", cfg.StorageLocalPath, "/data/uncord/media")
	}

	// Attachment defaults
	if cfg.MaxAttachmentsPerMessage != 10 {
		t.Errorf("MaxAttachmentsPerMessage = %d, want 10", cfg.MaxAttachmentsPerMessage)
	}
	if cfg.AttachmentOrphanTTL != time.Hour {
		t.Errorf("AttachmentOrphanTTL = %v, want 1h", cfg.AttachmentOrphanTTL)
	}

	// Upload rate limit defaults
	if cfg.RateLimitUploadCount != 10 {
		t.Errorf("RateLimitUploadCount = %d, want 10", cfg.RateLimitUploadCount)
	}
	if cfg.RateLimitUploadWindowSeconds != 60 {
		t.Errorf("RateLimitUploadWindowSeconds = %d, want 60", cfg.RateLimitUploadWindowSeconds)
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

	// Account deletion defaults
	if !cfg.DeletionTombstoneUsernames {
		t.Error("DeletionTombstoneUsernames = false, want true")
	}

	// Auth retention defaults
	if cfg.LoginAttemptRetention != 2160*time.Hour {
		t.Errorf("LoginAttemptRetention = %v, want 2160h", cfg.LoginAttemptRetention)
	}

	// Account deletion defaults
	if cfg.DeletionTombstoneRetention != 0 {
		t.Errorf("DeletionTombstoneRetention = %v, want 0", cfg.DeletionTombstoneRetention)
	}

	// Data retention defaults
	if cfg.DataCleanupInterval != 12*time.Hour {
		t.Errorf("DataCleanupInterval = %v, want 12h", cfg.DataCleanupInterval)
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
	t.Setenv("SERVER_SECRET", testServerSecret)
	t.Setenv("JWT_ACCESS_TTL", "30m")
	t.Setenv("JWT_REFRESH_TTL", "24h")
	t.Setenv("ABUSE_DISPOSABLE_EMAIL_BLOCKLIST_ENABLED", "false")
	t.Setenv("ABUSE_DISPOSABLE_EMAIL_BLOCKLIST_REFRESH_INTERVAL", "12h")
	t.Setenv("ABUSE_DISPOSABLE_EMAIL_BLOCKLIST_TIMEOUT", "20s")
	t.Setenv("VALKEY_DIAL_TIMEOUT", "10s")
	t.Setenv("TYPESENSE_TIMEOUT", "1m")
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
	if cfg.DisposableEmailBlocklistTimeout != 20*time.Second {
		t.Errorf("DisposableEmailBlocklistTimeout = %v, want 20s", cfg.DisposableEmailBlocklistTimeout)
	}
	if cfg.ValkeyDialTimeout != 10*time.Second {
		t.Errorf("ValkeyDialTimeout = %v, want 10s", cfg.ValkeyDialTimeout)
	}
	if cfg.TypesenseTimeout != time.Minute {
		t.Errorf("TypesenseTimeout = %v, want 1m", cfg.TypesenseTimeout)
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

func TestMaxUploadSizeBytes(t *testing.T) {
	cfg := &Config{MaxUploadSizeMB: 50}
	want := int64(50) * 1024 * 1024
	if got := cfg.MaxUploadSizeBytes(); got != want {
		t.Errorf("MaxUploadSizeBytes() = %d, want %d", got, want)
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
	t.Setenv("SERVER_SECRET", testServerSecret)
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
			t.Setenv("SERVER_SECRET", testServerSecret)
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
			t.Setenv("SERVER_SECRET", testServerSecret)
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

func TestLoadValidationRequiresServerSecret(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-for-defaults-minimum-32")
	t.Setenv("SERVER_SECRET", "")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() returned nil error, want validation error for missing SERVER_SECRET")
	}
	if !strings.Contains(err.Error(), "SERVER_SECRET is required") {
		t.Errorf("error %q does not mention SERVER_SECRET", err.Error())
	}
}

func TestLoadValidationServerSecretInvalidHex(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-for-defaults-minimum-32")
	t.Setenv("SERVER_SECRET", "not-valid-hex")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() returned nil error, want validation error for invalid SERVER_SECRET")
	}
	if !strings.Contains(err.Error(), "SERVER_SECRET must be exactly 64 hex characters") {
		t.Errorf("error %q does not mention expected format", err.Error())
	}
}

func TestLoadValidationServerSecretWrongLength(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-for-defaults-minimum-32")
	t.Setenv("SERVER_SECRET", "0123456789abcdef") // 16 hex chars = 8 bytes, too short

	_, err := Load()
	if err == nil {
		t.Fatal("Load() returned nil error, want validation error for short SERVER_SECRET")
	}
	if !strings.Contains(err.Error(), "SERVER_SECRET must be exactly 64 hex characters") {
		t.Errorf("error %q does not mention expected format", err.Error())
	}
}

func TestLoadDeletionTombstoneUsernamesDefault(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-for-defaults-minimum-32")
	t.Setenv("SERVER_SECRET", testServerSecret)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned unexpected error: %v", err)
	}
	if !cfg.DeletionTombstoneUsernames {
		t.Error("DeletionTombstoneUsernames = false, want true (default)")
	}
}

func TestLoadDeletionTombstoneUsernamesOverride(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-for-defaults-minimum-32")
	t.Setenv("SERVER_SECRET", testServerSecret)
	t.Setenv("DELETION_TOMBSTONE_USERNAMES", "false")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned unexpected error: %v", err)
	}
	if cfg.DeletionTombstoneUsernames {
		t.Error("DeletionTombstoneUsernames = true, want false")
	}
}

func TestLoadValidationLoginAttemptRetentionTooLow(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-for-defaults-minimum-32")
	t.Setenv("SERVER_SECRET", testServerSecret)
	t.Setenv("LOGIN_ATTEMPT_RETENTION", "30m")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() returned nil error, want validation error for LOGIN_ATTEMPT_RETENTION < 1h")
	}
	if !strings.Contains(err.Error(), "LOGIN_ATTEMPT_RETENTION must be at least 1h") {
		t.Errorf("error %q does not mention LOGIN_ATTEMPT_RETENTION", err.Error())
	}
}

func TestLoadValidationDataCleanupIntervalTooLow(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-for-defaults-minimum-32")
	t.Setenv("SERVER_SECRET", testServerSecret)
	t.Setenv("DATA_CLEANUP_INTERVAL", "30s")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() returned nil error, want validation error for DATA_CLEANUP_INTERVAL < 1m")
	}
	if !strings.Contains(err.Error(), "DATA_CLEANUP_INTERVAL must be at least 1m") {
		t.Errorf("error %q does not mention DATA_CLEANUP_INTERVAL", err.Error())
	}
}

func TestLoadValidationDeletionTombstoneRetentionNegative(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-for-defaults-minimum-32")
	t.Setenv("SERVER_SECRET", testServerSecret)

	cfg := &Config{
		JWTSecret:                    "test-secret-for-defaults-minimum-32",
		ServerPort:                   8080,
		DatabaseMaxConn:              25,
		DatabaseMinConn:              5,
		JWTAccessTTL:                 15 * time.Minute,
		JWTRefreshTTL:                7 * 24 * time.Hour,
		Argon2Memory:                 65536,
		Argon2Iterations:             3,
		Argon2Parallelism:            2,
		MaxUploadSizeMB:              100,
		MaxAvatarSizeMB:              8,
		MaxAvatarDimension:           1080,
		StorageBackend:               "local",
		StorageLocalPath:             "/data/uncord/media",
		MaxAttachmentsPerMessage:     10,
		AttachmentOrphanTTL:          time.Hour,
		RateLimitUploadCount:         10,
		RateLimitUploadWindowSeconds: 60,
		MaxChannels:                  500,
		MaxCategories:                50,
		RateLimitAPIRequests:         60,
		RateLimitAPIWindowSeconds:    60,
		RateLimitAuthCount:           5,
		RateLimitAuthWindowSeconds:   300,
		ServerSecret:                 testServerSecret,
		MFATicketTTL:                 5 * time.Minute,
		LoginAttemptRetention:        2160 * time.Hour,
		DeletionTombstoneRetention:   -time.Hour,
		DataCleanupInterval:          12 * time.Hour,
		MaxMessageLength:             4000,
	}
	err := cfg.validate()
	if err == nil {
		t.Fatal("validate() returned nil error, want validation error for negative DELETION_TOMBSTONE_RETENTION")
	}
	if !strings.Contains(err.Error(), "DELETION_TOMBSTONE_RETENTION must not be negative") {
		t.Errorf("error %q does not mention DELETION_TOMBSTONE_RETENTION", err.Error())
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

func TestLoadValidationStorageBackend(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-for-defaults-minimum-32")
	t.Setenv("SERVER_SECRET", testServerSecret)
	t.Setenv("STORAGE_BACKEND", "invalid")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() returned nil error, want validation error for invalid STORAGE_BACKEND")
	}
	if !strings.Contains(err.Error(), "STORAGE_BACKEND") {
		t.Errorf("error %q does not mention STORAGE_BACKEND", err.Error())
	}
}

func TestLoadValidationMaxAttachmentsPerMessage(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-for-defaults-minimum-32")
	t.Setenv("SERVER_SECRET", testServerSecret)
	t.Setenv("MAX_ATTACHMENTS_PER_MESSAGE", "0")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() returned nil error, want validation error for MAX_ATTACHMENTS_PER_MESSAGE < 1")
	}
	if !strings.Contains(err.Error(), "MAX_ATTACHMENTS_PER_MESSAGE") {
		t.Errorf("error %q does not mention MAX_ATTACHMENTS_PER_MESSAGE", err.Error())
	}
}

func TestLoadValidationAttachmentOrphanTTL(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-for-defaults-minimum-32")
	t.Setenv("SERVER_SECRET", testServerSecret)
	t.Setenv("ATTACHMENT_ORPHAN_TTL", "30s")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() returned nil error, want validation error for ATTACHMENT_ORPHAN_TTL < 1m")
	}
	if !strings.Contains(err.Error(), "ATTACHMENT_ORPHAN_TTL") {
		t.Errorf("error %q does not mention ATTACHMENT_ORPHAN_TTL", err.Error())
	}
}
