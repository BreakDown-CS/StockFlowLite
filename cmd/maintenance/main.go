package main

// Command maintenance runs read-only integrity checks and housekeeping tasks.
//
// Usage:
//
//	go run ./cmd/maintenance -action check-stock            # exit code 2 when drift is found
//	go run ./cmd/maintenance -action check-stock -only-drifted
//	go run ./cmd/maintenance -action purge-sessions -older-than 168h
//
// The command never prints credentials and only reports aggregate/sku level data.

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/BreakDown-CS/StockFlowLite/internal/adapter/repository/postgres"
	"github.com/BreakDown-CS/StockFlowLite/internal/config"
	"github.com/BreakDown-CS/StockFlowLite/internal/domain"
	"github.com/BreakDown-CS/StockFlowLite/internal/infrastructure/database"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	action := flag.String("action", "check-stock", "check-stock | purge-sessions")
	onlyDrifted := flag.Bool("only-drifted", false, "check-stock: print only products with drift")
	olderThan := flag.Duration("older-than", 7*24*time.Hour, "purge-sessions: remove sessions inactive before now-older-than")
	flag.Parse()

	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))

	cfg, err := config.LoadFromEnv()
	if err != nil {
		fail("configuration error: " + err.Error())
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pool, err := database.NewPool(ctx, &cfg.DB)
	if err != nil {
		fail("database pool error: " + err.Error())
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		fail("database is unreachable: " + err.Error())
	}

	// Housekeeping runs outside the HTTP request lifecycle; attribute it to the system actor.
	ctx = domain.WithActor(ctx, domain.SystemActor())

	switch *action {
	case "check-stock":
		runStockCheck(ctx, pool, *onlyDrifted)

	case "purge-sessions":
		cutoff := time.Now().UTC().Add(-*olderThan)
		deleted, purgeErr := postgres.NewSessionRepository(pool).PurgeInactive(ctx, cutoff)
		if purgeErr != nil {
			fail("failed to purge sessions: " + purgeErr.Error())
		}
		fmt.Printf("purged %d session(s) inactive before %s\n", deleted, cutoff.Format(time.RFC3339))

	default:
		fail("unknown action; valid actions are 'check-stock' and 'purge-sessions'")
	}
}

// runStockCheck compares products.current_stock with the movement ledger.
// Exit code 2 signals drift so cron/CI can alert without parsing output.
func runStockCheck(ctx context.Context, pool *pgxpool.Pool, onlyDrifted bool) {
	repo := postgres.NewReportRepository(pool)

	items, total, summary, err := repo.StockIntegrity(ctx, domain.StockIntegrityFilter{
		OnlyDrifted: onlyDrifted,
		Limit:       100,
		Offset:      0,
	})
	if err != nil {
		fail("stock integrity check failed: " + err.Error())
	}

	fmt.Printf("products checked : %d\n", summary.TotalProducts)
	fmt.Printf("drifted products : %d\n", summary.DriftedProducts)
	fmt.Printf("absolute drift   : %d unit(s)\n", summary.AbsoluteDrift)
	fmt.Printf("rows returned    : %d of %d\n", len(items), total)

	for _, row := range items {
		fmt.Printf("  %-30s current=%-10d ledger=%-10d drift=%+d active=%t\n",
			row.SKU, row.CurrentStock, row.LedgerStock, row.Drift, row.IsActive)
	}

	if summary.DriftedProducts > 0 {
		slog.Warn("Stock drift detected; investigate before trusting balances",
			"drifted_products", summary.DriftedProducts,
			"absolute_drift", summary.AbsoluteDrift,
		)
		os.Exit(2)
	}

	fmt.Println("no stock drift detected")
}

func fail(message string) {
	slog.Error(message)
	os.Exit(1)
}
