package domain_test

import (
	"testing"

	"github.com/BreakDown-CS/StockFlowLite/internal/domain"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewProduct_Success(t *testing.T) {
	p, err := domain.NewProduct("SKU-1001", "Ballpoint Pen Blue", "Box of 50 pens", "BOX", 5)
	require.NoError(t, err)
	assert.NotEqual(t, uuid.Nil, p.ID)
	assert.Equal(t, "SKU-1001", p.SKU)
	assert.Equal(t, "Ballpoint Pen Blue", p.Name)
	assert.Equal(t, "BOX", p.Unit)
	assert.Equal(t, 0, p.CurrentStock)
	assert.Equal(t, 5, p.MinStock)
	assert.True(t, p.IsActive)
}

func TestNewProduct_DefaultUnit(t *testing.T) {
	p, err := domain.NewProduct("SKU-1002", "Stapler", "Heavy duty", "", 2)
	require.NoError(t, err)
	assert.Equal(t, "PCS", p.Unit)
}

func TestProduct_Validate(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(p *domain.Product)
		wantErr error
	}{
		{
			name: "empty sku",
			mutate: func(p *domain.Product) {
				p.SKU = "   "
			},
			wantErr: domain.ErrEmptySKU,
		},
		{
			name: "empty name",
			mutate: func(p *domain.Product) {
				p.Name = ""
			},
			wantErr: domain.ErrEmptyProductName,
		},
		{
			name: "empty unit",
			mutate: func(p *domain.Product) {
				p.Unit = ""
			},
			wantErr: domain.ErrEmptyUnit,
		},
		{
			name: "negative current stock",
			mutate: func(p *domain.Product) {
				p.CurrentStock = -1
			},
			wantErr: domain.ErrNegativeStock,
		},
		{
			name: "negative min stock",
			mutate: func(p *domain.Product) {
				p.MinStock = -5
			},
			wantErr: domain.ErrNegativeMinStock,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := domain.NewProduct("SKU-VALID", "Valid Item", "desc", "PCS", 10)
			require.NoError(t, err)
			tt.mutate(p)
			err = p.Validate()
			require.ErrorIs(t, err, tt.wantErr)
		})
	}
}

func TestProduct_IsLowStock(t *testing.T) {
	p, err := domain.NewProduct("SKU-1001", "Item", "desc", "PCS", 10)
	require.NoError(t, err)

	// current stock is 0, min stock is 10 -> low stock
	assert.True(t, p.IsLowStock())

	// current stock is 10, min stock is 10 -> low stock
	p.CurrentStock = 10
	assert.True(t, p.IsLowStock())

	// current stock is 11, min stock is 10 -> normal stock
	p.CurrentStock = 11
	assert.False(t, p.IsLowStock())

	// inactive product should not trigger low stock alert
	p.IsActive = false
	p.CurrentStock = 2
	assert.False(t, p.IsLowStock())
}

func TestProduct_CanDeductStock(t *testing.T) {
	p, err := domain.NewProduct("SKU-1001", "Item", "desc", "PCS", 5)
	require.NoError(t, err)
	p.CurrentStock = 10

	// Valid deduction
	assert.NoError(t, p.CanDeductStock(5))
	assert.NoError(t, p.CanDeductStock(10))

	// Insufficient stock
	assert.ErrorIs(t, p.CanDeductStock(11), domain.ErrInsufficientStock)

	// Invalid quantity
	assert.ErrorIs(t, p.CanDeductStock(0), domain.ErrInvalidQuantity)
	assert.ErrorIs(t, p.CanDeductStock(-3), domain.ErrInvalidQuantity)
}

func TestProduct_ReceiveStock(t *testing.T) {
	p, err := domain.NewProduct("SKU-1001", "Item", "desc", "PCS", 5)
	require.NoError(t, err)
	assert.Equal(t, 0, p.CurrentStock)

	// Valid receipt
	err = p.ReceiveStock(25)
	require.NoError(t, err)
	assert.Equal(t, 25, p.CurrentStock)

	// Additional receipt accumulates correctly
	err = p.ReceiveStock(10)
	require.NoError(t, err)
	assert.Equal(t, 35, p.CurrentStock)

	// Reject non-positive quantities
	assert.ErrorIs(t, p.ReceiveStock(0), domain.ErrInvalidQuantity)
	assert.ErrorIs(t, p.ReceiveStock(-5), domain.ErrInvalidQuantity)

	// Inactive product cannot receive stock
	p.Deactivate()
	assert.ErrorIs(t, p.ReceiveStock(10), domain.ErrProductInactive)
}

func TestProduct_DeductStock(t *testing.T) {
	p, err := domain.NewProduct("SKU-1001", "Item", "desc", "PCS", 5)
	require.NoError(t, err)
	p.CurrentStock = 20

	require.NoError(t, p.DeductStock(5))
	assert.Equal(t, 15, p.CurrentStock)

	// Stock can never become negative.
	assert.ErrorIs(t, p.DeductStock(16), domain.ErrInsufficientStock)
	assert.Equal(t, 15, p.CurrentStock)

	// Non-positive quantities are rejected.
	assert.ErrorIs(t, p.DeductStock(0), domain.ErrInvalidQuantity)
	assert.ErrorIs(t, p.DeductStock(-3), domain.ErrInvalidQuantity)
	assert.Equal(t, 15, p.CurrentStock)

	// Inactive products cannot be issued.
	p.Deactivate()
	assert.ErrorIs(t, p.DeductStock(1), domain.ErrProductInactive)
	assert.Equal(t, 15, p.CurrentStock)
}
