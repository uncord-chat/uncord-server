// Package gateway implements the WebSocket event system. The Hub manages client connections, subscribes to Valkey
// pub/sub for gateway events, and dispatches events to connected clients with permission-based filtering. Each user may
// have multiple concurrent connections (one per browser tab). The package also integrates presence tracking and typing
// indicators.
package gateway
