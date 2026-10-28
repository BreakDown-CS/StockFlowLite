package domain

import (
	"strings"
	"time"

	"github.com/google/uuid"
)

// MovementType classifies the direction of an inventory transaction.
type MovementType string

const (
	MovementReceipt    MovementType = "RECEIPT"
	MovementIssue      MovementType = "ISSUE"
	MovementAdjustment MovementType = "ADJUSTMENT"
)

// ValidMovementTypes contains all valid movement classification values.
var ValidMovementTypes = []MovementType{
	MovementReceipt,
	MovementIssue,
	MovementAdjustment,
}

// StockMovement represents an immutable ledger entry of an inventory change.
type StockMovement struct {
	ID            int64        `json:"id"`
	ProductID     uuid.UUID    `json:"product_id"`
	MovementType  MovementType `json:"movement_type"`
	Quantity      int          `json:"quantity"` // Always positive; MovementType determines direction
	ReferenceType string       `json:"reference_type"`
	ReferenceID   string       `json:"reference_id,omitempty"`
	ActorID       *uuid.UUID   `json:"actor_id,omitempty"`
	Notes         string       `json:"notes,omitempty"`
	CreatedAt     time.Time    `json:"created_at"`
}

// NewStockMovement creates and validates a new stock movement record.
func NewStockMovement(
	productID uuid.UUID,
	movementType MovementType,
	quantity int,
	refType string,
	refID string,
	actorID *uuid.UUID,
	notes string,
) (*StockMovement, error) {
	sm := &StockMovement{
		ProductID:     productID,
		MovementType:  movementType,
		Quantity:      quantity,
		ReferenceType: strings.TrimSpace(refType),
		ReferenceID:   strings.TrimSpace(refID),
		ActorID:       actorID,
		Notes:         strings.TrimSpace(notes),
		CreatedAt:     time.Now().UTC(),
	}

	if err := sm.Validate(); err != nil {
		return nil, err
	}
	return sm, nil
}

// Validate verifies that the stock movement satisfies all ledger integrity rules.
func (sm *StockMovement) Validate() error {
	if sm.ProductID == uuid.Nil {
		return ErrMissingProductID
	}
	if !sm.IsValidType() {
		return ErrInvalidMovementType
	}
	if sm.Quantity <= 0 {
		return ErrInvalidQuantity
	}
	if strings.TrimSpace(sm.ReferenceType) == "" {
		return ErrEmptyReferenceType
	}
	return nil
}

// IsValidType checks if the movement type belongs to the recognized enum.
func (sm *StockMovement) IsValidType() bool {
	for _, t := range ValidMovementTypes {
		if sm.MovementType == t {
			return true
		}
	}
	return false
}

// NetQuantityChange returns the signed quantity delta applied to physical stock.
func (sm *StockMovement) NetQuantityChange() int {
	switch sm.MovementType {
	case MovementReceipt:
		return sm.Quantity
	case MovementIssue:
		return -sm.Quantity
	case MovementAdjustment:
		return sm.Quantity
	default:
		return 0
	}
}
