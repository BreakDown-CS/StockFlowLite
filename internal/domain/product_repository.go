package domain

import (
	"context"

	"github.com/google/uuid"
)

// ListProductFilter holds filtering and pagination parameters for product queries.
type ListProductFilter struct {
	Search   string
	Limit    int
	Offset   int
	IsActive *bool
}

// ProductRepository defines the persistence contract for Product domain entities.
type ProductRepository interface {
	Create(ctx context.Context, p *Product) error
	GetByID(ctx context.Context, id uuid.UUID) (*Product, error)
	GetBySKU(ctx context.Context, sku string) (*Product, error)
	List(ctx context.Context, filter ListProductFilter) ([]Product, int64, error)
	Update(ctx context.Context, p *Product) error
	Deactivate(ctx context.Context, id uuid.UUID) error
}
