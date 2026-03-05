package api

import (
	"context"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/uncord-chat/uncord-server/internal/httputil"
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

// healthResponse is the JSON structure returned by the health endpoint, wrapped in the standard data envelope.
type healthResponse struct {
	Status   string `json:"status"`
	Postgres string `json:"postgres"`
	Valkey   string `json:"valkey"`
}

// Health pings PostgreSQL and Valkey, returning component status.
func (h *HealthHandler) Health(c fiber.Ctx) error {
	ctx, cancel := context.WithTimeout(c, 3*time.Second)
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
	if pgStatus != "ok" || vkStatus != "ok" {
		overall = "degraded"
	}

	resp := healthResponse{
		Status:   overall,
		Postgres: pgStatus,
		Valkey:   vkStatus,
	}

	if overall != "ok" {
		return httputil.SuccessStatus(c, fiber.StatusServiceUnavailable, resp)
	}
	return httputil.Success(c, resp)
}
