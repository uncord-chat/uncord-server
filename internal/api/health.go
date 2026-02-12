package api

import (
	"context"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

// HealthHandler serves the health check endpoint.
type HealthHandler struct {
	DB    *pgxpool.Pool
	Redis *redis.Client
}

// Health pings PostgreSQL and Valkey, returning component status.
func (h *HealthHandler) Health(c *fiber.Ctx) error {
	ctx, cancel := context.WithTimeout(c.Context(), 3*time.Second)
	defer cancel()

	pgStatus := "ok"
	if err := h.DB.Ping(ctx); err != nil {
		pgStatus = "unavailable"
	}

	vkStatus := "ok"
	if err := h.Redis.Ping(ctx).Err(); err != nil {
		vkStatus = "unavailable"
	}

	overall := "ok"
	status := fiber.StatusOK
	if pgStatus != "ok" || vkStatus != "ok" {
		overall = "degraded"
		status = fiber.StatusServiceUnavailable
	}

	return SuccessStatus(c, status, fiber.Map{
		"status":   overall,
		"postgres": pgStatus,
		"valkey":   vkStatus,
	})
}
