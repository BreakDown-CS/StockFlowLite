package domain_test

import (
	"testing"

	"github.com/BreakDown-CS/StockFlowLite/internal/domain"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewStockMovement_Receipt_Success(t *testing.T) {
	prodID := uuid.New()
	actorID := uuid.New()

	sm, err := domain.NewStockMovement(
		prodID,
		domain.MovementReceipt,
		50,
		"PURCHASE_RECEIPT",
		"REC-20261027-001",
		&actorID,
		"Inbound from Supplier A",
	)
	require.NoError(t, err)
	assert.Equal(t, prodID, sm.ProductID)
	assert.Equal(t, domain.MovementReceipt, sm.MovementType)
	assert.Equal(t, 50, sm.Quantity)
	assert.Equal(t, 50, sm.NetQuantityChange())
	assert.Equal(t, "PURCHASE_RECEIPT", sm.ReferenceType)
	assert.Equal(t, "REC-20261027-001", sm.ReferenceID)
	assert.Equal(t, &actorID, sm.ActorID)
	assert.False(t, sm.CreatedAt.IsZero())
}

func TestNewStockMovement_Issue_Success(t *testing.T) {
	prodID := uuid.New()

	sm, err := domain.NewStockMovement(
		prodID,
		domain.MovementIssue,
		15,
		"STOCK_REQUEST",
		"REQ-20261027-001",
		nil,
		"Issued to Department",
	)
	require.NoError(t, err)
	assert.Equal(t, domain.MovementIssue, sm.MovementType)
	assert.Equal(t, 15, sm.Quantity)
	assert.Equal(t, -15, sm.NetQuantityChange())
}

func TestStockMovement_Validate_Errors(t *testing.T) {
	prodID := uuid.New()

	tests := []struct {
		name     string
		prodID   uuid.UUID
		mType    domain.MovementType
		quantity int
		refType  string
		wantErr  error
	}{
		{
			name:     "missing product id",
			prodID:   uuid.Nil,
			mType:    domain.MovementReceipt,
			quantity: 10,
			refType:  "RECEIPT",
			wantErr:  domain.ErrMissingProductID,
		},
		{
			name:     "invalid movement type",
			prodID:   prodID,
			mType:    domain.MovementType("UNKNOWN"),
			quantity: 10,
			refType:  "RECEIPT",
			wantErr:  domain.ErrInvalidMovementType,
		},
		{
			name:     "zero quantity",
			prodID:   prodID,
			mType:    domain.MovementReceipt,
			quantity: 0,
			refType:  "RECEIPT",
			wantErr:  domain.ErrInvalidQuantity,
		},
		{
			name:     "negative quantity",
			prodID:   prodID,
			mType:    domain.MovementReceipt,
			quantity: -10,
			refType:  "RECEIPT",
			wantErr:  domain.ErrInvalidQuantity,
		},
		{
			name:     "empty reference type",
			prodID:   prodID,
			mType:    domain.MovementReceipt,
			quantity: 10,
			refType:  "   ",
			wantErr:  domain.ErrEmptyReferenceType,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := domain.NewStockMovement(tt.prodID, tt.mType, tt.quantity, tt.refType, "REF-1", nil, "")
			require.ErrorIs(t, err, tt.wantErr)
		})
	}
}
