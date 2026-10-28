package fiber

import (
	"github.com/BreakDown-CS/StockFlowLite/internal/adapter/http/fiber/handler"
	"github.com/BreakDown-CS/StockFlowLite/internal/adapter/http/fiber/middleware"
	"github.com/BreakDown-CS/StockFlowLite/internal/domain"
	authusecase "github.com/BreakDown-CS/StockFlowLite/internal/usecase/auth"
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
	StockRequestHandler  *handler.StockRequestHandler
	AuthHandler          *handler.AuthHandler
	UserHandler          *handler.UserHandler
	AuditHandler         *handler.AuditHandler

	// AuthUsecase enables bearer-token authentication. When nil the route tree is registered
	// without authentication, which is only intended for isolated tests.
	AuthUsecase     authusecase.AuthUsecase
	AuditRepository domain.AuditRepository
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

	// Public authentication endpoint
	if cfg.AuthHandler != nil {
		api.Post("/auth/login", cfg.AuthHandler.Login)
	}

	// From this point on every route requires a valid bearer token.
	authEnabled := cfg.AuthUsecase != nil
	if authEnabled {
		api.Use(middleware.RequireAuth(cfg.AuthUsecase))
	}

	storekeeper := passThrough()
	adminOnly := passThrough()
	if authEnabled {
		storekeeper = middleware.RequireRoles(cfg.AuditRepository, domain.RoleStorekeeper, domain.RoleAdmin)
		adminOnly = middleware.RequireRoles(cfg.AuditRepository, domain.RoleAdmin)
	}

	// Authenticated self-service
	if cfg.AuthHandler != nil {
		api.Post("/auth/logout", cfg.AuthHandler.Logout)
		api.Get("/auth/me", cfg.AuthHandler.Me)
		api.Post("/auth/change-password", cfg.AuthHandler.ChangePassword)
	}

	// Products: everyone authenticated may read, storekeepers/admins may write.
	if cfg.ProductHandler != nil {
		products := api.Group("/products")
		products.Get("/", cfg.ProductHandler.List)
		products.Get("/:id", cfg.ProductHandler.GetByID)
		products.Post("/", storekeeper, cfg.ProductHandler.Create)
		products.Put("/:id", storekeeper, cfg.ProductHandler.Update)
		products.Patch("/:id/deactivate", storekeeper, cfg.ProductHandler.Deactivate)
	}

	// Stock Receipts (inbound deliveries)
	if cfg.StockReceiptHandler != nil {
		api.Post("/stock-receipts", storekeeper, cfg.StockReceiptHandler.Create)
	}

	// Stock Movements (read-only ledger)
	if cfg.StockMovementHandler != nil {
		api.Get("/stock-movements", cfg.StockMovementHandler.List)
	}

	// Stock Requests (request -> approve/reject -> issue / cancel)
	if cfg.StockRequestHandler != nil {
		requests := api.Group("/stock-requests")
		requests.Post("/", cfg.StockRequestHandler.Create)
		requests.Get("/", cfg.StockRequestHandler.List)
		requests.Get("/:id", cfg.StockRequestHandler.GetByID)
		requests.Post("/:id/approve", storekeeper, cfg.StockRequestHandler.Approve)
		requests.Post("/:id/reject", storekeeper, cfg.StockRequestHandler.Reject)
		requests.Post("/:id/cancel", cfg.StockRequestHandler.Cancel)
		requests.Post("/:id/issue", storekeeper, cfg.StockRequestHandler.Issue)
	}

	// User administration (ADMIN only)
	if cfg.UserHandler != nil {
		users := api.Group("/users", adminOnly)
		users.Post("/", cfg.UserHandler.Create)
		users.Get("/", cfg.UserHandler.List)
		users.Get("/:id", cfg.UserHandler.GetByID)
		users.Patch("/:id", cfg.UserHandler.Update)
		users.Post("/:id/reset-password", cfg.UserHandler.ResetPassword)
	}

	// Audit trail (ADMIN only, read-only)
	if cfg.AuditHandler != nil {
		api.Get("/audit-events", adminOnly, cfg.AuditHandler.List)
	}

	return app
}

// passThrough is used when authentication is not wired (isolated tests only).
func passThrough() fiber.Handler {
	return func(c *fiber.Ctx) error {
		return c.Next()
	}
}
