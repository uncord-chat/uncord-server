// Package disposable maintains a blocklist of disposable email domains fetched from a remote source. The blocklist is
// loaded lazily on first use and refreshed periodically in the background. All lookups are thread-safe. When the
// blocklist is disabled by configuration, IsBlocked returns false immediately.
package disposable
