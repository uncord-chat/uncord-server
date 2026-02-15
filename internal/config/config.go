package config

import (
	"encoding/hex"
	"errors"
	"fmt"
	"net/mail"
	"os"
	"strconv"
	"time"
)

// Config holds application configuration populated from environment variables.
type Config struct {
	// Core
	ServerName        string
	ServerDescription string
	ServerURL         string
	ServerPort        int
	ServerEnv         string // "development" or "production"
	LogHealthRequests bool

	// Database
	DatabaseURL     string
	DatabaseMaxConn int
	DatabaseMinConn int

	// Valkey
	ValkeyURL string

	// Argon2 password hashing
	Argon2Memory      uint32
	Argon2Iterations  uint32
	Argon2Parallelism uint8
	Argon2SaltLength  uint32
	Argon2KeyLength   uint32

	// JWT
	JWTSecret     string
	JWTAccessTTL  time.Duration
	JWTRefreshTTL time.Duration

	// Abuse / Disposable Email
	DisposableEmailBlocklistEnabled         bool
	DisposableEmailBlocklistURL             string
	DisposableEmailBlocklistRefreshInterval time.Duration

	// Typesense
	TypesenseURL    string
	TypesenseAPIKey string

	// First-run owner
	InitOwnerEmail    string
	InitOwnerPassword string

	// Onboarding
	OnboardingRequireRules             bool
	OnboardingRequireEmailVerification bool
	OnboardingMinAccountAge            int
	OnboardingRequirePhone             bool
	OnboardingRequireCaptcha           bool

	// Rate Limiting
	RateLimitAPIRequests       int
	RateLimitAPIWindowSeconds  int
	RateLimitAuthCount         int
	RateLimitAuthWindowSeconds int

	// Upload Limits
	MaxUploadSizeMB int

	// Entity Limits
	MaxChannels   int
	MaxCategories int
	MaxRoles      int

	// SMTP
	SMTPHost     string
	SMTPPort     int
	SMTPUsername string
	SMTPPassword string
	SMTPFrom     string

	// MFA
	MFAEncryptionKey string
	MFATicketTTL     time.Duration

	// Account Deletion
	ServerSecret               string // Required. Hex-encoded 32-byte HMAC key for tombstones and future use.
	DeletionTombstoneUsernames bool   // Also tombstone usernames on deletion. Default: true.

	// CORS
	CORSAllowOrigins string
}

// Load reads configuration from environment variables with defaults matching .env.example. It returns an error if any
// variable is set but cannot be parsed, or if required security values are missing.
func Load() (*Config, error) {
	p := &parser{}

	cfg := &Config{
		ServerName:        envStr("SERVER_NAME", "My Community"),
		ServerDescription: envStr("SERVER_DESCRIPTION", ""),
		ServerURL:         envStr("SERVER_URL", "https://chat.example.com"),
		ServerPort:        p.int("SERVER_PORT", 8080),
		ServerEnv:         envStr("SERVER_ENV", "production"),
		LogHealthRequests: p.bool("LOG_HEALTH_REQUESTS", true),

		DatabaseURL:     envStr("DATABASE_URL", "postgres://uncord:password@postgres:5432/uncord?sslmode=disable"),
		DatabaseMaxConn: p.int("DATABASE_MAX_CONNS", 25),
		DatabaseMinConn: p.int("DATABASE_MIN_CONNS", 5),

		ValkeyURL: envStr("VALKEY_URL", "valkey://valkey:6379/0"),

		Argon2Memory:      p.uint32("ARGON2_MEMORY", 65536),
		Argon2Iterations:  p.uint32("ARGON2_ITERATIONS", 3),
		Argon2Parallelism: p.uint8("ARGON2_PARALLELISM", 2),
		Argon2SaltLength:  p.uint32("ARGON2_SALT_LENGTH", 16),
		Argon2KeyLength:   p.uint32("ARGON2_KEY_LENGTH", 32),

		JWTSecret:     envStr("JWT_SECRET", ""),
		JWTAccessTTL:  p.duration("JWT_ACCESS_TTL", 15*time.Minute),
		JWTRefreshTTL: p.duration("JWT_REFRESH_TTL", 7*24*time.Hour),

		DisposableEmailBlocklistEnabled:         p.bool("ABUSE_DISPOSABLE_EMAIL_BLOCKLIST_ENABLED", true),
		DisposableEmailBlocklistURL:             envStr("ABUSE_DISPOSABLE_EMAIL_BLOCKLIST_URL", "https://raw.githubusercontent.com/disposable-email-domains/disposable-email-domains/master/disposable_email_blocklist.conf"),
		DisposableEmailBlocklistRefreshInterval: p.duration("ABUSE_DISPOSABLE_EMAIL_BLOCKLIST_REFRESH_INTERVAL", 24*time.Hour),

		TypesenseURL:    envStr("TYPESENSE_URL", "http://typesense:8108"),
		TypesenseAPIKey: envStr("TYPESENSE_API_KEY", "change-me-in-production"),

		InitOwnerEmail:    envStr("INIT_OWNER_EMAIL", ""),
		InitOwnerPassword: envStr("INIT_OWNER_PASSWORD", ""),

		OnboardingRequireRules:             p.bool("ONBOARDING_REQUIRE_RULES", true),
		OnboardingRequireEmailVerification: p.bool("ONBOARDING_REQUIRE_EMAIL_VERIFICATION", true),
		OnboardingMinAccountAge:            p.int("ONBOARDING_MIN_ACCOUNT_AGE", 0),
		OnboardingRequirePhone:             p.bool("ONBOARDING_REQUIRE_PHONE", false),
		OnboardingRequireCaptcha:           p.bool("ONBOARDING_REQUIRE_CAPTCHA", false),

		RateLimitAPIRequests:       p.int("RATE_LIMIT_API_REQUESTS", 60),
		RateLimitAPIWindowSeconds:  p.int("RATE_LIMIT_API_WINDOW_SECONDS", 60),
		RateLimitAuthCount:         p.int("RATE_LIMIT_AUTH_COUNT", 5),
		RateLimitAuthWindowSeconds: p.int("RATE_LIMIT_AUTH_WINDOW_SECONDS", 300),

		MaxUploadSizeMB: p.int("MAX_UPLOAD_SIZE_MB", 100),

		MaxChannels:   p.int("MAX_CHANNELS", 500),
		MaxCategories: p.int("MAX_CATEGORIES", 50),
		MaxRoles:      p.int("MAX_ROLES", 250),

		SMTPHost:     envStr("SMTP_HOST", ""),
		SMTPPort:     p.int("SMTP_PORT", 587),
		SMTPUsername: envStr("SMTP_USERNAME", ""),
		SMTPPassword: envStr("SMTP_PASSWORD", ""),
		SMTPFrom:     envStr("SMTP_FROM", "noreply@chat.example.com"),

		MFAEncryptionKey: envStr("MFA_ENCRYPTION_KEY", ""),
		MFATicketTTL:     p.duration("MFA_TICKET_TTL", 5*time.Minute),

		ServerSecret:               envStr("SERVER_SECRET", ""),
		DeletionTombstoneUsernames: p.bool("DELETION_TOMBSTONE_USERNAMES", true),

		CORSAllowOrigins: envStr("CORS_ALLOW_ORIGINS", "*"),
	}

	if parseErr := errors.Join(p.errs...); parseErr != nil {
		return nil, parseErr
	}

	// In development mode, override defaults so that everything works out of the box with Docker Compose. SMTP is
	// routed through Mailpit (the local mail catcher) and ServerURL points to the local server so that verification
	// links in emails resolve correctly.
	if cfg.IsDevelopment() {
		cfg.SMTPHost = "mailpit"
		cfg.SMTPPort = 1025
		cfg.SMTPUsername = ""
		cfg.SMTPPassword = ""
		cfg.ServerURL = fmt.Sprintf("http://localhost:%d", cfg.ServerPort)
	}

	if err := cfg.validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

// IsDevelopment returns true when running in development mode.
func (c *Config) IsDevelopment() bool {
	return c.ServerEnv == "development"
}

// SMTPConfigured returns true when an SMTP host is set, indicating that the server should attempt to send emails.
func (c *Config) SMTPConfigured() bool {
	return c.SMTPHost != ""
}

// MFAConfigured returns true when the MFA encryption key is set, indicating that TOTP-based MFA is available.
func (c *Config) MFAConfigured() bool {
	return c.MFAEncryptionKey != ""
}

// BodyLimitBytes returns the maximum request body size in bytes, derived from MaxUploadSizeMB with a small margin for
// multipart framing overhead.
func (c *Config) BodyLimitBytes() int {
	return (c.MaxUploadSizeMB + 1) * 1024 * 1024
}

func (c *Config) validate() error {
	var errs []error

	if c.JWTSecret == "" {
		errs = append(errs, fmt.Errorf("JWT_SECRET is required"))
	} else if len(c.JWTSecret) < 32 {
		errs = append(errs, fmt.Errorf("JWT_SECRET must be at least 32 characters"))
	}

	if c.ServerPort < 1 || c.ServerPort > 65535 {
		errs = append(errs, fmt.Errorf("SERVER_PORT must be between 1 and 65535"))
	}

	if c.DatabaseMaxConn < 1 {
		errs = append(errs, fmt.Errorf("DATABASE_MAX_CONNS must be at least 1"))
	}
	if c.DatabaseMinConn < 0 {
		errs = append(errs, fmt.Errorf("DATABASE_MIN_CONNS must not be negative"))
	}
	if c.DatabaseMinConn > c.DatabaseMaxConn {
		errs = append(errs, fmt.Errorf("DATABASE_MIN_CONNS (%d) must not exceed DATABASE_MAX_CONNS (%d)", c.DatabaseMinConn, c.DatabaseMaxConn))
	}

	if c.JWTAccessTTL < time.Second {
		errs = append(errs, fmt.Errorf("JWT_ACCESS_TTL must be at least 1s"))
	}
	if c.JWTRefreshTTL < time.Second {
		errs = append(errs, fmt.Errorf("JWT_REFRESH_TTL must be at least 1s"))
	}

	if c.Argon2Memory == 0 {
		errs = append(errs, fmt.Errorf("ARGON2_MEMORY must be greater than 0"))
	}
	if c.Argon2Iterations == 0 {
		errs = append(errs, fmt.Errorf("ARGON2_ITERATIONS must be greater than 0"))
	}
	if c.Argon2Parallelism == 0 {
		errs = append(errs, fmt.Errorf("ARGON2_PARALLELISM must be greater than 0"))
	}

	if c.MaxUploadSizeMB < 1 {
		errs = append(errs, fmt.Errorf("MAX_UPLOAD_SIZE_MB must be at least 1"))
	}

	if c.MaxChannels < 1 {
		errs = append(errs, fmt.Errorf("MAX_CHANNELS must be at least 1"))
	}
	if c.MaxCategories < 1 {
		errs = append(errs, fmt.Errorf("MAX_CATEGORIES must be at least 1"))
	}

	if c.RateLimitAPIRequests < 1 {
		errs = append(errs, fmt.Errorf("RATE_LIMIT_API_REQUESTS must be at least 1"))
	}
	if c.RateLimitAPIWindowSeconds < 1 {
		errs = append(errs, fmt.Errorf("RATE_LIMIT_API_WINDOW_SECONDS must be at least 1"))
	}
	if c.RateLimitAuthCount < 1 {
		errs = append(errs, fmt.Errorf("RATE_LIMIT_AUTH_COUNT must be at least 1"))
	}
	if c.RateLimitAuthWindowSeconds < 1 {
		errs = append(errs, fmt.Errorf("RATE_LIMIT_AUTH_WINDOW_SECONDS must be at least 1"))
	}

	if c.MFAEncryptionKey != "" {
		b, err := hex.DecodeString(c.MFAEncryptionKey)
		if err != nil || len(b) != 32 {
			errs = append(errs, fmt.Errorf("MFA_ENCRYPTION_KEY must be exactly 64 hex characters (32 bytes)"))
		}
	}

	if c.ServerSecret == "" {
		errs = append(errs, fmt.Errorf("SERVER_SECRET is required"))
	} else {
		b, err := hex.DecodeString(c.ServerSecret)
		if err != nil || len(b) != 32 {
			errs = append(errs, fmt.Errorf("SERVER_SECRET must be exactly 64 hex characters (32 bytes)"))
		}
	}

	if c.MFATicketTTL < time.Second {
		errs = append(errs, fmt.Errorf("MFA_TICKET_TTL must be at least 1s"))
	}

	if c.SMTPHost != "" {
		if c.SMTPPort < 1 || c.SMTPPort > 65535 {
			errs = append(errs, fmt.Errorf("SMTP_PORT must be between 1 and 65535"))
		}
		if _, err := mail.ParseAddress(c.SMTPFrom); err != nil {
			errs = append(errs, fmt.Errorf("SMTP_FROM is not a valid email address: %q", c.SMTPFrom))
		}
	}

	return errors.Join(errs...)
}

// parser collects parse errors so Load can report all invalid values at once.
type parser struct {
	errs []error
}

func (p *parser) int(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		p.errs = append(p.errs, fmt.Errorf("invalid value for %s: %q (expected integer)", key, v))
		return fallback
	}
	return n
}

func (p *parser) bool(key string, fallback bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		p.errs = append(p.errs, fmt.Errorf("invalid value for %s: %q (expected boolean)", key, v))
		return fallback
	}
	return b
}

func (p *parser) uint32(key string, fallback uint32) uint32 {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.ParseUint(v, 10, 32)
	if err != nil {
		p.errs = append(p.errs, fmt.Errorf("invalid value for %s: %q (expected unsigned 32-bit integer)", key, v))
		return fallback
	}
	return uint32(n)
}

func (p *parser) uint8(key string, fallback uint8) uint8 {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.ParseUint(v, 10, 8)
	if err != nil {
		p.errs = append(p.errs, fmt.Errorf("invalid value for %s: %q (expected unsigned 8-bit integer)", key, v))
		return fallback
	}
	return uint8(n)
}

func (p *parser) duration(key string, fallback time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		p.errs = append(p.errs, fmt.Errorf("invalid value for %s: %q (expected duration like \"24h\" or \"30m\")", key, v))
		return fallback
	}
	return d
}

func envStr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
