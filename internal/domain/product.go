package domain

import (
	"math"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Product represents an inventory item tracked by the warehouse.
type Product struct {
	ID           uuid.UUID `json:"id"`
	SKU          string    `json:"sku"`
	Name         string    `json:"name"`
	Description  string    `json:"description,omitempty"`
	Unit         string    `json:"unit"`
	CurrentStock int       `json:"current_stock"`
	MinStock     int       `json:"min_stock"`
	IsActive     bool      `json:"is_active"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// NewProduct creates and validates a new Product instance.
func NewProduct(sku, name, description, unit string, minStock int) (*Product, error) {
	now := time.Now().UTC()
	trimmedUnit := strings.TrimSpace(unit)
	if trimmedUnit == "" {
		trimmedUnit = "PCS"
	}

	p := &Product{
		ID:           uuid.New(),
		SKU:          strings.TrimSpace(sku),
		Name:         strings.TrimSpace(name),
		Description:  strings.TrimSpace(description),
		Unit:         trimmedUnit,
		CurrentStock: 0,
		MinStock:     minStock,
		IsActive:     true,
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	if err := p.Validate(); err != nil {
		return nil, err
	}
	return p, nil
}

// Validate ensures product attributes satisfy domain invariants.
func (p *Product) Validate() error {
	if strings.TrimSpace(p.SKU) == "" {
		return ErrEmptySKU
	}
	if len(p.SKU) > 50 {
		return ErrSKUTooLong
	}
	if strings.TrimSpace(p.Name) == "" {
		return ErrEmptyProductName
	}
	if len(p.Name) > 200 {
		return ErrProductNameTooLong
	}
	if strings.TrimSpace(p.Unit) == "" {
		return ErrEmptyUnit
	}
	if len(p.Unit) > 20 {
		return ErrUnitTooLong
	}
	if len(p.Description) > 1000 {
		return ErrDescriptionTooLong
	}
	if p.CurrentStock < 0 {
		return ErrNegativeStock
	}
	if p.MinStock < 0 {
		return ErrNegativeMinStock
	}
	return nil
}

// Update modifies editable attributes of the product while protecting SKU and current stock.
func (p *Product) Update(name, description, unit string, minStock int) error {
	trimmedName := strings.TrimSpace(name)
	trimmedDesc := strings.TrimSpace(description)
	trimmedUnit := strings.TrimSpace(unit)
	if trimmedUnit == "" {
		trimmedUnit = p.Unit
	}

	if trimmedName == "" {
		return ErrEmptyProductName
	}
	if len(trimmedName) > 200 {
		return ErrProductNameTooLong
	}
	if len(trimmedDesc) > 1000 {
		return ErrDescriptionTooLong
	}
	if len(trimmedUnit) > 20 {
		return ErrUnitTooLong
	}
	if minStock < 0 {
		return ErrNegativeMinStock
	}

	p.Name = trimmedName
	p.Description = trimmedDesc
	p.Unit = trimmedUnit
	p.MinStock = minStock
	p.UpdatedAt = time.Now().UTC()
	return nil
}

// ReceiveStock safely increments current stock by the given positive quantity.
func (p *Product) ReceiveStock(quantity int) error {
	if !p.IsActive {
		return ErrProductInactive
	}
	if quantity <= 0 {
		return ErrInvalidQuantity
	}
	// Check against PostgreSQL 32-bit signed integer boundary
	if int64(p.CurrentStock)+int64(quantity) > math.MaxInt32 {
		return ErrStockOverflow
	}

	p.CurrentStock += quantity
	p.UpdatedAt = time.Now().UTC()
	return nil
}

// DeductStock safely decrements current stock for an outbound operation (issuance).
// Inactive products and insufficient balances are rejected before any change is applied.
func (p *Product) DeductStock(quantity int) error {
	if !p.IsActive {
		return ErrProductInactive
	}
	if quantity <= 0 {
		return ErrInvalidQuantity
	}
	if err := p.CanDeductStock(quantity); err != nil {
		return err
	}

	p.CurrentStock -= quantity
	p.UpdatedAt = time.Now().UTC()
	return nil
}

// Deactivate sets the product active status to false.
func (p *Product) Deactivate() {
	p.IsActive = false
	p.UpdatedAt = time.Now().UTC()
}

// IsLowStock reports whether current inventory has reached or fallen below the threshold.
func (p *Product) IsLowStock() bool {
	return p.IsActive && p.CurrentStock <= p.MinStock
}

// CanDeductStock checks if the requested quantity can be safely deducted without going negative.
func (p *Product) CanDeductStock(quantity int) error {
	if quantity <= 0 {
		return ErrInvalidQuantity
	}
	if p.CurrentStock-quantity < 0 {
		return ErrInsufficientStock
	}
	return nil
}
