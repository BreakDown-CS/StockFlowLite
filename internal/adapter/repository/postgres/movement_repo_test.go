package postgres_test

import (
	"context"
	"fmt"
	"math"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/BreakDown-CS/StockFlowLite/internal/adapter/repository/postgres"
	"github.com/BreakDown-CS/StockFlowLite/internal/config"
	"github.com/BreakDown-CS/StockFlowLite/internal/domain"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testDatabaseURLEnv points the integration tests at a dedicated database. When it is
// not set, the same environment/configuration source the API uses is tried before the
// tests skip, so `go test ./...` stays green on machines without PostgreSQL.
const testDatabaseURLEnv = "TEST_DATABASE_URL"

// newTestPool returns a pool against the local schema or skips the test.
func newTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	dsn := strings.TrimSpace(os.Getenv(testDatabaseURLEnv))
	if dsn == "" {
		cfg, err := config.LoadFromEnv()
		if err != nil {
			t.Skipf("skipping PostgreSQL integration test: %s is not set and environment configuration is invalid: %v", testDatabaseURLEnv, err)
		}
		dsn = cfg.DB.ConnString()
	}

	poolCfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Skipf("skipping PostgreSQL integration test: invalid connection string: %v", err)
	}
	// The concurrency test needs several connections in flight at once.
	if poolCfg.MaxConns < 20 {
		poolCfg.MaxConns = 20
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		t.Skipf("skipping PostgreSQL integration test: cannot create pool: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("skipping PostgreSQL integration test: database unreachable (%v); start local PostgreSQL, apply migrations ('go run ./cmd/migrate -action up'), or set %s", err, testDatabaseURLEnv)
	}

	var schemaReady bool
	if err := pool.QueryRow(ctx,
		`SELECT to_regclass('public.products') IS NOT NULL AND to_regclass('public.stock_movements') IS NOT NULL`,
	).Scan(&schemaReady); err != nil || !schemaReady {
		pool.Close()
		t.Skip("skipping PostgreSQL integration test: required tables are missing; apply migrations with 'go run ./cmd/migrate -action up'")
	}

	t.Cleanup(pool.Close)
	return pool
}

// seedProduct inserts an isolated product row and removes it (plus its movements) on cleanup.
func seedProduct(t *testing.T, pool *pgxpool.Pool, isActive bool, currentStock int) uuid.UUID {
	t.Helper()

	id := uuid.New()
	sku := "TST-" + strings.ToUpper(strings.ReplaceAll(uuid.NewString(), "-", "")[:20])

	_, err := pool.Exec(context.Background(), `
		INSERT INTO products (id, sku, name, description, unit, current_stock, min_stock, is_active)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`, id, sku, "Phase 4 integration test product", "created by movement_repo_test", "PCS", currentStock, 0, isActive)
	require.NoError(t, err)

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		// stock_movements references products with ON DELETE RESTRICT.
		_, _ = pool.Exec(ctx, `DELETE FROM stock_movements WHERE product_id = $1`, id)
		_, _ = pool.Exec(ctx, `DELETE FROM products WHERE id = $1`, id)
	})

	return id
}

func readStock(t *testing.T, pool *pgxpool.Pool, id uuid.UUID) int {
	t.Helper()
	var stock int
	require.NoError(t, pool.QueryRow(context.Background(),
		`SELECT current_stock FROM products WHERE id = $1`, id).Scan(&stock))
	return stock
}

func countMovements(t *testing.T, pool *pgxpool.Pool, id uuid.UUID) int {
	t.Helper()
	var count int
	require.NoError(t, pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM stock_movements WHERE product_id = $1`, id).Scan(&count))
	return count
}

// TestStockMovementRepository_ReceiveStock_RejectsInvalidInputWithoutDatabase verifies the
// repository guards invalid input before touching the pool (no connection is required).
func TestStockMovementRepository_ReceiveStock_RejectsInvalidInputWithoutDatabase(t *testing.T) {
	repo := postgres.NewStockMovementRepository(nil)
	ctx := context.Background()

	_, err := repo.ReceiveStock(ctx, uuid.Nil, 5, "", "STOCK_RECEIPT", "")
	require.ErrorIs(t, err, domain.ErrMissingProductID)

	for _, qty := range []int{0, -1, -250} {
		_, err := repo.ReceiveStock(ctx, uuid.New(), qty, "", "STOCK_RECEIPT", "")
		require.ErrorIs(t, err, domain.ErrInvalidQuantity, "quantity %d must be rejected", qty)
	}
}

func TestStockMovementRepository_ReceiveStock_Success(t *testing.T) {
	pool := newTestPool(t)
	repo := postgres.NewStockMovementRepository(pool)
	ctx := context.Background()

	id := seedProduct(t, pool, true, 10)

	result, err := repo.ReceiveStock(ctx, id, 25, "Inbound delivery", "STOCK_RECEIPT", "")
	require.NoError(t, err)
	require.NotNil(t, result)

	// Movement (receipt) details returned to the caller.
	require.NotNil(t, result.Movement)
	assert.NotZero(t, result.Movement.ID)
	assert.Equal(t, id, result.Movement.ProductID)
	assert.Equal(t, domain.MovementReceipt, result.Movement.MovementType)
	assert.Equal(t, 25, result.Movement.Quantity)
	assert.Equal(t, "STOCK_RECEIPT", result.Movement.ReferenceType)
	assert.Equal(t, "Inbound delivery", result.Movement.Notes)
	assert.Nil(t, result.Movement.ActorID)
	assert.False(t, result.Movement.CreatedAt.IsZero())

	// Product snapshot reflects the committed balance.
	require.NotNil(t, result.Product)
	assert.Equal(t, 35, result.Product.CurrentStock)
	assert.True(t, result.Product.IsActive)

	// Nothing is served from memory: verify the committed rows.
	assert.Equal(t, 35, readStock(t, pool, id))
	assert.Equal(t, 1, countMovements(t, pool, id))
}

func TestStockMovementRepository_ReceiveStock_ProductNotFound(t *testing.T) {
	pool := newTestPool(t)
	repo := postgres.NewStockMovementRepository(pool)

	result, err := repo.ReceiveStock(context.Background(), uuid.New(), 5, "", "STOCK_RECEIPT", "")
	require.ErrorIs(t, err, domain.ErrProductNotFound)
	assert.Nil(t, result)
}

func TestStockMovementRepository_ReceiveStock_ProductInactive(t *testing.T) {
	pool := newTestPool(t)
	repo := postgres.NewStockMovementRepository(pool)

	id := seedProduct(t, pool, false, 12)

	result, err := repo.ReceiveStock(context.Background(), id, 5, "", "STOCK_RECEIPT", "")
	require.ErrorIs(t, err, domain.ErrProductInactive)
	assert.Nil(t, result)

	// Rejected receipt must not be persisted.
	assert.Equal(t, 12, readStock(t, pool, id))
	assert.Equal(t, 0, countMovements(t, pool, id))
}

func TestStockMovementRepository_ReceiveStock_InvalidQuantity(t *testing.T) {
	pool := newTestPool(t)
	repo := postgres.NewStockMovementRepository(pool)

	id := seedProduct(t, pool, true, 7)

	for _, qty := range []int{0, -5} {
		result, err := repo.ReceiveStock(context.Background(), id, qty, "", "STOCK_RECEIPT", "")
		require.ErrorIs(t, err, domain.ErrInvalidQuantity, "quantity %d must be rejected", qty)
		assert.Nil(t, result)
	}

	assert.Equal(t, 7, readStock(t, pool, id))
	assert.Equal(t, 0, countMovements(t, pool, id))
}

// TestStockMovementRepository_ReceiveStock_RollsBackStockUpdateWhenMovementInsertFails
// proves the receipt is atomic: the product row is updated first inside the transaction,
// then the movement insert is forced to fail because reference_id exceeds the
// VARCHAR(100) column defined in 000004_create_stock_movements.up.sql.
func TestStockMovementRepository_ReceiveStock_RollsBackStockUpdateWhenMovementInsertFails(t *testing.T) {
	pool := newTestPool(t)
	repo := postgres.NewStockMovementRepository(pool)

	id := seedProduct(t, pool, true, 40)

	result, err := repo.ReceiveStock(
		context.Background(),
		id,
		15,
		"",
		"STOCK_RECEIPT",
		strings.Repeat("X", 150), // overflows stock_movements.reference_id VARCHAR(100)
	)
	require.Error(t, err)
	assert.Nil(t, result)
	// The failure is an unexpected database error (HTTP 500 at the edge), not a domain conflict.
	assert.NotErrorIs(t, err, domain.ErrProductNotFound)
	assert.NotErrorIs(t, err, domain.ErrStockOverflow)

	assert.Equal(t, 40, readStock(t, pool, id), "stock update must be rolled back when the movement insert fails")
	assert.Equal(t, 0, countMovements(t, pool, id), "no movement row may survive a failed receipt")
}

func TestStockMovementRepository_ReceiveStock_RejectsStockOverflow(t *testing.T) {
	pool := newTestPool(t)
	repo := postgres.NewStockMovementRepository(pool)

	id := seedProduct(t, pool, true, math.MaxInt32)

	result, err := repo.ReceiveStock(context.Background(), id, 1000, "", "STOCK_RECEIPT", "")
	require.ErrorIs(t, err, domain.ErrStockOverflow)
	assert.Nil(t, result)

	assert.Equal(t, math.MaxInt32, readStock(t, pool, id))
	assert.Equal(t, 0, countMovements(t, pool, id))
}

// TestStockMovementRepository_ReceiveStock_ConcurrentReceiptsNoLostUpdates runs parallel
// receipts against the same product. Row-level locking (SELECT ... FOR UPDATE) must
// serialize the increments so no update is lost and the ledger matches the balance.
func TestStockMovementRepository_ReceiveStock_ConcurrentReceiptsNoLostUpdates(t *testing.T) {
	pool := newTestPool(t)
	repo := postgres.NewStockMovementRepository(pool)

	const workers = 16
	const quantityPerReceipt = 3
	const initialStock = 100

	id := seedProduct(t, pool, true, initialStock)

	start := make(chan struct{})
	errs := make(chan error, workers)
	var wg sync.WaitGroup

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start

			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			_, err := repo.ReceiveStock(ctx, id, quantityPerReceipt, "concurrent receipt", "STOCK_RECEIPT", "")
			errs <- err
		}()
	}

	close(start)
	wg.Wait()
	close(errs)

	for err := range errs {
		require.NoError(t, err)
	}

	expectedStock := initialStock + workers*quantityPerReceipt
	assert.Equal(t, expectedStock, readStock(t, pool, id), "concurrent receipts must not lose updates")
	assert.Equal(t, workers, countMovements(t, pool, id))

	// Ledger and balance must agree.
	movements, total, err := repo.ListMovements(context.Background(), domain.ListMovementFilter{
		ProductID: &id,
		Limit:     workers,
	})
	require.NoError(t, err)
	assert.EqualValues(t, workers, total)
	require.Len(t, movements, workers)

	ledgerDelta := 0
	for _, m := range movements {
		assert.Equal(t, domain.MovementReceipt, m.MovementType)
		ledgerDelta += m.NetQuantityChange()
	}
	assert.Equal(t, expectedStock-initialStock, ledgerDelta)
}

func TestStockMovementRepository_ListMovements_PaginationAndFilters(t *testing.T) {
	pool := newTestPool(t)
	repo := postgres.NewStockMovementRepository(pool)
	ctx := context.Background()

	id := seedProduct(t, pool, true, 0)
	for _, qty := range []int{5, 7, 11} {
		_, err := repo.ReceiveStock(ctx, id, qty, fmt.Sprintf("receipt %d", qty), "STOCK_RECEIPT", "")
		require.NoError(t, err)
	}

	// Page 1: newest first, deterministic ordering on (created_at DESC, id DESC).
	page1, total, err := repo.ListMovements(ctx, domain.ListMovementFilter{ProductID: &id, Limit: 2, Offset: 0})
	require.NoError(t, err)
	assert.EqualValues(t, 3, total)
	require.Len(t, page1, 2)
	assert.Equal(t, 11, page1[0].Quantity)
	assert.Equal(t, 7, page1[1].Quantity)

	// Page 2 contains the remaining row.
	page2, total, err := repo.ListMovements(ctx, domain.ListMovementFilter{ProductID: &id, Limit: 2, Offset: 2})
	require.NoError(t, err)
	assert.EqualValues(t, 3, total)
	require.Len(t, page2, 1)
	assert.Equal(t, 5, page2[0].Quantity)

	// Offset beyond the last page yields an empty, non-nil slice.
	empty, total, err := repo.ListMovements(ctx, domain.ListMovementFilter{ProductID: &id, Limit: 2, Offset: 10})
	require.NoError(t, err)
	assert.EqualValues(t, 3, total)
	assert.Empty(t, empty)
	assert.NotNil(t, empty)

	// Movement type filter: the seeded receipts match RECEIPT and never ISSUE.
	receiptType := domain.MovementReceipt
	receipts, total, err := repo.ListMovements(ctx, domain.ListMovementFilter{ProductID: &id, MovementType: &receiptType, Limit: 10})
	require.NoError(t, err)
	assert.EqualValues(t, 3, total)
	assert.Len(t, receipts, 3)

	issueType := domain.MovementIssue
	issues, total, err := repo.ListMovements(ctx, domain.ListMovementFilter{ProductID: &id, MovementType: &issueType, Limit: 10})
	require.NoError(t, err)
	assert.EqualValues(t, 0, total)
	assert.Empty(t, issues)

	// Unknown (but valid) product filter returns an empty page rather than an error.
	unknownProduct := uuid.New()
	none, total, err := repo.ListMovements(ctx, domain.ListMovementFilter{ProductID: &unknownProduct, Limit: 10})
	require.NoError(t, err)
	assert.EqualValues(t, 0, total)
	assert.Empty(t, none)

	// Negative/zero pagination values are clamped instead of producing invalid SQL.
	clamped, total, err := repo.ListMovements(ctx, domain.ListMovementFilter{ProductID: &id, Limit: 0, Offset: -5})
	require.NoError(t, err)
	assert.EqualValues(t, 3, total)
	assert.Len(t, clamped, 3)
}

func TestStockMovementRepository_ListMovements_OrderingIsStableAcrossPages(t *testing.T) {
	pool := newTestPool(t)
	repo := postgres.NewStockMovementRepository(pool)
	ctx := context.Background()

	id := seedProduct(t, pool, true, 0)
	for i := 0; i < 5; i++ {
		_, err := repo.ReceiveStock(ctx, id, i+1, "", "STOCK_RECEIPT", "")
		require.NoError(t, err)
	}

	var seen []int64
	for offset := 0; offset < 5; offset++ {
		page, _, err := repo.ListMovements(ctx, domain.ListMovementFilter{ProductID: &id, Limit: 1, Offset: offset})
		require.NoError(t, err)
		require.Len(t, page, 1)
		seen = append(seen, page[0].ID)
	}

	for i := 1; i < len(seen); i++ {
		assert.Greater(t, seen[i-1], seen[i], "movement ids must descend monotonically across pages")
	}
}
