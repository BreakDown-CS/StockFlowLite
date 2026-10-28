package domain_test

import (
	"testing"

	"github.com/BreakDown-CS/StockFlowLite/internal/domain"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewStockRequest_Success(t *testing.T) {
	prodID1 := uuid.New()
	prodID2 := uuid.New()
	requesterID := uuid.New()

	items := []domain.StockRequestItem{
		{ProductID: prodID1, RequestedQuantity: 5},
		{ProductID: prodID2, RequestedQuantity: 10},
	}

	req, err := domain.NewStockRequest("REQ-20261027-001", requesterID, "For accounting department", items)
	require.NoError(t, err)
	assert.NotEqual(t, uuid.Nil, req.ID)
	assert.Equal(t, "REQ-20261027-001", req.RequestNumber)
	assert.Equal(t, requesterID, req.RequesterID)
	assert.Equal(t, domain.StatusPending, req.Status)
	assert.Len(t, req.Items, 2)
	assert.Equal(t, req.ID, req.Items[0].RequestID)
	assert.Equal(t, req.ID, req.Items[1].RequestID)
}

func TestStockRequest_Validate_Errors(t *testing.T) {
	prodID := uuid.New()
	requesterID := uuid.New()

	tests := []struct {
		name    string
		reqNum  string
		reqID   uuid.UUID
		items   []domain.StockRequestItem
		wantErr error
	}{
		{
			name:    "empty request number",
			reqNum:  "   ",
			reqID:   requesterID,
			items:   []domain.StockRequestItem{{ProductID: prodID, RequestedQuantity: 1}},
			wantErr: domain.ErrEmptyRequestNumber,
		},
		{
			name:    "missing requester id",
			reqNum:  "REQ-001",
			reqID:   uuid.Nil,
			items:   []domain.StockRequestItem{{ProductID: prodID, RequestedQuantity: 1}},
			wantErr: domain.ErrMissingRequester,
		},
		{
			name:    "empty items list",
			reqNum:  "REQ-001",
			reqID:   requesterID,
			items:   []domain.StockRequestItem{},
			wantErr: domain.ErrEmptyRequestItems,
		},
		{
			name:    "item with missing product id",
			reqNum:  "REQ-001",
			reqID:   requesterID,
			items:   []domain.StockRequestItem{{ProductID: uuid.Nil, RequestedQuantity: 2}},
			wantErr: domain.ErrMissingProductID,
		},
		{
			name:    "item with negative quantity",
			reqNum:  "REQ-001",
			reqID:   requesterID,
			items:   []domain.StockRequestItem{{ProductID: prodID, RequestedQuantity: -5}},
			wantErr: domain.ErrInvalidQuantity,
		},
		{
			name:    "item with zero quantity",
			reqNum:  "REQ-001",
			reqID:   requesterID,
			items:   []domain.StockRequestItem{{ProductID: prodID, RequestedQuantity: 0}},
			wantErr: domain.ErrInvalidQuantity,
		},
		{
			name:   "duplicate products in items",
			reqNum: "REQ-001",
			reqID:  requesterID,
			items: []domain.StockRequestItem{
				{ProductID: prodID, RequestedQuantity: 2},
				{ProductID: prodID, RequestedQuantity: 3},
			},
			wantErr: domain.ErrDuplicateItem,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := domain.NewStockRequest(tt.reqNum, tt.reqID, "notes", tt.items)
			require.ErrorIs(t, err, tt.wantErr)
		})
	}
}

func TestStockRequest_StateTransitions(t *testing.T) {
	req := &domain.StockRequest{
		ID:            uuid.New(),
		RequestNumber: "REQ-001",
		RequesterID:   uuid.New(),
		Status:        domain.StatusPending,
		Items:         []domain.StockRequestItem{{ProductID: uuid.New(), RequestedQuantity: 1}},
	}

	// In PENDING status
	assert.True(t, req.CanBeReviewed())
	assert.True(t, req.CanBeCancelled())
	assert.False(t, req.CanBeIssued())

	// In APPROVED status
	req.Status = domain.StatusApproved
	assert.False(t, req.CanBeReviewed())
	assert.False(t, req.CanBeCancelled())
	assert.True(t, req.CanBeIssued())

	// In ISSUED status
	req.Status = domain.StatusIssued
	assert.False(t, req.CanBeReviewed())
	assert.False(t, req.CanBeCancelled())
	assert.False(t, req.CanBeIssued())

	// In REJECTED status
	req.Status = domain.StatusRejected
	assert.False(t, req.CanBeReviewed())
	assert.False(t, req.CanBeCancelled())
	assert.False(t, req.CanBeIssued())
}
