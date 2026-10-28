package fiber

import (
	"github.com/BreakDown-CS/StockFlowLite/internal/adapter/http/fiber/handler"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/gofiber/fiber/v2/middleware/requestid"
)

// RouterConfig holds dependencies required to configure HTTP routes.
type RouterConfig struct {
	HealthHandler        *handler.HealthHandler
	ProductHandler       *handler.ProductHandler
	StockReceiptHandler  *handler.StockReceiptHandler
	StockMovementHandler *handler.StockMovementHandler
}

// NewRouter initializes and configures the Fiber application and its route tree.
func NewRouter(cfg RouterConfig) *fiber.App {
	app := fiber.New(fiber.Config{
		AppName:               "StockFlow Lite API",
		DisableStartupMessage: true,
	})

	// Global middlewares
	app.Use(recover.New())
	app.Use(requestid.New())

	// Health and readiness probes
	if cfg.HealthHandler != nil {
		app.Get("/health", cfg.HealthHandler.Health)
		app.Get("/ready", cfg.HealthHandler.Ready)
	}

	// API v1 routes
	api := app.Group("/api/v1")

	// Products
	if cfg.ProductHandler != nil {
		products := api.Group("/products")
		products.Post("/", cfg.ProductHandler.Create)
		products.Get("/", cfg.ProductHandler.List)
		products.Get("/:id", cfg.ProductHandler.GetByID)
		products.Put("/:id", cfg.ProductHandler.Update)
		products.Patch("/:id/deactivate", cfg.ProductHandler.Deactivate)
	}

	// Stock Receipts
	if cfg.StockReceiptHandler != nil {
		api.Post("/stock-receipts", cfg.StockReceiptHandler.Create)
	}

	// Stock Movements
	if cfg.StockMovementHandler != nil {
		api.Get("/stock-movements", cfg.StockMovementHandler.List)
	}

	return app
}
