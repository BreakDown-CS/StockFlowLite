package postgres

import (
	"context"
	"fmt"
	"strings"

	"github.com/BreakDown-CS/StockFlowLite/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ReportRepository implements domain.ReportRepository using pgxpool.
type ReportRepository struct {
	pool *pgxpool.Pool
}

// NewReportRepository constructs a ReportRepository.
func NewReportRepository(pool *pgxpool.Pool) *ReportRepository {
	return &ReportRepository{pool: pool}
}

// readSnapshot opens a read-only, repeatable-read transaction so the count, the page and
// the summary of a single report all observe the same database snapshot.
func (r *ReportRepository) readSnapshot(ctx context.Context) (pgx.Tx, error) {
	return r.pool.BeginTx(ctx, pgx.TxOptions{
		IsoLevel:   pgx.RepeatableRead,
		AccessMode: pgx.ReadOnly,
	})
}

// Inventory reports current stock straight from products.current_stock (the source of
// truth) together with low-stock and out-of-stock flags and a filtered summary.
func (r *ReportRepository) Inventory(
	ctx context.Context,
	filter domain.InventoryReportFilter,
) ([]domain.InventoryReportRow, int64, domain.InventorySummary, error) {
	where := []string{"1=1"}
	args := make([]any, 0, 3)
	argIdx := 1

	if filter.IsActive != nil {
		where = append(where, fmt.Sprintf("is_active = $%d", argIdx))
		args = append(args, *filter.IsActive)
		argIdx++
	}
	if search := strings.TrimSpace(filter.Search); search != "" {
		where = append(where, fmt.Sprintf("(sku ILIKE $%d OR name ILIKE $%d)", argIdx, argIdx))
		args = append(args, "%"+search+"%")
		argIdx++
	}
	if filter.LowStock {
		// Mirrors domain.Product.IsLowStock: inactive products are never "low stock".
		where = append(where, "is_active AND current_stock <= min_stock")
	}

	whereClause := strings.Join(where, " AND ")

	tx, err := r.readSnapshot(ctx)
	if err != nil {
		return nil, 0, domain.InventorySummary{}, fmt.Errorf("failed to begin inventory report transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	var total int64
	countQuery := fmt.Sprintf("SELECT COUNT(*)::bigint FROM products WHERE %s", whereClause)
	if err := tx.QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, domain.InventorySummary{}, fmt.Errorf("failed to count inventory report rows: %w", err)
	}

	pageQuery := fmt.Sprintf(`
		SELECT id, sku, name, unit, current_stock, min_stock, is_active, updated_at
		FROM products
		WHERE %s
		ORDER BY sku ASC
		LIMIT $%d OFFSET $%d;
	`, whereClause, argIdx, argIdx+1)

	pageArgs := append(append([]any{}, args...), filter.Limit, filter.Offset)
	rows, err := tx.Query(ctx, pageQuery, pageArgs...)
	if err != nil {
		return nil, 0, domain.InventorySummary{}, fmt.Errorf("failed to query inventory report rows: %w", err)
	}
	defer rows.Close()

	items := make([]domain.InventoryReportRow, 0, filter.Limit)
	for rows.Next() {
		var row domain.InventoryReportRow
		if err := rows.Scan(
			&row.ID,
			&row.SKU,
			&row.Name,
			&row.Unit,
			&row.CurrentStock,
			&row.MinStock,
			&row.IsActive,
			&row.UpdatedAt,
		); err != nil {
			return nil, 0, domain.InventorySummary{}, fmt.Errorf("failed to scan inventory report row: %w", err)
		}
		row.IsLowStock = row.IsActive && row.CurrentStock <= row.MinStock
		row.IsOutOfStock = row.CurrentStock == 0
		items = append(items, row)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, domain.InventorySummary{}, fmt.Errorf("error during inventory report iteration: %w", err)
	}

	summaryQuery := fmt.Sprintf(`
		SELECT
			COUNT(*)::bigint,
			COUNT(*) FILTER (WHERE is_active)::bigint,
			COUNT(*) FILTER (WHERE NOT is_active)::bigint,
			COUNT(*) FILTER (WHERE is_active AND current_stock <= min_stock)::bigint,
			COUNT(*) FILTER (WHERE current_stock = 0)::bigint
		FROM products
		WHERE %s;
	`, whereClause)

	var summary domain.InventorySummary
	if err := tx.QueryRow(ctx, summaryQuery, args...).Scan(
		&summary.TotalProducts,
		&summary.ActiveProducts,
		&summary.InactiveProducts,
		&summary.LowStockProducts,
		&summary.OutOfStockProducts,
	); err != nil {
		return nil, 0, domain.InventorySummary{}, fmt.Errorf("failed to aggregate inventory report: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, 0, domain.InventorySummary{}, fmt.Errorf("failed to commit inventory report transaction: %w", err)
	}

	return items, total, summary, nil
}

// Movements reports ledger rows enriched with product information plus per-type totals.
// The product join is strictly one-to-one (primary key), so rows are never duplicated.
func (r *ReportRepository) Movements(
	ctx context.Context,
	filter domain.MovementReportFilter,
) ([]domain.MovementReportRow, int64, domain.MovementSummary, error) {
	where := []string{"1=1"}
	args := make([]any, 0, 4)
	argIdx := 1

	if filter.ProductID != nil {
		where = append(where, fmt.Sprintf("m.product_id = $%d", argIdx))
		args = append(args, *filter.ProductID)
		argIdx++
	}
	if filter.MovementType != nil {
		where = append(where, fmt.Sprintf("m.movement_type = $%d", argIdx))
		args = append(args, string(*filter.MovementType))
		argIdx++
	}
	if filter.From != nil {
		where = append(where, fmt.Sprintf("m.created_at >= $%d", argIdx))
		args = append(args, *filter.From)
		argIdx++
	}
	if filter.To != nil {
		where = append(where, fmt.Sprintf("m.created_at <= $%d", argIdx))
		args = append(args, *filter.To)
		argIdx++
	}

	whereClause := strings.Join(where, " AND ")

	tx, err := r.readSnapshot(ctx)
	if err != nil {
		return nil, 0, domain.MovementSummary{}, fmt.Errorf("failed to begin movement report transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	var total int64
	countQuery := fmt.Sprintf("SELECT COUNT(*)::bigint FROM stock_movements m WHERE %s", whereClause)
	if err := tx.QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, domain.MovementSummary{}, fmt.Errorf("failed to count movement report rows: %w", err)
	}

	pageQuery := fmt.Sprintf(`
		SELECT m.id, m.product_id, COALESCE(p.sku, ''), COALESCE(p.name, ''),
		       m.movement_type, m.quantity, m.reference_type, COALESCE(m.reference_id, ''),
		       m.actor_id, COALESCE(m.notes, ''), m.created_at
		FROM stock_movements m
		LEFT JOIN products p ON p.id = m.product_id
		WHERE %s
		ORDER BY m.created_at DESC, m.id DESC
		LIMIT $%d OFFSET $%d;
	`, whereClause, argIdx, argIdx+1)

	pageArgs := append(append([]any{}, args...), filter.Limit, filter.Offset)
	rows, err := tx.Query(ctx, pageQuery, pageArgs...)
	if err != nil {
		return nil, 0, domain.MovementSummary{}, fmt.Errorf("failed to query movement report rows: %w", err)
	}
	defer rows.Close()

	items := make([]domain.MovementReportRow, 0, filter.Limit)
	for rows.Next() {
		var row domain.MovementReportRow
		var movementType string
		if err := rows.Scan(
			&row.ID,
			&row.ProductID,
			&row.SKU,
			&row.ProductName,
			&movementType,
			&row.Quantity,
			&row.ReferenceType,
			&row.ReferenceID,
			&row.ActorID,
			&row.Notes,
			&row.CreatedAt,
		); err != nil {
			return nil, 0, domain.MovementSummary{}, fmt.Errorf("failed to scan movement report row: %w", err)
		}

		row.MovementType = domain.MovementType(movementType)
		movement := domain.StockMovement{MovementType: row.MovementType, Quantity: row.Quantity}
		row.SignedQuantity = movement.NetQuantityChange()
		items = append(items, row)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, domain.MovementSummary{}, fmt.Errorf("error during movement report iteration: %w", err)
	}

	summaryQuery := fmt.Sprintf(`
		SELECT
			COUNT(*)::bigint,
			COALESCE(SUM(m.quantity) FILTER (WHERE m.movement_type = 'RECEIPT'), 0)::bigint,
			COALESCE(SUM(m.quantity) FILTER (WHERE m.movement_type = 'ISSUE'), 0)::bigint,
			COALESCE(SUM(m.quantity) FILTER (WHERE m.movement_type = 'ADJUSTMENT'), 0)::bigint
		FROM stock_movements m
		WHERE %s;
	`, whereClause)

	var summary domain.MovementSummary
	if err := tx.QueryRow(ctx, summaryQuery, args...).Scan(
		&summary.MovementCount,
		&summary.ReceiptTotalQuantity,
		&summary.IssueTotalQuantity,
		&summary.AdjustmentTotalQuantity,
	); err != nil {
		return nil, 0, domain.MovementSummary{}, fmt.Errorf("failed to aggregate movement report: %w", err)
	}
	summary.NetChange = summary.ReceiptTotalQuantity - summary.IssueTotalQuantity + summary.AdjustmentTotalQuantity

	if err := tx.Commit(ctx); err != nil {
		return nil, 0, domain.MovementSummary{}, fmt.Errorf("failed to commit movement report transaction: %w", err)
	}

	return items, total, summary, nil
}

// requestItemsAggregate pre-aggregates line items so joining it never multiplies rows.
const requestItemsAggregate = `(
	SELECT request_id,
	       COUNT(*)::int AS item_count,
	       SUM(requested_quantity)::int AS requested_units
	FROM stock_request_items
	GROUP BY request_id
)`

// Requests reports request lines with requester, item totals and fulfillment totals.
// Every join is unique per request (users primary key, issuances UNIQUE(request_id),
// pre-aggregated items), so counts and sums are never inflated.
func (r *ReportRepository) Requests(
	ctx context.Context,
	filter domain.RequestReportFilter,
) ([]domain.RequestReportRow, int64, domain.RequestSummary, error) {
	where := []string{"1=1"}
	args := make([]any, 0, 4)
	argIdx := 1

	if filter.Status != nil {
		where = append(where, fmt.Sprintf("r.status = $%d", argIdx))
		args = append(args, string(*filter.Status))
		argIdx++
	}
	if filter.RequesterID != nil {
		where = append(where, fmt.Sprintf("r.requester_id = $%d", argIdx))
		args = append(args, *filter.RequesterID)
		argIdx++
	}
	if filter.From != nil {
		where = append(where, fmt.Sprintf("r.created_at >= $%d", argIdx))
		args = append(args, *filter.From)
		argIdx++
	}
	if filter.To != nil {
		where = append(where, fmt.Sprintf("r.created_at <= $%d", argIdx))
		args = append(args, *filter.To)
		argIdx++
	}

	whereClause := strings.Join(where, " AND ")

	tx, err := r.readSnapshot(ctx)
	if err != nil {
		return nil, 0, domain.RequestSummary{}, fmt.Errorf("failed to begin request report transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	var total int64
	countQuery := fmt.Sprintf("SELECT COUNT(*)::bigint FROM stock_requests r WHERE %s", whereClause)
	if err := tx.QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, domain.RequestSummary{}, fmt.Errorf("failed to count request report rows: %w", err)
	}

	pageQuery := fmt.Sprintf(`
		SELECT r.id, r.request_number, r.status, r.requester_id, COALESCE(u.username, ''),
		       COALESCE(i.item_count, 0), COALESCE(i.requested_units, 0), COALESCE(iss.total_quantity, 0),
		       r.created_at, r.reviewed_at, iss.created_at,
		       COALESCE(r.rejection_reason, ''), COALESCE(r.cancellation_reason, '')
		FROM stock_requests r
		LEFT JOIN users u ON u.id = r.requester_id
		LEFT JOIN stock_request_issuances iss ON iss.request_id = r.id
		LEFT JOIN %s i ON i.request_id = r.id
		WHERE %s
		ORDER BY r.created_at DESC, r.id DESC
		LIMIT $%d OFFSET $%d;
	`, requestItemsAggregate, whereClause, argIdx, argIdx+1)

	pageArgs := append(append([]any{}, args...), filter.Limit, filter.Offset)
	rows, err := tx.Query(ctx, pageQuery, pageArgs...)
	if err != nil {
		return nil, 0, domain.RequestSummary{}, fmt.Errorf("failed to query request report rows: %w", err)
	}
	defer rows.Close()

	items := make([]domain.RequestReportRow, 0, filter.Limit)
	for rows.Next() {
		var row domain.RequestReportRow
		var status string
		if err := rows.Scan(
			&row.ID,
			&row.RequestNumber,
			&status,
			&row.RequesterID,
			&row.RequesterUsername,
			&row.ItemCount,
			&row.RequestedUnits,
			&row.IssuedUnits,
			&row.CreatedAt,
			&row.ReviewedAt,
			&row.IssuedAt,
			&row.RejectionReason,
			&row.CancellationReason,
		); err != nil {
			return nil, 0, domain.RequestSummary{}, fmt.Errorf("failed to scan request report row: %w", err)
		}
		row.Status = domain.RequestStatus(status)
		items = append(items, row)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, domain.RequestSummary{}, fmt.Errorf("error during request report iteration: %w", err)
	}

	summaryQuery := fmt.Sprintf(`
		SELECT
			COUNT(*)::bigint,
			COUNT(*) FILTER (WHERE r.status = 'PENDING')::bigint,
			COUNT(*) FILTER (WHERE r.status = 'APPROVED')::bigint,
			COUNT(*) FILTER (WHERE r.status = 'REJECTED')::bigint,
			COUNT(*) FILTER (WHERE r.status = 'ISSUED')::bigint,
			COUNT(*) FILTER (WHERE r.status = 'CANCELLED')::bigint,
			COALESCE(SUM(i.requested_units), 0)::bigint,
			COALESCE(SUM(iss.total_quantity), 0)::bigint
		FROM stock_requests r
		LEFT JOIN stock_request_issuances iss ON iss.request_id = r.id
		LEFT JOIN %s i ON i.request_id = r.id
		WHERE %s;
	`, requestItemsAggregate, whereClause)

	var summary domain.RequestSummary
	if err := tx.QueryRow(ctx, summaryQuery, args...).Scan(
		&summary.TotalRequests,
		&summary.PendingRequests,
		&summary.ApprovedRequests,
		&summary.RejectedRequests,
		&summary.IssuedRequests,
		&summary.CancelledRequests,
		&summary.RequestedUnitsTotal,
		&summary.IssuedUnitsTotal,
	); err != nil {
		return nil, 0, domain.RequestSummary{}, fmt.Errorf("failed to aggregate request report: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, 0, domain.RequestSummary{}, fmt.Errorf("failed to commit request report transaction: %w", err)
	}

	return items, total, summary, nil
}

// ledgerAggregate reconstructs the expected balance from the immutable ledger.
// quantity is always positive and the direction is encoded by movement_type.
const ledgerAggregate = `(
	SELECT product_id,
	       SUM(CASE WHEN movement_type = 'ISSUE' THEN -quantity ELSE quantity END)::bigint AS ledger_stock
	FROM stock_movements
	GROUP BY product_id
)`

// StockIntegrity compares products.current_stock with the movement ledger and reports drift.
// It is read-only and never repairs data.
func (r *ReportRepository) StockIntegrity(
	ctx context.Context,
	filter domain.StockIntegrityFilter,
) ([]domain.StockIntegrityRow, int64, domain.StockIntegritySummary, error) {
	// Products without movements expect a zero ledger balance, so COALESCE is applied.
	const driftExpr = "p.current_stock::bigint - COALESCE(l.ledger_stock, 0)"

	where := "1=1"
	if filter.OnlyDrifted {
		where = fmt.Sprintf("%s <> 0", driftExpr)
	}

	tx, err := r.readSnapshot(ctx)
	if err != nil {
		return nil, 0, domain.StockIntegritySummary{}, fmt.Errorf("failed to begin integrity report transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	var total int64
	countQuery := fmt.Sprintf(`
		SELECT COUNT(*)::bigint
		FROM products p
		LEFT JOIN %s l ON l.product_id = p.id
		WHERE %s;
	`, ledgerAggregate, where)
	if err := tx.QueryRow(ctx, countQuery).Scan(&total); err != nil {
		return nil, 0, domain.StockIntegritySummary{}, fmt.Errorf("failed to count integrity rows: %w", err)
	}

	pageQuery := fmt.Sprintf(`
		SELECT p.id, p.sku, p.name, p.is_active, p.current_stock,
		       COALESCE(l.ledger_stock, 0)::bigint AS ledger_stock,
		       %s AS drift
		FROM products p
		LEFT JOIN %s l ON l.product_id = p.id
		WHERE %s
		ORDER BY abs(%s) DESC, p.sku ASC
		LIMIT $1 OFFSET $2;
	`, driftExpr, ledgerAggregate, where, driftExpr)

	rows, err := tx.Query(ctx, pageQuery, filter.Limit, filter.Offset)
	if err != nil {
		return nil, 0, domain.StockIntegritySummary{}, fmt.Errorf("failed to query integrity rows: %w", err)
	}
	defer rows.Close()

	items := make([]domain.StockIntegrityRow, 0, filter.Limit)
	for rows.Next() {
		var row domain.StockIntegrityRow
		if err := rows.Scan(
			&row.ProductID,
			&row.SKU,
			&row.Name,
			&row.IsActive,
			&row.CurrentStock,
			&row.LedgerStock,
			&row.Drift,
		); err != nil {
			return nil, 0, domain.StockIntegritySummary{}, fmt.Errorf("failed to scan integrity row: %w", err)
		}
		items = append(items, row)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, domain.StockIntegritySummary{}, fmt.Errorf("error during integrity iteration: %w", err)
	}

	summaryQuery := fmt.Sprintf(`
		SELECT
			COUNT(*)::bigint,
			COUNT(*) FILTER (WHERE %s <> 0)::bigint,
			COALESCE(SUM(abs(%s)), 0)::bigint
		FROM products p
		LEFT JOIN %s l ON l.product_id = p.id;
	`, driftExpr, driftExpr, ledgerAggregate)

	var summary domain.StockIntegritySummary
	if err := tx.QueryRow(ctx, summaryQuery).Scan(
		&summary.TotalProducts,
		&summary.DriftedProducts,
		&summary.AbsoluteDrift,
	); err != nil {
		return nil, 0, domain.StockIntegritySummary{}, fmt.Errorf("failed to aggregate integrity report: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, 0, domain.StockIntegritySummary{}, fmt.Errorf("failed to commit integrity report transaction: %w", err)
	}

	return items, total, summary, nil
}
