package postgres

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strings"

	"github.com/BreakDown-CS/StockFlowLite/internal/domain"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgreSQL SQLSTATE codes and constraint names produced by the existing
// migrations. Only these known conflicts are translated into domain errors;
// anything else is surfaced as an unexpected failure (HTTP 500 at the edge).
const (
	pgErrCheckViolation      = "23514"
	pgErrForeignKeyViolation = "23503"

	constraintProductsCurrentStock  = "products_current_stock_check"
	constraintMovementQuantityCheck = "stock_movements_quantity_check"
	constraintMovementProductIDFK   = "stock_movements_product_id_fkey"
)

// insertStockMovementSQL appends one immutable ledger row and returns its generated id/timestamp.
// Shared by stock receipts (movement_repo.go) and request issuance (stock_request_repo.go).
const insertStockMovementSQL = `
	INSERT INTO stock_movements (
		product_id, movement_type, quantity, reference_type, reference_id, actor_id, notes, created_at
	) VALUES (
		$1, $2, $3, $4, $5, $6, $7, now()
	)
	RETURNING id, created_at;
`

// StockMovementRepository implements domain.StockMovementRepository using pgxpool.
type StockMovementRepository struct {
	pool *pgxpool.Pool
}

// NewStockMovementRepository constructs a new StockMovementRepository.
func NewStockMovementRepository(pool *pgxpool.Pool) *StockMovementRepository {
	return &StockMovementRepository{pool: pool}
}

// ReceiveStock executes an atomic database transaction with row-level locking on the product.
func (r *StockMovementRepository) ReceiveStock(
	ctx context.Context,
	productID uuid.UUID,
	quantity int,
	notes string,
	refType string,
	refID string,
) (*domain.StockReceiptResult, error) {
	// Defence in depth: the use case validates these too, but the ledger invariant
	// (quantity is always strictly positive) must never depend on a single layer.
	if productID == uuid.Nil {
		return nil, domain.ErrMissingProductID
	}
	if quantity <= 0 {
		return nil, domain.ErrInvalidQuantity
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	// Rollback is a no-op after a successful commit, so it is safe to defer
	// unconditionally: every error path below therefore leaves no partial writes.
	defer func() {
		if rErr := tx.Rollback(ctx); rErr != nil && !errors.Is(rErr, pgx.ErrTxClosed) {
			slog.Error("Failed to roll back stock receipt transaction",
				"product_id", productID.String(),
				"error", rErr,
			)
		}
	}()

	// 1. Lock product row exclusively using row-level locking (FOR UPDATE)
	const selectProductQuery = `
		SELECT id, sku, name, COALESCE(description, ''), unit, current_stock, min_stock, is_active, created_at, updated_at
		FROM products
		WHERE id = $1
		FOR UPDATE;
	`

	p := &domain.Product{}
	err = tx.QueryRow(ctx, selectProductQuery, productID).Scan(
		&p.ID,
		&p.SKU,
		&p.Name,
		&p.Description,
		&p.Unit,
		&p.CurrentStock,
		&p.MinStock,
		&p.IsActive,
		&p.CreatedAt,
		&p.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrProductNotFound
		}
		return nil, fmt.Errorf("failed to lock product: %w", err)
	}

	// 2. Validate product active status
	if !p.IsActive {
		return nil, domain.ErrProductInactive
	}

	// 3. Validate numeric boundary against PostgreSQL INT limit (signed 32-bit integer)
	if int64(p.CurrentStock)+int64(quantity) > math.MaxInt32 {
		return nil, domain.ErrStockOverflow
	}

	// 4. Update product current_stock atomically
	const updateStockQuery = `
		UPDATE products
		SET current_stock = current_stock + $2, updated_at = now()
		WHERE id = $1
		RETURNING current_stock, updated_at;
	`

	err = tx.QueryRow(ctx, updateStockQuery, productID, quantity).Scan(&p.CurrentStock, &p.UpdatedAt)
	if err != nil {
		return nil, mapWriteError("update product stock", err)
	}

	// 5. Insert stock movement record (SQL shared with request issuance)
	movement := &domain.StockMovement{
		ProductID:     productID,
		MovementType:  domain.MovementReceipt,
		Quantity:      quantity,
		ReferenceType: refType,
		ReferenceID:   refID,
		Notes:         notes,
	}

	var actorID *uuid.UUID = nil
	var refIDVal *string = nil
	if refID != "" {
		refIDVal = &refID
	}
	var notesVal *string = nil
	if notes != "" {
		notesVal = &notes
	}

	err = tx.QueryRow(
		ctx,
		insertStockMovementSQL,
		productID,
		string(domain.MovementReceipt),
		quantity,
		refType,
		refIDVal,
		actorID,
		notesVal,
	).Scan(&movement.ID, &movement.CreatedAt)
	if err != nil {
		return nil, mapWriteError("insert stock movement", err)
	}

	receivedEvent := auditEventFromContext(ctx, domain.AuditStockReceived, domain.AuditEntityProduct, &p.ID)
	receivedEvent.Reference = p.SKU
	receivedEvent.Metadata = map[string]interface{}{
		"quantity":    quantity,
		"movement_id": movement.ID,
		"sku":         p.SKU,
	}
	if err := insertAuditEvent(ctx, tx, receivedEvent); err != nil {
		return nil, err
	}

	// 6. Commit transaction atomically
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	return &domain.StockReceiptResult{
		Product:  p,
		Movement: movement,
	}, nil
}

// ListMovements retrieves paginated and filtered movement history records.
func (r *StockMovementRepository) ListMovements(ctx context.Context, filter domain.ListMovementFilter) ([]domain.StockMovement, int64, error) {
	// Defend the query against unbounded or negative pagination values even when the
	// caller bypasses the use case (LIMIT must never be negative).
	limit := filter.Limit
	if limit <= 0 {
		limit = 20
	} else if limit > 100 {
		limit = 100
	}

	offset := filter.Offset
	if offset < 0 {
		offset = 0
	}

	where := []string{"1=1"}
	args := []interface{}{}
	argIdx := 1

	if filter.ProductID != nil {
		where = append(where, fmt.Sprintf("product_id = $%d", argIdx))
		args = append(args, *filter.ProductID)
		argIdx++
	}

	if filter.MovementType != nil {
		where = append(where, fmt.Sprintf("movement_type = $%d", argIdx))
		args = append(args, string(*filter.MovementType))
		argIdx++
	}

	if filter.From != nil {
		where = append(where, fmt.Sprintf("created_at >= $%d", argIdx))
		args = append(args, *filter.From)
		argIdx++
	}

	if filter.To != nil {
		where = append(where, fmt.Sprintf("created_at <= $%d", argIdx))
		args = append(args, *filter.To)
		argIdx++
	}

	whereClause := strings.Join(where, " AND ")

	// 1. Total count query
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM stock_movements WHERE %s", whereClause)
	var total int64
	if err := r.pool.QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("failed to count stock movements: %w", err)
	}

	// 2. Data rows query with pagination
	dataQuery := fmt.Sprintf(`
		SELECT id, product_id, movement_type, quantity, reference_type, 
		       COALESCE(reference_id, ''), actor_id, COALESCE(notes, ''), created_at
		FROM stock_movements
		WHERE %s
		ORDER BY created_at DESC, id DESC
		LIMIT $%d OFFSET $%d;
	`, whereClause, argIdx, argIdx+1)

	queryArgs := append(args, limit, offset)

	rows, err := r.pool.Query(ctx, dataQuery, queryArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to query stock movements: %w", err)
	}
	defer rows.Close()

	movements := make([]domain.StockMovement, 0, limit)
	for rows.Next() {
		var sm domain.StockMovement
		var mTypeStr string
		var refID string
		var notes string

		if err := rows.Scan(
			&sm.ID,
			&sm.ProductID,
			&mTypeStr,
			&sm.Quantity,
			&sm.ReferenceType,
			&refID,
			&sm.ActorID,
			&notes,
			&sm.CreatedAt,
		); err != nil {
			return nil, 0, fmt.Errorf("failed to scan stock movement row: %w", err)
		}

		sm.MovementType = domain.MovementType(mTypeStr)
		sm.ReferenceID = refID
		sm.Notes = notes
		movements = append(movements, sm)
	}

	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("error during stock movements iteration: %w", err)
	}

	return movements, total, nil
}

// mapWriteError translates only the constraint conflicts this phase understands into
// domain errors. Anything else is wrapped, so the HTTP layer answers 500 without
// leaking SQL, credentials, or internal details to the client.
func mapWriteError(operation string, err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch {
		case pgErr.Code == pgErrCheckViolation && pgErr.ConstraintName == constraintProductsCurrentStock:
			return domain.ErrStockOverflow
		case pgErr.Code == pgErrCheckViolation && pgErr.ConstraintName == constraintMovementQuantityCheck:
			return domain.ErrInvalidQuantity
		case pgErr.Code == pgErrForeignKeyViolation && pgErr.ConstraintName == constraintMovementProductIDFK:
			return domain.ErrProductNotFound
		}
	}
	return fmt.Errorf("failed to %s: %w", operation, err)
}
