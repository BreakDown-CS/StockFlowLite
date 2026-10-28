package domain

import (
	"context"

	"github.com/google/uuid"
)

// ListMovementFilter holds filtering and pagination parameters for stock movement queries.
type ListMovementFilter struct {
	ProductID    *uuid.UUID
	MovementType *MovementType
	Limit        int
	Offset       int
}

// StockReceiptResult encapsulates the atomic outcome of a stock receipt operation.
type StockReceiptResult struct {
	Product  *Product       `json:"product"`
	Movement *StockMovement `json:"movement"`
}

// StockMovementRepository defines persistence contracts for stock movements and atomic receipts.
type StockMovementRepository interface {
	// ReceiveStock executes an atomic transaction with row-level locking on the product,
	// increments current stock, inserts the movement ledger entry, and commits.
	ReceiveStock(ctx context.Context, productID uuid.UUID, quantity int, notes string, refType string, refID string) (*StockReceiptResult, error)

	// ListMovements retrieves paginated and filtered movement history records.
	ListMovements(ctx context.Context, filter ListMovementFilter) ([]StockMovement, int64, error)
}
