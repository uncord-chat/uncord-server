// Package auth implements authentication and account lifecycle operations including registration, login, email
// verification, MFA, password management, and refresh token rotation. Passwords are hashed with Argon2id using
// configurable parameters. Refresh tokens are rotated atomically via Valkey Lua scripts; reuse of a revoked token is
// detected and treated as a security event. Validation failures are reported as sentinel errors that callers map to HTTP
// status codes.
package auth
