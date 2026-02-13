package httputil

import (
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/requestid"
	"github.com/rs/zerolog"
)

// RequestLogger returns Fiber middleware that logs every request through the provided zerolog logger. Paths passed in
// skipPaths are excluded from logging. It should be registered after the requestid middleware so that the request ID is
// available in Locals.
func RequestLogger(logger zerolog.Logger, skipPaths ...string) fiber.Handler {
	skip := make(map[string]struct{}, len(skipPaths))
	for _, p := range skipPaths {
		skip[p] = struct{}{}
	}

	return func(c fiber.Ctx) error {
		start := time.Now()
		err := c.Next()

		if _, ok := skip[c.Path()]; ok {
			return err
		}

		status := c.Response().StatusCode()
		event := levelForStatus(logger, status)

		if rid := requestid.FromContext(c); rid != "" {
			event.Str("request_id", rid)
		}

		event.
			Str("method", c.Method()).
			Str("path", c.Path()).
			Int("status", status).
			Str("latency", strings.ReplaceAll(time.Since(start).String(), "µ", "u")).
			Str("ip", c.IP()).
			Msg("Request")

		return err
	}
}

// levelForStatus selects the appropriate log level based on the HTTP status code: Error for 5xx, Warn for 4xx, and
// Info for everything else.
func levelForStatus(logger zerolog.Logger, status int) *zerolog.Event {
	switch {
	case status >= 500:
		return logger.Error()
	case status >= 400:
		return logger.Warn()
	default:
		return logger.Info()
	}
}
