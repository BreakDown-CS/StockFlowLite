package handler

import (
	"errors"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/BreakDown-CS/StockFlowLite/internal/domain"
	"github.com/BreakDown-CS/StockFlowLite/internal/usecase/stockrequest"
	"github.com/BreakDown-CS/StockFlowLite/pkg/response"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

// StockRequestHandler manages HTTP endpoints for the stock request/approval workflow.
type StockRequestHandler struct {
	usecase stockrequest.StockRequestUsecase
}

// NewStockRequestHandler constructs a new StockRequestHandler.
func NewStockRequestHandler(usecase stockrequest.StockRequestUsecase) *StockRequestHandler {
	return &StockRequestHandler{usecase: usecase}
}

// CreateStockRequestItem is a single requested line item in the request payload.
type CreateStockRequestItem struct {
	ProductID string `json:"product_id"`
	Quantity  *int   `json:"quantity"`
}

// CreateStockRequestRequest defines the payload for submitting a stock request.
type CreateStockRequestRequest struct {
	Items []CreateStockRequestItem `json:"items"`
	Notes string                   `json:"notes"`
}

// RejectStockRequestRequest defines the optional payload for rejecting a request.
type RejectStockRequestRequest struct {
	RejectionReason string `json:"rejection_reason"`
}

// CancelStockRequestRequest defines the optional payload for cancelling a request.
type CancelStockRequestRequest struct {
	Reason string `json:"reason"`
}

// Create handles POST /api/v1/stock-requests
func (h *StockRequestHandler) Create(c *fiber.Ctx) error {
	var req CreateStockRequestRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, fiber.StatusBadRequest, "BAD_REQUEST", "Malformed request payload")
	}

	if len(req.Items) == 0 {
		return response.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "items must contain at least one product")
	}
	if len(req.Items) > stockrequest.MaxRequestItems {
		return response.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "items must not exceed 100 entries")
	}

	items := make([]stockrequest.ItemInput, 0, len(req.Items))
	for _, item := range req.Items {
		productIDStr := strings.TrimSpace(item.ProductID)
		if productIDStr == "" {
			return response.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "product_id is required for every item")
		}
		productID, err := uuid.Parse(productIDStr)
		if err != nil {
			return response.Error(c, fiber.StatusBadRequest, "INVALID_ID", "Invalid product ID format")
		}
		if item.Quantity == nil {
			return response.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "quantity is required for every item")
		}
		if *item.Quantity <= 0 {
			return response.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "quantity must be strictly positive")
		}

		items = append(items, stockrequest.ItemInput{ProductID: productID, Quantity: *item.Quantity})
	}

	notes := strings.TrimSpace(req.Notes)
	if len(notes) > 1000 {
		return response.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "notes must not exceed 1000 characters")
	}

	input := stockrequest.CreateStockRequestInput{Items: items, Notes: notes}

	created, err := h.usecase.CreateRequest(c.UserContext(), input)
	if err != nil {
		// Referenced product state is a payload problem at creation time (Phase 4 convention).
		if errors.Is(err, domain.ErrProductInactive) {
			return response.Error(c, fiber.StatusBadRequest, "PRODUCT_INACTIVE", "Stock request references an inactive product")
		}
		return respondStockRequestError(c, "create", "", err)
	}

	return response.Created(c, created, "Stock request created successfully")
}

// List handles GET /api/v1/stock-requests
func (h *StockRequestHandler) List(c *fiber.Ctx) error {
	limit := 20
	if limitStr := c.Query("limit"); limitStr != "" {
		parsedLimit, err := strconv.Atoi(limitStr)
		if err != nil || parsedLimit <= 0 {
			return response.Error(c, fiber.StatusBadRequest, "INVALID_PARAMETER", "limit must be a positive integer")
		}
		limit = parsedLimit
	}
	// Mirror the use case bound so the reported metadata matches the actual page size.
	if limit > stockrequest.MaxPageSize {
		limit = stockrequest.MaxPageSize
	}

	offset := 0
	if offsetStr := c.Query("offset"); offsetStr != "" {
		parsedOffset, err := strconv.Atoi(offsetStr)
		if err != nil || parsedOffset < 0 {
			return response.Error(c, fiber.StatusBadRequest, "INVALID_PARAMETER", "offset must be a non-negative integer")
		}
		offset = parsedOffset
	}

	var status *domain.RequestStatus
	if statusStr := strings.TrimSpace(c.Query("status")); statusStr != "" {
		parsedStatus := domain.RequestStatus(strings.ToUpper(statusStr))
		valid := false
		for _, s := range domain.ValidRequestStatuses {
			if parsedStatus == s {
				valid = true
				break
			}
		}
		if !valid {
			return response.Error(c, fiber.StatusBadRequest, "INVALID_PARAMETER", "status must be PENDING, APPROVED, REJECTED, ISSUED, or CANCELLED")
		}
		status = &parsedStatus
	}

	input := stockrequest.ListStockRequestsInput{Status: status, Limit: limit, Offset: offset}

	requests, total, err := h.usecase.ListRequests(c.UserContext(), input)
	if err != nil {
		return respondStockRequestError(c, "list", "", err)
	}

	meta := response.PageMeta{Limit: limit, Offset: offset, Total: total}
	return response.SuccessWithMeta(c, requests, meta)
}

// GetByID handles GET /api/v1/stock-requests/:id
func (h *StockRequestHandler) GetByID(c *fiber.Ctx) error {
	id, err := parseStockRequestID(c)
	if err != nil {
		return response.Error(c, fiber.StatusBadRequest, "INVALID_ID", "Invalid stock request ID format")
	}

	request, err := h.usecase.GetRequest(c.UserContext(), id)
	if err != nil {
		return respondStockRequestError(c, "get", id.String(), err)
	}

	return response.OK(c, request)
}

// Approve handles POST /api/v1/stock-requests/:id/approve
// Approval only records the review decision; stock is moved by the issue endpoint.
func (h *StockRequestHandler) Approve(c *fiber.Ctx) error {
	id, err := parseStockRequestID(c)
	if err != nil {
		return response.Error(c, fiber.StatusBadRequest, "INVALID_ID", "Invalid stock request ID format")
	}

	request, err := h.usecase.ApproveRequest(c.UserContext(), id)
	if err != nil {
		return respondStockRequestError(c, "approve", id.String(), err)
	}

	return response.OK(c, request, "Stock request approved successfully")
}

// Reject handles POST /api/v1/stock-requests/:id/reject
func (h *StockRequestHandler) Reject(c *fiber.Ctx) error {
	id, err := parseStockRequestID(c)
	if err != nil {
		return response.Error(c, fiber.StatusBadRequest, "INVALID_ID", "Invalid stock request ID format")
	}

	var req RejectStockRequestRequest
	if len(c.Body()) > 0 {
		if err := c.BodyParser(&req); err != nil {
			return response.Error(c, fiber.StatusBadRequest, "BAD_REQUEST", "Malformed request payload")
		}
	}

	reason := strings.TrimSpace(req.RejectionReason)
	if len(reason) > 1000 {
		return response.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "rejection_reason must not exceed 1000 characters")
	}

	request, err := h.usecase.RejectRequest(c.UserContext(), id, stockrequest.RejectStockRequestInput{Reason: reason})
	if err != nil {
		return respondStockRequestError(c, "reject", id.String(), err)
	}

	return response.OK(c, request, "Stock request rejected successfully")
}

// Issue handles POST /api/v1/stock-requests/:id/issue
// Issuance deducts every requested item and writes one ISSUE movement per item atomically.
// An optional Idempotency-Key header makes a retried call replay the original result.
func (h *StockRequestHandler) Issue(c *fiber.Ctx) error {
	id, err := parseStockRequestID(c)
	if err != nil {
		return response.Error(c, fiber.StatusBadRequest, "INVALID_ID", "Invalid stock request ID format")
	}

	result, err := h.usecase.IssueRequest(c.UserContext(), id, c.Get("Idempotency-Key"))
	if err != nil {
		return respondStockRequestError(c, "issue", id.String(), err)
	}

	meta := map[string]interface{}{"idempotent_replay": result.IdempotentReplay}
	if result.Issuance != nil {
		meta["issuance_id"] = result.Issuance.ID.String()
		meta["issued_at"] = result.Issuance.CreatedAt.UTC().Format(time.RFC3339Nano)
	}

	message := "Stock request issued successfully"
	if result.IdempotentReplay {
		message = "Stock request already issued (idempotent replay)"
	}

	return response.SuccessWithMeta(c, result, meta, message)
}

// Cancel handles POST /api/v1/stock-requests/:id/cancel
// Cancellation moves a PENDING or APPROVED request to CANCELLED without touching stock.
// An optional body may carry {"reason": "..."} which is stored and audited.
func (h *StockRequestHandler) Cancel(c *fiber.Ctx) error {
	id, err := parseStockRequestID(c)
	if err != nil {
		return response.Error(c, fiber.StatusBadRequest, "INVALID_ID", "Invalid stock request ID format")
	}

	var req CancelStockRequestRequest
	if len(c.Body()) > 0 {
		if err := c.BodyParser(&req); err != nil {
			return response.Error(c, fiber.StatusBadRequest, "BAD_REQUEST", "Malformed request payload")
		}
	}

	reason := strings.TrimSpace(req.Reason)
	if len(reason) > 1000 {
		return response.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "reason must not exceed 1000 characters")
	}

	request, err := h.usecase.CancelRequest(c.UserContext(), id, reason)
	if err != nil {
		return respondStockRequestError(c, "cancel", id.String(), err)
	}

	return response.OK(c, request, "Stock request cancelled successfully")
}

// parseStockRequestID extracts and validates the :id route parameter.
func parseStockRequestID(c *fiber.Ctx) (uuid.UUID, error) {
	return uuid.Parse(c.Params("id"))
}

// respondStockRequestError maps domain/usecase failures onto the established response envelope.
func respondStockRequestError(c *fiber.Ctx, action, id string, err error) error {
	// Typed errors carry machine-readable details and unwrap to the sentinels below.
	var shortageErr *domain.InsufficientStockError
	if errors.As(err, &shortageErr) {
		return response.ErrorWithDetails(c, fiber.StatusConflict, "INSUFFICIENT_STOCK",
			"Insufficient stock to fulfil the requested items",
			map[string]interface{}{"items": shortageErr.Items})
	}

	var stateErr *domain.StateConflictError
	if errors.As(err, &stateErr) {
		return response.ErrorWithDetails(c, fiber.StatusConflict, "INVALID_STATE",
			"Stock request cannot be processed in its current status",
			stateErr.Details())
	}

	switch {
	case errors.Is(err, domain.ErrUnauthenticated), errors.Is(err, domain.ErrUserInactive):
		return response.Error(c, fiber.StatusUnauthorized, "UNAUTHORIZED", "Authentication required")
	case errors.Is(err, domain.ErrSelfApproval):
		return response.Error(c, fiber.StatusForbidden, "SELF_APPROVAL_FORBIDDEN", domain.ErrSelfApproval.Error())
	case errors.Is(err, domain.ErrForbidden):
		return response.Error(c, fiber.StatusForbidden, "FORBIDDEN", domain.ErrForbidden.Error())
	case errors.Is(err, domain.ErrInvalidCancellationReason):
		return response.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", domain.ErrInvalidCancellationReason.Error())
	case errors.Is(err, domain.ErrInvalidIdempotencyKey):
		return response.Error(c, fiber.StatusBadRequest, "INVALID_IDEMPOTENCY_KEY", domain.ErrInvalidIdempotencyKey.Error())
	case errors.Is(err, domain.ErrIdempotencyKeyReused):
		return response.Error(c, fiber.StatusConflict, "IDEMPOTENCY_KEY_REUSED", "Idempotency key was already used for another stock request")
	case errors.Is(err, domain.ErrInvalidRequestID):
		return response.Error(c, fiber.StatusBadRequest, "INVALID_ID", "Invalid stock request ID format")
	case errors.Is(err, domain.ErrStockRequestNotFound):
		return response.Error(c, fiber.StatusNotFound, "NOT_FOUND", "Stock request not found")
	case errors.Is(err, domain.ErrProductNotFound):
		return response.Error(c, fiber.StatusNotFound, "NOT_FOUND", "Product not found")
	case errors.Is(err, domain.ErrCannotReview), errors.Is(err, domain.ErrCannotIssue), errors.Is(err, domain.ErrCannotCancel):
		return response.Error(c, fiber.StatusConflict, "INVALID_STATE", "Stock request cannot be processed in its current status")
	case errors.Is(err, domain.ErrInsufficientStock):
		return response.Error(c, fiber.StatusConflict, "INSUFFICIENT_STOCK", "Insufficient stock to fulfil the requested items")
	case errors.Is(err, domain.ErrProductInactive):
		return response.Error(c, fiber.StatusConflict, "PRODUCT_INACTIVE", "Stock request references an inactive product")
	case errors.Is(err, domain.ErrDuplicateItem):
		return response.Error(c, fiber.StatusBadRequest, "DUPLICATE_ITEM", "Duplicate product in stock request items")
	case errors.Is(err, domain.ErrDuplicateRequestNumber):
		return response.Error(c, fiber.StatusConflict, "DUPLICATE_REQUEST_NUMBER", "Stock request number already exists")
	case errors.Is(err, domain.ErrEmptyRequestItems),
		errors.Is(err, domain.ErrTooManyRequestItems),
		errors.Is(err, domain.ErrInvalidQuantity),
		errors.Is(err, domain.ErrMissingProductID),
		errors.Is(err, domain.ErrInvalidStatus):
		return response.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", err.Error())
	default:
		slog.Error("Stock request operation failed", "action", action, "request_id", id, "error", err)
		return response.Error(c, fiber.StatusInternalServerError, "INTERNAL_ERROR", "Failed to process stock request")
	}
}
