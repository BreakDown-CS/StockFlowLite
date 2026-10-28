package domain

import (
	"context"

	"github.com/google/uuid"
)

// ListStockRequestFilter holds filtering and pagination parameters for stock request queries.
type ListStockRequestFilter struct {
	Status      *RequestStatus
	RequesterID *uuid.UUID
	Limit       int
	Offset      int
}

// StockIssueResult encapsulates the atomic outcome of issuing an approved stock request.
type StockIssueResult struct {
	Request          *StockRequest         `json:"request"`
	Movements        []StockMovement       `json:"movements"`
	Issuance         *StockRequestIssuance `json:"issuance,omitempty"`
	IdempotentReplay bool                  `json:"-"`
}

// StockRequestRepository defines the persistence contract for the request/approval workflow.
//
// Approval and issuance are intentionally separate operations because the schema models them
// as distinct states (PENDING -> APPROVED -> ISSUED): approval records review metadata only,
// while issuance performs the inventory deduction and ledger writes atomically.
type StockRequestRepository interface {
	// Create persists a request and its items in one transaction without touching stock.
	Create(ctx context.Context, req *StockRequest) error

	// GetByID returns a request with its items, or ErrStockRequestNotFound.
	GetByID(ctx context.Context, id uuid.UUID) (*StockRequest, error)

	// List returns a filtered, paginated page of requests (with items) and the total count.
	List(ctx context.Context, filter ListStockRequestFilter) ([]StockRequest, int64, error)

	// Approve moves a PENDING request to APPROVED under a row lock, recording reviewer metadata.
	Approve(ctx context.Context, id uuid.UUID, reviewerID uuid.UUID) (*StockRequest, error)

	// Reject moves a PENDING request to REJECTED under a row lock without touching stock.
	Reject(ctx context.Context, id uuid.UUID, reviewerID uuid.UUID, reason string) (*StockRequest, error)

	// Cancel moves a PENDING or APPROVED request to CANCELLED under a row lock.
	// No stock is deducted and no movement is written; the optional reason is stored
	// on the request and in the audit trail.
	Cancel(ctx context.Context, id uuid.UUID, cancelledBy uuid.UUID, reason string) (*StockRequest, error)

	// Issue deducts every requested item and writes one ISSUE movement per item in a single
	// transaction, then marks the request ISSUED. Any failure rolls everything back.
	// An optional idempotencyKey (may be empty) makes retries replay the original result
	// instead of failing, and is persisted for audit purposes.
	Issue(ctx context.Context, id uuid.UUID, actorID uuid.UUID, idempotencyKey string) (*StockIssueResult, error)
}
