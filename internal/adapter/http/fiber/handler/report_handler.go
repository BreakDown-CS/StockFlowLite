package handler

import (
	"errors"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/BreakDown-CS/StockFlowLite/internal/domain"
	reportuc "github.com/BreakDown-CS/StockFlowLite/internal/usecase/report"
	"github.com/BreakDown-CS/StockFlowLite/pkg/response"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

// ReportHandler exposes the read-only reporting endpoints.
type ReportHandler struct {
	usecase reportuc.Usecase
}

// NewReportHandler constructs a ReportHandler.
func NewReportHandler(usecase reportuc.Usecase) *ReportHandler {
	return &ReportHandler{usecase: usecase}
}

// Inventory handles GET /api/v1/reports/inventory
func (h *ReportHandler) Inventory(c *fiber.Ctx) error {
	limit, offset, err := parseReportPagination(c)
	if err != nil {
		return response.Error(c, fiber.StatusBadRequest, "INVALID_PARAMETER", err.Error())
	}

	// Default mirrors GET /products: active products only.
	active := true
	isActive := &active
	if raw := strings.TrimSpace(c.Query("is_active")); raw != "" {
		switch strings.ToLower(raw) {
		case "true":
			isActive = &active
		case "false":
			inactive := false
			isActive = &inactive
		case "all":
			isActive = nil
		default:
			return response.Error(c, fiber.StatusBadRequest, "INVALID_PARAMETER", "is_active must be true, false, or all")
		}
	}

	lowStock := false
	if raw := strings.TrimSpace(c.Query("low_stock")); raw != "" {
		switch strings.ToLower(raw) {
		case "true":
			lowStock = true
		case "false":
			lowStock = false
		default:
			return response.Error(c, fiber.StatusBadRequest, "INVALID_PARAMETER", "low_stock must be true or false")
		}
	}

	result, total, err := h.usecase.Inventory(c.UserContext(), reportuc.InventoryInput{
		Search:   c.Query("search"),
		IsActive: isActive,
		LowStock: lowStock,
		Limit:    limit,
		Offset:   offset,
	})
	if err != nil {
		return respondReportError(c, err)
	}

	return response.SuccessWithMeta(c, result, response.PageMeta{Limit: limit, Offset: offset, Total: total})
}

// Movements handles GET /api/v1/reports/stock-movements
func (h *ReportHandler) Movements(c *fiber.Ctx) error {
	limit, offset, err := parseReportPagination(c)
	if err != nil {
		return response.Error(c, fiber.StatusBadRequest, "INVALID_PARAMETER", err.Error())
	}

	var productID *uuid.UUID
	if raw := strings.TrimSpace(c.Query("product_id")); raw != "" {
		parsed, parseErr := uuid.Parse(raw)
		if parseErr != nil {
			return response.Error(c, fiber.StatusBadRequest, "INVALID_PARAMETER", "product_id must be a UUID")
		}
		productID = &parsed
	}

	var movementType *domain.MovementType
	if raw := strings.TrimSpace(c.Query("movement_type")); raw != "" {
		parsed := domain.MovementType(strings.ToUpper(raw))
		movementType = &parsed
	}

	from, err := parseReportTimestamp(c.Query("from"))
	if err != nil {
		return response.Error(c, fiber.StatusBadRequest, "INVALID_PARAMETER", "from must be an RFC3339 timestamp")
	}
	to, err := parseReportTimestamp(c.Query("to"))
	if err != nil {
		return response.Error(c, fiber.StatusBadRequest, "INVALID_PARAMETER", "to must be an RFC3339 timestamp")
	}

	result, total, err := h.usecase.Movements(c.UserContext(), reportuc.MovementInput{
		ProductID:    productID,
		MovementType: movementType,
		From:         from,
		To:           to,
		Limit:        limit,
		Offset:       offset,
	})
	if err != nil {
		return respondReportError(c, err)
	}

	return response.SuccessWithMeta(c, result, response.PageMeta{Limit: limit, Offset: offset, Total: total})
}

// Requests handles GET /api/v1/reports/stock-requests
func (h *ReportHandler) Requests(c *fiber.Ctx) error {
	limit, offset, err := parseReportPagination(c)
	if err != nil {
		return response.Error(c, fiber.StatusBadRequest, "INVALID_PARAMETER", err.Error())
	}

	var status *domain.RequestStatus
	if raw := strings.TrimSpace(c.Query("status")); raw != "" {
		parsed := domain.RequestStatus(strings.ToUpper(raw))
		status = &parsed
	}

	var requesterID *uuid.UUID
	if raw := strings.TrimSpace(c.Query("requester_id")); raw != "" {
		parsed, parseErr := uuid.Parse(raw)
		if parseErr != nil {
			return response.Error(c, fiber.StatusBadRequest, "INVALID_PARAMETER", "requester_id must be a UUID")
		}
		requesterID = &parsed
	}

	from, err := parseReportTimestamp(c.Query("from"))
	if err != nil {
		return response.Error(c, fiber.StatusBadRequest, "INVALID_PARAMETER", "from must be an RFC3339 timestamp")
	}
	to, err := parseReportTimestamp(c.Query("to"))
	if err != nil {
		return response.Error(c, fiber.StatusBadRequest, "INVALID_PARAMETER", "to must be an RFC3339 timestamp")
	}

	// The use case forces requesters to their own scope regardless of requester_id.
	result, total, err := h.usecase.Requests(c.UserContext(), reportuc.RequestInput{
		Status:      status,
		RequesterID: requesterID,
		From:        from,
		To:          to,
		Limit:       limit,
		Offset:      offset,
	})
	if err != nil {
		return respondReportError(c, err)
	}

	return response.SuccessWithMeta(c, result, response.PageMeta{Limit: limit, Offset: offset, Total: total})
}

// Integrity handles GET /api/v1/reports/integrity/stock
// It compares products.current_stock with the movement ledger and never repairs data.
func (h *ReportHandler) Integrity(c *fiber.Ctx) error {
	limit, offset, err := parseReportPagination(c)
	if err != nil {
		return response.Error(c, fiber.StatusBadRequest, "INVALID_PARAMETER", err.Error())
	}

	onlyDrifted := false
	if raw := strings.TrimSpace(c.Query("only_drifted")); raw != "" {
		switch strings.ToLower(raw) {
		case "true":
			onlyDrifted = true
		case "false":
			onlyDrifted = false
		default:
			return response.Error(c, fiber.StatusBadRequest, "INVALID_PARAMETER", "only_drifted must be true or false")
		}
	}

	result, total, err := h.usecase.StockIntegrity(c.UserContext(), reportuc.StockIntegrityInput{
		OnlyDrifted: onlyDrifted,
		Limit:       limit,
		Offset:      offset,
	})
	if err != nil {
		return respondReportError(c, err)
	}

	return response.SuccessWithMeta(c, result, response.PageMeta{Limit: limit, Offset: offset, Total: total})
}

// parseReportPagination parses and bounds limit/offset for report endpoints.
func parseReportPagination(c *fiber.Ctx) (int, int, error) {
	limit := reportuc.DefaultPageSize
	if raw := strings.TrimSpace(c.Query("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			return 0, 0, errors.New("limit must be a positive integer")
		}
		limit = parsed
	}
	if limit > reportuc.MaxPageSize {
		limit = reportuc.MaxPageSize
	}

	offset := 0
	if raw := strings.TrimSpace(c.Query("offset")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 0 {
			return 0, 0, errors.New("offset must be a non-negative integer")
		}
		offset = parsed
	}

	return limit, offset, nil
}

// parseReportTimestamp parses an optional RFC3339 timestamp into UTC.
func parseReportTimestamp(raw string) (*time.Time, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, nil
	}

	parsed, err := time.Parse(time.RFC3339, trimmed)
	if err != nil {
		return nil, err
	}

	utc := parsed.UTC()
	return &utc, nil
}

// respondReportError maps report failures onto the established response envelope.
func respondReportError(c *fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, domain.ErrUnauthenticated), errors.Is(err, domain.ErrUserInactive):
		return response.Error(c, fiber.StatusUnauthorized, "UNAUTHORIZED", "Authentication required")
	case errors.Is(err, domain.ErrInvalidDateRange):
		return response.Error(c, fiber.StatusBadRequest, "INVALID_PARAMETER", domain.ErrInvalidDateRange.Error())
	case errors.Is(err, domain.ErrInvalidMovementType):
		return response.Error(c, fiber.StatusBadRequest, "INVALID_PARAMETER", domain.ErrInvalidMovementType.Error())
	case errors.Is(err, domain.ErrInvalidStatus):
		return response.Error(c, fiber.StatusBadRequest, "INVALID_PARAMETER", domain.ErrInvalidStatus.Error())
	default:
		slog.Error("Report operation failed", "error", err)
		return response.Error(c, fiber.StatusInternalServerError, "INTERNAL_ERROR", "Failed to generate report")
	}
}
