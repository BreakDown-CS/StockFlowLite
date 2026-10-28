package domain_test

import (
	"testing"
	"time"

	"github.com/BreakDown-CS/StockFlowLite/internal/domain"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newPendingRequest builds a valid PENDING request for workflow tests.
func newPendingRequest() *domain.StockRequest {
	return &domain.StockRequest{
		ID:            uuid.New(),
		RequestNumber: "REQ-TEST-001",
		RequesterID:   uuid.New(),
		Status:        domain.StatusPending,
		Items: []domain.StockRequestItem{
			{ID: uuid.New(), ProductID: uuid.New(), RequestedQuantity: 2},
		},
	}
}

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

	// In APPROVED status (still cancellable: nothing has been deducted yet)
	req.Status = domain.StatusApproved
	assert.False(t, req.CanBeReviewed())
	assert.True(t, req.CanBeCancelled())
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

func TestStockRequest_Approve(t *testing.T) {
	reviewerID := uuid.New()
	now := time.Now().UTC()

	req := newPendingRequest()
	require.NoError(t, req.Approve(reviewerID, now))
	assert.Equal(t, domain.StatusApproved, req.Status)
	require.NotNil(t, req.ReviewerID)
	assert.Equal(t, reviewerID, *req.ReviewerID)
	require.NotNil(t, req.ReviewedAt)
	assert.True(t, req.ReviewedAt.Equal(now))
	assert.Empty(t, req.RejectionReason)
	assert.True(t, req.UpdatedAt.Equal(now))

	// Approving again is an invalid state transition.
	assert.ErrorIs(t, req.Approve(reviewerID, now), domain.ErrCannotReview)

	// A reviewer identity is mandatory.
	assert.ErrorIs(t, newPendingRequest().Approve(uuid.Nil, now), domain.ErrMissingReviewer)
}

func TestStockRequest_Reject(t *testing.T) {
	reviewerID := uuid.New()
	now := time.Now().UTC()

	req := newPendingRequest()
	require.NoError(t, req.Reject(reviewerID, "  Out of budget  ", now))
	assert.Equal(t, domain.StatusRejected, req.Status)
	require.NotNil(t, req.ReviewerID)
	assert.Equal(t, reviewerID, *req.ReviewerID)
	require.NotNil(t, req.ReviewedAt)
	assert.True(t, req.ReviewedAt.Equal(now))
	assert.Equal(t, "Out of budget", req.RejectionReason)

	// Repeated rejection and rejection after approval are conflicts.
	assert.ErrorIs(t, req.Reject(reviewerID, "", now), domain.ErrCannotReview)

	approved := newPendingRequest()
	require.NoError(t, approved.Approve(reviewerID, now))
	assert.ErrorIs(t, approved.Reject(reviewerID, "", now), domain.ErrCannotReview)

	// A reviewer identity is mandatory.
	assert.ErrorIs(t, newPendingRequest().Reject(uuid.Nil, "", now), domain.ErrMissingReviewer)
}

func TestStockRequest_MarkIssued(t *testing.T) {
	reviewerID := uuid.New()
	now := time.Now().UTC()

	req := newPendingRequest()
	assert.ErrorIs(t, req.MarkIssued(now), domain.ErrCannotIssue, "a pending request cannot be issued")

	require.NoError(t, req.Approve(reviewerID, now))
	require.NoError(t, req.MarkIssued(now))
	assert.Equal(t, domain.StatusIssued, req.Status)
	assert.True(t, req.UpdatedAt.Equal(now))

	// Issuing twice must never be possible.
	assert.ErrorIs(t, req.MarkIssued(now), domain.ErrCannotIssue)
}

func TestStockRequest_Cancel(t *testing.T) {
	now := time.Now().UTC()
	reviewer := uuid.New()

	// PENDING -> CANCELLED
	pending := newPendingRequest()
	require.NoError(t, pending.Cancel(now))
	assert.Equal(t, domain.StatusCancelled, pending.Status)
	assert.True(t, pending.UpdatedAt.Equal(now))
	assert.ErrorIs(t, pending.Cancel(now), domain.ErrCannotCancel, "a cancelled request cannot be cancelled twice")

	// APPROVED -> CANCELLED (no stock has been deducted yet)
	approved := newPendingRequest()
	require.NoError(t, approved.Approve(reviewer, now))
	require.NoError(t, approved.Cancel(now))
	assert.Equal(t, domain.StatusCancelled, approved.Status)

	// ISSUED cannot be cancelled
	issued := newPendingRequest()
	require.NoError(t, issued.Approve(reviewer, now))
	require.NoError(t, issued.MarkIssued(now))
	assert.ErrorIs(t, issued.Cancel(now), domain.ErrCannotCancel)

	// REJECTED cannot be cancelled
	rejected := newPendingRequest()
	require.NoError(t, rejected.Reject(reviewer, "no budget", now))
	assert.ErrorIs(t, rejected.Cancel(now), domain.ErrCannotCancel)
}
