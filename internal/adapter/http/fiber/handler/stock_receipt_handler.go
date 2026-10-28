package handler

import (
	"errors"
	"log/slog"
	"strings"

	"github.com/BreakDown-CS/StockFlowLite/internal/domain"
	"github.com/BreakDown-CS/StockFlowLite/internal/usecase/stockmovement"
	"github.com/BreakDown-CS/StockFlowLite/pkg/response"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

// StockReceiptHandler manages HTTP endpoints for stock receipts.
type StockReceiptHandler struct {
	usecase stockmovement.StockMovementUsecase
}

// NewStockReceiptHandler constructs a new StockReceiptHandler.
func NewStockReceiptHandler(usecase stockmovement.StockMovementUsecase) *StockReceiptHandler {
	return &StockReceiptHandler{usecase: usecase}
}

// CreateStockReceiptRequest defines the payload for recording an inbound stock receipt.
type CreateStockReceiptRequest struct {
	ProductID string `json:"product_id"`
	Quantity  *int   `json:"quantity"`
	Notes     string `json:"notes"`
}

// Create handles POST /api/v1/stock-receipts
func (h *StockReceiptHandler) Create(c *fiber.Ctx) error {
	var req CreateStockReceiptRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, fiber.StatusBadRequest, "BAD_REQUEST", "Malformed request payload")
	}

	prodIDStr := strings.TrimSpace(req.ProductID)
	if prodIDStr == "" {
		return response.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "product_id is required")
	}

	prodID, err := uuid.Parse(prodIDStr)
	if err != nil {
		return response.Error(c, fiber.StatusBadRequest, "INVALID_ID", "Invalid product ID format")
	}

	if req.Quantity == nil {
		return response.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "quantity is required")
	}
	if *req.Quantity <= 0 {
		return response.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "quantity must be strictly positive")
	}

	notes := strings.TrimSpace(req.Notes)
	if len(notes) > 1000 {
		return response.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "notes must not exceed 1000 characters")
	}

	input := stockmovement.ReceiveStockInput{
		ProductID:     prodID,
		Quantity:      *req.Quantity,
		Notes:         notes,
		ReferenceType: "STOCK_RECEIPT",
	}

	ctx := c.UserContext()
	result, err := h.usecase.ReceiveStock(ctx, input)
	if err != nil {
		if errors.Is(err, domain.ErrProductNotFound) {
			return response.Error(c, fiber.StatusNotFound, "NOT_FOUND", "Product not found")
		}
		if errors.Is(err, domain.ErrProductInactive) {
			return response.Error(c, fiber.StatusBadRequest, "PRODUCT_INACTIVE", "Cannot receive stock for inactive product")
		}
		if errors.Is(err, domain.ErrInvalidQuantity) {
			return response.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "quantity must be strictly positive")
		}
		if errors.Is(err, domain.ErrStockOverflow) {
			return response.Error(c, fiber.StatusBadRequest, "STOCK_OVERFLOW", "Stock quantity exceeds maximum supported limit")
		}

		slog.Error("Failed to process stock receipt", "product_id", prodIDStr, "error", err)
		return response.Error(c, fiber.StatusInternalServerError, "INTERNAL_ERROR", "Failed to process stock receipt")
	}

	return response.Created(c, result, "Stock received successfully")
}
