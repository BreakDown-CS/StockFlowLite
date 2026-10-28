package handler

import (
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/BreakDown-CS/StockFlowLite/internal/domain"
	"github.com/BreakDown-CS/StockFlowLite/pkg/response"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

// AuditHandler exposes the read-only audit trail (ADMIN only).
type AuditHandler struct {
	repo domain.AuditRepository
}

// NewAuditHandler constructs an AuditHandler.
func NewAuditHandler(repo domain.AuditRepository) *AuditHandler {
	return &AuditHandler{repo: repo}
}

// List handles GET /api/v1/audit-events
func (h *AuditHandler) List(c *fiber.Ctx) error {
	limit := 20
	if limitStr := c.Query("limit"); limitStr != "" {
		parsed, err := strconv.Atoi(limitStr)
		if err != nil || parsed <= 0 {
			return response.Error(c, fiber.StatusBadRequest, "INVALID_PARAMETER", "limit must be a positive integer")
		}
		limit = parsed
	}
	if limit > 200 {
		limit = 200
	}

	offset := 0
	if offsetStr := c.Query("offset"); offsetStr != "" {
		parsed, err := strconv.Atoi(offsetStr)
		if err != nil || parsed < 0 {
			return response.Error(c, fiber.StatusBadRequest, "INVALID_PARAMETER", "offset must be a non-negative integer")
		}
		offset = parsed
	}

	var eventType *domain.AuditEventType
	if raw := strings.TrimSpace(c.Query("event_type")); raw != "" {
		candidate := domain.AuditEventType(strings.ToUpper(raw))
		valid := false
		for _, known := range domain.ValidAuditEventTypes {
			if candidate == known {
				valid = true
				break
			}
		}
		if !valid {
			return response.Error(c, fiber.StatusBadRequest, "INVALID_PARAMETER", "unknown event_type")
		}
		eventType = &candidate
	}

	var entityID *uuid.UUID
	if raw := strings.TrimSpace(c.Query("entity_id")); raw != "" {
		parsed, err := uuid.Parse(raw)
		if err != nil {
			return response.Error(c, fiber.StatusBadRequest, "INVALID_PARAMETER", "entity_id must be a UUID")
		}
		entityID = &parsed
	}

	var actorID *uuid.UUID
	if raw := strings.TrimSpace(c.Query("actor_id")); raw != "" {
		parsed, err := uuid.Parse(raw)
		if err != nil {
			return response.Error(c, fiber.StatusBadRequest, "INVALID_PARAMETER", "actor_id must be a UUID")
		}
		actorID = &parsed
	}

	var from, to *time.Time
	if raw := strings.TrimSpace(c.Query("from")); raw != "" {
		parsed, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return response.Error(c, fiber.StatusBadRequest, "INVALID_PARAMETER", "from must be an RFC3339 timestamp")
		}
		from = &parsed
	}
	if raw := strings.TrimSpace(c.Query("to")); raw != "" {
		parsed, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return response.Error(c, fiber.StatusBadRequest, "INVALID_PARAMETER", "to must be an RFC3339 timestamp")
		}
		to = &parsed
	}

	events, total, err := h.repo.List(c.UserContext(), domain.ListAuditFilter{
		EventType:  eventType,
		EntityType: strings.TrimSpace(c.Query("entity_type")),
		EntityID:   entityID,
		ActorID:    actorID,
		From:       from,
		To:         to,
		Limit:      limit,
		Offset:     offset,
	})
	if err != nil {
		slog.Error("Failed to list audit events", "error", err)
		return response.Error(c, fiber.StatusInternalServerError, "INTERNAL_ERROR", "Failed to retrieve audit events")
	}

	return response.SuccessWithMeta(c, events, response.PageMeta{Limit: limit, Offset: offset, Total: total})
}
