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

// minJWTSecretLength is the minimum number of characters required for the JWT signing secret.
const minJWTSecretLength = 32

const (
	envDevelopment      = "development"
	storageBackendLocal = "local"
	storageBackendS3    = "s3"
)

// Config holds application configuration populated from environment variables.
type Config struct {
	// Core
	ServerName           string
	ServerDescription    string
	ServerURL            string
	ServerPort           int
	ServerEnv            string // "development" or "production"
	LogHealthRequests    bool
	RequestTimeout       time.Duration // Maximum duration for REST request processing. Default: 30s.
	ShutdownTimeout      time.Duration // HTTP server shutdown timeout. Default: 15s.
	ShutdownGraceTimeout time.Duration // Maximum wait for background goroutines to stop after shutdown. Default: 10s.

	// Database
	DatabaseURL     Secret
	DatabaseMaxConn int
	DatabaseMinConn int

	// Valkey
	ValkeyURL         Secret
	ValkeyDialTimeout time.Duration

	// Argon2 password hashing
	Argon2Memory      uint32
	Argon2Iterations  uint32
	Argon2Parallelism uint8
	Argon2SaltLength  uint32
	Argon2KeyLength   uint32

	// JWT
	JWTSecret     Secret
	JWTAccessTTL  time.Duration
	JWTRefreshTTL time.Duration

	// Login attempt retention
	LoginAttemptRetention time.Duration // How long to retain login attempts. Default: 2160h (90 days).

	// Abuse / Disposable Email
	DisposableEmailBlocklistEnabled         bool
	DisposableEmailBlocklistURL             string
	DisposableEmailBlocklistRefreshInterval time.Duration
	DisposableEmailBlocklistTimeout         time.Duration

	// Typesense
	TypesenseURL     string
	TypesenseAPIKey  Secret
	TypesenseTimeout time.Duration

	// First-run owner
	InitOwnerEmail    string
	InitOwnerPassword Secret
	InitOwnerUsername string

	// Data directory
	DataDir string

	// Onboarding
	OnboardingOpenJoin                 bool
	OnboardingRequireEmailVerification bool
	OnboardingMinAccountAge            int
	OnboardingRequirePhone             bool
	OnboardingRequireCaptcha           bool

	// Gateway
	GatewayHeartbeatIntervalMS int           // Heartbeat interval sent to clients in the Hello frame. Default: 20000.
	GatewayOfflineDelayMS      int           // Grace period before broadcasting offline presence after disconnect. Default: 3000.
	GatewaySessionTTL          time.Duration // How long a disconnected session remains resumable. Default: 300s.
	GatewayReplayBufferSize    int           // Maximum number of events retained for session replay. Default: 1000.
	GatewayMaxConnections      int           // Maximum concurrent WebSocket connections. Default: 10000.
	GatewayReadyMemberLimit    int           // Maximum number of members included in the READY payload. Default: 1000.
	GatewayPublishWorkers      int           // Number of worker goroutines consuming the publish queue. Default: 4.
	GatewayPublishQueueSize    int           // Buffer size of the publish queue channel. Default: 1024.
	GatewayIdentifyTimeout     time.Duration // How long a client has to send Identify or Resume after connecting. Default: 30s.
	GatewayPublishTimeout      time.Duration // Per-publish timeout for Valkey operations. Default: 5s.

	// Rate Limiting
	RateLimitAPIRequests            int
	RateLimitAPIWindowSeconds       int
	RateLimitAuthCount              int
	RateLimitAuthWindowSeconds      int
	RateLimitWSCount                int // Maximum WebSocket messages per window. Default: 120.
	RateLimitWSWindowSeconds        int // WebSocket rate limit window in seconds. Default: 60.
	RateLimitMsgCount               int // Per-channel message rate limit per user. Default: 5.
	RateLimitMsgWindowSeconds       int // Per-channel message rate limit window in seconds. Default: 5.
	RateLimitMsgGlobalCount         int // Global message rate limit per user across all channels. Default: 30.
	RateLimitMsgGlobalWindowSeconds int // Global message rate limit window in seconds. Default: 60.

	// Upload Limits
	MaxUploadSizeMB    int
	MaxAvatarSizeMB    int
	MaxAvatarDimension int
	MaxBannerWidth     int
	MaxBannerHeight    int

	// Storage
	StorageBackend   string // "local" or "s3"
	StorageLocalPath string

	// Attachments
	MaxAttachmentsPerMessage int
	AttachmentOrphanTTL      time.Duration

	// Rate Limiting (Uploads)
	RateLimitUploadCount         int
	RateLimitUploadWindowSeconds int

	// Entity Limits
	MaxChannels      int
	MaxCategories    int
	MaxRoles         int
	MaxMessageLength int

	// Emoji
	MaxEmojiSizeKB    int // Maximum emoji file size in kilobytes. Default: 256.
	MaxEmojiPerServer int // Maximum number of custom emoji per server. Default: 200.

	// SMTP
	SMTPHost     string
	SMTPPort     int
	SMTPUsername string
	SMTPPassword Secret
	SMTPFrom     string

	// MFA
	MFAEncryptionKey Secret
	MFATicketTTL     time.Duration

	// Account Deletion
	ServerSecret               Secret        // Required. Hex-encoded 32-byte HMAC key for tombstones and future use.
	DeletionTombstoneUsernames bool          // Also tombstone usernames on deletion. Default: true.
	DeletionTombstoneRetention time.Duration // How long to retain deletion tombstones. 0 = permanent. Default: 0.

	// Data Retention
	DataCleanupInterval time.Duration // How often the retention cleanup goroutine runs. Default: 12h.

	// E2EE
	E2EEOPKLowThreshold   int // Number of remaining OPKs that triggers a KEY_BUNDLE_LOW event. Default: 10.
	E2EEMaxOPKBatch       int // Maximum one-time pre-keys per upload batch. Default: 100.
	E2EEMaxDevicesPerUser int // Maximum registered devices per user. Default: 5.

	// CORS
	CORSAllowOrigins string
}

// Load reads configuration from environment variables with defaults matching .env.example. It returns an error if any
// variable is set but cannot be parsed, or if required security values are missing.
func Load() (*Config, error) {
	p := &parser{}

	cfg := &Config{
		ServerName:           envStr("SERVER_NAME", "My Community"),
		ServerDescription:    envStr("SERVER_DESCRIPTION", ""),
		ServerURL:            envStr("SERVER_URL", "https://chat.example.com"),
		ServerPort:           p.int("SERVER_PORT", 8080),
		ServerEnv:            envStr("SERVER_ENV", "production"),
		LogHealthRequests:    p.bool("LOG_HEALTH_REQUESTS", true),
		RequestTimeout:       p.duration("REQUEST_TIMEOUT", 30*time.Second),
		ShutdownTimeout:      p.duration("SHUTDOWN_TIMEOUT", 15*time.Second),
		ShutdownGraceTimeout: p.duration("SHUTDOWN_GRACE_TIMEOUT", 10*time.Second),

		DatabaseURL:     NewSecret(envStr("DATABASE_URL", "postgres://uncord:password@postgres:5432/uncord?sslmode=disable")),
		DatabaseMaxConn: p.int("DATABASE_MAX_CONNS", 25),
		DatabaseMinConn: p.int("DATABASE_MIN_CONNS", 5),

		ValkeyURL:         NewSecret(envStr("VALKEY_URL", "valkey://valkey:6379/0")),
		ValkeyDialTimeout: p.duration("VALKEY_DIAL_TIMEOUT", 5*time.Second),

		Argon2Memory:      p.uint32("ARGON2_MEMORY", 65536),
		Argon2Iterations:  p.uint32("ARGON2_ITERATIONS", 3),
		Argon2Parallelism: p.uint8("ARGON2_PARALLELISM", 2),
		Argon2SaltLength:  p.uint32("ARGON2_SALT_LENGTH", 16),
		Argon2KeyLength:   p.uint32("ARGON2_KEY_LENGTH", 32),

		JWTSecret:             NewSecret(envStr("JWT_SECRET", "")),
		JWTAccessTTL:          p.duration("JWT_ACCESS_TTL", 15*time.Minute),
		JWTRefreshTTL:         p.duration("JWT_REFRESH_TTL", 7*24*time.Hour),
		LoginAttemptRetention: p.duration("LOGIN_ATTEMPT_RETENTION", 2160*time.Hour),

		DisposableEmailBlocklistEnabled:         p.bool("ABUSE_DISPOSABLE_EMAIL_BLOCKLIST_ENABLED", true),
		DisposableEmailBlocklistURL:             envStr("ABUSE_DISPOSABLE_EMAIL_BLOCKLIST_URL", "https://raw.githubusercontent.com/disposable-email-domains/disposable-email-domains/master/disposable_email_blocklist.conf"),
		DisposableEmailBlocklistRefreshInterval: p.duration("ABUSE_DISPOSABLE_EMAIL_BLOCKLIST_REFRESH_INTERVAL", 24*time.Hour),
		DisposableEmailBlocklistTimeout:         p.duration("ABUSE_DISPOSABLE_EMAIL_BLOCKLIST_TIMEOUT", 10*time.Second),

		TypesenseURL:     envStr("TYPESENSE_URL", "http://typesense:8108"),
		TypesenseAPIKey:  NewSecret(envStr("TYPESENSE_API_KEY", "change-me-in-production")),
		TypesenseTimeout: p.duration("TYPESENSE_TIMEOUT", 30*time.Second),

		InitOwnerEmail:    envStr("INIT_OWNER_EMAIL", ""),
		InitOwnerPassword: NewSecret(envStr("INIT_OWNER_PASSWORD", "")),
		InitOwnerUsername: envStr("INIT_OWNER_USERNAME", ""),

		DataDir: envStr("DATA_DIR", ""),

		OnboardingOpenJoin:                 p.bool("ONBOARDING_OPEN_JOIN", false),
		OnboardingRequireEmailVerification: p.bool("ONBOARDING_REQUIRE_EMAIL_VERIFICATION", true),
		OnboardingMinAccountAge:            p.int("ONBOARDING_MIN_ACCOUNT_AGE", 0),
		OnboardingRequirePhone:             p.bool("ONBOARDING_REQUIRE_PHONE", false),
		OnboardingRequireCaptcha:           p.bool("ONBOARDING_REQUIRE_CAPTCHA", false),

		GatewayHeartbeatIntervalMS: p.int("GATEWAY_HEARTBEAT_INTERVAL_MS", 20000),
		GatewayOfflineDelayMS:      p.int("GATEWAY_OFFLINE_DELAY_MS", 3000),
		GatewaySessionTTL:          time.Duration(p.int("GATEWAY_SESSION_TTL_SECONDS", 300)) * time.Second,
		GatewayReplayBufferSize:    p.int("GATEWAY_REPLAY_BUFFER_SIZE", 1000),
		GatewayMaxConnections:      p.int("GATEWAY_MAX_CONNECTIONS", 10000),
		GatewayReadyMemberLimit:    p.int("GATEWAY_READY_MEMBER_LIMIT", 1000),
		GatewayPublishWorkers:      p.int("GATEWAY_PUBLISH_WORKERS", 4),
		GatewayPublishQueueSize:    p.int("GATEWAY_PUBLISH_QUEUE_SIZE", 1024),
		GatewayIdentifyTimeout:     p.duration("GATEWAY_IDENTIFY_TIMEOUT", 30*time.Second),
		GatewayPublishTimeout:      p.duration("GATEWAY_PUBLISH_TIMEOUT", 5*time.Second),

		RateLimitAPIRequests:            p.int("RATE_LIMIT_API_REQUESTS", 60),
		RateLimitAPIWindowSeconds:       p.int("RATE_LIMIT_API_WINDOW_SECONDS", 60),
		RateLimitAuthCount:              p.int("RATE_LIMIT_AUTH_COUNT", 5),
		RateLimitAuthWindowSeconds:      p.int("RATE_LIMIT_AUTH_WINDOW_SECONDS", 300),
		RateLimitWSCount:                p.int("RATE_LIMIT_WS_COUNT", 120),
		RateLimitWSWindowSeconds:        p.int("RATE_LIMIT_WS_WINDOW_SECONDS", 60),
		RateLimitMsgCount:               p.int("RATE_LIMIT_MSG_COUNT", 5),
		RateLimitMsgWindowSeconds:       p.int("RATE_LIMIT_MSG_WINDOW_SECONDS", 5),
		RateLimitMsgGlobalCount:         p.int("RATE_LIMIT_MSG_GLOBAL_COUNT", 30),
		RateLimitMsgGlobalWindowSeconds: p.int("RATE_LIMIT_MSG_GLOBAL_WINDOW_SECONDS", 60),

		MaxUploadSizeMB:    p.int("MAX_UPLOAD_SIZE_MB", 100),
		MaxAvatarSizeMB:    p.int("MAX_AVATAR_SIZE_MB", 8),
		MaxAvatarDimension: p.int("MAX_AVATAR_DIMENSION", 1080),
		MaxBannerWidth:     p.int("MAX_BANNER_WIDTH", 1920),
		MaxBannerHeight:    p.int("MAX_BANNER_HEIGHT", 480),

		StorageBackend:   envStr("STORAGE_BACKEND", storageBackendLocal),
		StorageLocalPath: envStr("STORAGE_LOCAL_PATH", "/data/uncord/media"),

		MaxAttachmentsPerMessage: p.int("MAX_ATTACHMENTS_PER_MESSAGE", 10),
		AttachmentOrphanTTL:      p.duration("ATTACHMENT_ORPHAN_TTL", time.Hour),

		RateLimitUploadCount:         p.int("RATE_LIMIT_UPLOAD_COUNT", 10),
		RateLimitUploadWindowSeconds: p.int("RATE_LIMIT_UPLOAD_WINDOW_SECONDS", 60),

		MaxChannels:      p.int("MAX_CHANNELS", 500),
		MaxCategories:    p.int("MAX_CATEGORIES", 50),
		MaxRoles:         p.int("MAX_ROLES", 250),
		MaxMessageLength: p.int("MAX_MESSAGE_LENGTH", 4000),

		MaxEmojiSizeKB:    p.int("MAX_EMOJI_SIZE_KB", 256),
		MaxEmojiPerServer: p.int("MAX_EMOJI_PER_SERVER", 200),

		SMTPHost:     envStr("SMTP_HOST", ""),
		SMTPPort:     p.int("SMTP_PORT", 587),
		SMTPUsername: envStr("SMTP_USERNAME", ""),
		SMTPPassword: NewSecret(envStr("SMTP_PASSWORD", "")),
		SMTPFrom:     envStr("SMTP_FROM", "noreply@chat.example.com"),

		MFAEncryptionKey: NewSecret(envStr("MFA_ENCRYPTION_KEY", "")),
		MFATicketTTL:     p.duration("MFA_TICKET_TTL", 5*time.Minute),

		ServerSecret:               NewSecret(envStr("SERVER_SECRET", "")),
		DeletionTombstoneUsernames: p.bool("DELETION_TOMBSTONE_USERNAMES", true),
		DeletionTombstoneRetention: p.duration("DELETION_TOMBSTONE_RETENTION", 0),

		DataCleanupInterval: p.duration("DATA_CLEANUP_INTERVAL", 12*time.Hour),

		E2EEOPKLowThreshold:   p.int("E2EE_OPK_LOW_THRESHOLD", 10),
		E2EEMaxOPKBatch:       p.int("E2EE_MAX_OPK_BATCH", 100),
		E2EEMaxDevicesPerUser: p.int("E2EE_MAX_DEVICES_PER_USER", 5),

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
		cfg.SMTPPassword = NewSecret("")
		cfg.ServerURL = fmt.Sprintf("http://localhost:%d", cfg.ServerPort)
	}

	if err := cfg.validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

// IsDevelopment returns true when running in development mode.
func (c *Config) IsDevelopment() bool {
	return c.ServerEnv == envDevelopment
}

// SMTPConfigured returns true when an SMTP host is set, indicating that the server should attempt to send emails.
func (c *Config) SMTPConfigured() bool {
	return c.SMTPHost != ""
}

// MFAConfigured returns true when the MFA encryption key is set, indicating that TOTP-based MFA is available.
func (c *Config) MFAConfigured() bool {
	return c.MFAEncryptionKey.IsSet()
}

// BodyLimitBytes returns the maximum request body size in bytes, derived from MaxUploadSizeMB with a small margin for
// multipart framing overhead.
func (c *Config) BodyLimitBytes() int {
	return (c.MaxUploadSizeMB + 1) * 1024 * 1024
}

// BodyLimitJSONBytes returns the maximum body size in bytes for non-upload (JSON) requests. This is intentionally much
// smaller than BodyLimitBytes to prevent memory exhaustion from oversized payloads sent to regular API endpoints.
func (c *Config) BodyLimitJSONBytes() int {
	return 1024 * 1024 // 1 MiB
}

// MaxUploadSizeBytes returns the maximum file upload size in bytes.
func (c *Config) MaxUploadSizeBytes() int64 {
	return int64(c.MaxUploadSizeMB) * 1024 * 1024
}

// MaxAvatarSizeBytes returns the maximum avatar/banner upload size in bytes.
func (c *Config) MaxAvatarSizeBytes() int64 {
	return int64(c.MaxAvatarSizeMB) * 1024 * 1024
}

// MaxEmojiSizeBytes returns the maximum emoji file size in bytes.
func (c *Config) MaxEmojiSizeBytes() int64 {
	return int64(c.MaxEmojiSizeKB) * 1024
}

func (c *Config) validate() error {
	var errs []error

	if !c.JWTSecret.IsSet() {
		errs = append(errs, fmt.Errorf("JWT_SECRET is required"))
	} else if len(c.JWTSecret.Expose()) < minJWTSecretLength {
		errs = append(errs, fmt.Errorf("JWT_SECRET must be at least %d characters", minJWTSecretLength))
	}

	if c.ServerPort < 1 || c.ServerPort > 65535 {
		errs = append(errs, fmt.Errorf("SERVER_PORT must be between 1 and 65535"))
	}
	if c.RequestTimeout < time.Second {
		errs = append(errs, fmt.Errorf("REQUEST_TIMEOUT must be at least 1s"))
	}
	if c.ShutdownTimeout < time.Second {
		errs = append(errs, fmt.Errorf("SHUTDOWN_TIMEOUT must be at least 1s"))
	}
	if c.ShutdownGraceTimeout < time.Second {
		errs = append(errs, fmt.Errorf("SHUTDOWN_GRACE_TIMEOUT must be at least 1s"))
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

	if c.Argon2Memory < 15360 {
		errs = append(errs, fmt.Errorf("ARGON2_MEMORY must be at least 15360 (15 MiB)"))
	}
	if c.Argon2Iterations < 2 {
		errs = append(errs, fmt.Errorf("ARGON2_ITERATIONS must be at least 2"))
	}
	if c.Argon2Parallelism == 0 {
		errs = append(errs, fmt.Errorf("ARGON2_PARALLELISM must be greater than 0"))
	}

	if c.MaxUploadSizeMB < 1 {
		errs = append(errs, fmt.Errorf("MAX_UPLOAD_SIZE_MB must be at least 1"))
	}
	if c.MaxAvatarSizeMB < 1 {
		errs = append(errs, fmt.Errorf("MAX_AVATAR_SIZE_MB must be at least 1"))
	}
	if c.MaxAvatarDimension < 1 {
		errs = append(errs, fmt.Errorf("MAX_AVATAR_DIMENSION must be at least 1"))
	}
	if c.MaxBannerWidth < 1 {
		errs = append(errs, fmt.Errorf("MAX_BANNER_WIDTH must be at least 1"))
	}
	if c.MaxBannerHeight < 1 {
		errs = append(errs, fmt.Errorf("MAX_BANNER_HEIGHT must be at least 1"))
	}

	if c.StorageBackend != storageBackendLocal && c.StorageBackend != storageBackendS3 {
		errs = append(errs, fmt.Errorf("STORAGE_BACKEND must be \"local\" or \"s3\""))
	}
	if c.StorageBackend == storageBackendLocal && c.StorageLocalPath == "" {
		errs = append(errs, fmt.Errorf("STORAGE_LOCAL_PATH is required when STORAGE_BACKEND is \"local\""))
	}

	if c.MaxAttachmentsPerMessage < 1 {
		errs = append(errs, fmt.Errorf("MAX_ATTACHMENTS_PER_MESSAGE must be at least 1"))
	}
	if c.AttachmentOrphanTTL < time.Minute {
		errs = append(errs, fmt.Errorf("ATTACHMENT_ORPHAN_TTL must be at least 1m"))
	}

	if c.RateLimitUploadCount < 1 {
		errs = append(errs, fmt.Errorf("RATE_LIMIT_UPLOAD_COUNT must be at least 1"))
	}
	if c.RateLimitUploadWindowSeconds < 1 {
		errs = append(errs, fmt.Errorf("RATE_LIMIT_UPLOAD_WINDOW_SECONDS must be at least 1"))
	}

	if c.ServerEnv != envDevelopment && c.TypesenseAPIKey.Expose() == "change-me-in-production" {
		errs = append(errs, fmt.Errorf("TYPESENSE_API_KEY must be changed from the default value in production"))
	}

	if c.MaxChannels < 1 {
		errs = append(errs, fmt.Errorf("MAX_CHANNELS must be at least 1"))
	}
	if c.MaxCategories < 1 {
		errs = append(errs, fmt.Errorf("MAX_CATEGORIES must be at least 1"))
	}
	if c.MaxRoles < 1 {
		errs = append(errs, fmt.Errorf("MAX_ROLES must be at least 1"))
	}
	if c.MaxMessageLength < 1 {
		errs = append(errs, fmt.Errorf("MAX_MESSAGE_LENGTH must be at least 1"))
	}
	if c.MaxEmojiSizeKB < 1 {
		errs = append(errs, fmt.Errorf("MAX_EMOJI_SIZE_KB must be at least 1"))
	}
	if c.MaxEmojiPerServer < 1 {
		errs = append(errs, fmt.Errorf("MAX_EMOJI_PER_SERVER must be at least 1"))
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
	if c.RateLimitWSCount < 1 {
		errs = append(errs, fmt.Errorf("RATE_LIMIT_WS_COUNT must be at least 1"))
	}
	if c.RateLimitWSWindowSeconds < 1 {
		errs = append(errs, fmt.Errorf("RATE_LIMIT_WS_WINDOW_SECONDS must be at least 1"))
	}
	if c.RateLimitMsgCount < 1 {
		errs = append(errs, fmt.Errorf("RATE_LIMIT_MSG_COUNT must be at least 1"))
	}
	if c.RateLimitMsgWindowSeconds < 1 {
		errs = append(errs, fmt.Errorf("RATE_LIMIT_MSG_WINDOW_SECONDS must be at least 1"))
	}
	if c.RateLimitMsgGlobalCount < 1 {
		errs = append(errs, fmt.Errorf("RATE_LIMIT_MSG_GLOBAL_COUNT must be at least 1"))
	}
	if c.RateLimitMsgGlobalWindowSeconds < 1 {
		errs = append(errs, fmt.Errorf("RATE_LIMIT_MSG_GLOBAL_WINDOW_SECONDS must be at least 1"))
	}

	if c.GatewayHeartbeatIntervalMS < 1000 {
		errs = append(errs, fmt.Errorf("GATEWAY_HEARTBEAT_INTERVAL_MS must be at least 1000"))
	}
	if c.GatewayOfflineDelayMS < 1000 {
		errs = append(errs, fmt.Errorf("GATEWAY_OFFLINE_DELAY_MS must be at least 1000"))
	}
	if c.GatewaySessionTTL < 10*time.Second {
		errs = append(errs, fmt.Errorf("GATEWAY_SESSION_TTL_SECONDS must be at least 10"))
	}
	if c.GatewayReplayBufferSize < 1 {
		errs = append(errs, fmt.Errorf("GATEWAY_REPLAY_BUFFER_SIZE must be at least 1"))
	}
	if c.GatewayMaxConnections < 1 {
		errs = append(errs, fmt.Errorf("GATEWAY_MAX_CONNECTIONS must be at least 1"))
	}
	if c.GatewayReadyMemberLimit < 1 {
		errs = append(errs, fmt.Errorf("GATEWAY_READY_MEMBER_LIMIT must be at least 1"))
	}
	if c.GatewayPublishWorkers < 1 {
		errs = append(errs, fmt.Errorf("GATEWAY_PUBLISH_WORKERS must be at least 1"))
	}
	if c.GatewayPublishQueueSize < 1 {
		errs = append(errs, fmt.Errorf("GATEWAY_PUBLISH_QUEUE_SIZE must be at least 1"))
	}
	if c.GatewayIdentifyTimeout < 5*time.Second {
		errs = append(errs, fmt.Errorf("GATEWAY_IDENTIFY_TIMEOUT must be at least 5s"))
	}
	if c.GatewayPublishTimeout < time.Second {
		errs = append(errs, fmt.Errorf("GATEWAY_PUBLISH_TIMEOUT must be at least 1s"))
	}

	if c.MFAEncryptionKey.IsSet() {
		if err := validHexSecret(c.MFAEncryptionKey); err != nil {
			errs = append(errs, fmt.Errorf("MFA_ENCRYPTION_KEY %w", err))
		}
	}

	if !c.ServerSecret.IsSet() {
		errs = append(errs, fmt.Errorf("SERVER_SECRET is required"))
	} else if err := validHexSecret(c.ServerSecret); err != nil {
		errs = append(errs, fmt.Errorf("SERVER_SECRET %w", err))
	}

	if c.MFATicketTTL < time.Second {
		errs = append(errs, fmt.Errorf("MFA_TICKET_TTL must be at least 1s"))
	}

	if c.LoginAttemptRetention < time.Hour {
		errs = append(errs, fmt.Errorf("LOGIN_ATTEMPT_RETENTION must be at least 1h"))
	}
	if c.DeletionTombstoneRetention < 0 {
		errs = append(errs, fmt.Errorf("DELETION_TOMBSTONE_RETENTION must not be negative"))
	}
	if c.DataCleanupInterval < time.Minute {
		errs = append(errs, fmt.Errorf("DATA_CLEANUP_INTERVAL must be at least 1m"))
	}

	if c.E2EEOPKLowThreshold < 1 {
		errs = append(errs, fmt.Errorf("E2EE_OPK_LOW_THRESHOLD must be at least 1"))
	}
	if c.E2EEMaxOPKBatch < 1 {
		errs = append(errs, fmt.Errorf("E2EE_MAX_OPK_BATCH must be at least 1"))
	}
	if c.E2EEMaxDevicesPerUser < 1 {
		errs = append(errs, fmt.Errorf("E2EE_MAX_DEVICES_PER_USER must be at least 1"))
	}

	if c.CORSAllowOrigins == "*" && !c.IsDevelopment() {
		errs = append(errs, fmt.Errorf("CORS_ALLOW_ORIGINS must specify explicit origins in production"))
	}

	if c.SMTPHost != "" {
		if c.SMTPPort < 1 || c.SMTPPort > 65535 {
			errs = append(errs, fmt.Errorf("SMTP_PORT must be between 1 and 65535"))
		}
		if _, err := mail.ParseAddress(c.SMTPFrom); err != nil {
			errs = append(errs, fmt.Errorf("SMTP_FROM is not a valid email address: %q", c.SMTPFrom))
		}
		hasUser := c.SMTPUsername != ""
		hasPass := c.SMTPPassword.Expose() != ""
		if hasUser != hasPass {
			errs = append(errs, fmt.Errorf("SMTP_USERNAME and SMTP_PASSWORD must both be set or both be empty"))
		}
	}

	return errors.Join(errs...)
}

// validHexSecret checks that s decodes to exactly 32 bytes of hex.
func validHexSecret(s Secret) error {
	b, err := hex.DecodeString(s.Expose())
	if err != nil || len(b) != 32 {
		return fmt.Errorf("must be exactly 64 hex characters (32 bytes)")
	}
	return nil
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
