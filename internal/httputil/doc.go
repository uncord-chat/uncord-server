// Package httputil provides shared JSON response helpers for HTTP handlers. Success wraps a payload in a {"data": ...}
// envelope. Fail constructs an error response with a typed error code from the protocol errors package and a
// human-readable message. These helpers ensure a consistent response shape across all API endpoints.
package httputil
