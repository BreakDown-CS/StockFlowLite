package stockmovement_test

import (
	"context"
	"testing"

	"github.com/BreakDown-CS/StockFlowLite/internal/domain"
	"github.com/BreakDown-CS/StockFlowLite/internal/usecase/stockmovement"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type mockStockMovementRepo struct {
	mock.Mock
}

func (m *mockStockMovementRepo) ReceiveStock(
	ctx context.Context,
	productID uuid.UUID,
	quantity int,
	notes string,
	refType string,
	refID string,
) (*domain.StockReceiptResult, error) {
	args := m.Called(ctx, productID, quantity, notes, refType, refID)
	if res := args.Get(0); res != nil {
		return res.(*domain.StockReceiptResult), args.Error(1)
	}
	return nil, args.Error(1)
}

func (m *mockStockMovementRepo) ListMovements(
	ctx context.Context,
	filter domain.ListMovementFilter,
) ([]domain.StockMovement, int64, error) {
	args := m.Called(ctx, filter)
	return args.Get(0).([]domain.StockMovement), args.Get(1).(int64), args.Error(2)
}

func TestStockMovementUsecase_ReceiveStock_Success(t *testing.T) {
	repo := new(mockStockMovementRepo)
	uc := stockmovement.NewStockMovementUsecase(repo)

	prodID := uuid.New()
	p, _ := domain.NewProduct("SKU-100", "Widget", "desc", "PCS", 5)
	p.ID = prodID
	p.CurrentStock = 20

	movement, _ := domain.NewStockMovement(prodID, domain.MovementReceipt, 20, "STOCK_RECEIPT", "", nil, "Inbound delivery")

	expectedResult := &domain.StockReceiptResult{
		Product:  p,
		Movement: movement,
	}

	repo.On("ReceiveStock", mock.Anything, prodID, 20, "Inbound delivery", "STOCK_RECEIPT", "").
		Return(expectedResult, nil)

	result, err := uc.ReceiveStock(context.Background(), stockmovement.ReceiveStockInput{
		ProductID: prodID,
		Quantity:  20,
		Notes:     "Inbound delivery",
	})
	require.NoError(t, err)
	assert.Equal(t, 20, result.Product.CurrentStock)
	assert.Equal(t, domain.MovementReceipt, result.Movement.MovementType)
	repo.AssertExpectations(t)
}

func TestStockMovementUsecase_ReceiveStock_ValidationErrors(t *testing.T) {
	repo := new(mockStockMovementRepo)
	uc := stockmovement.NewStockMovementUsecase(repo)

	prodID := uuid.New()

	// Missing product ID
	_, err := uc.ReceiveStock(context.Background(), stockmovement.ReceiveStockInput{
		ProductID: uuid.Nil,
		Quantity:  10,
	})
	assert.ErrorIs(t, err, domain.ErrMissingProductID)

	// Zero quantity
	_, err = uc.ReceiveStock(context.Background(), stockmovement.ReceiveStockInput{
		ProductID: prodID,
		Quantity:  0,
	})
	assert.ErrorIs(t, err, domain.ErrInvalidQuantity)

	// Negative quantity
	_, err = uc.ReceiveStock(context.Background(), stockmovement.ReceiveStockInput{
		ProductID: prodID,
		Quantity:  -5,
	})
	assert.ErrorIs(t, err, domain.ErrInvalidQuantity)

	repo.AssertNotCalled(t, "ReceiveStock", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

func TestStockMovementUsecase_ReceiveStock_ProductNotFound(t *testing.T) {
	repo := new(mockStockMovementRepo)
	uc := stockmovement.NewStockMovementUsecase(repo)

	prodID := uuid.New()
	repo.On("ReceiveStock", mock.Anything, prodID, 10, "", "STOCK_RECEIPT", "").
		Return(nil, domain.ErrProductNotFound)

	_, err := uc.ReceiveStock(context.Background(), stockmovement.ReceiveStockInput{
		ProductID: prodID,
		Quantity:  10,
	})
	assert.ErrorIs(t, err, domain.ErrProductNotFound)
	repo.AssertExpectations(t)
}

func TestStockMovementUsecase_ReceiveStock_ProductInactive(t *testing.T) {
	repo := new(mockStockMovementRepo)
	uc := stockmovement.NewStockMovementUsecase(repo)

	prodID := uuid.New()
	repo.On("ReceiveStock", mock.Anything, prodID, 10, "", "STOCK_RECEIPT", "").
		Return(nil, domain.ErrProductInactive)

	_, err := uc.ReceiveStock(context.Background(), stockmovement.ReceiveStockInput{
		ProductID: prodID,
		Quantity:  10,
	})
	assert.ErrorIs(t, err, domain.ErrProductInactive)
	repo.AssertExpectations(t)
}

func TestStockMovementUsecase_ListMovements_PaginationDefaults(t *testing.T) {
	repo := new(mockStockMovementRepo)
	uc := stockmovement.NewStockMovementUsecase(repo)

	repo.On("ListMovements", mock.Anything, mock.MatchedBy(func(f domain.ListMovementFilter) bool {
		return f.Limit == 20 && f.Offset == 0 && f.ProductID == nil && f.MovementType == nil
	})).Return([]domain.StockMovement{}, int64(0), nil)

	movements, total, err := uc.ListMovements(context.Background(), stockmovement.ListMovementsInput{})
	require.NoError(t, err)
	assert.Empty(t, movements)
	assert.Equal(t, int64(0), total)
	repo.AssertExpectations(t)
}

func TestStockMovementUsecase_ListMovements_InvalidMovementType(t *testing.T) {
	repo := new(mockStockMovementRepo)
	uc := stockmovement.NewStockMovementUsecase(repo)

	invalidType := domain.MovementType("UNKNOWN_TYPE")
	_, _, err := uc.ListMovements(context.Background(), stockmovement.ListMovementsInput{
		MovementType: &invalidType,
	})
	assert.ErrorIs(t, err, domain.ErrInvalidMovementType)
	repo.AssertNotCalled(t, "ListMovements", mock.Anything, mock.Anything)
}
