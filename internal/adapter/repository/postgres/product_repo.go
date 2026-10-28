package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/BreakDown-CS/StockFlowLite/internal/domain"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ProductRepository implements domain.ProductRepository using pgxpool.
type ProductRepository struct {
	pool *pgxpool.Pool
}

// NewProductRepository creates a new ProductRepository instance.
func NewProductRepository(pool *pgxpool.Pool) *ProductRepository {
	return &ProductRepository{pool: pool}
}

// Create inserts a new product record into PostgreSQL.
func (r *ProductRepository) Create(ctx context.Context, p *domain.Product) error {
	const query = `
		INSERT INTO products (
			id, sku, name, description, unit, current_stock, min_stock, is_active, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10
		);
	`

	_, err := r.pool.Exec(
		ctx,
		query,
		p.ID,
		p.SKU,
		p.Name,
		p.Description,
		p.Unit,
		p.CurrentStock,
		p.MinStock,
		p.IsActive,
		p.CreatedAt,
		p.UpdatedAt,
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return domain.ErrDuplicateSKU
		}
		return fmt.Errorf("failed to insert product: %w", err)
	}

	return nil
}

// GetByID retrieves a product by its unique identifier.
func (r *ProductRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.Product, error) {
	const query = `
		SELECT id, sku, name, description, unit, current_stock, min_stock, is_active, created_at, updated_at
		FROM products
		WHERE id = $1;
	`

	p := &domain.Product{}
	err := r.pool.QueryRow(ctx, query, id).Scan(
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
		return nil, fmt.Errorf("failed to query product by id: %w", err)
	}

	return p, nil
}

// GetBySKU retrieves a product by its unique SKU code.
func (r *ProductRepository) GetBySKU(ctx context.Context, sku string) (*domain.Product, error) {
	const query = `
		SELECT id, sku, name, description, unit, current_stock, min_stock, is_active, created_at, updated_at
		FROM products
		WHERE sku = $1;
	`

	p := &domain.Product{}
	err := r.pool.QueryRow(ctx, query, sku).Scan(
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
		return nil, fmt.Errorf("failed to query product by sku: %w", err)
	}

	return p, nil
}

// List queries products with search and pagination filters.
func (r *ProductRepository) List(ctx context.Context, filter domain.ListProductFilter) ([]domain.Product, int64, error) {
	where := []string{"1=1"}
	args := []interface{}{}
	argIdx := 1

	if strings.TrimSpace(filter.Search) != "" {
		pattern := "%" + strings.TrimSpace(filter.Search) + "%"
		where = append(where, fmt.Sprintf("(sku ILIKE $%d OR name ILIKE $%d)", argIdx, argIdx))
		args = append(args, pattern)
		argIdx++
	}

	if filter.IsActive != nil {
		where = append(where, fmt.Sprintf("is_active = $%d", argIdx))
		args = append(args, *filter.IsActive)
		argIdx++
	}

	whereClause := strings.Join(where, " AND ")

	// 1. Total count query
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM products WHERE %s", whereClause)
	var total int64
	if err := r.pool.QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("failed to count products: %w", err)
	}

	// 2. Data rows query with pagination
	dataQuery := fmt.Sprintf(`
		SELECT id, sku, name, description, unit, current_stock, min_stock, is_active, created_at, updated_at
		FROM products
		WHERE %s
		ORDER BY created_at DESC
		LIMIT $%d OFFSET $%d;
	`, whereClause, argIdx, argIdx+1)

	queryArgs := append(args, filter.Limit, filter.Offset)

	rows, err := r.pool.Query(ctx, dataQuery, queryArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to list products: %w", err)
	}
	defer rows.Close()

	products := make([]domain.Product, 0, filter.Limit)
	for rows.Next() {
		var p domain.Product
		if err := rows.Scan(
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
		); err != nil {
			return nil, 0, fmt.Errorf("failed to scan product row: %w", err)
		}
		products = append(products, p)
	}

	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("error during products iteration: %w", err)
	}

	return products, total, nil
}

// Update updates the editable fields of an existing product.
func (r *ProductRepository) Update(ctx context.Context, p *domain.Product) error {
	const query = `
		UPDATE products
		SET name = $2, description = $3, unit = $4, min_stock = $5, updated_at = $6
		WHERE id = $1;
	`

	cmdTag, err := r.pool.Exec(
		ctx,
		query,
		p.ID,
		p.Name,
		p.Description,
		p.Unit,
		p.MinStock,
		p.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("failed to update product: %w", err)
	}

	if cmdTag.RowsAffected() == 0 {
		return domain.ErrProductNotFound
	}

	return nil
}

// Deactivate marks a product as inactive.
func (r *ProductRepository) Deactivate(ctx context.Context, id uuid.UUID) error {
	const query = `
		UPDATE products
		SET is_active = false, updated_at = now()
		WHERE id = $1;
	`

	cmdTag, err := r.pool.Exec(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to deactivate product: %w", err)
	}

	if cmdTag.RowsAffected() == 0 {
		return domain.ErrProductNotFound
	}

	return nil
}
