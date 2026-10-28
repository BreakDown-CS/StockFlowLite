package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/BreakDown-CS/StockFlowLite/internal/adapter/http/fiber/handler"
	"github.com/BreakDown-CS/StockFlowLite/internal/domain"
	"github.com/BreakDown-CS/StockFlowLite/internal/usecase/stockrequest"
	"github.com/BreakDown-CS/StockFlowLite/pkg/response"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type mockStockRequestUsecase struct {
	mock.Mock
}

func (m *mockStockRequestUsecase) CreateRequest(ctx context.Context, input stockrequest.CreateStockRequestInput) (*domain.StockRequest, error) {
	args := m.Called(ctx, input)
	if res := args.Get(0); res != nil {
		return res.(*domain.StockRequest), args.Error(1)
	}
	return nil, args.Error(1)
}

func (m *mockStockRequestUsecase) GetRequest(ctx context.Context, id uuid.UUID) (*domain.StockRequest, error) {
	args := m.Called(ctx, id)
	if res := args.Get(0); res != nil {
		return res.(*domain.StockRequest), args.Error(1)
	}
	return nil, args.Error(1)
}

func (m *mockStockRequestUsecase) ListRequests(ctx context.Context, input stockrequest.ListStockRequestsInput) ([]domain.StockRequest, int64, error) {
	args := m.Called(ctx, input)
	return args.Get(0).([]domain.StockRequest), args.Get(1).(int64), args.Error(2)
}

func (m *mockStockRequestUsecase) ApproveRequest(ctx context.Context, id uuid.UUID) (*domain.StockRequest, error) {
	args := m.Called(ctx, id)
	if res := args.Get(0); res != nil {
		return res.(*domain.StockRequest), args.Error(1)
	}
	return nil, args.Error(1)
}

func (m *mockStockRequestUsecase) RejectRequest(ctx context.Context, id uuid.UUID, input stockrequest.RejectStockRequestInput) (*domain.StockRequest, error) {
	args := m.Called(ctx, id, input)
	if res := args.Get(0); res != nil {
		return res.(*domain.StockRequest), args.Error(1)
	}
	return nil, args.Error(1)
}

func (m *mockStockRequestUsecase) CancelRequest(ctx context.Context, id uuid.UUID, reason string) (*domain.StockRequest, error) {
	args := m.Called(ctx, id, reason)
	if res := args.Get(0); res != nil {
		return res.(*domain.StockRequest), args.Error(1)
	}
	return nil, args.Error(1)
}

func (m *mockStockRequestUsecase) IssueRequest(ctx context.Context, id uuid.UUID, idempotencyKey string) (*domain.StockIssueResult, error) {
	args := m.Called(ctx, id, idempotencyKey)
	if res := args.Get(0); res != nil {
		return res.(*domain.StockIssueResult), args.Error(1)
	}
	return nil, args.Error(1)
}

func setupRequestTestApp(h *handler.StockRequestHandler) *fiber.App {
	app := fiber.New(fiber.Config{DisableStartupMessage: true})
	app.Post("/api/v1/stock-requests", h.Create)
	app.Get("/api/v1/stock-requests", h.List)
	app.Get("/api/v1/stock-requests/:id", h.GetByID)
	app.Post("/api/v1/stock-requests/:id/approve", h.Approve)
	app.Post("/api/v1/stock-requests/:id/reject", h.Reject)
	app.Post("/api/v1/stock-requests/:id/cancel", h.Cancel)
	app.Post("/api/v1/stock-requests/:id/issue", h.Issue)
	return app
}

func doJSON(app *fiber.App, method, url string, payload any) (*http.Response, error) {
	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		body = bytes.NewReader(encoded)
	}

	req := httptest.NewRequest(method, url, body)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return app.Test(req, -1)
}

func decodeEnvelope(t *testing.T, body io.Reader) response.Envelope {
	t.Helper()
	var envelope response.Envelope
	require.NoError(t, json.NewDecoder(body).Decode(&envelope))
	return envelope
}

func TestStockRequestHandler_Create_Success(t *testing.T) {
	uc := new(mockStockRequestUsecase)
	h := handler.NewStockRequestHandler(uc)
	app := setupRequestTestApp(h)

	productID := uuid.New()
	created := &domain.StockRequest{
		ID:            uuid.New(),
		RequestNumber: "REQ-20261028-ABCD1234",
		RequesterID:   uuid.New(),
		Status:        domain.StatusPending,
		Items:         []domain.StockRequestItem{{ProductID: productID, RequestedQuantity: 5}},
	}

	uc.On("CreateRequest", mock.Anything, mock.MatchedBy(func(input stockrequest.CreateStockRequestInput) bool {
		return len(input.Items) == 1 &&
			input.Items[0].ProductID == productID &&
			input.Items[0].Quantity == 5 &&
			input.Notes == "Monthly consumables"
	})).Return(created, nil)

	resp, err := doJSON(app, "POST", "/api/v1/stock-requests", map[string]any{
		"items": []map[string]any{{"product_id": productID.String(), "quantity": 5}},
		"notes": "  Monthly consumables  ",
	})
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, fiber.StatusCreated, resp.StatusCode)
	envelope := decodeEnvelope(t, resp.Body)
	assert.True(t, envelope.Success)
	assert.Equal(t, "Stock request created successfully", envelope.Message)
	uc.AssertExpectations(t)
}

func TestStockRequestHandler_Create_ValidationErrors(t *testing.T) {
	validID := uuid.New().String()

	cases := []struct {
		name     string
		payload  any
		wantCode string
	}{
		{
			name:     "empty items",
			payload:  map[string]any{"items": []map[string]any{}},
			wantCode: "VALIDATION_ERROR",
		},
		{
			name:     "missing items field",
			payload:  map[string]any{"notes": "no items"},
			wantCode: "VALIDATION_ERROR",
		},
		{
			name:     "missing product id",
			payload:  map[string]any{"items": []map[string]any{{"quantity": 3}}},
			wantCode: "VALIDATION_ERROR",
		},
		{
			name:     "invalid product id",
			payload:  map[string]any{"items": []map[string]any{{"product_id": "not-a-uuid", "quantity": 3}}},
			wantCode: "INVALID_ID",
		},
		{
			name:     "missing quantity",
			payload:  map[string]any{"items": []map[string]any{{"product_id": validID}}},
			wantCode: "VALIDATION_ERROR",
		},
		{
			name:     "zero quantity",
			payload:  map[string]any{"items": []map[string]any{{"product_id": validID, "quantity": 0}}},
			wantCode: "VALIDATION_ERROR",
		},
		{
			name:     "negative quantity",
			payload:  map[string]any{"items": []map[string]any{{"product_id": validID, "quantity": -4}}},
			wantCode: "VALIDATION_ERROR",
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			uc := new(mockStockRequestUsecase)
			h := handler.NewStockRequestHandler(uc)
			app := setupRequestTestApp(h)

			resp, err := doJSON(app, "POST", "/api/v1/stock-requests", tt.payload)
			require.NoError(t, err)
			defer resp.Body.Close()

			assert.Equal(t, fiber.StatusBadRequest, resp.StatusCode)
			envelope := decodeEnvelope(t, resp.Body)
			assert.False(t, envelope.Success)
			detail, ok := envelope.Error.(map[string]any)
			require.True(t, ok)
			assert.Equal(t, tt.wantCode, detail["code"])
			uc.AssertNotCalled(t, "CreateRequest", mock.Anything, mock.Anything)
		})
	}
}

func TestStockRequestHandler_Create_TooManyItems(t *testing.T) {
	uc := new(mockStockRequestUsecase)
	h := handler.NewStockRequestHandler(uc)
	app := setupRequestTestApp(h)

	items := make([]map[string]any, 0, stockrequest.MaxRequestItems+1)
	for i := 0; i <= stockrequest.MaxRequestItems; i++ {
		items = append(items, map[string]any{"product_id": uuid.New().String(), "quantity": 1})
	}

	resp, err := doJSON(app, "POST", "/api/v1/stock-requests", map[string]any{"items": items})
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, fiber.StatusBadRequest, resp.StatusCode)
	uc.AssertNotCalled(t, "CreateRequest", mock.Anything, mock.Anything)
}

func TestStockRequestHandler_Create_MalformedJSON(t *testing.T) {
	uc := new(mockStockRequestUsecase)
	h := handler.NewStockRequestHandler(uc)
	app := setupRequestTestApp(h)

	req := httptest.NewRequest("POST", "/api/v1/stock-requests", bytes.NewReader([]byte("{invalid")))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req, -1)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, fiber.StatusBadRequest, resp.StatusCode)
	uc.AssertNotCalled(t, "CreateRequest", mock.Anything, mock.Anything)
}

func TestStockRequestHandler_Create_ProductStateAndConflictErrors(t *testing.T) {
	cases := []struct {
		name       string
		usecaseErr error
		wantStatus int
		wantCode   string
	}{
		{name: "product not found", usecaseErr: domain.ErrProductNotFound, wantStatus: fiber.StatusNotFound, wantCode: "NOT_FOUND"},
		{name: "product inactive", usecaseErr: domain.ErrProductInactive, wantStatus: fiber.StatusBadRequest, wantCode: "PRODUCT_INACTIVE"},
		{name: "duplicate item", usecaseErr: domain.ErrDuplicateItem, wantStatus: fiber.StatusBadRequest, wantCode: "DUPLICATE_ITEM"},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			uc := new(mockStockRequestUsecase)
			h := handler.NewStockRequestHandler(uc)
			app := setupRequestTestApp(h)

			uc.On("CreateRequest", mock.Anything, mock.Anything).Return(nil, tt.usecaseErr)

			resp, err := doJSON(app, "POST", "/api/v1/stock-requests", map[string]any{
				"items": []map[string]any{{"product_id": uuid.New().String(), "quantity": 2}},
			})
			require.NoError(t, err)
			defer resp.Body.Close()

			assert.Equal(t, tt.wantStatus, resp.StatusCode)
			envelope := decodeEnvelope(t, resp.Body)
			detail, ok := envelope.Error.(map[string]any)
			require.True(t, ok)
			assert.Equal(t, tt.wantCode, detail["code"])
			uc.AssertExpectations(t)
		})
	}
}

func TestStockRequestHandler_Create_UnexpectedErrorDoesNotLeakInternals(t *testing.T) {
	uc := new(mockStockRequestUsecase)
	h := handler.NewStockRequestHandler(uc)
	app := setupRequestTestApp(h)

	uc.On("CreateRequest", mock.Anything, mock.Anything).
		Return(nil, errors.New("pq: relation \"stock_requests\" does not exist"))

	resp, err := doJSON(app, "POST", "/api/v1/stock-requests", map[string]any{
		"items": []map[string]any{{"product_id": uuid.New().String(), "quantity": 2}},
	})
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, fiber.StatusInternalServerError, resp.StatusCode)
	envelope := decodeEnvelope(t, resp.Body)
	detail, ok := envelope.Error.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "INTERNAL_ERROR", detail["code"])
	assert.NotContains(t, detail["message"], "stock_requests")
	assert.NotContains(t, detail["message"], "pq:")
	uc.AssertExpectations(t)
}

func TestStockRequestHandler_List_SuccessWithPaginationAndFilter(t *testing.T) {
	uc := new(mockStockRequestUsecase)
	h := handler.NewStockRequestHandler(uc)
	app := setupRequestTestApp(h)

	requestID := uuid.New()
	uc.On("ListRequests", mock.Anything, mock.MatchedBy(func(input stockrequest.ListStockRequestsInput) bool {
		return input.Limit == 5 &&
			input.Offset == 2 &&
			input.Status != nil &&
			*input.Status == domain.StatusApproved
	})).Return([]domain.StockRequest{{ID: requestID, Status: domain.StatusApproved}}, int64(7), nil)

	resp, err := doJSON(app, "GET", "/api/v1/stock-requests?limit=5&offset=2&status=approved", nil)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, fiber.StatusOK, resp.StatusCode)

	var envelope struct {
		Success bool              `json:"success"`
		Data    []json.RawMessage `json:"data"`
		Meta    response.PageMeta `json:"meta"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&envelope))
	assert.True(t, envelope.Success)
	assert.Len(t, envelope.Data, 1)
	assert.Equal(t, 5, envelope.Meta.Limit)
	assert.Equal(t, 2, envelope.Meta.Offset)
	assert.Equal(t, int64(7), envelope.Meta.Total)
	uc.AssertExpectations(t)
}

func TestStockRequestHandler_List_InvalidFilters(t *testing.T) {
	queries := []string{
		"limit=0",
		"limit=abc",
		"offset=-1",
		"status=DELIVERED",
	}

	for _, query := range queries {
		t.Run(query, func(t *testing.T) {
			uc := new(mockStockRequestUsecase)
			h := handler.NewStockRequestHandler(uc)
			app := setupRequestTestApp(h)

			resp, err := doJSON(app, "GET", "/api/v1/stock-requests?"+query, nil)
			require.NoError(t, err)
			defer resp.Body.Close()

			assert.Equal(t, fiber.StatusBadRequest, resp.StatusCode)
			uc.AssertNotCalled(t, "ListRequests", mock.Anything, mock.Anything)
		})
	}
}

func TestStockRequestHandler_List_ClampsLimit(t *testing.T) {
	uc := new(mockStockRequestUsecase)
	h := handler.NewStockRequestHandler(uc)
	app := setupRequestTestApp(h)

	uc.On("ListRequests", mock.Anything, mock.MatchedBy(func(input stockrequest.ListStockRequestsInput) bool {
		return input.Limit == stockrequest.MaxPageSize
	})).Return([]domain.StockRequest{}, int64(0), nil)

	resp, err := doJSON(app, "GET", "/api/v1/stock-requests?limit=1000", nil)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, fiber.StatusOK, resp.StatusCode)

	var envelope struct {
		Meta response.PageMeta `json:"meta"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&envelope))
	assert.Equal(t, stockrequest.MaxPageSize, envelope.Meta.Limit)
	uc.AssertExpectations(t)
}

func TestStockRequestHandler_GetByID(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		uc := new(mockStockRequestUsecase)
		h := handler.NewStockRequestHandler(uc)
		app := setupRequestTestApp(h)

		requestID := uuid.New()
		expected := &domain.StockRequest{
			ID:            requestID,
			RequestNumber: "REQ-20261028-AAAA1111",
			Status:        domain.StatusPending,
			Items:         []domain.StockRequestItem{{ProductID: uuid.New(), RequestedQuantity: 3}},
		}
		uc.On("GetRequest", mock.Anything, requestID).Return(expected, nil)

		resp, err := doJSON(app, "GET", "/api/v1/stock-requests/"+requestID.String(), nil)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, fiber.StatusOK, resp.StatusCode)
		envelope := decodeEnvelope(t, resp.Body)
		assert.True(t, envelope.Success)
		uc.AssertExpectations(t)
	})

	t.Run("invalid id", func(t *testing.T) {
		uc := new(mockStockRequestUsecase)
		h := handler.NewStockRequestHandler(uc)
		app := setupRequestTestApp(h)

		resp, err := doJSON(app, "GET", "/api/v1/stock-requests/not-a-uuid", nil)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, fiber.StatusBadRequest, resp.StatusCode)
		uc.AssertNotCalled(t, "GetRequest", mock.Anything, mock.Anything)
	})

	t.Run("not found", func(t *testing.T) {
		uc := new(mockStockRequestUsecase)
		h := handler.NewStockRequestHandler(uc)
		app := setupRequestTestApp(h)

		requestID := uuid.New()
		uc.On("GetRequest", mock.Anything, requestID).Return(nil, domain.ErrStockRequestNotFound)

		resp, err := doJSON(app, "GET", "/api/v1/stock-requests/"+requestID.String(), nil)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, fiber.StatusNotFound, resp.StatusCode)
		envelope := decodeEnvelope(t, resp.Body)
		detail, ok := envelope.Error.(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "NOT_FOUND", detail["code"])
		uc.AssertExpectations(t)
	})
}

func TestStockRequestHandler_Approve(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		uc := new(mockStockRequestUsecase)
		h := handler.NewStockRequestHandler(uc)
		app := setupRequestTestApp(h)

		requestID := uuid.New()
		approved := &domain.StockRequest{ID: requestID, Status: domain.StatusApproved}
		uc.On("ApproveRequest", mock.Anything, requestID).Return(approved, nil)

		resp, err := doJSON(app, "POST", "/api/v1/stock-requests/"+requestID.String()+"/approve", nil)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, fiber.StatusOK, resp.StatusCode)
		uc.AssertExpectations(t)
	})

	t.Run("invalid state", func(t *testing.T) {
		uc := new(mockStockRequestUsecase)
		h := handler.NewStockRequestHandler(uc)
		app := setupRequestTestApp(h)

		requestID := uuid.New()
		uc.On("ApproveRequest", mock.Anything, requestID).Return(nil, domain.ErrCannotReview)

		resp, err := doJSON(app, "POST", "/api/v1/stock-requests/"+requestID.String()+"/approve", nil)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, fiber.StatusConflict, resp.StatusCode)
		envelope := decodeEnvelope(t, resp.Body)
		detail, ok := envelope.Error.(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "INVALID_STATE", detail["code"])
		uc.AssertExpectations(t)
	})

	t.Run("invalid id", func(t *testing.T) {
		uc := new(mockStockRequestUsecase)
		h := handler.NewStockRequestHandler(uc)
		app := setupRequestTestApp(h)

		resp, err := doJSON(app, "POST", "/api/v1/stock-requests/bogus/approve", nil)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, fiber.StatusBadRequest, resp.StatusCode)
		uc.AssertNotCalled(t, "ApproveRequest", mock.Anything, mock.Anything)
	})
}

func TestStockRequestHandler_Reject(t *testing.T) {
	t.Run("success with reason", func(t *testing.T) {
		uc := new(mockStockRequestUsecase)
		h := handler.NewStockRequestHandler(uc)
		app := setupRequestTestApp(h)

		requestID := uuid.New()
		rejected := &domain.StockRequest{ID: requestID, Status: domain.StatusRejected, RejectionReason: "Out of budget"}
		uc.On("RejectRequest", mock.Anything, requestID, mock.MatchedBy(func(input stockrequest.RejectStockRequestInput) bool {
			return input.Reason == "Out of budget"
		})).Return(rejected, nil)

		resp, err := doJSON(app, "POST", "/api/v1/stock-requests/"+requestID.String()+"/reject",
			map[string]any{"rejection_reason": "  Out of budget  "})
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, fiber.StatusOK, resp.StatusCode)
		uc.AssertExpectations(t)
	})

	t.Run("success without body", func(t *testing.T) {
		uc := new(mockStockRequestUsecase)
		h := handler.NewStockRequestHandler(uc)
		app := setupRequestTestApp(h)

		requestID := uuid.New()
		rejected := &domain.StockRequest{ID: requestID, Status: domain.StatusRejected}
		uc.On("RejectRequest", mock.Anything, requestID, stockrequest.RejectStockRequestInput{Reason: ""}).
			Return(rejected, nil)

		resp, err := doJSON(app, "POST", "/api/v1/stock-requests/"+requestID.String()+"/reject", nil)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, fiber.StatusOK, resp.StatusCode)
		uc.AssertExpectations(t)
	})

	t.Run("invalid state", func(t *testing.T) {
		uc := new(mockStockRequestUsecase)
		h := handler.NewStockRequestHandler(uc)
		app := setupRequestTestApp(h)

		requestID := uuid.New()
		uc.On("RejectRequest", mock.Anything, requestID, mock.Anything).Return(nil, domain.ErrCannotReview)

		resp, err := doJSON(app, "POST", "/api/v1/stock-requests/"+requestID.String()+"/reject", nil)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, fiber.StatusConflict, resp.StatusCode)
		envelope := decodeEnvelope(t, resp.Body)
		detail, ok := envelope.Error.(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "INVALID_STATE", detail["code"])
		uc.AssertExpectations(t)
	})
}

func TestStockRequestHandler_Issue(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		uc := new(mockStockRequestUsecase)
		h := handler.NewStockRequestHandler(uc)
		app := setupRequestTestApp(h)

		requestID := uuid.New()
		result := &domain.StockIssueResult{
			Request: &domain.StockRequest{ID: requestID, Status: domain.StatusIssued},
			Movements: []domain.StockMovement{
				{ID: 10, MovementType: domain.MovementIssue, Quantity: 4, ReferenceType: "STOCK_REQUEST"},
			},
		}
		uc.On("IssueRequest", mock.Anything, requestID, "").Return(result, nil)

		resp, err := doJSON(app, "POST", "/api/v1/stock-requests/"+requestID.String()+"/issue", nil)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, fiber.StatusOK, resp.StatusCode)
		envelope := decodeEnvelope(t, resp.Body)
		assert.True(t, envelope.Success)
		uc.AssertExpectations(t)
	})

	conflictCases := []struct {
		name       string
		usecaseErr error
		wantCode   string
	}{
		{name: "not approved", usecaseErr: domain.ErrCannotIssue, wantCode: "INVALID_STATE"},
		{name: "insufficient stock", usecaseErr: domain.ErrInsufficientStock, wantCode: "INSUFFICIENT_STOCK"},
		{name: "inactive product", usecaseErr: domain.ErrProductInactive, wantCode: "PRODUCT_INACTIVE"},
	}

	for _, tt := range conflictCases {
		t.Run(tt.name, func(t *testing.T) {
			uc := new(mockStockRequestUsecase)
			h := handler.NewStockRequestHandler(uc)
			app := setupRequestTestApp(h)

			requestID := uuid.New()
			uc.On("IssueRequest", mock.Anything, requestID, "").Return(nil, tt.usecaseErr)

			resp, err := doJSON(app, "POST", "/api/v1/stock-requests/"+requestID.String()+"/issue", nil)
			require.NoError(t, err)
			defer resp.Body.Close()

			assert.Equal(t, fiber.StatusConflict, resp.StatusCode)
			envelope := decodeEnvelope(t, resp.Body)
			detail, ok := envelope.Error.(map[string]any)
			require.True(t, ok)
			assert.Equal(t, tt.wantCode, detail["code"])
			uc.AssertExpectations(t)
		})
	}

	t.Run("request not found", func(t *testing.T) {
		uc := new(mockStockRequestUsecase)
		h := handler.NewStockRequestHandler(uc)
		app := setupRequestTestApp(h)

		requestID := uuid.New()
		uc.On("IssueRequest", mock.Anything, requestID, "").Return(nil, domain.ErrStockRequestNotFound)

		resp, err := doJSON(app, "POST", "/api/v1/stock-requests/"+requestID.String()+"/issue", nil)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, fiber.StatusNotFound, resp.StatusCode)
		uc.AssertExpectations(t)
	})
}

func TestStockRequestHandler_Cancel(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		uc := new(mockStockRequestUsecase)
		h := handler.NewStockRequestHandler(uc)
		app := setupRequestTestApp(h)

		requestID := uuid.New()
		cancelled := &domain.StockRequest{ID: requestID, Status: domain.StatusCancelled}
		uc.On("CancelRequest", mock.Anything, requestID, "").Return(cancelled, nil)

		resp, err := doJSON(app, "POST", "/api/v1/stock-requests/"+requestID.String()+"/cancel", nil)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, fiber.StatusOK, resp.StatusCode)
		envelope := decodeEnvelope(t, resp.Body)
		assert.True(t, envelope.Success)
		assert.Equal(t, "Stock request cancelled successfully", envelope.Message)
		uc.AssertExpectations(t)
	})

	t.Run("invalid state includes current status", func(t *testing.T) {
		uc := new(mockStockRequestUsecase)
		h := handler.NewStockRequestHandler(uc)
		app := setupRequestTestApp(h)

		requestID := uuid.New()
		uc.On("CancelRequest", mock.Anything, requestID, "").Return(nil, &domain.StateConflictError{
			Cause:         domain.ErrCannotCancel,
			CurrentStatus: domain.StatusIssued,
		})

		resp, err := doJSON(app, "POST", "/api/v1/stock-requests/"+requestID.String()+"/cancel", nil)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, fiber.StatusConflict, resp.StatusCode)

		var envelope struct {
			Success bool `json:"success"`
			Error   struct {
				Code    string                 `json:"code"`
				Details map[string]interface{} `json:"details"`
			} `json:"error"`
		}
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&envelope))
		assert.False(t, envelope.Success)
		assert.Equal(t, "INVALID_STATE", envelope.Error.Code)
		assert.Equal(t, "ISSUED", envelope.Error.Details["current_status"])
		uc.AssertExpectations(t)
	})

	t.Run("not found", func(t *testing.T) {
		uc := new(mockStockRequestUsecase)
		h := handler.NewStockRequestHandler(uc)
		app := setupRequestTestApp(h)

		requestID := uuid.New()
		uc.On("CancelRequest", mock.Anything, requestID, "").Return(nil, domain.ErrStockRequestNotFound)

		resp, err := doJSON(app, "POST", "/api/v1/stock-requests/"+requestID.String()+"/cancel", nil)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, fiber.StatusNotFound, resp.StatusCode)
		uc.AssertExpectations(t)
	})

	t.Run("invalid id", func(t *testing.T) {
		uc := new(mockStockRequestUsecase)
		h := handler.NewStockRequestHandler(uc)
		app := setupRequestTestApp(h)

		resp, err := doJSON(app, "POST", "/api/v1/stock-requests/not-a-uuid/cancel", nil)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, fiber.StatusBadRequest, resp.StatusCode)
		uc.AssertNotCalled(t, "CancelRequest", mock.Anything, mock.Anything)
	})
}

func TestStockRequestHandler_Issue_IdempotencyKeyHandling(t *testing.T) {
	t.Run("replay returns meta flag", func(t *testing.T) {
		uc := new(mockStockRequestUsecase)
		h := handler.NewStockRequestHandler(uc)
		app := setupRequestTestApp(h)

		requestID := uuid.New()
		issuanceID := uuid.New()
		result := &domain.StockIssueResult{
			Request:          &domain.StockRequest{ID: requestID, Status: domain.StatusIssued},
			Movements:        []domain.StockMovement{{ID: 7, MovementType: domain.MovementIssue, Quantity: 3}},
			Issuance:         &domain.StockRequestIssuance{ID: issuanceID, RequestID: requestID},
			IdempotentReplay: true,
		}
		uc.On("IssueRequest", mock.Anything, requestID, "checkout-abc123").Return(result, nil)

		req := httptest.NewRequest("POST", "/api/v1/stock-requests/"+requestID.String()+"/issue", nil)
		req.Header.Set("Idempotency-Key", "checkout-abc123")
		resp, err := app.Test(req, -1)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, fiber.StatusOK, resp.StatusCode)

		var envelope struct {
			Success bool                   `json:"success"`
			Message string                 `json:"message"`
			Meta    map[string]interface{} `json:"meta"`
		}
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&envelope))
		assert.True(t, envelope.Success)
		assert.Equal(t, true, envelope.Meta["idempotent_replay"])
		assert.Equal(t, issuanceID.String(), envelope.Meta["issuance_id"])
		assert.Contains(t, envelope.Message, "idempotent replay")
		uc.AssertExpectations(t)
	})

	t.Run("invalid key maps to 400", func(t *testing.T) {
		uc := new(mockStockRequestUsecase)
		h := handler.NewStockRequestHandler(uc)
		app := setupRequestTestApp(h)

		requestID := uuid.New()
		uc.On("IssueRequest", mock.Anything, requestID, "bad key").Return(nil, domain.ErrInvalidIdempotencyKey)

		req := httptest.NewRequest("POST", "/api/v1/stock-requests/"+requestID.String()+"/issue", nil)
		req.Header.Set("Idempotency-Key", "bad key")
		resp, err := app.Test(req, -1)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, fiber.StatusBadRequest, resp.StatusCode)
		envelope := decodeEnvelope(t, resp.Body)
		detail, ok := envelope.Error.(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, "INVALID_IDEMPOTENCY_KEY", detail["code"])
		uc.AssertExpectations(t)
	})

	t.Run("key reused maps to conflict", func(t *testing.T) {
		uc := new(mockStockRequestUsecase)
		h := handler.NewStockRequestHandler(uc)
		app := setupRequestTestApp(h)

		requestID := uuid.New()
		uc.On("IssueRequest", mock.Anything, requestID, "checkout-abc123").Return(nil, domain.ErrIdempotencyKeyReused)

		req := httptest.NewRequest("POST", "/api/v1/stock-requests/"+requestID.String()+"/issue", nil)
		req.Header.Set("Idempotency-Key", "checkout-abc123")
		resp, err := app.Test(req, -1)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, fiber.StatusConflict, resp.StatusCode)
		envelope := decodeEnvelope(t, resp.Body)
		detail, ok := envelope.Error.(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, "IDEMPOTENCY_KEY_REUSED", detail["code"])
		uc.AssertExpectations(t)
	})
}

func TestStockRequestHandler_Issue_ReturnsStructuredDetails(t *testing.T) {
	t.Run("insufficient stock lists every shortage", func(t *testing.T) {
		uc := new(mockStockRequestUsecase)
		h := handler.NewStockRequestHandler(uc)
		app := setupRequestTestApp(h)

		requestID := uuid.New()
		productID := uuid.New()
		uc.On("IssueRequest", mock.Anything, requestID, "").Return(nil, &domain.InsufficientStockError{
			Items: []domain.StockShortage{{ProductID: productID, Requested: 10, Available: 3, Shortfall: 7}},
		})

		resp, err := doJSON(app, "POST", "/api/v1/stock-requests/"+requestID.String()+"/issue", nil)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, fiber.StatusConflict, resp.StatusCode)

		var envelope struct {
			Error struct {
				Code    string `json:"code"`
				Details struct {
					Items []struct {
						ProductID string `json:"product_id"`
						Requested int    `json:"requested"`
						Available int    `json:"available"`
						Shortfall int    `json:"shortfall"`
					} `json:"items"`
				} `json:"details"`
			} `json:"error"`
		}
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&envelope))
		assert.Equal(t, "INSUFFICIENT_STOCK", envelope.Error.Code)
		require.Len(t, envelope.Error.Details.Items, 1)
		assert.Equal(t, productID.String(), envelope.Error.Details.Items[0].ProductID)
		assert.Equal(t, 10, envelope.Error.Details.Items[0].Requested)
		assert.Equal(t, 3, envelope.Error.Details.Items[0].Available)
		assert.Equal(t, 7, envelope.Error.Details.Items[0].Shortfall)
		uc.AssertExpectations(t)
	})

	t.Run("already issued exposes issuance metadata", func(t *testing.T) {
		uc := new(mockStockRequestUsecase)
		h := handler.NewStockRequestHandler(uc)
		app := setupRequestTestApp(h)

		requestID := uuid.New()
		issuanceID := uuid.New()
		uc.On("IssueRequest", mock.Anything, requestID, "").Return(nil, &domain.StateConflictError{
			Cause:         domain.ErrCannotIssue,
			CurrentStatus: domain.StatusIssued,
			IssuanceID:    &issuanceID,
		})

		resp, err := doJSON(app, "POST", "/api/v1/stock-requests/"+requestID.String()+"/issue", nil)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, fiber.StatusConflict, resp.StatusCode)

		var envelope struct {
			Error struct {
				Code    string                 `json:"code"`
				Details map[string]interface{} `json:"details"`
			} `json:"error"`
		}
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&envelope))
		assert.Equal(t, "INVALID_STATE", envelope.Error.Code)
		assert.Equal(t, "ISSUED", envelope.Error.Details["current_status"])
		assert.Equal(t, issuanceID.String(), envelope.Error.Details["issuance_id"])
		uc.AssertExpectations(t)
	})
}
