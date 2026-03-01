// Package valkey establishes connections to a Valkey or Redis instance. Connect parses the provided address, converting
// the valkey:// URI scheme to redis:// for go-redis compatibility, and verifies the connection with a ping.
package valkey
