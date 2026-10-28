package handler

import (
	"errors"
	"log/slog"
	"strconv"
	"strings"

	"github.com/BreakDown-CS/StockFlowLite/internal/domain"
	productusecase "github.com/BreakDown-CS/StockFlowLite/internal/usecase/product"
	"github.com/BreakDown-CS/StockFlowLite/pkg/response"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

// ProductHandler handles HTTP requests for product management.
type ProductHandler struct {
	usecase productusecase.ProductUsecase
}

// NewProductHandler constructs a new ProductHandler.
func NewProductHandler(usecase productusecase.ProductUsecase) *ProductHandler {
	return &ProductHandler{usecase: usecase}
}

// CreateProductRequest defines the payload for creating a product.
type CreateProductRequest struct {
	SKU         string `json:"sku"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Unit        string `json:"unit"`
	MinStock    *int   `json:"min_stock"`
}

// UpdateProductRequest defines the payload for updating an existing product.
type UpdateProductRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Unit        string `json:"unit"`
	MinStock    *int   `json:"min_stock"`
}

// Create handles POST /api/v1/products
func (h *ProductHandler) Create(c *fiber.Ctx) error {
	var req CreateProductRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, fiber.StatusBadRequest, "BAD_REQUEST", "Malformed request payload")
	}

	sku := strings.TrimSpace(req.SKU)
	if sku == "" {
		return response.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "SKU is required")
	}
	if len(sku) > 50 {
		return response.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "SKU must not exceed 50 characters")
	}

	name := strings.TrimSpace(req.Name)
	if name == "" {
		return response.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "Product name is required")
	}
	if len(name) > 200 {
		return response.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "Product name must not exceed 200 characters")
	}

	unit := strings.TrimSpace(req.Unit)
	if unit == "" {
		unit = "PCS"
	}
	if len(unit) > 20 {
		return response.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "Unit must not exceed 20 characters")
	}

	desc := strings.TrimSpace(req.Description)
	if len(desc) > 1000 {
		return response.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "Description must not exceed 1000 characters")
	}

	minStock := 0
	if req.MinStock != nil {
		if *req.MinStock < 0 {
			return response.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "Minimum stock cannot be negative")
		}
		minStock = *req.MinStock
	}

	input := productusecase.CreateProductInput{
		SKU:         sku,
		Name:        name,
		Description: desc,
		Unit:        unit,
		MinStock:    minStock,
	}

	ctx := c.UserContext()
	p, err := h.usecase.CreateProduct(ctx, input)
	if err != nil {
		if errors.Is(err, domain.ErrDuplicateSKU) {
			return response.Error(c, fiber.StatusConflict, "DUPLICATE_SKU", "A product with this SKU already exists")
		}
		slog.Error("Failed to create product", "error", err)
		return response.Error(c, fiber.StatusInternalServerError, "INTERNAL_ERROR", "Failed to create product")
	}

	return response.Created(c, p, "Product created successfully")
}

// List handles GET /api/v1/products
func (h *ProductHandler) List(c *fiber.Ctx) error {
	limit := 20
	if limitStr := c.Query("limit"); limitStr != "" {
		parsedLimit, err := strconv.Atoi(limitStr)
		if err != nil || parsedLimit <= 0 {
			return response.Error(c, fiber.StatusBadRequest, "INVALID_PARAMETER", "limit must be a positive integer")
		}
		limit = parsedLimit
	}

	offset := 0
	if offsetStr := c.Query("offset"); offsetStr != "" {
		parsedOffset, err := strconv.Atoi(offsetStr)
		if err != nil || parsedOffset < 0 {
			return response.Error(c, fiber.StatusBadRequest, "INVALID_PARAMETER", "offset must be a non-negative integer")
		}
		offset = parsedOffset
	}

	search := strings.TrimSpace(c.Query("search"))

	var isActive *bool
	if activeStr := c.Query("is_active"); activeStr != "" {
		if activeStr == "true" {
			val := true
			isActive = &val
		} else if activeStr == "false" {
			val := false
			isActive = &val
		} else if activeStr != "all" {
			return response.Error(c, fiber.StatusBadRequest, "INVALID_PARAMETER", "is_active must be 'true', 'false', or 'all'")
		}
	}

	input := productusecase.ListProductsInput{
		Search:   search,
		Limit:    limit,
		Offset:   offset,
		IsActive: isActive,
	}

	ctx := c.UserContext()
	products, total, err := h.usecase.ListProducts(ctx, input)
	if err != nil {
		slog.Error("Failed to list products", "error", err)
		return response.Error(c, fiber.StatusInternalServerError, "INTERNAL_ERROR", "Failed to retrieve products")
	}

	meta := response.PageMeta{
		Limit:  limit,
		Offset: offset,
		Total:  total,
	}

	return response.SuccessWithMeta(c, products, meta)
}

// GetByID handles GET /api/v1/products/:id
func (h *ProductHandler) GetByID(c *fiber.Ctx) error {
	idStr := c.Params("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return response.Error(c, fiber.StatusBadRequest, "INVALID_ID", "Invalid product ID format")
	}

	ctx := c.UserContext()
	p, err := h.usecase.GetProductByID(ctx, id)
	if err != nil {
		if errors.Is(err, domain.ErrProductNotFound) {
			return response.Error(c, fiber.StatusNotFound, "NOT_FOUND", "Product not found")
		}
		slog.Error("Failed to get product by id", "id", idStr, "error", err)
		return response.Error(c, fiber.StatusInternalServerError, "INTERNAL_ERROR", "Failed to retrieve product")
	}

	return response.OK(c, p)
}

// Update handles PUT /api/v1/products/:id
func (h *ProductHandler) Update(c *fiber.Ctx) error {
	idStr := c.Params("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return response.Error(c, fiber.StatusBadRequest, "INVALID_ID", "Invalid product ID format")
	}

	var req UpdateProductRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, fiber.StatusBadRequest, "BAD_REQUEST", "Malformed request payload")
	}

	name := strings.TrimSpace(req.Name)
	if name == "" {
		return response.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "Product name is required")
	}
	if len(name) > 200 {
		return response.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "Product name must not exceed 200 characters")
	}

	unit := strings.TrimSpace(req.Unit)
	if len(unit) > 20 {
		return response.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "Unit must not exceed 20 characters")
	}

	desc := strings.TrimSpace(req.Description)
	if len(desc) > 1000 {
		return response.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "Description must not exceed 1000 characters")
	}

	minStock := 0
	if req.MinStock != nil {
		if *req.MinStock < 0 {
			return response.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "Minimum stock cannot be negative")
		}
		minStock = *req.MinStock
	}

	input := productusecase.UpdateProductInput{
		Name:        name,
		Description: desc,
		Unit:        unit,
		MinStock:    minStock,
	}

	ctx := c.UserContext()
	p, err := h.usecase.UpdateProduct(ctx, id, input)
	if err != nil {
		if errors.Is(err, domain.ErrProductNotFound) {
			return response.Error(c, fiber.StatusNotFound, "NOT_FOUND", "Product not found")
		}
		slog.Error("Failed to update product", "id", idStr, "error", err)
		return response.Error(c, fiber.StatusInternalServerError, "INTERNAL_ERROR", "Failed to update product")
	}

	return response.OK(c, p, "Product updated successfully")
}

// Deactivate handles PATCH /api/v1/products/:id/deactivate
func (h *ProductHandler) Deactivate(c *fiber.Ctx) error {
	idStr := c.Params("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return response.Error(c, fiber.StatusBadRequest, "INVALID_ID", "Invalid product ID format")
	}

	ctx := c.UserContext()
	if err := h.usecase.DeactivateProduct(ctx, id); err != nil {
		if errors.Is(err, domain.ErrProductNotFound) {
			return response.Error(c, fiber.StatusNotFound, "NOT_FOUND", "Product not found")
		}
		slog.Error("Failed to deactivate product", "id", idStr, "error", err)
		return response.Error(c, fiber.StatusInternalServerError, "INTERNAL_ERROR", "Failed to deactivate product")
	}

	return response.OK(c, nil, "Product deactivated successfully")
}
