package domain

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

// StockShortage describes a single line item that cannot be fulfilled from current stock.
type StockShortage struct {
	ProductID uuid.UUID `json:"product_id"`
	Requested int       `json:"requested"`
	Available int       `json:"available"`
	Shortfall int       `json:"shortfall"`
}

// StockRequestIssuance is the immutable audit record of a fulfilled request.
// The UNIQUE(request_id) constraint makes a fulfilled request impossible to fulfil twice.
type StockRequestIssuance struct {
	ID             uuid.UUID  `json:"id"`
	RequestID      uuid.UUID  `json:"request_id"`
	IdempotencyKey string     `json:"idempotency_key,omitempty"`
	ActorID        *uuid.UUID `json:"actor_id,omitempty"`
	ItemCount      int        `json:"item_count"`
	TotalQuantity  int        `json:"total_quantity"`
	CreatedAt      time.Time  `json:"created_at"`
}

// InsufficientStockError reports every line item that could not be fulfilled.
// It unwraps to ErrInsufficientStock so existing error mapping keeps working.
type InsufficientStockError struct {
	Items []StockShortage
}

// Error implements the error interface.
func (e *InsufficientStockError) Error() string {
	return fmt.Sprintf("%s (%d item(s) short)", ErrInsufficientStock.Error(), len(e.Items))
}

// Unwrap exposes the sentinel so callers can keep using errors.Is.
func (e *InsufficientStockError) Unwrap() error {
	return ErrInsufficientStock
}

// StateConflictError reports an invalid workflow transition together with the
// authoritative current status (and fulfillment metadata when already issued).
type StateConflictError struct {
	Cause         error
	CurrentStatus RequestStatus
	IssuanceID    *uuid.UUID
	IssuedAt      *time.Time
}

// Error implements the error interface.
func (e *StateConflictError) Error() string {
	if e.CurrentStatus == "" {
		return e.Cause.Error()
	}
	return fmt.Sprintf("%s (current status: %s)", e.Cause.Error(), e.CurrentStatus)
}

// Unwrap exposes the sentinel (ErrCannotIssue / ErrCannotReview / ErrCannotCancel).
func (e *StateConflictError) Unwrap() error {
	return e.Cause
}

// Details builds the client-facing error details payload, omitting empty values.
func (e *StateConflictError) Details() map[string]interface{} {
	details := map[string]interface{}{}
	if e.CurrentStatus != "" {
		details["current_status"] = string(e.CurrentStatus)
	}
	if e.IssuanceID != nil {
		details["issuance_id"] = e.IssuanceID.String()
	}
	if e.IssuedAt != nil {
		details["issued_at"] = e.IssuedAt.UTC().Format(time.RFC3339Nano)
	}
	return details
}
