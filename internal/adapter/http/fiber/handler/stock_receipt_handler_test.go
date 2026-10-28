package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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

type mockStockMovementUsecase struct {
	mock.Mock
}

func (m *mockStockMovementUsecase) ReceiveStock(
	ctx context.Context,
	input stockmovement.ReceiveStockInput,
) (*domain.StockReceiptResult, error) {
	args := m.Called(ctx, input)
	if res := args.Get(0); res != nil {
		return res.(*domain.StockReceiptResult), args.Error(1)
	}
	return nil, args.Error(1)
}

func (m *mockStockMovementUsecase) ListMovements(
	ctx context.Context,
	input stockmovement.ListMovementsInput,
) ([]domain.StockMovement, int64, error) {
	args := m.Called(ctx, input)
	return args.Get(0).([]domain.StockMovement), args.Get(1).(int64), args.Error(2)
}

func setupReceiptTestApp(h *handler.StockReceiptHandler) *fiber.App {
	app := fiber.New(fiber.Config{
		DisableStartupMessage: true,
	})
	app.Post("/api/v1/stock-receipts", h.Create)
	return app
}

func TestStockReceiptHandler_Create_Success(t *testing.T) {
	uc := new(mockStockMovementUsecase)
	h := handler.NewStockReceiptHandler(uc)
	app := setupReceiptTestApp(h)

	prodID := uuid.New()
	p, _ := domain.NewProduct("SKU-100", "Widget", "desc", "PCS", 5)
	p.ID = prodID
	p.CurrentStock = 30

	movement, _ := domain.NewStockMovement(prodID, domain.MovementReceipt, 30, "STOCK_RECEIPT", "", nil, "Initial delivery")

	expected := &domain.StockReceiptResult{
		Product:  p,
		Movement: movement,
	}

	uc.On("ReceiveStock", mock.Anything, mock.MatchedBy(func(input stockmovement.ReceiveStockInput) bool {
		return input.ProductID == prodID && input.Quantity == 30 && input.Notes == "Initial delivery"
	})).Return(expected, nil)

	payload := map[string]interface{}{
		"product_id": prodID.String(),
		"quantity":   30,
		"notes":      "Initial delivery",
	}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest("POST", "/api/v1/stock-receipts", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, fiber.StatusCreated, resp.StatusCode)

	var res response.Envelope
	err = json.NewDecoder(resp.Body).Decode(&res)
	require.NoError(t, err)
	assert.True(t, res.Success)
	assert.Equal(t, "Stock received successfully", res.Message)
	uc.AssertExpectations(t)
}

func TestStockReceiptHandler_Create_ValidationErrors(t *testing.T) {
	uc := new(mockStockMovementUsecase)
	h := handler.NewStockReceiptHandler(uc)
	app := setupReceiptTestApp(h)

	validID := uuid.New().String()

	tests := []struct {
		name       string
		payload    map[string]interface{}
		wantStatus int
		wantCode   string
	}{
		{
			name: "missing product_id",
			payload: map[string]interface{}{
				"quantity": 10,
			},
			wantStatus: fiber.StatusBadRequest,
			wantCode:   "VALIDATION_ERROR",
		},
		{
			name: "invalid product_id format",
			payload: map[string]interface{}{
				"product_id": "not-a-valid-uuid",
				"quantity":   10,
			},
			wantStatus: fiber.StatusBadRequest,
			wantCode:   "INVALID_ID",
		},
		{
			name: "missing quantity",
			payload: map[string]interface{}{
				"product_id": validID,
			},
			wantStatus: fiber.StatusBadRequest,
			wantCode:   "VALIDATION_ERROR",
		},
		{
			name: "zero quantity",
			payload: map[string]interface{}{
				"product_id": validID,
				"quantity":   0,
			},
			wantStatus: fiber.StatusBadRequest,
			wantCode:   "VALIDATION_ERROR",
		},
		{
			name: "negative quantity",
			payload: map[string]interface{}{
				"product_id": validID,
				"quantity":   -15,
			},
			wantStatus: fiber.StatusBadRequest,
			wantCode:   "VALIDATION_ERROR",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, _ := json.Marshal(tt.payload)
			req := httptest.NewRequest("POST", "/api/v1/stock-receipts", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			resp, err := app.Test(req)
			require.NoError(t, err)
			defer resp.Body.Close()

			assert.Equal(t, tt.wantStatus, resp.StatusCode)
		})
	}
}

func TestStockReceiptHandler_Create_ProductNotFound(t *testing.T) {
	uc := new(mockStockMovementUsecase)
	h := handler.NewStockReceiptHandler(uc)
	app := setupReceiptTestApp(h)

	prodID := uuid.New()
	uc.On("ReceiveStock", mock.Anything, mock.Anything).Return(nil, domain.ErrProductNotFound)

	payload := map[string]interface{}{
		"product_id": prodID.String(),
		"quantity":   10,
	}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest("POST", "/api/v1/stock-receipts", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, fiber.StatusNotFound, resp.StatusCode)
	uc.AssertExpectations(t)
}

func TestStockReceiptHandler_Create_ProductInactive(t *testing.T) {
	uc := new(mockStockMovementUsecase)
	h := handler.NewStockReceiptHandler(uc)
	app := setupReceiptTestApp(h)

	prodID := uuid.New()
	uc.On("ReceiveStock", mock.Anything, mock.Anything).Return(nil, domain.ErrProductInactive)

	payload := map[string]interface{}{
		"product_id": prodID.String(),
		"quantity":   10,
	}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest("POST", "/api/v1/stock-receipts", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, fiber.StatusBadRequest, resp.StatusCode)

	var res response.Envelope
	err = json.NewDecoder(resp.Body).Decode(&res)
	require.NoError(t, err)
	assert.False(t, res.Success)
	uc.AssertExpectations(t)
}

func TestStockReceiptHandler_Create_UnexpectedDatabaseError(t *testing.T) {
	uc := new(mockStockMovementUsecase)
	h := handler.NewStockReceiptHandler(uc)
	app := setupReceiptTestApp(h)

	prodID := uuid.New()
	uc.On("ReceiveStock", mock.Anything, mock.Anything).Return(nil, errors.New("pq: connection terminated unexpectedly"))

	payload := map[string]interface{}{
		"product_id": prodID.String(),
		"quantity":   10,
	}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest("POST", "/api/v1/stock-receipts", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, fiber.StatusInternalServerError, resp.StatusCode)

	var res response.Envelope
	err = json.NewDecoder(resp.Body).Decode(&res)
	require.NoError(t, err)
	assert.False(t, res.Success)
	// Must not leak internal SQL traces
	errMap := res.Error.(map[string]interface{})
	assert.NotContains(t, errMap["message"], "pq: connection terminated")
	uc.AssertExpectations(t)
}
