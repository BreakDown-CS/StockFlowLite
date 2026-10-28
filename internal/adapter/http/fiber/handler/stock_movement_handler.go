package handler

import (
	"errors"
	"log/slog"
	"strconv"
	"strings"

	"github.com/BreakDown-CS/StockFlowLite/internal/domain"
	"github.com/BreakDown-CS/StockFlowLite/internal/usecase/stockmovement"
	"github.com/BreakDown-CS/StockFlowLite/pkg/response"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

// StockMovementHandler manages HTTP endpoints for reading movement history.
type StockMovementHandler struct {
	usecase stockmovement.StockMovementUsecase
}

// NewStockMovementHandler constructs a new StockMovementHandler.
func NewStockMovementHandler(usecase stockmovement.StockMovementUsecase) *StockMovementHandler {
	return &StockMovementHandler{usecase: usecase}
}

// List handles GET /api/v1/stock-movements
func (h *StockMovementHandler) List(c *fiber.Ctx) error {
	limit := 20
	if limitStr := c.Query("limit"); limitStr != "" {
		parsedLimit, err := strconv.Atoi(limitStr)
		if err != nil || parsedLimit <= 0 {
			return response.Error(c, fiber.StatusBadRequest, "INVALID_PARAMETER", "limit must be a positive integer")
		}
		limit = parsedLimit
	}

	offset := 0
	if offsetStr := c.Query("offset"); offsetStr != "" {
		parsedOffset, err := strconv.Atoi(offsetStr)
		if err != nil || parsedOffset < 0 {
			return response.Error(c, fiber.StatusBadRequest, "INVALID_PARAMETER", "offset must be a non-negative integer")
		}
		offset = parsedOffset
	}

	var productID *uuid.UUID
	if prodIDStr := strings.TrimSpace(c.Query("product_id")); prodIDStr != "" {
		parsedID, err := uuid.Parse(prodIDStr)
		if err != nil {
			return response.Error(c, fiber.StatusBadRequest, "INVALID_PARAMETER", "Invalid product_id format")
		}
		productID = &parsedID
	}

	var mType *domain.MovementType
	if mTypeStr := strings.TrimSpace(c.Query("movement_type")); mTypeStr != "" {
		parsedType := domain.MovementType(strings.ToUpper(mTypeStr))
		valid := false
		for _, t := range domain.ValidMovementTypes {
			if parsedType == t {
				valid = true
				break
			}
		}
		if !valid {
			return response.Error(c, fiber.StatusBadRequest, "INVALID_PARAMETER", "movement_type must be RECEIPT, ISSUE, or ADJUSTMENT")
		}
		mType = &parsedType
	}

	input := stockmovement.ListMovementsInput{
		ProductID:    productID,
		MovementType: mType,
		Limit:        limit,
		Offset:       offset,
	}

	ctx := c.UserContext()
	movements, total, err := h.usecase.ListMovements(ctx, input)
	if err != nil {
		if errors.Is(err, domain.ErrInvalidMovementType) {
			return response.Error(c, fiber.StatusBadRequest, "INVALID_PARAMETER", "Invalid movement_type")
		}
		slog.Error("Failed to list stock movements", "error", err)
		return response.Error(c, fiber.StatusInternalServerError, "INTERNAL_ERROR", "Failed to retrieve stock movements")
	}

	meta := response.PageMeta{
		Limit:  limit,
		Offset: offset,
		Total:  total,
	}

	return response.SuccessWithMeta(c, movements, meta)
}
