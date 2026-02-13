package main

import (
	"testing"

	"github.com/gofiber/fiber/v3"
	apierrors "github.com/uncord-chat/uncord-protocol/errors"
)

func TestFiberStatusToAPICode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		status int
		want   apierrors.Code
	}{
		{"not found", fiber.StatusNotFound, apierrors.NotFound},
		{"method not allowed", fiber.StatusMethodNotAllowed, apierrors.ValidationError},
		{"too many requests", fiber.StatusTooManyRequests, apierrors.RateLimited},
		{"request entity too large", fiber.StatusRequestEntityTooLarge, apierrors.PayloadTooLarge},
		{"service unavailable", fiber.StatusServiceUnavailable, apierrors.ServiceUnavailable},
		{"generic 4xx falls back to validation error", fiber.StatusConflict, apierrors.ValidationError},
		{"another 4xx", fiber.StatusGone, apierrors.ValidationError},
		{"5xx falls back to internal error", fiber.StatusInternalServerError, apierrors.InternalError},
		{"502 falls back to internal error", fiber.StatusBadGateway, apierrors.InternalError},
		{"unknown status falls back to internal error", 600, apierrors.InternalError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := fiberStatusToAPICode(tt.status)
			if got != tt.want {
				t.Errorf("fiberStatusToAPICode(%d) = %q, want %q", tt.status, got, tt.want)
			}
		})
	}
}
