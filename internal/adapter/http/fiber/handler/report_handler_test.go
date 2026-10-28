package handler_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/BreakDown-CS/StockFlowLite/internal/adapter/http/fiber/handler"
	"github.com/BreakDown-CS/StockFlowLite/internal/domain"
	reportuc "github.com/BreakDown-CS/StockFlowLite/internal/usecase/report"
	"github.com/BreakDown-CS/StockFlowLite/pkg/response"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type mockReportUsecase struct{ mock.Mock }

func (m *mockReportUsecase) Inventory(ctx context.Context, input reportuc.InventoryInput) (*reportuc.InventoryReport, int64, error) {
	args := m.Called(ctx, input)
	report, _ := args.Get(0).(*reportuc.InventoryReport)
	return report, args.Get(1).(int64), args.Error(2)
}

func (m *mockReportUsecase) Movements(ctx context.Context, input reportuc.MovementInput) (*reportuc.MovementReport, int64, error) {
	args := m.Called(ctx, input)
	report, _ := args.Get(0).(*reportuc.MovementReport)
	return report, args.Get(1).(int64), args.Error(2)
}

func (m *mockReportUsecase) Requests(ctx context.Context, input reportuc.RequestInput) (*reportuc.RequestReport, int64, error) {
	args := m.Called(ctx, input)
	report, _ := args.Get(0).(*reportuc.RequestReport)
	return report, args.Get(1).(int64), args.Error(2)
}

func (m *mockReportUsecase) StockIntegrity(ctx context.Context, input reportuc.StockIntegrityInput) (*reportuc.StockIntegrityReport, int64, error) {
	args := m.Called(ctx, input)
	report, _ := args.Get(0).(*reportuc.StockIntegrityReport)
	return report, args.Get(1).(int64), args.Error(2)
}

func setupReportTestApp(h *handler.ReportHandler) *fiber.App {
	app := fiber.New(fiber.Config{DisableStartupMessage: true})
	app.Get("/api/v1/reports/inventory", h.Inventory)
	app.Get("/api/v1/reports/stock-movements", h.Movements)
	app.Get("/api/v1/reports/stock-requests", h.Requests)
	app.Get("/api/v1/reports/integrity/stock", h.Integrity)
	return app
}

func doReportRequest(app *fiber.App, url string) (*http.Response, error) {
	return app.Test(httptest.NewRequest("GET", url, nil), -1)
}

func TestReportHandler_Inventory_Success(t *testing.T) {
	uc := new(mockReportUsecase)
	h := handler.NewReportHandler(uc)
	app := setupReportTestApp(h)

	uc.On("Inventory", mock.Anything, mock.MatchedBy(func(input reportuc.InventoryInput) bool {
		// Defaults mirror GET /products: active products only.
		return input.IsActive != nil && *input.IsActive && !input.LowStock &&
			input.Limit == reportuc.DefaultPageSize && input.Offset == 0
	})).Return(&reportuc.InventoryReport{
		Summary: domain.InventorySummary{TotalProducts: 1, ActiveProducts: 1, LowStockProducts: 1},
		Items: []domain.InventoryReportRow{{
			SKU: "SKU-1", CurrentStock: 0, MinStock: 5, IsActive: true, IsLowStock: true, IsOutOfStock: true,
		}},
	}, int64(1), nil)

	resp, err := doReportRequest(app, "/api/v1/reports/inventory")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, fiber.StatusOK, resp.StatusCode)

	var envelope struct {
		Success bool `json:"success"`
		Data    struct {
			Summary domain.InventorySummary     `json:"summary"`
			Items   []domain.InventoryReportRow `json:"items"`
		} `json:"data"`
		Meta response.PageMeta `json:"meta"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&envelope))
	assert.True(t, envelope.Success)
	assert.EqualValues(t, 1, envelope.Data.Summary.LowStockProducts)
	require.Len(t, envelope.Data.Items, 1)
	assert.Equal(t, "SKU-1", envelope.Data.Items[0].SKU)
	assert.True(t, envelope.Data.Items[0].IsLowStock)
	assert.Equal(t, 20, envelope.Meta.Limit)
	assert.EqualValues(t, 1, envelope.Meta.Total)
	uc.AssertExpectations(t)
}

func TestReportHandler_Inventory_ForwardsFilters(t *testing.T) {
	uc := new(mockReportUsecase)
	h := handler.NewReportHandler(uc)
	app := setupReportTestApp(h)

	uc.On("Inventory", mock.Anything, mock.MatchedBy(func(input reportuc.InventoryInput) bool {
		return input.IsActive == nil && input.LowStock && input.Search == "pen" &&
			input.Limit == 5 && input.Offset == 2
	})).Return(&reportuc.InventoryReport{Items: []domain.InventoryReportRow{}}, int64(0), nil)

	resp, err := doReportRequest(app, "/api/v1/reports/inventory?is_active=all&low_stock=true&search=pen&limit=5&offset=2")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, fiber.StatusOK, resp.StatusCode)
	uc.AssertExpectations(t)
}

func TestReportHandler_InvalidParameters(t *testing.T) {
	paths := []string{
		"/api/v1/reports/inventory?is_active=maybe",
		"/api/v1/reports/inventory?low_stock=maybe",
		"/api/v1/reports/inventory?limit=0",
		"/api/v1/reports/inventory?limit=abc",
		"/api/v1/reports/inventory?offset=-1",
		"/api/v1/reports/stock-movements?product_id=not-a-uuid",
		"/api/v1/reports/stock-movements?from=yesterday",
		"/api/v1/reports/stock-movements?to=2026-10-28",
		"/api/v1/reports/stock-requests?requester_id=nope",
		"/api/v1/reports/stock-requests?from=bad",
	}

	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			uc := new(mockReportUsecase)
			h := handler.NewReportHandler(uc)
			app := setupReportTestApp(h)

			resp, err := doReportRequest(app, path)
			require.NoError(t, err)
			defer resp.Body.Close()

			assert.Equal(t, fiber.StatusBadRequest, resp.StatusCode)

			var envelope response.Envelope
			require.NoError(t, json.NewDecoder(resp.Body).Decode(&envelope))
			detail, ok := envelope.Error.(map[string]interface{})
			require.True(t, ok)
			assert.Equal(t, "INVALID_PARAMETER", detail["code"])
			uc.AssertNotCalled(t, "Inventory", mock.Anything, mock.Anything)
		})
	}
}

func TestReportHandler_Movements_SuccessForwardsFilters(t *testing.T) {
	uc := new(mockReportUsecase)
	h := handler.NewReportHandler(uc)
	app := setupReportTestApp(h)

	productID := uuid.New()
	from := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 10, 31, 23, 59, 59, 0, time.UTC)

	uc.On("Movements", mock.Anything, mock.MatchedBy(func(input reportuc.MovementInput) bool {
		return input.ProductID != nil && *input.ProductID == productID &&
			input.MovementType != nil && *input.MovementType == domain.MovementIssue &&
			input.From != nil && input.From.Equal(from) &&
			input.To != nil && input.To.Equal(to)
	})).Return(&reportuc.MovementReport{
		Summary: domain.MovementSummary{MovementCount: 1, IssueTotalQuantity: 4, NetChange: -4},
		Items:   []domain.MovementReportRow{{ID: 1, SignedQuantity: -4}},
	}, int64(1), nil)

	url := "/api/v1/reports/stock-movements?product_id=" + productID.String() +
		"&movement_type=issue&from=" + from.Format(time.RFC3339) + "&to=" + to.Format(time.RFC3339)

	resp, err := doReportRequest(app, url)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, fiber.StatusOK, resp.StatusCode)

	var envelope struct {
		Data struct {
			Summary domain.MovementSummary     `json:"summary"`
			Items   []domain.MovementReportRow `json:"items"`
		} `json:"data"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&envelope))
	assert.EqualValues(t, -4, envelope.Data.Summary.NetChange)
	assert.Len(t, envelope.Data.Items, 1)
	uc.AssertExpectations(t)
}

func TestReportHandler_Movements_MapsDomainErrors(t *testing.T) {
	cases := []struct {
		name       string
		usecaseErr error
	}{
		{name: "invalid date range", usecaseErr: domain.ErrInvalidDateRange},
		{name: "invalid movement type", usecaseErr: domain.ErrInvalidMovementType},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			uc := new(mockReportUsecase)
			h := handler.NewReportHandler(uc)
			app := setupReportTestApp(h)

			uc.On("Movements", mock.Anything, mock.Anything).Return(nil, int64(0), tc.usecaseErr)

			resp, err := doReportRequest(app, "/api/v1/reports/stock-movements")
			require.NoError(t, err)
			defer resp.Body.Close()

			assert.Equal(t, fiber.StatusBadRequest, resp.StatusCode)
			uc.AssertExpectations(t)
		})
	}
}

func TestReportHandler_Requests_Success(t *testing.T) {
	uc := new(mockReportUsecase)
	h := handler.NewReportHandler(uc)
	app := setupReportTestApp(h)

	requesterID := uuid.New()
	uc.On("Requests", mock.Anything, mock.MatchedBy(func(input reportuc.RequestInput) bool {
		return input.Status != nil && *input.Status == domain.StatusIssued &&
			input.RequesterID != nil && *input.RequesterID == requesterID
	})).Return(&reportuc.RequestReport{
		Summary: domain.RequestSummary{
			TotalRequests: 1, IssuedRequests: 1, RequestedUnitsTotal: 10, IssuedUnitsTotal: 10,
		},
		Items: []domain.RequestReportRow{{
			ID: uuid.New(), RequestNumber: "REQ-1", Status: domain.StatusIssued,
			RequesterID: requesterID, RequesterUsername: "alice",
			ItemCount: 2, RequestedUnits: 10, IssuedUnits: 10,
		}},
	}, int64(1), nil)

	url := "/api/v1/reports/stock-requests?status=issued&requester_id=" + requesterID.String()
	resp, err := doReportRequest(app, url)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, fiber.StatusOK, resp.StatusCode)

	var envelope struct {
		Data struct {
			Summary domain.RequestSummary     `json:"summary"`
			Items   []domain.RequestReportRow `json:"items"`
		} `json:"data"`
		Meta response.PageMeta `json:"meta"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&envelope))
	assert.EqualValues(t, 10, envelope.Data.Summary.IssuedUnitsTotal)
	require.Len(t, envelope.Data.Items, 1)
	assert.Equal(t, 10, envelope.Data.Items[0].IssuedUnits)
	assert.Equal(t, "alice", envelope.Data.Items[0].RequesterUsername)
	assert.EqualValues(t, 1, envelope.Meta.Total)
	uc.AssertExpectations(t)
}

func TestReportHandler_Requests_UnauthenticatedMapsTo401(t *testing.T) {
	uc := new(mockReportUsecase)
	h := handler.NewReportHandler(uc)
	app := setupReportTestApp(h)

	uc.On("Requests", mock.Anything, mock.Anything).Return(nil, int64(0), domain.ErrUnauthenticated)

	resp, err := doReportRequest(app, "/api/v1/reports/stock-requests")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, fiber.StatusUnauthorized, resp.StatusCode)
	uc.AssertExpectations(t)
}

func TestReportHandler_Integrity(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		uc := new(mockReportUsecase)
		h := handler.NewReportHandler(uc)
		app := setupReportTestApp(h)

		uc.On("StockIntegrity", mock.Anything, mock.MatchedBy(func(input reportuc.StockIntegrityInput) bool {
			return input.OnlyDrifted && input.Limit == reportuc.DefaultPageSize && input.Offset == 0
		})).Return(&reportuc.StockIntegrityReport{
			Summary: domain.StockIntegritySummary{TotalProducts: 2, DriftedProducts: 1, AbsoluteDrift: 3},
			Items: []domain.StockIntegrityRow{{
				SKU: "SKU-1", CurrentStock: 7, LedgerStock: 10, Drift: -3, IsActive: true,
			}},
		}, int64(1), nil)

		resp, err := doReportRequest(app, "/api/v1/reports/integrity/stock?only_drifted=true")
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, fiber.StatusOK, resp.StatusCode)

		var envelope struct {
			Data struct {
				Summary domain.StockIntegritySummary `json:"summary"`
				Items   []domain.StockIntegrityRow   `json:"items"`
			} `json:"data"`
			Meta response.PageMeta `json:"meta"`
		}
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&envelope))
		assert.EqualValues(t, 1, envelope.Data.Summary.DriftedProducts)
		require.Len(t, envelope.Data.Items, 1)
		assert.EqualValues(t, -3, envelope.Data.Items[0].Drift)
		assert.EqualValues(t, 1, envelope.Meta.Total)
		uc.AssertExpectations(t)
	})

	t.Run("invalid only_drifted", func(t *testing.T) {
		uc := new(mockReportUsecase)
		h := handler.NewReportHandler(uc)
		app := setupReportTestApp(h)

		resp, err := doReportRequest(app, "/api/v1/reports/integrity/stock?only_drifted=maybe")
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, fiber.StatusBadRequest, resp.StatusCode)
		uc.AssertNotCalled(t, "StockIntegrity", mock.Anything, mock.Anything)
	})
}

func TestReportHandler_UnexpectedErrorDoesNotLeakInternals(t *testing.T) {
	uc := new(mockReportUsecase)
	h := handler.NewReportHandler(uc)
	app := setupReportTestApp(h)

	uc.On("Requests", mock.Anything, mock.Anything).
		Return(nil, int64(0), errors.New(`pq: relation "stock_requests" does not exist`))

	resp, err := doReportRequest(app, "/api/v1/reports/stock-requests")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, fiber.StatusInternalServerError, resp.StatusCode)

	var envelope response.Envelope
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&envelope))
	detail, ok := envelope.Error.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "INTERNAL_ERROR", detail["code"])
	assert.NotContains(t, detail["message"], "stock_requests")
	assert.NotContains(t, detail["message"], "pq:")
	uc.AssertExpectations(t)
}
