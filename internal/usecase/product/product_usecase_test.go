package product_test

import (
	"context"
	"testing"

	"github.com/BreakDown-CS/StockFlowLite/internal/domain"
	productusecase "github.com/BreakDown-CS/StockFlowLite/internal/usecase/product"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type mockProductRepo struct {
	mock.Mock
}

func (m *mockProductRepo) Create(ctx context.Context, p *domain.Product) error {
	args := m.Called(ctx, p)
	return args.Error(0)
}

func (m *mockProductRepo) GetByID(ctx context.Context, id uuid.UUID) (*domain.Product, error) {
	args := m.Called(ctx, id)
	if p := args.Get(0); p != nil {
		return p.(*domain.Product), args.Error(1)
	}
	return nil, args.Error(1)
}

func (m *mockProductRepo) GetBySKU(ctx context.Context, sku string) (*domain.Product, error) {
	args := m.Called(ctx, sku)
	if p := args.Get(0); p != nil {
		return p.(*domain.Product), args.Error(1)
	}
	return nil, args.Error(1)
}

func (m *mockProductRepo) List(ctx context.Context, filter domain.ListProductFilter) ([]domain.Product, int64, error) {
	args := m.Called(ctx, filter)
	return args.Get(0).([]domain.Product), args.Get(1).(int64), args.Error(2)
}

func (m *mockProductRepo) Update(ctx context.Context, p *domain.Product) error {
	args := m.Called(ctx, p)
	return args.Error(0)
}

func (m *mockProductRepo) Deactivate(ctx context.Context, id uuid.UUID) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

func TestProductUsecase_CreateProduct_Success(t *testing.T) {
	repo := new(mockProductRepo)
	uc := productusecase.NewProductUsecase(repo)

	input := productusecase.CreateProductInput{
		SKU:         "SKU-001",
		Name:        "Test Product",
		Description: "A test item",
		Unit:        "PCS",
		MinStock:    5,
	}

	repo.On("Create", mock.Anything, mock.MatchedBy(func(p *domain.Product) bool {
		return p.SKU == "SKU-001" && p.Name == "Test Product" && p.CurrentStock == 0 && p.IsActive
	})).Return(nil)

	p, err := uc.CreateProduct(context.Background(), input)
	require.NoError(t, err)
	assert.Equal(t, "SKU-001", p.SKU)
	assert.Equal(t, 0, p.CurrentStock)
	assert.True(t, p.IsActive)
	repo.AssertExpectations(t)
}

func TestProductUsecase_CreateProduct_DuplicateSKU(t *testing.T) {
	repo := new(mockProductRepo)
	uc := productusecase.NewProductUsecase(repo)

	input := productusecase.CreateProductInput{
		SKU:      "SKU-EXISTS",
		Name:     "Existing Item",
		Unit:     "PCS",
		MinStock: 0,
	}

	repo.On("Create", mock.Anything, mock.Anything).Return(domain.ErrDuplicateSKU)

	p, err := uc.CreateProduct(context.Background(), input)
	require.ErrorIs(t, err, domain.ErrDuplicateSKU)
	assert.Nil(t, p)
	repo.AssertExpectations(t)
}

func TestProductUsecase_GetProductByID(t *testing.T) {
	repo := new(mockProductRepo)
	uc := productusecase.NewProductUsecase(repo)

	prodID := uuid.New()
	existingProd, _ := domain.NewProduct("SKU-001", "Item", "desc", "PCS", 2)
	existingProd.ID = prodID

	repo.On("GetByID", mock.Anything, prodID).Return(existingProd, nil)
	repo.On("GetByID", mock.Anything, uuid.Nil).Return(nil, domain.ErrInvalidProductID)

	// Success
	p, err := uc.GetProductByID(context.Background(), prodID)
	require.NoError(t, err)
	assert.Equal(t, prodID, p.ID)

	// Nil UUID check
	_, err = uc.GetProductByID(context.Background(), uuid.Nil)
	require.ErrorIs(t, err, domain.ErrInvalidProductID)
}

func TestProductUsecase_ListProducts_PaginationDefaults(t *testing.T) {
	repo := new(mockProductRepo)
	uc := productusecase.NewProductUsecase(repo)

	// Expect normalized pagination (default limit 20, offset 0, isActive default true)
	repo.On("List", mock.Anything, mock.MatchedBy(func(f domain.ListProductFilter) bool {
		return f.Limit == 20 && f.Offset == 0 && *f.IsActive == true
	})).Return([]domain.Product{}, int64(0), nil)

	products, total, err := uc.ListProducts(context.Background(), productusecase.ListProductsInput{})
	require.NoError(t, err)
	assert.Empty(t, products)
	assert.Equal(t, int64(0), total)
	repo.AssertExpectations(t)
}

func TestProductUsecase_UpdateProduct_Success(t *testing.T) {
	repo := new(mockProductRepo)
	uc := productusecase.NewProductUsecase(repo)

	prodID := uuid.New()
	existing, _ := domain.NewProduct("SKU-ORIGINAL", "Old Name", "Old Desc", "PCS", 5)
	existing.ID = prodID
	existing.CurrentStock = 50 // existing stock must NOT be altered

	repo.On("GetByID", mock.Anything, prodID).Return(existing, nil)
	repo.On("Update", mock.Anything, mock.MatchedBy(func(p *domain.Product) bool {
		return p.ID == prodID && p.SKU == "SKU-ORIGINAL" && p.Name == "New Name" && p.CurrentStock == 50
	})).Return(nil)

	updated, err := uc.UpdateProduct(context.Background(), prodID, productusecase.UpdateProductInput{
		Name:        "New Name",
		Description: "New Desc",
		Unit:        "BOX",
		MinStock:    10,
	})
	require.NoError(t, err)
	assert.Equal(t, "New Name", updated.Name)
	assert.Equal(t, "SKU-ORIGINAL", updated.SKU) // SKU unchanged
	assert.Equal(t, 50, updated.CurrentStock)    // Stock unchanged
	repo.AssertExpectations(t)
}

func TestProductUsecase_DeactivateProduct_Success(t *testing.T) {
	repo := new(mockProductRepo)
	uc := productusecase.NewProductUsecase(repo)

	prodID := uuid.New()
	existing, _ := domain.NewProduct("SKU-001", "Item", "desc", "PCS", 2)
	existing.ID = prodID
	existing.IsActive = true

	repo.On("GetByID", mock.Anything, prodID).Return(existing, nil)
	repo.On("Deactivate", mock.Anything, prodID).Return(nil)

	err := uc.DeactivateProduct(context.Background(), prodID)
	require.NoError(t, err)
	repo.AssertExpectations(t)
}

func TestProductUsecase_DeactivateProduct_NotFound(t *testing.T) {
	repo := new(mockProductRepo)
	uc := productusecase.NewProductUsecase(repo)

	prodID := uuid.New()
	repo.On("GetByID", mock.Anything, prodID).Return(nil, domain.ErrProductNotFound)

	err := uc.DeactivateProduct(context.Background(), prodID)
	require.ErrorIs(t, err, domain.ErrProductNotFound)
	repo.AssertExpectations(t)
}
