package postgres

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/BreakDown-CS/StockFlowLite/internal/domain"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Constraint names from migrations 000003/000004 used for conflict mapping.
const (
	pgErrUniqueViolation          = "23505"
	constraintRequestNumberUnique = "stock_requests_request_number_key"
	constraintRequestItemUnique   = "uq_stock_request_items_req_prod"
	constraintRequestRequesterFK  = "stock_requests_requester_id_fkey"
	constraintRequestReviewerFK   = "stock_requests_reviewer_id_fkey"

	constraintIssuanceRequestUnique = "uq_stock_request_issuances_request"
	constraintIssuanceKeyUnique     = "uq_stock_request_issuances_key"

	movementReferenceStockRequest = "STOCK_REQUEST"
)

// querier is implemented by both *pgxpool.Pool and pgx.Tx.
type querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// StockRequestRepository implements domain.StockRequestRepository using pgxpool.
type StockRequestRepository struct {
	pool *pgxpool.Pool
}

// NewStockRequestRepository constructs a new StockRequestRepository.
func NewStockRequestRepository(pool *pgxpool.Pool) *StockRequestRepository {
	return &StockRequestRepository{pool: pool}
}

// Create persists a PENDING request and its items in one transaction. Stock is never touched.
func (r *StockRequestRepository) Create(ctx context.Context, req *domain.StockRequest) error {
	if req == nil {
		return domain.ErrEmptyRequestItems
	}
	// Defence in depth: the use case validated this already.
	if err := req.Validate(); err != nil {
		return err
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() {
		if rErr := tx.Rollback(ctx); rErr != nil && !errors.Is(rErr, pgx.ErrTxClosed) {
			logRollbackFailure("create stock request", req.ID, rErr)
		}
	}()

	// 1. Revalidate that every referenced product exists and is active inside the transaction.
	if err := ensureProductsRequestable(ctx, tx, req.Items); err != nil {
		return err
	}

	// 2. Insert the request header with the PENDING status.
	// The initial status is enforced here as well so no caller can create an already-reviewed request.
	req.Status = domain.StatusPending
	const insertRequest = `
		INSERT INTO stock_requests (id, request_number, requester_id, status, notes, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7);
	`
	if _, err := tx.Exec(ctx, insertRequest,
		req.ID,
		req.RequestNumber,
		req.RequesterID,
		string(domain.StatusPending),
		nullableText(req.Notes),
		req.CreatedAt,
		req.UpdatedAt,
	); err != nil {
		return mapStockRequestWriteError("insert stock request", err)
	}

	// 3. Insert all line items with parameterized multi-row VALUES.
	values := make([]string, 0, len(req.Items))
	args := make([]any, 0, len(req.Items)*4)
	for i, item := range req.Items {
		base := i * 4
		values = append(values, fmt.Sprintf("($%d, $%d, $%d, $%d)", base+1, base+2, base+3, base+4))
		args = append(args, item.ID, item.RequestID, item.ProductID, item.RequestedQuantity)
	}

	insertItems := fmt.Sprintf(`
		INSERT INTO stock_request_items (id, request_id, product_id, requested_quantity)
		VALUES %s;
	`, strings.Join(values, ", "))

	if _, err := tx.Exec(ctx, insertItems, args...); err != nil {
		return mapStockRequestWriteError("insert stock request items", err)
	}

	// Audit the creation atomically with the request row.
	createdEvent := auditEventFromContext(ctx, domain.AuditRequestCreated, domain.AuditEntityStockRequest, &req.ID)
	createdEvent.Reference = req.RequestNumber
	createdEvent.ToStatus = string(domain.StatusPending)
	createdEvent.Metadata = map[string]interface{}{"item_count": len(req.Items)}
	if err := insertAuditEvent(ctx, tx, createdEvent); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit stock request transaction: %w", err)
	}

	return nil
}

// GetByID returns a request with its items.
func (r *StockRequestRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.StockRequest, error) {
	const query = `
		SELECT id, request_number, requester_id, status, notes, reviewer_id, reviewed_at, rejection_reason, created_at, updated_at
		FROM stock_requests
		WHERE id = $1;
	`

	req, err := scanStockRequest(r.pool.QueryRow(ctx, query, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrStockRequestNotFound
		}
		return nil, fmt.Errorf("failed to query stock request: %w", err)
	}

	items, err := loadStockRequestItems(ctx, r.pool, req.ID)
	if err != nil {
		return nil, err
	}
	req.Items = items

	issuance, err := loadIssuance(ctx, r.pool, req.ID)
	if err != nil {
		return nil, err
	}
	req.Issuance = issuance

	return req, nil
}

// List returns a filtered, paginated page of requests with their items plus the total count.
func (r *StockRequestRepository) List(ctx context.Context, filter domain.ListStockRequestFilter) ([]domain.StockRequest, int64, error) {
	// Defend the query against invalid pagination even when the caller bypasses the use case.
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
	args := make([]any, 0, 1)
	argIdx := 1

	if filter.Status != nil {
		where = append(where, fmt.Sprintf("status = $%d", argIdx))
		args = append(args, string(*filter.Status))
		argIdx++
	}

	if filter.RequesterID != nil {
		where = append(where, fmt.Sprintf("requester_id = $%d", argIdx))
		args = append(args, *filter.RequesterID)
		argIdx++
	}

	whereClause := strings.Join(where, " AND ")

	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM stock_requests WHERE %s", whereClause)
	var total int64
	if err := r.pool.QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("failed to count stock requests: %w", err)
	}

	dataQuery := fmt.Sprintf(`
		SELECT id, request_number, requester_id, status, notes, reviewer_id, reviewed_at, rejection_reason, created_at, updated_at
		FROM stock_requests
		WHERE %s
		ORDER BY created_at DESC, id DESC
		LIMIT $%d OFFSET $%d;
	`, whereClause, argIdx, argIdx+1)

	rows, err := r.pool.Query(ctx, dataQuery, append(args, limit, offset)...)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to query stock requests: %w", err)
	}
	defer rows.Close()

	requests := make([]domain.StockRequest, 0, limit)
	for rows.Next() {
		req, err := scanStockRequest(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to scan stock request row: %w", err)
		}
		requests = append(requests, *req)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("error during stock requests iteration: %w", err)
	}

	if len(requests) == 0 {
		return requests, total, nil
	}

	ids := make([]uuid.UUID, 0, len(requests))
	for _, req := range requests {
		ids = append(ids, req.ID)
	}

	itemsByRequest, err := loadItemsForRequests(ctx, r.pool, ids)
	if err != nil {
		return nil, 0, err
	}
	for i := range requests {
		requests[i].Items = itemsByRequest[requests[i].ID]
	}

	return requests, total, nil
}

// Approve moves a PENDING request to APPROVED under a row lock. No stock is moved here;
// the schema models approval and physical issuance as separate states.
func (r *StockRequestRepository) Approve(ctx context.Context, id uuid.UUID, reviewerID uuid.UUID) (*domain.StockRequest, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() {
		if rErr := tx.Rollback(ctx); rErr != nil && !errors.Is(rErr, pgx.ErrTxClosed) {
			logRollbackFailure("approve stock request", id, rErr)
		}
	}()

	// 1. Lock the request row; the status check below is therefore race-free.
	req, err := loadStockRequestForUpdate(ctx, tx, id)
	if err != nil {
		return nil, err
	}

	// Four-eyes rule: a requester may never approve their own request.
	if req.RequesterID == reviewerID {
		recordAuditBestEffort(ctx, r.pool, selfApprovalEvent(ctx, req, "approve"))
		if rErr := tx.Rollback(ctx); rErr != nil && !errors.Is(rErr, pgx.ErrTxClosed) {
			logRollbackFailure("approve stock request", id, rErr)
		}
		return nil, domain.ErrSelfApproval
	}

	now := time.Now().UTC()
	if err := req.Approve(reviewerID, now); err != nil {
		return nil, err
	}

	// 2. Conditional update: only a request that is still PENDING can transition.
	const approveQuery = `
		UPDATE stock_requests
		SET status = $2, reviewer_id = $3, reviewed_at = $4, rejection_reason = NULL, updated_at = $5
		WHERE id = $1 AND status = $6;
	`
	tag, err := tx.Exec(ctx, approveQuery, id, string(domain.StatusApproved), reviewerID, now, now, string(domain.StatusPending))
	if err != nil {
		return nil, mapStockRequestWriteError("approve stock request", err)
	}
	if tag.RowsAffected() == 0 {
		return nil, domain.ErrCannotReview
	}

	items, err := loadStockRequestItems(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	req.Items = items

	approvedEvent := auditEventFromContext(ctx, domain.AuditRequestApproved, domain.AuditEntityStockRequest, &id)
	approvedEvent.Reference = req.RequestNumber
	approvedEvent.FromStatus = string(domain.StatusPending)
	approvedEvent.ToStatus = string(domain.StatusApproved)
	if err := insertAuditEvent(ctx, tx, approvedEvent); err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("failed to commit approval transaction: %w", err)
	}

	return req, nil
}

// Reject moves a PENDING request to REJECTED under a row lock. Stock and movement history are untouched.
func (r *StockRequestRepository) Reject(ctx context.Context, id uuid.UUID, reviewerID uuid.UUID, reason string) (*domain.StockRequest, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() {
		if rErr := tx.Rollback(ctx); rErr != nil && !errors.Is(rErr, pgx.ErrTxClosed) {
			logRollbackFailure("reject stock request", id, rErr)
		}
	}()

	req, err := loadStockRequestForUpdate(ctx, tx, id)
	if err != nil {
		return nil, err
	}

	// Four-eyes rule: a requester may never review (reject) their own request.
	if req.RequesterID == reviewerID {
		recordAuditBestEffort(ctx, r.pool, selfApprovalEvent(ctx, req, "reject"))
		if rErr := tx.Rollback(ctx); rErr != nil && !errors.Is(rErr, pgx.ErrTxClosed) {
			logRollbackFailure("reject stock request", id, rErr)
		}
		return nil, domain.ErrSelfApproval
	}

	now := time.Now().UTC()
	if err := req.Reject(reviewerID, reason, now); err != nil {
		return nil, err
	}

	const rejectQuery = `
		UPDATE stock_requests
		SET status = $2, reviewer_id = $3, reviewed_at = $4, rejection_reason = $5, updated_at = $6
		WHERE id = $1 AND status = $7;
	`
	tag, err := tx.Exec(ctx, rejectQuery,
		id,
		string(domain.StatusRejected),
		reviewerID,
		now,
		nullableText(req.RejectionReason),
		now,
		string(domain.StatusPending),
	)
	if err != nil {
		return nil, mapStockRequestWriteError("reject stock request", err)
	}
	if tag.RowsAffected() == 0 {
		return nil, domain.ErrCannotReview
	}

	items, err := loadStockRequestItems(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	req.Items = items

	rejectedEvent := auditEventFromContext(ctx, domain.AuditRequestRejected, domain.AuditEntityStockRequest, &id)
	rejectedEvent.Reference = req.RequestNumber
	rejectedEvent.FromStatus = string(domain.StatusPending)
	rejectedEvent.ToStatus = string(domain.StatusRejected)
	rejectedEvent.Reason = req.RejectionReason
	if err := insertAuditEvent(ctx, tx, rejectedEvent); err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("failed to commit rejection transaction: %w", err)
	}

	return req, nil
}

// Cancel moves a PENDING or APPROVED request to CANCELLED. Stock and the ledger stay untouched.
// The optional reason is stored on the request and recorded in the audit trail within the
// same transaction, so a cancellation can never commit without its audit entry.
func (r *StockRequestRepository) Cancel(ctx context.Context, id uuid.UUID, cancelledBy uuid.UUID, reason string) (*domain.StockRequest, error) {
	if cancelledBy == uuid.Nil {
		return nil, domain.ErrMissingReviewer
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() {
		if rErr := tx.Rollback(ctx); rErr != nil && !errors.Is(rErr, pgx.ErrTxClosed) {
			logRollbackFailure("cancel stock request", id, rErr)
		}
	}()

	// 1. Lock the request row so a concurrent approve/issue cannot interleave.
	req, err := loadStockRequestForUpdate(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	previousStatus := req.Status

	// 2. Domain guard: only PENDING or APPROVED requests may be cancelled.
	now := time.Now().UTC()
	if err := req.Cancel(now); err != nil {
		conflict := &domain.StateConflictError{Cause: err, CurrentStatus: req.Status}
		// Surface the fulfillment metadata when the request was already issued.
		if req.Status == domain.StatusIssued {
			issuance, iErr := loadIssuance(ctx, tx, id)
			if iErr != nil {
				return nil, iErr
			}
			if issuance != nil {
				issuanceID := issuance.ID
				issuedAt := issuance.CreatedAt
				conflict.IssuanceID = &issuanceID
				conflict.IssuedAt = &issuedAt
			}
		}
		return nil, conflict
	}

	trimmedReason := strings.TrimSpace(reason)

	// 3. Conditional update: the status may only change while it is still cancellable.
	const cancelQuery = `
		UPDATE stock_requests
		SET status = $2, cancellation_reason = $3, updated_at = $4
		WHERE id = $1 AND status IN ($5, $6);
	`
	tag, err := tx.Exec(ctx, cancelQuery,
		id,
		string(domain.StatusCancelled),
		nullableText(trimmedReason),
		now,
		string(domain.StatusPending),
		string(domain.StatusApproved),
	)
	if err != nil {
		return nil, mapStockRequestWriteError("cancel stock request", err)
	}
	if tag.RowsAffected() == 0 {
		return nil, &domain.StateConflictError{Cause: domain.ErrCannotCancel, CurrentStatus: previousStatus}
	}

	items, err := loadStockRequestItems(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	req.Items = items

	// 4. Audit the cancellation atomically with the state change.
	event := auditEventFromContext(ctx, domain.AuditRequestCancelled, domain.AuditEntityStockRequest, &id)
	event.Reference = req.RequestNumber
	event.FromStatus = string(previousStatus)
	event.ToStatus = string(domain.StatusCancelled)
	event.Reason = trimmedReason
	if err := insertAuditEvent(ctx, tx, event); err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("failed to commit cancellation transaction: %w", err)
	}

	return req, nil
}

// Issue deducts every requested item and writes one ISSUE movement per item inside a single
// transaction, then marks the request ISSUED. Any failure rolls back balances, movements and status.
// An optional idempotencyKey makes a retried request replay the original result.
func (r *StockRequestRepository) Issue(ctx context.Context, id uuid.UUID, actorID uuid.UUID, idempotencyKey string) (*domain.StockIssueResult, error) {
	if actorID == uuid.Nil {
		return nil, domain.ErrMissingReviewer
	}
	key := strings.TrimSpace(idempotencyKey)

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() {
		if rErr := tx.Rollback(ctx); rErr != nil && !errors.Is(rErr, pgx.ErrTxClosed) {
			logRollbackFailure("issue stock request", id, rErr)
		}
	}()

	// 1. Lock the request row (serializes approve/reject/issue/cancel for this request).
	req, err := loadStockRequestForUpdate(ctx, tx, id)
	if err != nil {
		return nil, err
	}

	issuance, err := loadIssuance(ctx, tx, id)
	if err != nil {
		return nil, err
	}

	// 2. Already fulfilled: replay for the same key, otherwise report a state conflict.
	if req.Status == domain.StatusIssued {
		if key != "" && issuance != nil && issuance.IdempotencyKey == key {
			return replayIssuance(ctx, tx, req, issuance)
		}
		return nil, issuanceConflict(req, issuance)
	}

	// 3. Refuse an idempotency key that is already bound to another request.
	// The four-eyes rule also forbids a requester from issuing their own request.
	if req.RequesterID == actorID {
		recordAuditBestEffort(ctx, r.pool, selfApprovalEvent(ctx, req, "issue"))
		if rErr := tx.Rollback(ctx); rErr != nil && !errors.Is(rErr, pgx.ErrTxClosed) {
			logRollbackFailure("issue stock request", id, rErr)
		}
		return nil, domain.ErrSelfApproval
	}

	if key != "" {
		usedByOther, err := isIdempotencyKeyUsedElsewhere(ctx, tx, key, id)
		if err != nil {
			return nil, err
		}
		if usedByOther {
			return nil, domain.ErrIdempotencyKeyReused
		}
	}

	items, err := loadStockRequestItems(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, domain.ErrEmptyRequestItems
	}
	req.Items = items

	now := time.Now().UTC()
	if err := req.MarkIssued(now); err != nil { // domain guard: APPROVED only
		return nil, &domain.StateConflictError{Cause: err, CurrentStatus: req.Status}
	}

	// 4. Lock every affected product row in ascending id order to reduce deadlock risk.
	productIDs := make([]uuid.UUID, 0, len(items))
	for _, item := range items {
		productIDs = append(productIDs, item.ProductID)
	}
	sort.Slice(productIDs, func(i, j int) bool {
		return productIDs[i].String() < productIDs[j].String()
	})

	products, err := lockProductsForIssue(ctx, tx, productIDs)
	if err != nil {
		return nil, err
	}

	// 5. Verify every item before applying any change and collect all shortages at once,
	// so the client learns exactly what is missing instead of one item at a time.
	shortages := make([]domain.StockShortage, 0)
	for _, item := range items {
		product, ok := products[item.ProductID]
		if !ok {
			return nil, domain.ErrProductNotFound
		}
		if !product.IsActive {
			return nil, domain.ErrProductInactive
		}
		if item.RequestedQuantity <= 0 {
			return nil, domain.ErrInvalidQuantity
		}
		if product.CurrentStock < item.RequestedQuantity {
			shortages = append(shortages, domain.StockShortage{
				ProductID: item.ProductID,
				Requested: item.RequestedQuantity,
				Available: product.CurrentStock,
				Shortfall: item.RequestedQuantity - product.CurrentStock,
			})
		}
	}
	if len(shortages) > 0 {
		return nil, &domain.InsufficientStockError{Items: shortages}
	}

	// 6. Apply the deductions and write one ISSUE movement per item.
	movements := make([]domain.StockMovement, 0, len(items))
	totalQuantity := 0
	for _, item := range items {
		product := products[item.ProductID]

		// Re-apply the domain rule so the "never negative" invariant lives in one place.
		if err := product.DeductStock(item.RequestedQuantity); err != nil {
			return nil, err
		}

		const deductQuery = `
			UPDATE products
			SET current_stock = current_stock - $2, updated_at = $3
			WHERE id = $1 AND current_stock - $2 >= 0
			RETURNING current_stock, updated_at;
		`
		err := tx.QueryRow(ctx, deductQuery, product.ID, item.RequestedQuantity, now).
			Scan(&product.CurrentStock, &product.UpdatedAt)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, domain.ErrInsufficientStock
			}
			return nil, mapWriteError("deduct product stock", err)
		}

		movement, err := domain.NewStockMovement(
			item.ProductID,
			domain.MovementIssue,
			item.RequestedQuantity,
			movementReferenceStockRequest,
			req.RequestNumber,
			&actorID,
			"",
		)
		if err != nil {
			return nil, err
		}

		err = tx.QueryRow(ctx, insertStockMovementSQL,
			movement.ProductID,
			string(movement.MovementType),
			movement.Quantity,
			movement.ReferenceType,
			nullableText(movement.ReferenceID),
			movement.ActorID,
			nullableText(movement.Notes),
		).Scan(&movement.ID, &movement.CreatedAt)
		if err != nil {
			return nil, mapWriteError("insert stock movement", err)
		}

		movements = append(movements, *movement)
		totalQuantity += item.RequestedQuantity
	}

	// 7. Persist the single fulfillment event. UNIQUE(request_id) is the database-level
	// guarantee that a request can never be fulfilled twice.
	const insertIssuance = `
		INSERT INTO stock_request_issuances (request_id, idempotency_key, actor_id, item_count, total_quantity, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, created_at;
	`
	issuanceRecord := &domain.StockRequestIssuance{
		RequestID:      id,
		IdempotencyKey: key,
		ActorID:        &actorID,
		ItemCount:      len(items),
		TotalQuantity:  totalQuantity,
	}
	err = tx.QueryRow(ctx, insertIssuance,
		id,
		nullableText(key),
		actorID,
		len(items),
		totalQuantity,
		now,
	).Scan(&issuanceRecord.ID, &issuanceRecord.CreatedAt)
	if err != nil {
		return nil, mapIssuanceWriteError(err)
	}

	// 8. Mark the request ISSUED with a conditional update.
	const issueQuery = `
		UPDATE stock_requests
		SET status = $2, updated_at = $3
		WHERE id = $1 AND status = $4;
	`
	tag, err := tx.Exec(ctx, issueQuery, id, string(domain.StatusIssued), now, string(domain.StatusApproved))
	if err != nil {
		return nil, mapStockRequestWriteError("mark stock request issued", err)
	}
	if tag.RowsAffected() == 0 {
		return nil, &domain.StateConflictError{Cause: domain.ErrCannotIssue, CurrentStatus: domain.StatusApproved}
	}
	req.Issuance = issuanceRecord

	issuedEvent := auditEventFromContext(ctx, domain.AuditRequestIssued, domain.AuditEntityStockRequest, &id)
	issuedEvent.Reference = req.RequestNumber
	issuedEvent.FromStatus = string(domain.StatusApproved)
	issuedEvent.ToStatus = string(domain.StatusIssued)
	issuedEvent.Metadata = map[string]interface{}{
		"item_count":     len(items),
		"total_quantity": totalQuantity,
		"issuance_id":    issuanceRecord.ID.String(),
	}
	if err := insertAuditEvent(ctx, tx, issuedEvent); err != nil {
		return nil, err
	}

	// 9. Commit only when every item was deducted and recorded.
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("failed to commit issuance transaction: %w", err)
	}

	return &domain.StockIssueResult{Request: req, Movements: movements, Issuance: issuanceRecord}, nil
}

// ensureProductsRequestable verifies that every referenced product exists and is active,
// and rejects duplicate product IDs. It runs inside the caller's transaction.
func ensureProductsRequestable(ctx context.Context, tx pgx.Tx, items []domain.StockRequestItem) error {
	ids := make([]uuid.UUID, 0, len(items))
	seen := make(map[uuid.UUID]struct{}, len(items))
	for _, item := range items {
		if _, exists := seen[item.ProductID]; exists {
			return domain.ErrDuplicateItem
		}
		seen[item.ProductID] = struct{}{}
		ids = append(ids, item.ProductID)
	}

	placeholders, args := uuidPlaceholders(1, ids)
	query := fmt.Sprintf("SELECT id, is_active FROM products WHERE id IN (%s)", placeholders)

	rows, err := tx.Query(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("failed to validate request products: %w", err)
	}
	defer rows.Close()

	activeByID := make(map[uuid.UUID]bool, len(ids))
	for rows.Next() {
		var id uuid.UUID
		var isActive bool
		if err := rows.Scan(&id, &isActive); err != nil {
			return fmt.Errorf("failed to scan product state: %w", err)
		}
		activeByID[id] = isActive
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("error during product validation: %w", err)
	}

	for _, id := range ids {
		active, ok := activeByID[id]
		if !ok {
			return domain.ErrProductNotFound
		}
		if !active {
			return domain.ErrProductInactive
		}
	}

	return nil
}

// lockProductsForIssue locks the affected product rows with FOR UPDATE in a deterministic
// (ascending id) order so concurrent issuances cannot deadlock or overdraw inventory.
func lockProductsForIssue(ctx context.Context, tx pgx.Tx, ids []uuid.UUID) (map[uuid.UUID]*domain.Product, error) {
	placeholders, args := uuidPlaceholders(1, ids)
	query := fmt.Sprintf(`
		SELECT id, sku, name, COALESCE(description, ''), unit, current_stock, min_stock, is_active, created_at, updated_at
		FROM products
		WHERE id IN (%s)
		ORDER BY id
		FOR UPDATE;
	`, placeholders)

	rows, err := tx.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to lock request products: %w", err)
	}
	defer rows.Close()

	products := make(map[uuid.UUID]*domain.Product, len(ids))
	for rows.Next() {
		p := &domain.Product{}
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
			return nil, fmt.Errorf("failed to scan locked product row: %w", err)
		}
		products[p.ID] = p
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error during product locking: %w", err)
	}

	return products, nil
}

// loadStockRequestForUpdate loads a single request while holding a row-level lock.
func loadStockRequestForUpdate(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*domain.StockRequest, error) {
	const query = `
		SELECT id, request_number, requester_id, status, notes, reviewer_id, reviewed_at, rejection_reason, created_at, updated_at
		FROM stock_requests
		WHERE id = $1
		FOR UPDATE;
	`

	req, err := scanStockRequest(tx.QueryRow(ctx, query, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrStockRequestNotFound
		}
		return nil, fmt.Errorf("failed to lock stock request: %w", err)
	}

	return req, nil
}

// rowScanner is implemented by pgx.Row and pgx.Rows.
type rowScanner interface {
	Scan(dest ...any) error
}

// scanStockRequest maps a stock_requests row into the domain entity.
func scanStockRequest(row rowScanner) (*domain.StockRequest, error) {
	var (
		req            domain.StockRequest
		status         string
		notes          *string
		reviewerID     *uuid.UUID
		reviewedAt     *time.Time
		rejectionCause *string
	)

	err := row.Scan(
		&req.ID,
		&req.RequestNumber,
		&req.RequesterID,
		&status,
		&notes,
		&reviewerID,
		&reviewedAt,
		&rejectionCause,
		&req.CreatedAt,
		&req.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	req.Status = domain.RequestStatus(status)
	if notes != nil {
		req.Notes = *notes
	}
	req.ReviewerID = reviewerID
	req.ReviewedAt = reviewedAt
	if rejectionCause != nil {
		req.RejectionReason = *rejectionCause
	}

	return &req, nil
}

// loadStockRequestItems loads the line items of a single request.
func loadStockRequestItems(ctx context.Context, q querier, requestID uuid.UUID) ([]domain.StockRequestItem, error) {
	byRequest, err := loadItemsForRequests(ctx, q, []uuid.UUID{requestID})
	if err != nil {
		return nil, err
	}

	items := byRequest[requestID]
	if items == nil {
		items = []domain.StockRequestItem{}
	}
	return items, nil
}

// loadItemsForRequests loads and groups the line items of several requests with one query.
func loadItemsForRequests(ctx context.Context, q querier, requestIDs []uuid.UUID) (map[uuid.UUID][]domain.StockRequestItem, error) {
	grouped := make(map[uuid.UUID][]domain.StockRequestItem, len(requestIDs))
	if len(requestIDs) == 0 {
		return grouped, nil
	}

	placeholders, args := uuidPlaceholders(1, requestIDs)
	query := fmt.Sprintf(`
		SELECT id, request_id, product_id, requested_quantity
		FROM stock_request_items
		WHERE request_id IN (%s)
		ORDER BY request_id, product_id;
	`, placeholders)

	rows, err := q.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query stock request items: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var item domain.StockRequestItem
		if err := rows.Scan(&item.ID, &item.RequestID, &item.ProductID, &item.RequestedQuantity); err != nil {
			return nil, fmt.Errorf("failed to scan stock request item: %w", err)
		}
		grouped[item.RequestID] = append(grouped[item.RequestID], item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error during stock request items iteration: %w", err)
	}

	return grouped, nil
}

// uuidPlaceholders builds "$n, $n+1, ..." placeholders plus the matching argument slice.
func uuidPlaceholders(start int, ids []uuid.UUID) (string, []any) {
	placeholders := make([]string, 0, len(ids))
	args := make([]any, 0, len(ids))
	for i, id := range ids {
		placeholders = append(placeholders, fmt.Sprintf("$%d", start+i))
		args = append(args, id)
	}
	return strings.Join(placeholders, ", "), args
}

// nullableText converts an empty string into a SQL NULL for nullable text columns.
func nullableText(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

// mapStockRequestWriteError translates only known request-workflow conflicts into domain errors.
func mapStockRequestWriteError(operation string, err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch {
		case pgErr.Code == pgErrUniqueViolation && pgErr.ConstraintName == constraintRequestNumberUnique:
			return domain.ErrDuplicateRequestNumber
		case pgErr.Code == pgErrUniqueViolation && pgErr.ConstraintName == constraintRequestItemUnique:
			return domain.ErrDuplicateItem
		case pgErr.Code == pgErrForeignKeyViolation &&
			(pgErr.ConstraintName == constraintRequestRequesterFK || pgErr.ConstraintName == constraintRequestReviewerFK):
			// Attribution identities are seeded by migration 000005; a missing row is an
			// environment provisioning problem, not a client error.
			return fmt.Errorf("failed to %s: attribution identity is not provisioned: %w", operation, err)
		}
	}
	return mapWriteError(operation, err)
}

// logRollbackFailure reports a failed rollback without leaking SQL or credentials.
func logRollbackFailure(operation string, requestID uuid.UUID, err error) {
	slog.Error("Failed to roll back stock request transaction",
		"operation", operation,
		"request_id", requestID.String(),
		"error", err,
	)
}

// loadIssuance returns the fulfillment record of a request, or nil when it has not been issued.
func loadIssuance(ctx context.Context, q querier, requestID uuid.UUID) (*domain.StockRequestIssuance, error) {
	const query = `
		SELECT id, request_id, COALESCE(idempotency_key, ''), actor_id, item_count, total_quantity, created_at
		FROM stock_request_issuances
		WHERE request_id = $1;
	`

	var issuance domain.StockRequestIssuance
	err := q.QueryRow(ctx, query, requestID).Scan(
		&issuance.ID,
		&issuance.RequestID,
		&issuance.IdempotencyKey,
		&issuance.ActorID,
		&issuance.ItemCount,
		&issuance.TotalQuantity,
		&issuance.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to load stock request issuance: %w", err)
	}

	return &issuance, nil
}

// loadMovementsByReference reads the immutable ledger rows written for a reference
// (used to replay the original issuance result without re-reading business state).
func loadMovementsByReference(ctx context.Context, q querier, referenceType, referenceID string) ([]domain.StockMovement, error) {
	const query = `
		SELECT id, product_id, movement_type, quantity, reference_type,
		       COALESCE(reference_id, ''), actor_id, COALESCE(notes, ''), created_at
		FROM stock_movements
		WHERE reference_type = $1 AND reference_id = $2
		ORDER BY id;
	`

	rows, err := q.Query(ctx, query, referenceType, referenceID)
	if err != nil {
		return nil, fmt.Errorf("failed to query movements by reference: %w", err)
	}
	defer rows.Close()

	movements := make([]domain.StockMovement, 0, 4)
	for rows.Next() {
		var movement domain.StockMovement
		var movementType string
		if err := rows.Scan(
			&movement.ID,
			&movement.ProductID,
			&movementType,
			&movement.Quantity,
			&movement.ReferenceType,
			&movement.ReferenceID,
			&movement.ActorID,
			&movement.Notes,
			&movement.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan movement row: %w", err)
		}
		movement.MovementType = domain.MovementType(movementType)
		movements = append(movements, movement)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error during movements iteration: %w", err)
	}

	return movements, nil
}

// replayIssuance returns the original issuance outcome for a retried request.
// It performs no writes: the transaction is committed (read-only) and closed.
func replayIssuance(
	ctx context.Context,
	tx pgx.Tx,
	req *domain.StockRequest,
	issuance *domain.StockRequestIssuance,
) (*domain.StockIssueResult, error) {
	items, err := loadStockRequestItems(ctx, tx, req.ID)
	if err != nil {
		return nil, err
	}
	req.Items = items

	movements, err := loadMovementsByReference(ctx, tx, movementReferenceStockRequest, req.RequestNumber)
	if err != nil {
		return nil, err
	}
	req.Issuance = issuance

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("failed to commit issuance replay transaction: %w", err)
	}

	return &domain.StockIssueResult{
		Request:          req,
		Movements:        movements,
		Issuance:         issuance,
		IdempotentReplay: true,
	}, nil
}

// issuanceConflict builds a state-conflict error carrying the authoritative fulfillment metadata.
func issuanceConflict(req *domain.StockRequest, issuance *domain.StockRequestIssuance) error {
	conflict := &domain.StateConflictError{
		Cause:         domain.ErrCannotIssue,
		CurrentStatus: req.Status,
	}
	if issuance != nil {
		id := issuance.ID
		issuedAt := issuance.CreatedAt
		conflict.IssuanceID = &id
		conflict.IssuedAt = &issuedAt
	}
	return conflict
}

// isIdempotencyKeyUsedElsewhere reports whether the key is bound to a different request.
func isIdempotencyKeyUsedElsewhere(ctx context.Context, q querier, key string, requestID uuid.UUID) (bool, error) {
	const query = `SELECT request_id FROM stock_request_issuances WHERE idempotency_key = $1;`

	var existing uuid.UUID
	err := q.QueryRow(ctx, query, key).Scan(&existing)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("failed to check idempotency key usage: %w", err)
	}

	return existing != requestID, nil
}

// mapIssuanceWriteError translates the issuance unique constraints into domain conflicts.
func mapIssuanceWriteError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch {
		case pgErr.Code == pgErrUniqueViolation && pgErr.ConstraintName == constraintIssuanceRequestUnique:
			// Defence in depth: the row lock should already have serialized this.
			return &domain.StateConflictError{Cause: domain.ErrCannotIssue, CurrentStatus: domain.StatusIssued}
		case pgErr.Code == pgErrUniqueViolation && pgErr.ConstraintName == constraintIssuanceKeyUnique:
			return domain.ErrIdempotencyKeyReused
		}
	}
	return mapWriteError("insert stock request issuance", err)
}
