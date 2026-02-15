package gateway

import "errors"

// Custom WebSocket close codes used by the gateway protocol. Standard codes (1000, 1001) are defined by RFC 6455; the
// 4000 range is reserved for application use.
const (
	CloseUnknownError         = 4000
	CloseUnknownOpcode        = 4001
	CloseDecodeError          = 4002
	CloseNotAuthenticated     = 4003
	CloseAuthFailed           = 4004
	CloseAlreadyAuthenticated = 4005
	CloseInvalidSequence      = 4007
	CloseRateLimited          = 4008
	CloseSessionTimedOut      = 4009
)

// Sentinel errors for gateway failure modes. Each maps to a close code above.
var (
	ErrNotAuthenticated     = errors.New("connection is not authenticated")
	ErrAlreadyAuthenticated = errors.New("connection is already authenticated")
	ErrAuthenticationFailed = errors.New("authentication failed")
	ErrInvalidSequence      = errors.New("invalid resume sequence")
	ErrSessionTimedOut      = errors.New("session timed out")
	ErrSessionNotFound      = errors.New("session not found or expired")
	ErrRateLimited          = errors.New("rate limit exceeded")
	ErrUnknownOpcode        = errors.New("unknown opcode")
	ErrDecodeError          = errors.New("payload decode error")
	ErrMaxConnections       = errors.New("maximum connections reached")
)
