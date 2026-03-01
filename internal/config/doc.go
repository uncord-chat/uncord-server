// Package config loads application configuration from environment variables into a Config struct. Over 40 variables are
// parsed for database, Valkey, Typesense, JWT, Argon2, SMTP, and onboarding settings. The Secret type wraps sensitive
// values (passwords, API keys, DSNs) to prevent accidental logging. In development mode, SMTP settings are overridden
// to route email through Mailpit.
package config
