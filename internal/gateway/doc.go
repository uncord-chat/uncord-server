// Package gateway implements the WebSocket event system. The Hub manages client connections, subscribes to Valkey
// pub/sub for gateway events, and dispatches events to connected clients with permission-based filtering. Each user may
// have multiple concurrent connections (one per browser tab). The package also integrates presence tracking and typing
// indicators.
//
// The Publisher uses a bounded in-memory queue to decouple HTTP handlers from Valkey pub/sub latency. When the queue is
// full, new events are silently dropped (with a warning log) rather than applying back-pressure to callers. This is an
// intentional trade-off: in a chat system, momentary event loss under extreme load is preferable to blocking request
// handlers. Operators should monitor the "Gateway publish queue full" warning and tune the queue size and worker count
// if drops become frequent.
package gateway
