package api

import (
	"context"
	"time"

	"github.com/gofiber/fiber/v2"
)

// Pinger checks connectivity to a backing service.
type Pinger interface {
	Ping(ctx context.Context) error
}

// HealthHandler serves the health check endpoint.
type HealthHandler struct {
	db     Pinger
	valkey Pinger
}

// NewHealthHandler creates a new health check handler.
func NewHealthHandler(db, valkey Pinger) *HealthHandler {
	return &HealthHandler{db: db, valkey: valkey}
}

// healthResponse is the JSON structure returned by the health endpoint. The response is not wrapped in the standard
// success/error envelope so that monitoring systems receive a simple, predictable JSON body.
type healthResponse struct {
	Status   string `json:"status"`
	Postgres string `json:"postgres"`
	Valkey   string `json:"valkey"`
}

// Health pings PostgreSQL and Valkey, returning component status.
func (h *HealthHandler) Health(c *fiber.Ctx) error {
	ctx, cancel := context.WithTimeout(c.Context(), 3*time.Second)
	defer cancel()

	pgStatus := "ok"
	if err := h.db.Ping(ctx); err != nil {
		pgStatus = "unavailable"
	}

	vkStatus := "ok"
	if err := h.valkey.Ping(ctx); err != nil {
		vkStatus = "unavailable"
	}

	overall := "ok"
	status := fiber.StatusOK
	if pgStatus != "ok" || vkStatus != "ok" {
		overall = "degraded"
		status = fiber.StatusServiceUnavailable
	}

	return c.Status(status).JSON(healthResponse{
		Status:   overall,
		Postgres: pgStatus,
		Valkey:   vkStatus,
	})
}
