// Package permission implements a four-step permission resolver: owner bypass, role union, category overrides, and
// channel overrides. Resolved permissions are cached in Valkey with a 300-second TTL and invalidated via pub/sub when
// overrides change. RequirePermission provides Fiber middleware that checks channel or server level permissions before
// allowing a request to proceed.
package permission
