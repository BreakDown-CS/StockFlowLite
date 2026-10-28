package stockmovement

import (
	"context"
	"strings"
	"time"

	"github.com/BreakDown-CS/StockFlowLite/internal/domain"
	"github.com/google/uuid"
)

// ReceiveStockInput holds parameters for processing an inbound stock receipt.
type ReceiveStockInput struct {
	ProductID     uuid.UUID
	Quantity      int
	Notes         string
	ReferenceType string
	ReferenceID   string
}

// ListMovementsInput holds query parameters for retrieving stock movement history.
type ListMovementsInput struct {
	ProductID    *uuid.UUID
	MovementType *domain.MovementType
	From         *time.Time
	To           *time.Time
	Limit        int
	Offset       int
}

// StockMovementUsecase defines business application operations for receipts and movement history.
type StockMovementUsecase interface {
	ReceiveStock(ctx context.Context, input ReceiveStockInput) (*domain.StockReceiptResult, error)
	ListMovements(ctx context.Context, input ListMovementsInput) ([]domain.StockMovement, int64, error)
}

type stockMovementUsecase struct {
	repo domain.StockMovementRepository
}

// NewStockMovementUsecase constructs a new StockMovementUsecase.
func NewStockMovementUsecase(repo domain.StockMovementRepository) StockMovementUsecase {
	return &stockMovementUsecase{repo: repo}
}

// ReceiveStock validates input and delegates atomic stock receipt to the repository.
func (uc *stockMovementUsecase) ReceiveStock(ctx context.Context, input ReceiveStockInput) (*domain.StockReceiptResult, error) {
	if input.ProductID == uuid.Nil {
		return nil, domain.ErrMissingProductID
	}
	if input.Quantity <= 0 {
		return nil, domain.ErrInvalidQuantity
	}

	refType := strings.TrimSpace(input.ReferenceType)
	if refType == "" {
		refType = "STOCK_RECEIPT"
	}

	notes := strings.TrimSpace(input.Notes)
	refID := strings.TrimSpace(input.ReferenceID)

	return uc.repo.ReceiveStock(ctx, input.ProductID, input.Quantity, notes, refType, refID)
}

// ListMovements validates filter bounds and queries the repository.
func (uc *stockMovementUsecase) ListMovements(ctx context.Context, input ListMovementsInput) ([]domain.StockMovement, int64, error) {
	limit := input.Limit
	if limit <= 0 {
		limit = 20
	} else if limit > 100 {
		limit = 100
	}

	offset := input.Offset
	if offset < 0 {
		offset = 0
	}

	if input.MovementType != nil {
		valid := false
		for _, t := range domain.ValidMovementTypes {
			if *input.MovementType == t {
				valid = true
				break
			}
		}
		if !valid {
			return nil, 0, domain.ErrInvalidMovementType
		}
	}

	if input.From != nil && input.To != nil && input.From.After(*input.To) {
		return nil, 0, domain.ErrInvalidDateRange
	}

	filter := domain.ListMovementFilter{
		ProductID:    input.ProductID,
		MovementType: input.MovementType,
		From:         input.From,
		To:           input.To,
		Limit:        limit,
		Offset:       offset,
	}

	return uc.repo.ListMovements(ctx, filter)
}
