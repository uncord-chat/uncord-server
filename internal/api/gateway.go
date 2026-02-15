package api

import (
	"github.com/gofiber/contrib/v3/websocket"
	"github.com/gofiber/fiber/v3"

	"github.com/uncord-chat/uncord-server/internal/gateway"
)

// GatewayHandler serves the WebSocket upgrade endpoint for the real-time gateway.
type GatewayHandler struct {
	hub *gateway.Hub
}

// NewGatewayHandler creates a new gateway handler.
func NewGatewayHandler(hub *gateway.Hub) *GatewayHandler {
	return &GatewayHandler{hub: hub}
}

// Upgrade handles GET /api/v1/gateway. It upgrades the HTTP connection to a WebSocket and hands it to the Hub.
func (h *GatewayHandler) Upgrade(c fiber.Ctx) error {
	if !websocket.IsWebSocketUpgrade(c) {
		return fiber.ErrUpgradeRequired
	}
	return websocket.New(func(conn *websocket.Conn) {
		h.hub.ServeWebSocket(conn.Conn)
	})(c)
}
