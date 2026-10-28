package middleware

import (
	"log/slog"
	"time"

	"github.com/gofiber/fiber/v2"
)

// AccessLog emits one structured line per request. It deliberately logs no headers,
// bodies, query strings, tokens or credentials.
func AccessLog() fiber.Handler {
	return func(c *fiber.Ctx) error {
		start := time.Now()
		err := c.Next()

		path := c.Path()
		if path == "/health" || path == "/ready" {
			return err
		}

		status := c.Response().StatusCode()
		attributes := []any{
			"method", c.Method(),
			"path", path,
			"status", status,
			"duration_ms", time.Since(start).Milliseconds(),
			"request_id", RequestID(c),
			"ip", c.IP(),
		}
		if actor, ok := CurrentActor(c); ok {
			attributes = append(attributes, "user_id", actor.ID.String(), "role", string(actor.Role))
		}

		switch {
		case status >= fiber.StatusInternalServerError:
			slog.Error("HTTP request failed", attributes...)
		case status >= fiber.StatusBadRequest:
			slog.Warn("HTTP request rejected", attributes...)
		default:
			slog.Info("HTTP request", attributes...)
		}

		return err
	}
}
