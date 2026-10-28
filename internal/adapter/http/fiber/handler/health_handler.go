package handler

import (
	"context"
	"time"

	"github.com/BreakDown-CS/StockFlowLite/internal/infrastructure/database"
	"github.com/gofiber/fiber/v2"
)

// HealthHandler manages liveness and readiness HTTP endpoints.
type HealthHandler struct {
	pinger        database.Pinger
	healthTimeout time.Duration
}

// NewHealthHandler constructs a HealthHandler with dependencies.
func NewHealthHandler(pinger database.Pinger, healthTimeout time.Duration) *HealthHandler {
	if healthTimeout <= 0 {
		healthTimeout = 3 * time.Second
	}
	return &HealthHandler{
		pinger:        pinger,
		healthTimeout: healthTimeout,
	}
}

// HealthResponse represents the standard liveness JSON payload.
type HealthResponse struct {
	Status  string `json:"status"`
	Service string `json:"service"`
}

// ReadyResponse represents the standard readiness JSON payload.
type ReadyResponse struct {
	Status   string `json:"status"`
	Database string `json:"database"`
}

// Health handles GET /health (Liveness).
// It verifies that the HTTP server process is running and responding.
// Does NOT depend on database reachability.
func (h *HealthHandler) Health(c *fiber.Ctx) error {
	return c.Status(fiber.StatusOK).JSON(HealthResponse{
		Status:  "ok",
		Service: "stockflow-lite",
	})
}

// Ready handles GET /ready (Readiness).
// It performs a bounded connectivity check against the PostgreSQL database.
// Returns 200 OK when database is reachable, 503 Service Unavailable when unreachable.
// Does NOT leak internal errors or credentials.
func (h *HealthHandler) Ready(c *fiber.Ctx) error {
	ctx := c.UserContext()
	if ctx == nil {
		ctx = context.Background()
	}

	err := database.CheckReadiness(ctx, h.pinger, h.healthTimeout)
	if err != nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(ReadyResponse{
			Status:   "unavailable",
			Database: "down",
		})
	}

	return c.Status(fiber.StatusOK).JSON(ReadyResponse{
		Status:   "ready",
		Database: "up",
	})
}
