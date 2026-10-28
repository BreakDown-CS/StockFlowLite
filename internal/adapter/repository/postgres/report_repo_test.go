package postgres_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/BreakDown-CS/StockFlowLite/internal/adapter/repository/postgres"
	"github.com/BreakDown-CS/StockFlowLite/internal/domain"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// seedReportProduct inserts a product with an explicit min_stock and registers cleanup.
func seedReportProduct(t *testing.T, pool *pgxpool.Pool, sku string, stock, minStock int, active bool) uuid.UUID {
	t.Helper()

	id := uuid.New()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO products (id, sku, name, description, unit, current_stock, min_stock, is_active)
		VALUES ($1, $2, $3, '', 'PCS', $4, $5, $6)
	`, id, sku, "Report Product "+sku, stock, minStock, active)
	require.NoError(t, err)

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, _ = pool.Exec(ctx, `DELETE FROM stock_movements WHERE product_id = $1`, id)
		_, _ = pool.Exec(ctx, `DELETE FROM audit_events WHERE entity_id = $1`, id)
		_, _ = pool.Exec(ctx, `DELETE FROM products WHERE id = $1`, id)
	})

	return id
}

// insertReportMovement writes a ledger row with a controlled timestamp.
func insertReportMovement(t *testing.T, pool *pgxpool.Pool, productID uuid.UUID, movementType string, quantity int, createdAt time.Time) int64 {
	t.Helper()

	var id int64
	require.NoError(t, pool.QueryRow(context.Background(), `
		INSERT INTO stock_movements (product_id, movement_type, quantity, reference_type, reference_id, created_at)
		VALUES ($1, $2, $3, 'TEST', $4, $5)
		RETURNING id
	`, productID, movementType, quantity, "RPT-"+strings.ToUpper(uuid.NewString()[:8]), createdAt).Scan(&id))

	return id
}

// createReportRequest persists a request with a controlled created_at and registers cleanup.
func createReportRequest(
	t *testing.T,
	repo *postgres.StockRequestRepository,
	pool *pgxpool.Pool,
	requesterID uuid.UUID,
	createdAt time.Time,
	specs ...requestItemSpec,
) *domain.StockRequest {
	t.Helper()

	items := make([]domain.StockRequestItem, 0, len(specs))
	for _, spec := range specs {
		items = append(items, domain.StockRequestItem{ProductID: spec.ProductID, RequestedQuantity: spec.Quantity})
	}

	req, err := domain.NewStockRequest(testRequestNumber(), requesterID, "", items)
	require.NoError(t, err)
	req.CreatedAt = createdAt
	req.UpdatedAt = createdAt

	require.NoError(t, repo.Create(context.Background(), req))

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, _ = pool.Exec(ctx, `DELETE FROM audit_events WHERE entity_id = $1`, req.ID)
		_, _ = pool.Exec(ctx, `DELETE FROM stock_request_issuances WHERE request_id = $1`, req.ID)
		_, _ = pool.Exec(ctx, `DELETE FROM stock_request_items WHERE request_id = $1`, req.ID)
		_, _ = pool.Exec(ctx, `DELETE FROM stock_requests WHERE id = $1`, req.ID)
	})

	return req
}

// reportSKUPrefix returns a per-run prefix so report queries only see this test's rows.
func reportSKUPrefix() string {
	return "RPT" + strings.ToUpper(strings.ReplaceAll(uuid.NewString(), "-", "")[:8])
}

func skuFor(prefix, suffix string) string {
	return fmt.Sprintf("%s-%s", prefix, suffix)
}

func TestReportRepository_Inventory_SummaryFlagsAndFilters(t *testing.T) {
	pool := newTestPool(t)
	repo := postgres.NewReportRepository(pool)
	ctx := context.Background()

	prefix := reportSKUPrefix()
	seedReportProduct(t, pool, skuFor(prefix, "A"), 0, 0, true)  // out of stock + low
	seedReportProduct(t, pool, skuFor(prefix, "B"), 5, 5, true)  // low (<= min_stock)
	seedReportProduct(t, pool, skuFor(prefix, "C"), 10, 2, true) // healthy
	seedReportProduct(t, pool, skuFor(prefix, "D"), 0, 0, false) // inactive (never low)

	activeOnly := true
	items, total, summary, err := repo.Inventory(ctx, domain.InventoryReportFilter{
		Search: prefix, IsActive: &activeOnly, Limit: 100,
	})
	require.NoError(t, err)
	assert.EqualValues(t, 3, total)
	assert.Equal(t, domain.InventorySummary{
		TotalProducts: 3, ActiveProducts: 3, InactiveProducts: 0,
		LowStockProducts: 2, OutOfStockProducts: 1,
	}, summary)
	require.Len(t, items, 3)
	assert.Equal(t, skuFor(prefix, "A"), items[0].SKU)
	assert.True(t, items[0].IsLowStock)
	assert.True(t, items[0].IsOutOfStock)
	assert.False(t, items[2].IsLowStock)
	assert.False(t, items[2].IsOutOfStock)

	// All products: inactive are listed but never counted as low stock.
	items, total, summary, err = repo.Inventory(ctx, domain.InventoryReportFilter{Search: prefix, Limit: 100})
	require.NoError(t, err)
	assert.EqualValues(t, 4, total)
	assert.Len(t, items, 4)
	assert.EqualValues(t, 1, summary.InactiveProducts)
	assert.EqualValues(t, 2, summary.LowStockProducts)
	assert.EqualValues(t, 2, summary.OutOfStockProducts)

	// Low-stock-only filter mirrors domain.Product.IsLowStock.
	items, total, summary, err = repo.Inventory(ctx, domain.InventoryReportFilter{
		Search: prefix, IsActive: &activeOnly, LowStock: true, Limit: 100,
	})
	require.NoError(t, err)
	assert.EqualValues(t, 2, total)
	require.Len(t, items, 2)
	assert.Equal(t, skuFor(prefix, "A"), items[0].SKU)
	assert.Equal(t, skuFor(prefix, "B"), items[1].SKU)
	assert.EqualValues(t, 2, summary.LowStockProducts)

	// Pagination never changes the summary (it aggregates the whole filtered set).
	page, total, summary, err := repo.Inventory(ctx, domain.InventoryReportFilter{
		Search: prefix, IsActive: &activeOnly, Limit: 2, Offset: 2,
	})
	require.NoError(t, err)
	assert.EqualValues(t, 3, total)
	require.Len(t, page, 1)
	assert.EqualValues(t, 2, summary.LowStockProducts)
}

func TestReportRepository_Movements_SummaryFiltersAndOrdering(t *testing.T) {
	pool := newTestPool(t)
	repo := postgres.NewReportRepository(pool)
	movements := postgres.NewStockMovementRepository(pool)
	ctx := context.Background()

	prefix := reportSKUPrefix()
	product := seedReportProduct(t, pool, skuFor(prefix, "M"), 100, 0, true)

	now := time.Now().UTC().Truncate(time.Second)
	insertReportMovement(t, pool, product, "RECEIPT", 10, now.Add(-72*time.Hour))
	insertReportMovement(t, pool, product, "RECEIPT", 5, now.Add(-48*time.Hour))
	issueID := insertReportMovement(t, pool, product, "ISSUE", 4, now.Add(-24*time.Hour))
	adjustmentID := insertReportMovement(t, pool, product, "ADJUSTMENT", 2, now)

	items, total, summary, err := repo.Movements(ctx, domain.MovementReportFilter{ProductID: &product, Limit: 100})
	require.NoError(t, err)
	assert.EqualValues(t, 4, total)
	assert.Equal(t, domain.MovementSummary{
		MovementCount:           4,
		ReceiptTotalQuantity:    15,
		IssueTotalQuantity:      4,
		AdjustmentTotalQuantity: 2,
		NetChange:               13,
	}, summary)
	require.Len(t, items, 4, "the product join must not duplicate ledger rows")

	// Newest first, id as deterministic tiebreaker.
	assert.Equal(t, adjustmentID, items[0].ID)
	assert.Equal(t, issueID, items[1].ID)
	assert.Equal(t, -4, items[1].SignedQuantity)
	assert.Equal(t, skuFor(prefix, "M"), items[0].SKU)
	assert.Equal(t, "Report Product "+skuFor(prefix, "M"), items[0].ProductName)

	// Date range is inclusive on both boundaries (the 48h receipt is included).
	from := now.Add(-48 * time.Hour)
	items, total, summary, err = repo.Movements(ctx, domain.MovementReportFilter{ProductID: &product, From: &from, Limit: 100})
	require.NoError(t, err)
	assert.EqualValues(t, 3, total)
	assert.Len(t, items, 3)
	assert.EqualValues(t, 5, summary.ReceiptTotalQuantity)
	assert.EqualValues(t, 3, summary.NetChange)

	// Type filter.
	issueType := domain.MovementIssue
	items, total, summary, err = repo.Movements(ctx, domain.MovementReportFilter{
		ProductID: &product, MovementType: &issueType, Limit: 100,
	})
	require.NoError(t, err)
	assert.EqualValues(t, 1, total)
	require.Len(t, items, 1)
	assert.Equal(t, issueID, items[0].ID)
	assert.EqualValues(t, 4, summary.IssueTotalQuantity)
	assert.EqualValues(t, -4, summary.NetChange)

	// The pre-existing /stock-movements endpoint now shares the same date-range semantics.
	list, listTotal, err := movements.ListMovements(ctx, domain.ListMovementFilter{
		ProductID: &product, From: &from, Limit: 100,
	})
	require.NoError(t, err)
	assert.EqualValues(t, 3, listTotal)
	assert.Len(t, list, 3)
}

func TestReportRepository_StockIntegrity_DetectsDrift(t *testing.T) {
	pool := newTestPool(t)
	repo := postgres.NewReportRepository(pool)
	ctx := context.Background()

	prefix := reportSKUPrefix()
	now := time.Now().UTC()

	// Ledger +10 then -4 = 6, stored balance 6 -> no drift.
	consistent := seedReportProduct(t, pool, skuFor(prefix, "C"), 6, 0, true)
	insertReportMovement(t, pool, consistent, "RECEIPT", 10, now.Add(-2*time.Hour))
	insertReportMovement(t, pool, consistent, "ISSUE", 4, now.Add(-time.Hour))

	// Product without movements: expected ledger balance is 0.
	untouched := seedReportProduct(t, pool, skuFor(prefix, "U"), 0, 0, true)

	// Deliberate drift introduced by a direct write (test-only data, removed on cleanup).
	drifted := seedReportProduct(t, pool, skuFor(prefix, "D"), 0, 0, true)
	_, err := pool.Exec(ctx, `UPDATE products SET current_stock = current_stock + 7 WHERE id = $1`, drifted)
	require.NoError(t, err)

	items, total, summary, err := repo.StockIntegrity(ctx, domain.StockIntegrityFilter{Limit: 100})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, total, int64(3))
	assert.GreaterOrEqual(t, summary.DriftedProducts, int64(1))
	assert.GreaterOrEqual(t, summary.AbsoluteDrift, int64(7))

	byProduct := map[uuid.UUID]domain.StockIntegrityRow{}
	for _, item := range items {
		byProduct[item.ProductID] = item
	}

	require.Contains(t, byProduct, consistent)
	assert.EqualValues(t, 6, byProduct[consistent].LedgerStock)
	assert.EqualValues(t, 0, byProduct[consistent].Drift)

	require.Contains(t, byProduct, untouched)
	assert.EqualValues(t, 0, byProduct[untouched].LedgerStock)
	assert.EqualValues(t, 0, byProduct[untouched].Drift)

	require.Contains(t, byProduct, drifted)
	assert.EqualValues(t, 0, byProduct[drifted].LedgerStock)
	assert.EqualValues(t, 7, byProduct[drifted].Drift)

	// only_drifted excludes healthy products and never repairs data.
	driftedItems, driftedTotal, _, err := repo.StockIntegrity(ctx, domain.StockIntegrityFilter{OnlyDrifted: true, Limit: 100})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, driftedTotal, int64(1))
	require.NotEmpty(t, driftedItems)
	for _, item := range driftedItems {
		assert.NotZero(t, item.Drift)
	}

	var stored int
	require.NoError(t, pool.QueryRow(ctx, `SELECT current_stock FROM products WHERE id = $1`, drifted).Scan(&stored))
	assert.Equal(t, 7, stored, "integrity reporting must be read-only")
}

func TestReportRepository_Requests_SummaryStatusesAndScoping(t *testing.T) {
	pool := newTestPool(t)
	repo := postgres.NewStockRequestRepository(pool)
	reports := postgres.NewReportRepository(pool)
	users := postgres.NewUserRepository(pool)
	ctx := context.Background()

	requester, _ := createTestUser(t, users, pool, domain.RoleRequester)
	otherRequester, _ := createTestUser(t, users, pool, domain.RoleRequester)

	prefix := reportSKUPrefix()
	firstProduct := seedReportProduct(t, pool, skuFor(prefix, "R1"), 50, 0, true)
	secondProduct := seedReportProduct(t, pool, skuFor(prefix, "R2"), 50, 0, true)

	base := time.Now().UTC().Truncate(time.Second).Add(-2 * time.Hour)

	pending := createReportRequest(t, repo, pool, requester.ID, base,
		requestItemSpec{ProductID: firstProduct, Quantity: 5},
		requestItemSpec{ProductID: secondProduct, Quantity: 3},
	)

	approved := createReportRequest(t, repo, pool, requester.ID, base.Add(10*time.Minute),
		requestItemSpec{ProductID: firstProduct, Quantity: 1},
	)
	_, err := repo.Approve(ctx, approved.ID, testReviewerID)
	require.NoError(t, err)

	issued := createReportRequest(t, repo, pool, requester.ID, base.Add(20*time.Minute),
		requestItemSpec{ProductID: firstProduct, Quantity: 10},
	)
	_, err = repo.Approve(ctx, issued.ID, testReviewerID)
	require.NoError(t, err)
	_, err = repo.Issue(ctx, issued.ID, testReviewerID, "")
	require.NoError(t, err)

	rejected := createReportRequest(t, repo, pool, requester.ID, base.Add(30*time.Minute),
		requestItemSpec{ProductID: firstProduct, Quantity: 2},
	)
	_, err = repo.Reject(ctx, rejected.ID, testReviewerID, "no budget")
	require.NoError(t, err)

	cancelled := createReportRequest(t, repo, pool, requester.ID, base.Add(40*time.Minute),
		requestItemSpec{ProductID: firstProduct, Quantity: 1},
	)
	_, err = repo.Cancel(ctx, cancelled.ID, requester.ID, "changed my mind")
	require.NoError(t, err)

	items, total, summary, err := reports.Requests(ctx, domain.RequestReportFilter{
		RequesterID: &requester.ID,
		Limit:       100,
	})
	require.NoError(t, err)
	assert.EqualValues(t, 5, total)
	assert.Equal(t, domain.RequestSummary{
		TotalRequests:       5,
		PendingRequests:     1,
		ApprovedRequests:    1,
		RejectedRequests:    1,
		IssuedRequests:      1,
		CancelledRequests:   1,
		RequestedUnitsTotal: 22,
		IssuedUnitsTotal:    10,
	}, summary)
	require.Len(t, items, 5)

	// Newest first (created_at DESC, id DESC).
	assert.Equal(t, cancelled.ID, items[0].ID)
	assert.Equal(t, pending.ID, items[4].ID)

	for _, row := range items {
		assert.Equal(t, requester.Username, row.RequesterUsername)
		assert.Greater(t, row.ItemCount, 0)

		switch row.ID {
		case issued.ID:
			assert.Equal(t, 10, row.IssuedUnits)
			require.NotNil(t, row.IssuedAt)
		case rejected.ID:
			assert.Equal(t, 0, row.IssuedUnits)
			assert.Nil(t, row.IssuedAt)
			assert.Equal(t, "no budget", row.RejectionReason)
		case cancelled.ID:
			assert.Equal(t, 0, row.IssuedUnits)
			assert.Equal(t, "changed my mind", row.CancellationReason)
		case pending.ID:
			assert.Equal(t, 0, row.IssuedUnits)
			assert.Equal(t, 2, row.ItemCount)
			assert.Equal(t, 8, row.RequestedUnits)
		default:
			assert.Equal(t, 0, row.IssuedUnits)
			assert.Nil(t, row.IssuedAt)
		}
	}

	// Status filter keeps the fulfillment totals consistent.
	issuedStatus := domain.StatusIssued
	items, total, summary, err = reports.Requests(ctx, domain.RequestReportFilter{
		RequesterID: &requester.ID,
		Status:      &issuedStatus,
		Limit:       100,
	})
	require.NoError(t, err)
	assert.EqualValues(t, 1, total)
	require.Len(t, items, 1)
	assert.Equal(t, issued.ID, items[0].ID)
	assert.EqualValues(t, 10, summary.IssuedUnitsTotal)

	// Ownership scoping: another requester sees none of these requests.
	items, total, _, err = reports.Requests(ctx, domain.RequestReportFilter{
		RequesterID: &otherRequester.ID,
		Limit:       100,
	})
	require.NoError(t, err)
	assert.EqualValues(t, 0, total)
	assert.Empty(t, items)

	// Date range in the future returns nothing.
	future := time.Now().UTC().Add(time.Hour)
	_, total, _, err = reports.Requests(ctx, domain.RequestReportFilter{
		RequesterID: &requester.ID,
		From:        &future,
		Limit:       100,
	})
	require.NoError(t, err)
	assert.EqualValues(t, 0, total)
}
