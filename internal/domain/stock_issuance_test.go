package domain_test

import (
	"errors"
	"testing"
	"time"

	"github.com/BreakDown-CS/StockFlowLite/internal/domain"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInsufficientStockError_UnwrapsSentinel(t *testing.T) {
	productID := uuid.New()
	err := &domain.InsufficientStockError{
		Items: []domain.StockShortage{{ProductID: productID, Requested: 10, Available: 3, Shortfall: 7}},
	}

	require.ErrorIs(t, err, domain.ErrInsufficientStock)

	var typed *domain.InsufficientStockError
	require.True(t, errors.As(err, &typed))
	require.Len(t, typed.Items, 1)
	assert.Equal(t, productID, typed.Items[0].ProductID)
	assert.Equal(t, 10, typed.Items[0].Requested)
	assert.Equal(t, 3, typed.Items[0].Available)
	assert.Equal(t, 7, typed.Items[0].Shortfall)
}

func TestStateConflictError_UnwrapsAndBuildsDetails(t *testing.T) {
	issuanceID := uuid.New()
	issuedAt := time.Date(2026, 10, 28, 9, 0, 0, 0, time.UTC)
	err := &domain.StateConflictError{
		Cause:         domain.ErrCannotIssue,
		CurrentStatus: domain.StatusIssued,
		IssuanceID:    &issuanceID,
		IssuedAt:      &issuedAt,
	}

	require.ErrorIs(t, err, domain.ErrCannotIssue)
	assert.Contains(t, err.Error(), "ISSUED")

	details := err.Details()
	assert.Equal(t, "ISSUED", details["current_status"])
	assert.Equal(t, issuanceID.String(), details["issuance_id"])
	assert.Contains(t, details["issued_at"], "2026-10-28T09:00:00")
}

func TestStateConflictError_WithoutIssuance(t *testing.T) {
	err := &domain.StateConflictError{Cause: domain.ErrCannotCancel, CurrentStatus: domain.StatusPending}

	require.ErrorIs(t, err, domain.ErrCannotCancel)
	details := err.Details()
	assert.Equal(t, "PENDING", details["current_status"])
	assert.NotContains(t, details, "issuance_id")
	assert.NotContains(t, details, "issued_at")
}
