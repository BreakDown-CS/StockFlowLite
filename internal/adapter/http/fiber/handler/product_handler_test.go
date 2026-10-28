package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http/httptest"
	"testing"

	"github.com/BreakDown-CS/StockFlowLite/internal/adapter/http/fiber/handler"
	"github.com/BreakDown-CS/StockFlowLite/internal/domain"
	productusecase "github.com/BreakDown-CS/StockFlowLite/internal/usecase/product"
	"github.com/BreakDown-CS/StockFlowLite/pkg/response"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type mockProductUsecase struct {
	mock.Mock
}

func (m *mockProductUsecase) CreateProduct(ctx context.Context, input productusecase.CreateProductInput) (*domain.Product, error) {
	args := m.Called(ctx, input)
	if p := args.Get(0); p != nil {
		return p.(*domain.Product), args.Error(1)
	}
	return nil, args.Error(1)
}

func (m *mockProductUsecase) GetProductByID(ctx context.Context, id uuid.UUID) (*domain.Product, error) {
	args := m.Called(ctx, id)
	if p := args.Get(0); p != nil {
		return p.(*domain.Product), args.Error(1)
	}
	return nil, args.Error(1)
}

func (m *mockProductUsecase) ListProducts(ctx context.Context, input productusecase.ListProductsInput) ([]domain.Product, int64, error) {
	args := m.Called(ctx, input)
	return args.Get(0).([]domain.Product), args.Get(1).(int64), args.Error(2)
}

func (m *mockProductUsecase) UpdateProduct(ctx context.Context, id uuid.UUID, input productusecase.UpdateProductInput) (*domain.Product, error) {
	args := m.Called(ctx, id, input)
	if p := args.Get(0); p != nil {
		return p.(*domain.Product), args.Error(1)
	}
	return nil, args.Error(1)
}

func (m *mockProductUsecase) DeactivateProduct(ctx context.Context, id uuid.UUID) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

func setupProductTestApp(h *handler.ProductHandler) *fiber.App {
	app := fiber.New(fiber.Config{
		DisableStartupMessage: true,
	})
	api := app.Group("/api/v1/products")
	api.Post("/", h.Create)
	api.Get("/", h.List)
	api.Get("/:id", h.GetByID)
	api.Put("/:id", h.Update)
	api.Patch("/:id/deactivate", h.Deactivate)
	return app
}

func TestProductHandler_Create_Success(t *testing.T) {
	uc := new(mockProductUsecase)
	h := handler.NewProductHandler(uc)
	app := setupProductTestApp(h)

	created, _ := domain.NewProduct("SKU-100", "Widget", "desc", "PCS", 5)
	uc.On("CreateProduct", mock.Anything, mock.MatchedBy(func(input productusecase.CreateProductInput) bool {
		return input.SKU == "SKU-100" && input.Name == "Widget" && input.MinStock == 5
	})).Return(created, nil)

	payload := map[string]interface{}{
		"sku":         "SKU-100",
		"name":        "Widget",
		"description": "desc",
		"unit":        "PCS",
		"min_stock":   5,
	}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest("POST", "/api/v1/products", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, fiber.StatusCreated, resp.StatusCode)

	var res response.Envelope
	err = json.NewDecoder(resp.Body).Decode(&res)
	require.NoError(t, err)
	assert.True(t, res.Success)
	assert.Equal(t, "Product created successfully", res.Message)
	uc.AssertExpectations(t)
}

func TestProductHandler_Create_ValidationErrors(t *testing.T) {
	uc := new(mockProductUsecase)
	h := handler.NewProductHandler(uc)
	app := setupProductTestApp(h)

	tests := []struct {
		name       string
		payload    map[string]interface{}
		wantStatus int
		wantCode   string
	}{
		{
			name: "missing sku",
			payload: map[string]interface{}{
				"name": "Widget",
			},
			wantStatus: fiber.StatusBadRequest,
			wantCode:   "VALIDATION_ERROR",
		},
		{
			name: "missing name",
			payload: map[string]interface{}{
				"sku": "SKU-100",
			},
			wantStatus: fiber.StatusBadRequest,
			wantCode:   "VALIDATION_ERROR",
		},
		{
			name: "negative min stock",
			payload: map[string]interface{}{
				"sku":       "SKU-100",
				"name":      "Widget",
				"min_stock": -5,
			},
			wantStatus: fiber.StatusBadRequest,
			wantCode:   "VALIDATION_ERROR",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, _ := json.Marshal(tt.payload)
			req := httptest.NewRequest("POST", "/api/v1/products", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			resp, err := app.Test(req)
			require.NoError(t, err)
			defer resp.Body.Close()

			assert.Equal(t, tt.wantStatus, resp.StatusCode)
		})
	}
}

func TestProductHandler_Create_DuplicateSKU(t *testing.T) {
	uc := new(mockProductUsecase)
	h := handler.NewProductHandler(uc)
	app := setupProductTestApp(h)

	uc.On("CreateProduct", mock.Anything, mock.Anything).Return(nil, domain.ErrDuplicateSKU)

	payload := map[string]interface{}{
		"sku":  "SKU-DUP",
		"name": "Duplicate Item",
	}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest("POST", "/api/v1/products", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, fiber.StatusConflict, resp.StatusCode)

	var res response.Envelope
	err = json.NewDecoder(resp.Body).Decode(&res)
	require.NoError(t, err)
	assert.False(t, res.Success)
	uc.AssertExpectations(t)
}

func TestProductHandler_GetByID_Success(t *testing.T) {
	uc := new(mockProductUsecase)
	h := handler.NewProductHandler(uc)
	app := setupProductTestApp(h)

	prodID := uuid.New()
	prod, _ := domain.NewProduct("SKU-100", "Widget", "desc", "PCS", 2)
	prod.ID = prodID

	uc.On("GetProductByID", mock.Anything, prodID).Return(prod, nil)

	req := httptest.NewRequest("GET", "/api/v1/products/"+prodID.String(), nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, fiber.StatusOK, resp.StatusCode)
	uc.AssertExpectations(t)
}

func TestProductHandler_GetByID_NotFound(t *testing.T) {
	uc := new(mockProductUsecase)
	h := handler.NewProductHandler(uc)
	app := setupProductTestApp(h)

	prodID := uuid.New()
	uc.On("GetProductByID", mock.Anything, prodID).Return(nil, domain.ErrProductNotFound)

	req := httptest.NewRequest("GET", "/api/v1/products/"+prodID.String(), nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, fiber.StatusNotFound, resp.StatusCode)
	uc.AssertExpectations(t)
}

func TestProductHandler_GetByID_InvalidUUID(t *testing.T) {
	uc := new(mockProductUsecase)
	h := handler.NewProductHandler(uc)
	app := setupProductTestApp(h)

	req := httptest.NewRequest("GET", "/api/v1/products/not-a-valid-uuid", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, fiber.StatusBadRequest, resp.StatusCode)
}

func TestProductHandler_List_Success(t *testing.T) {
	uc := new(mockProductUsecase)
	h := handler.NewProductHandler(uc)
	app := setupProductTestApp(h)

	p1, _ := domain.NewProduct("SKU-1", "Product 1", "desc", "PCS", 2)
	p2, _ := domain.NewProduct("SKU-2", "Product 2", "desc", "PCS", 5)

	uc.On("ListProducts", mock.Anything, mock.MatchedBy(func(input productusecase.ListProductsInput) bool {
		return input.Limit == 10 && input.Offset == 0 && input.Search == "Product"
	})).Return([]domain.Product{*p1, *p2}, int64(2), nil)

	req := httptest.NewRequest("GET", "/api/v1/products?limit=10&offset=0&search=Product", nil)
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

func TestProductHandler_Update_Success(t *testing.T) {
	uc := new(mockProductUsecase)
	h := handler.NewProductHandler(uc)
	app := setupProductTestApp(h)

	prodID := uuid.New()
	updatedProd, _ := domain.NewProduct("SKU-100", "Updated Widget", "new desc", "BOX", 10)
	updatedProd.ID = prodID

	uc.On("UpdateProduct", mock.Anything, prodID, mock.MatchedBy(func(input productusecase.UpdateProductInput) bool {
		return input.Name == "Updated Widget" && input.Unit == "BOX" && input.MinStock == 10
	})).Return(updatedProd, nil)

	payload := map[string]interface{}{
		"name":        "Updated Widget",
		"description": "new desc",
		"unit":        "BOX",
		"min_stock":   10,
	}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest("PUT", "/api/v1/products/"+prodID.String(), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, fiber.StatusOK, resp.StatusCode)
	uc.AssertExpectations(t)
}

func TestProductHandler_Deactivate_Success(t *testing.T) {
	uc := new(mockProductUsecase)
	h := handler.NewProductHandler(uc)
	app := setupProductTestApp(h)

	prodID := uuid.New()
	uc.On("DeactivateProduct", mock.Anything, prodID).Return(nil)

	req := httptest.NewRequest("PATCH", "/api/v1/products/"+prodID.String()+"/deactivate", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, fiber.StatusOK, resp.StatusCode)

	bodyBytes, _ := io.ReadAll(resp.Body)
	assert.Contains(t, string(bodyBytes), "Product deactivated successfully")
	uc.AssertExpectations(t)
}
