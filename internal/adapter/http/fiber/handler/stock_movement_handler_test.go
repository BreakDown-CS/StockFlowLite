package handler_test

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/BreakDown-CS/StockFlowLite/internal/adapter/http/fiber/handler"
	"github.com/BreakDown-CS/StockFlowLite/internal/domain"
	"github.com/BreakDown-CS/StockFlowLite/internal/usecase/stockmovement"
	"github.com/BreakDown-CS/StockFlowLite/pkg/response"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func setupMovementTestApp(h *handler.StockMovementHandler) *fiber.App {
	app := fiber.New(fiber.Config{
		DisableStartupMessage: true,
	})
	app.Get("/api/v1/stock-movements", h.List)
	return app
}

func TestStockMovementHandler_List_Success(t *testing.T) {
	uc := new(mockStockMovementUsecase)
	h := handler.NewStockMovementHandler(uc)
	app := setupMovementTestApp(h)

	prodID := uuid.New()
	m1, _ := domain.NewStockMovement(prodID, domain.MovementReceipt, 50, "STOCK_RECEIPT", "", nil, "Batch 1")
	m1.ID = 1
	m2, _ := domain.NewStockMovement(prodID, domain.MovementReceipt, 20, "STOCK_RECEIPT", "", nil, "Batch 2")
	m2.ID = 2

	uc.On("ListMovements", mock.Anything, mock.MatchedBy(func(input stockmovement.ListMovementsInput) bool {
		return input.Limit == 10 && input.Offset == 0
	})).Return([]domain.StockMovement{*m1, *m2}, int64(2), nil)

	req := httptest.NewRequest("GET", "/api/v1/stock-movements?limit=10&offset=0", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, fiber.StatusOK, resp.StatusCode)

	var res response.Envelope
	err = json.NewDecoder(resp.Body).Decode(&res)
	require.NoError(t, err)
	assert.True(t, res.Success)
	uc.AssertExpectations(t)
}

func TestStockMovementHandler_List_WithFilters(t *testing.T) {
	uc := new(mockStockMovementUsecase)
	h := handler.NewStockMovementHandler(uc)
	app := setupMovementTestApp(h)

	prodID := uuid.New()
	mType := domain.MovementReceipt

	uc.On("ListMovements", mock.Anything, mock.MatchedBy(func(input stockmovement.ListMovementsInput) bool {
		return input.ProductID != nil && *input.ProductID == prodID &&
			input.MovementType != nil && *input.MovementType == mType
	})).Return([]domain.StockMovement{}, int64(0), nil)

	url := "/api/v1/stock-movements?product_id=" + prodID.String() + "&movement_type=RECEIPT"
	req := httptest.NewRequest("GET", url, nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, fiber.StatusOK, resp.StatusCode)
	uc.AssertExpectations(t)
}

func TestStockMovementHandler_List_InvalidProductID(t *testing.T) {
	uc := new(mockStockMovementUsecase)
	h := handler.NewStockMovementHandler(uc)
	app := setupMovementTestApp(h)

	req := httptest.NewRequest("GET", "/api/v1/stock-movements?product_id=not-a-uuid", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, fiber.StatusBadRequest, resp.StatusCode)
}

func TestStockMovementHandler_List_InvalidMovementType(t *testing.T) {
	uc := new(mockStockMovementUsecase)
	h := handler.NewStockMovementHandler(uc)
	app := setupMovementTestApp(h)

	req := httptest.NewRequest("GET", "/api/v1/stock-movements?movement_type=BOGUS", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, fiber.StatusBadRequest, resp.StatusCode)
}
