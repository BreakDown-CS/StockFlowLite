package postgres

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/BreakDown-CS/StockFlowLite/internal/domain"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

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
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	// 1. Lock product row exclusively using row-level locking (FOR UPDATE)
	const selectProductQuery = `
		SELECT id, sku, name, description, unit, current_stock, min_stock, is_active, created_at, updated_at
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
		return nil, fmt.Errorf("failed to update product stock: %w", err)
	}

	// 5. Insert stock movement record
	const insertMovementQuery = `
		INSERT INTO stock_movements (
			product_id, movement_type, quantity, reference_type, reference_id, actor_id, notes, created_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, now()
		)
		RETURNING id, created_at;
	`

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
		insertMovementQuery,
		productID,
		string(domain.MovementReceipt),
		quantity,
		refType,
		refIDVal,
		actorID,
		notesVal,
	).Scan(&movement.ID, &movement.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("failed to insert stock movement: %w", err)
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

	queryArgs := append(args, filter.Limit, filter.Offset)

	rows, err := r.pool.Query(ctx, dataQuery, queryArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to query stock movements: %w", err)
	}
	defer rows.Close()

	movements := make([]domain.StockMovement, 0, filter.Limit)
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
