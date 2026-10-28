package product

import (
	"context"

	"github.com/BreakDown-CS/StockFlowLite/internal/domain"
	"github.com/google/uuid"
)

// CreateProductInput holds validated data for creating a new product.
type CreateProductInput struct {
	SKU         string
	Name        string
	Description string
	Unit        string
	MinStock    int
}

// UpdateProductInput holds validated data for updating a product.
type UpdateProductInput struct {
	Name        string
	Description string
	Unit        string
	MinStock    int
}

// ListProductsInput defines query constraints for product listings.
type ListProductsInput struct {
	Search   string
	Limit    int
	Offset   int
	IsActive *bool
}

// ProductUsecase defines the business application operations for products.
type ProductUsecase interface {
	CreateProduct(ctx context.Context, input CreateProductInput) (*domain.Product, error)
	GetProductByID(ctx context.Context, id uuid.UUID) (*domain.Product, error)
	ListProducts(ctx context.Context, input ListProductsInput) ([]domain.Product, int64, error)
	UpdateProduct(ctx context.Context, id uuid.UUID, input UpdateProductInput) (*domain.Product, error)
	DeactivateProduct(ctx context.Context, id uuid.UUID) error
}

type productUsecase struct {
	repo domain.ProductRepository
}

// NewProductUsecase constructs a ProductUsecase with the given repository.
func NewProductUsecase(repo domain.ProductRepository) ProductUsecase {
	return &productUsecase{
		repo: repo,
	}
}

func (uc *productUsecase) CreateProduct(ctx context.Context, input CreateProductInput) (*domain.Product, error) {
	// Constructing domain model enforces initial stock = 0 and domain validations
	p, err := domain.NewProduct(input.SKU, input.Name, input.Description, input.Unit, input.MinStock)
	if err != nil {
		return nil, err
	}

	if err := uc.repo.Create(ctx, p); err != nil {
		return nil, err
	}

	return p, nil
}

func (uc *productUsecase) GetProductByID(ctx context.Context, id uuid.UUID) (*domain.Product, error) {
	if id == uuid.Nil {
		return nil, domain.ErrInvalidProductID
	}
	return uc.repo.GetByID(ctx, id)
}

func (uc *productUsecase) ListProducts(ctx context.Context, input ListProductsInput) ([]domain.Product, int64, error) {
	// Apply safe pagination boundaries
	limit := input.Limit
	if limit <= 0 {
		limit = 20
	} else if limit > 100 {
		limit = 100
	}

	offset := input.Offset
	if offset < 0 {
		offset = 0
	}

	// Exclude inactive products by default unless explicitly specified
	isActive := input.IsActive
	if isActive == nil {
		defaultActive := true
		isActive = &defaultActive
	}

	filter := domain.ListProductFilter{
		Search:   input.Search,
		Limit:    limit,
		Offset:   offset,
		IsActive: isActive,
	}

	return uc.repo.List(ctx, filter)
}

func (uc *productUsecase) UpdateProduct(ctx context.Context, id uuid.UUID, input UpdateProductInput) (*domain.Product, error) {
	if id == uuid.Nil {
		return nil, domain.ErrInvalidProductID
	}

	p, err := uc.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	// Update domain entity (protects SKU, current stock, and invariants)
	if err := p.Update(input.Name, input.Description, input.Unit, input.MinStock); err != nil {
		return nil, err
	}

	if err := uc.repo.Update(ctx, p); err != nil {
		return nil, err
	}

	return p, nil
}

func (uc *productUsecase) DeactivateProduct(ctx context.Context, id uuid.UUID) error {
	if id == uuid.Nil {
		return domain.ErrInvalidProductID
	}

	p, err := uc.repo.GetByID(ctx, id)
	if err != nil {
		return err
	}

	if !p.IsActive {
		// Idempotent: already deactivated
		return nil
	}

	return uc.repo.Deactivate(ctx, id)
}
