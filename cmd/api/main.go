package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	httpfiber "github.com/BreakDown-CS/StockFlowLite/internal/adapter/http/fiber"
	"github.com/BreakDown-CS/StockFlowLite/internal/adapter/http/fiber/handler"
	"github.com/BreakDown-CS/StockFlowLite/internal/adapter/repository/postgres"
	"github.com/BreakDown-CS/StockFlowLite/internal/config"
	"github.com/BreakDown-CS/StockFlowLite/internal/infrastructure/database"
	productusecase "github.com/BreakDown-CS/StockFlowLite/internal/usecase/product"
	"github.com/BreakDown-CS/StockFlowLite/internal/usecase/stockmovement"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	// 1. Initialize structured logger
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	// 2. Load and validate configuration
	cfg, err := config.LoadFromEnv()
	if err != nil {
		slog.Error("Configuration error", "error", err)
		os.Exit(1)
	}

	slog.Info("Starting StockFlow Lite API",
		"env", cfg.App.Env,
		"port", cfg.App.Port,
		"db_target", cfg.DB.RedactedConnString(),
	)

	// 3. Initialize PostgreSQL connection pool
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var pool *pgxpool.Pool
	pool, err = database.NewPool(ctx, &cfg.DB)
	if err != nil {
		slog.Error("Failed to initialize database pool configuration", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	// Initial connectivity verification (non-fatal so server can still serve /health)
	pingCtx, pingCancel := context.WithTimeout(context.Background(), cfg.DB.HealthTimeout)
	if err := pool.Ping(pingCtx); err != nil {
		slog.Warn("Initial database ping failed; database may be offline or starting up",
			"error", err.Error(),
			"db_target", cfg.DB.RedactedConnString(),
		)
	} else {
		slog.Info("Successfully established connection to PostgreSQL",
			"db_target", cfg.DB.RedactedConnString(),
		)
	}
	pingCancel()

	// 4. Construct repositories, use cases, and HTTP handlers
	healthHandler := handler.NewHealthHandler(pool, cfg.DB.HealthTimeout)

	productRepo := postgres.NewProductRepository(pool)
	productUC := productusecase.NewProductUsecase(productRepo)
	productHandler := handler.NewProductHandler(productUC)

	movementRepo := postgres.NewStockMovementRepository(pool)
	movementUC := stockmovement.NewStockMovementUsecase(movementRepo)
	stockReceiptHandler := handler.NewStockReceiptHandler(movementUC)
	stockMovementHandler := handler.NewStockMovementHandler(movementUC)

	app := httpfiber.NewRouter(httpfiber.RouterConfig{
		HealthHandler:        healthHandler,
		ProductHandler:       productHandler,
		StockReceiptHandler:  stockReceiptHandler,
		StockMovementHandler: stockMovementHandler,
	})

	// 5. Start HTTP server in a separate goroutine
	serverErrChan := make(chan error, 1)
	go func() {
		addr := fmt.Sprintf(":%d", cfg.App.Port)
		slog.Info("HTTP server listening", "address", addr)
		if err := app.Listen(addr); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErrChan <- err
		}
	}()

	// 6. Graceful shutdown orchestration
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM, syscall.SIGINT)

	select {
	case sig := <-quit:
		slog.Info("Received shutdown signal", "signal", sig.String())
	case err := <-serverErrChan:
		slog.Error("HTTP server failed unexpectedly", "error", err)
	}

	slog.Info("Initiating graceful shutdown...")

	// 7. Shutdown HTTP server with timeout
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), cfg.App.ShutdownTimeout)
	defer shutdownCancel()

	if err := app.ShutdownWithContext(shutdownCtx); err != nil {
		slog.Error("HTTP server shutdown encountered an error", "error", err)
	} else {
		slog.Info("HTTP server stopped cleanly")
	}

	// 8. Close database pool (deferred pool.Close() will run here upon exit)
	slog.Info("StockFlow Lite API stopped")
}
