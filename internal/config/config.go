package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
)

// Config holds application configuration populated from environment variables.
type Config struct {
	// Core
	ServerName        string
	ServerDescription string
	ServerURL         string
	ServerPort        int
	ServerEnv         string // "development" or "production"

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
}

// Load reads configuration from environment variables with defaults matching
// .env.example. It returns an error if any variable is set but cannot be parsed.
func Load() (Config, error) {
	p := &parser{}

	cfg := Config{
		ServerName:        envStr("SERVER_NAME", "My Community"),
		ServerDescription: envStr("SERVER_DESCRIPTION", ""),
		ServerURL:         envStr("SERVER_URL", "https://chat.example.com"),
		ServerPort:        p.int("SERVER_PORT", 8080),
		ServerEnv:         envStr("SERVER_ENV", "production"),

		DatabaseURL:     envStr("DATABASE_URL", "postgres://uncord:password@postgres:5432/uncord?sslmode=disable"),
		DatabaseMaxConn: p.int("DATABASE_MAX_CONNS", 25),
		DatabaseMinConn: p.int("DATABASE_MIN_CONNS", 5),

		ValkeyURL: envStr("VALKEY_URL", "valkey://valkey:6379/0"),

		Argon2Memory:      uint32(p.int("ARGON2_MEMORY", 65536)),
		Argon2Iterations:  uint32(p.int("ARGON2_ITERATIONS", 3)),
		Argon2Parallelism: uint8(p.int("ARGON2_PARALLELISM", 2)),
		Argon2SaltLength:  uint32(p.int("ARGON2_SALT_LENGTH", 16)),
		Argon2KeyLength:   uint32(p.int("ARGON2_KEY_LENGTH", 32)),

		TypesenseURL:    envStr("TYPESENSE_URL", "http://typesense:8108"),
		TypesenseAPIKey: envStr("TYPESENSE_API_KEY", "change-me-in-production"),

		InitOwnerEmail:    envStr("INIT_OWNER_EMAIL", ""),
		InitOwnerPassword: envStr("INIT_OWNER_PASSWORD", ""),

		OnboardingRequireRules:             p.bool("ONBOARDING_REQUIRE_RULES", true),
		OnboardingRequireEmailVerification: p.bool("ONBOARDING_REQUIRE_EMAIL_VERIFICATION", true),
		OnboardingMinAccountAge:            p.int("ONBOARDING_MIN_ACCOUNT_AGE", 0),
		OnboardingRequirePhone:             p.bool("ONBOARDING_REQUIRE_PHONE", false),
		OnboardingRequireCaptcha:           p.bool("ONBOARDING_REQUIRE_CAPTCHA", false),
	}

	return cfg, errors.Join(p.errs...)
}

// IsDevelopment returns true when running in development mode.
func (c Config) IsDevelopment() bool {
	return c.ServerEnv == "development"
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

func envStr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
