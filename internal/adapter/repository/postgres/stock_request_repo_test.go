package postgres_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/BreakDown-CS/StockFlowLite/internal/adapter/repository/postgres"
	"github.com/BreakDown-CS/StockFlowLite/internal/domain"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test actors: rows seeded by migration 000005. Phase 7 disables them for login, but they
// remain valid foreign keys for attribution columns.
var (
	testRequesterID = uuid.MustParse("00000000-0000-4000-8000-000000000001")
	testReviewerID  = uuid.MustParse("00000000-0000-4000-8000-000000000002")
)

// requireDevIdentities skips request tests when migration 000005 has not been applied,
// because stock_requests.requester_id is a NOT NULL foreign key to users(id).
func requireDevIdentities(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()

	var count int
	err := pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM users WHERE id IN ($1, $2)`,
		testRequesterID,
		testReviewerID,
	).Scan(&count)
	if err != nil {
		t.Skipf("skipping stock request integration test: cannot verify attribution identities: %v", err)
	}
	if count != 2 {
		t.Skip("skipping stock request integration test: development identities are missing; apply migrations with 'go run ./cmd/migrate -action up' (migration 000005)")
	}
}

// testRequestNumber builds a unique, traceable request number for the test run.
func testRequestNumber() string {
	return "TST-" + strings.ToUpper(strings.ReplaceAll(uuid.NewString(), "-", "")[:12])
}

type requestItemSpec struct {
	ProductID uuid.UUID
	Quantity  int
}

// createStockRequest persists a PENDING request through the repository and registers cleanup.
func createStockRequest(t *testing.T, repo *postgres.StockRequestRepository, pool *pgxpool.Pool, specs []requestItemSpec) *domain.StockRequest {
	t.Helper()

	items := make([]domain.StockRequestItem, 0, len(specs))
	for _, spec := range specs {
		items = append(items, domain.StockRequestItem{ProductID: spec.ProductID, RequestedQuantity: spec.Quantity})
	}

	req, err := domain.NewStockRequest(testRequestNumber(), testRequesterID, "integration test", items)
	require.NoError(t, err)
	require.NoError(t, repo.Create(context.Background(), req))

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, _ = pool.Exec(ctx, `DELETE FROM stock_movements WHERE reference_type = 'STOCK_REQUEST' AND reference_id = $1`, req.RequestNumber)
		_, _ = pool.Exec(ctx, `DELETE FROM stock_request_issuances WHERE request_id = $1`, req.ID)
		_, _ = pool.Exec(ctx, `DELETE FROM audit_events WHERE entity_id = $1`, req.ID)
		_, _ = pool.Exec(ctx, `DELETE FROM stock_request_items WHERE request_id = $1`, req.ID)
		_, _ = pool.Exec(ctx, `DELETE FROM stock_requests WHERE id = $1`, req.ID)
	})

	return req
}

// countRequestMovements counts ledger rows written for a request number.
func countRequestMovements(t *testing.T, pool *pgxpool.Pool, requestNumber string) int {
	t.Helper()

	var count int
	require.NoError(t, pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM stock_movements WHERE reference_type = 'STOCK_REQUEST' AND reference_id = $1`,
		requestNumber).Scan(&count))
	return count
}

// countIssuances counts fulfillment records written for a request.
func countIssuances(t *testing.T, pool *pgxpool.Pool, requestID uuid.UUID) int {
	t.Helper()

	var count int
	require.NoError(t, pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM stock_request_issuances WHERE request_id = $1`, requestID).Scan(&count))
	return count
}

func TestStockRequestRepository_Create_PersistsPendingWithoutTouchingStock(t *testing.T) {
	pool := newTestPool(t)
	requireDevIdentities(t, pool)
	repo := postgres.NewStockRequestRepository(pool)

	firstProduct := seedProduct(t, pool, true, 10)
	secondProduct := seedProduct(t, pool, true, 20)

	req := createStockRequest(t, repo, pool, []requestItemSpec{
		{ProductID: firstProduct, Quantity: 4},
		{ProductID: secondProduct, Quantity: 6},
	})

	assert.Equal(t, domain.StatusPending, req.Status)
	assert.Equal(t, testRequesterID, req.RequesterID)
	assert.True(t, strings.HasPrefix(req.RequestNumber, "TST-"))

	// Creation must not modify balances or the ledger.
	assert.Equal(t, 10, readStock(t, pool, firstProduct))
	assert.Equal(t, 20, readStock(t, pool, secondProduct))
	assert.Equal(t, 0, countMovements(t, pool, firstProduct))
	assert.Equal(t, 0, countMovements(t, pool, secondProduct))

	stored, err := repo.GetByID(context.Background(), req.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.StatusPending, stored.Status)
	assert.Len(t, stored.Items, 2)
	assert.Nil(t, stored.ReviewerID)
	assert.Nil(t, stored.ReviewedAt)

	totalRequested := 0
	for _, item := range stored.Items {
		totalRequested += item.RequestedQuantity
	}
	assert.Equal(t, 10, totalRequested)
}

func TestStockRequestRepository_Create_ProductValidation(t *testing.T) {
	pool := newTestPool(t)
	requireDevIdentities(t, pool)
	repo := postgres.NewStockRequestRepository(pool)
	ctx := context.Background()

	t.Run("missing product", func(t *testing.T) {
		req, err := domain.NewStockRequest(testRequestNumber(), testRequesterID, "",
			[]domain.StockRequestItem{{ProductID: uuid.New(), RequestedQuantity: 2}})
		require.NoError(t, err)

		err = repo.Create(ctx, req)
		require.ErrorIs(t, err, domain.ErrProductNotFound)
	})

	t.Run("inactive product", func(t *testing.T) {
		inactiveProduct := seedProduct(t, pool, false, 5)
		req, err := domain.NewStockRequest(testRequestNumber(), testRequesterID, "",
			[]domain.StockRequestItem{{ProductID: inactiveProduct, RequestedQuantity: 2}})
		require.NoError(t, err)

		err = repo.Create(ctx, req)
		require.ErrorIs(t, err, domain.ErrProductInactive)
	})

	t.Run("duplicate product", func(t *testing.T) {
		product := seedProduct(t, pool, true, 5)

		// Bypass NewStockRequest so the repository-level duplicate guard is exercised.
		req := &domain.StockRequest{
			ID:            uuid.New(),
			RequestNumber: testRequestNumber(),
			RequesterID:   testRequesterID,
			Status:        domain.StatusPending,
			CreatedAt:     time.Now().UTC(),
			UpdatedAt:     time.Now().UTC(),
			Items: []domain.StockRequestItem{
				{ID: uuid.New(), ProductID: product, RequestedQuantity: 1},
				{ID: uuid.New(), ProductID: product, RequestedQuantity: 2},
			},
		}

		err := repo.Create(ctx, req)
		require.ErrorIs(t, err, domain.ErrDuplicateItem)
	})
}

func TestStockRequestRepository_Approve_RecordsReviewWithoutStockChange(t *testing.T) {
	pool := newTestPool(t)
	requireDevIdentities(t, pool)
	repo := postgres.NewStockRequestRepository(pool)
	ctx := context.Background()

	product := seedProduct(t, pool, true, 15)
	req := createStockRequest(t, repo, pool, []requestItemSpec{{ProductID: product, Quantity: 5}})

	approved, err := repo.Approve(ctx, req.ID, testReviewerID)
	require.NoError(t, err)
	require.NotNil(t, approved)
	assert.Equal(t, domain.StatusApproved, approved.Status)
	require.NotNil(t, approved.ReviewerID)
	assert.Equal(t, testReviewerID, *approved.ReviewerID)
	require.NotNil(t, approved.ReviewedAt)
	assert.Len(t, approved.Items, 1)

	// Approval alone never changes inventory or the ledger.
	assert.Equal(t, 15, readStock(t, pool, product))
	assert.Equal(t, 0, countMovements(t, pool, product))

	// Re-approving an approved request is a conflict.
	_, err = repo.Approve(ctx, req.ID, testReviewerID)
	require.ErrorIs(t, err, domain.ErrCannotReview)

	// Unknown requests are reported as not found.
	_, err = repo.Approve(ctx, uuid.New(), testReviewerID)
	require.ErrorIs(t, err, domain.ErrStockRequestNotFound)
}

func TestStockRequestRepository_Reject_RecordsReasonWithoutStockChange(t *testing.T) {
	pool := newTestPool(t)
	requireDevIdentities(t, pool)
	repo := postgres.NewStockRequestRepository(pool)
	ctx := context.Background()

	product := seedProduct(t, pool, true, 9)
	req := createStockRequest(t, repo, pool, []requestItemSpec{{ProductID: product, Quantity: 3}})

	rejected, err := repo.Reject(ctx, req.ID, testReviewerID, "Out of budget")
	require.NoError(t, err)
	assert.Equal(t, domain.StatusRejected, rejected.Status)
	assert.Equal(t, "Out of budget", rejected.RejectionReason)
	require.NotNil(t, rejected.ReviewerID)
	assert.Equal(t, testReviewerID, *rejected.ReviewerID)
	require.NotNil(t, rejected.ReviewedAt)

	// Rejection must not alter stock or write movements.
	assert.Equal(t, 9, readStock(t, pool, product))
	assert.Equal(t, 0, countMovements(t, pool, product))

	// Repeated rejection is a conflict, and so is rejecting an approved request.
	_, err = repo.Reject(ctx, req.ID, testReviewerID, "again")
	require.ErrorIs(t, err, domain.ErrCannotReview)

	approvedReq := createStockRequest(t, repo, pool, []requestItemSpec{{ProductID: product, Quantity: 1}})
	_, err = repo.Approve(ctx, approvedReq.ID, testReviewerID)
	require.NoError(t, err)
	_, err = repo.Reject(ctx, approvedReq.ID, testReviewerID, "too late")
	require.ErrorIs(t, err, domain.ErrCannotReview)
}

func TestStockRequestRepository_Issue_DeductsStockAndWritesIssueMovements(t *testing.T) {
	pool := newTestPool(t)
	requireDevIdentities(t, pool)
	repo := postgres.NewStockRequestRepository(pool)
	ctx := context.Background()

	firstProduct := seedProduct(t, pool, true, 10)
	secondProduct := seedProduct(t, pool, true, 20)

	req := createStockRequest(t, repo, pool, []requestItemSpec{
		{ProductID: firstProduct, Quantity: 4},
		{ProductID: secondProduct, Quantity: 5},
	})
	_, err := repo.Approve(ctx, req.ID, testReviewerID)
	require.NoError(t, err)

	result, err := repo.Issue(ctx, req.ID, testReviewerID, "")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, domain.StatusIssued, result.Request.Status)
	require.Len(t, result.Movements, 2)

	// Balances reflect the deduction, and the ledger matches.
	assert.Equal(t, 6, readStock(t, pool, firstProduct))
	assert.Equal(t, 15, readStock(t, pool, secondProduct))
	assert.Equal(t, 1, countMovements(t, pool, firstProduct))
	assert.Equal(t, 1, countMovements(t, pool, secondProduct))
	assert.Equal(t, 2, countRequestMovements(t, pool, req.RequestNumber))

	quantities := map[uuid.UUID]int{}
	for _, movement := range result.Movements {
		assert.Equal(t, domain.MovementIssue, movement.MovementType)
		assert.Equal(t, "STOCK_REQUEST", movement.ReferenceType)
		assert.Equal(t, req.RequestNumber, movement.ReferenceID)
		require.NotNil(t, movement.ActorID)
		assert.Equal(t, testReviewerID, *movement.ActorID)
		assert.NotZero(t, movement.ID)
		quantities[movement.ProductID] += movement.NetQuantityChange()
	}
	assert.Equal(t, -4, quantities[firstProduct])
	assert.Equal(t, -5, quantities[secondProduct])

	// Issuing twice must never issue stock twice.
	_, err = repo.Issue(ctx, req.ID, testReviewerID, "")
	require.ErrorIs(t, err, domain.ErrCannotIssue)
	assert.Equal(t, 6, readStock(t, pool, firstProduct))
	assert.Equal(t, 15, readStock(t, pool, secondProduct))
	assert.Equal(t, 2, countRequestMovements(t, pool, req.RequestNumber))
}

func TestStockRequestRepository_Issue_RequiresApproval(t *testing.T) {
	pool := newTestPool(t)
	requireDevIdentities(t, pool)
	repo := postgres.NewStockRequestRepository(pool)
	ctx := context.Background()

	product := seedProduct(t, pool, true, 8)
	req := createStockRequest(t, repo, pool, []requestItemSpec{{ProductID: product, Quantity: 2}})

	_, err := repo.Issue(ctx, req.ID, testReviewerID, "")
	require.ErrorIs(t, err, domain.ErrCannotIssue)

	assert.Equal(t, 8, readStock(t, pool, product))
	assert.Equal(t, 0, countMovements(t, pool, product))
	assert.Equal(t, 0, countRequestMovements(t, pool, req.RequestNumber))

	stored, err := repo.GetByID(ctx, req.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.StatusPending, stored.Status)
}

func TestStockRequestRepository_Issue_InsufficientStockRollsBackEveryItem(t *testing.T) {
	pool := newTestPool(t)
	requireDevIdentities(t, pool)
	repo := postgres.NewStockRequestRepository(pool)
	ctx := context.Background()

	// The first item is satisfiable, the second is not; nothing may be issued at all.
	plentifulProduct := seedProduct(t, pool, true, 50)
	scarceProduct := seedProduct(t, pool, true, 3)

	req := createStockRequest(t, repo, pool, []requestItemSpec{
		{ProductID: plentifulProduct, Quantity: 10},
		{ProductID: scarceProduct, Quantity: 5},
	})
	_, err := repo.Approve(ctx, req.ID, testReviewerID)
	require.NoError(t, err)

	_, err = repo.Issue(ctx, req.ID, testReviewerID, "")
	require.ErrorIs(t, err, domain.ErrInsufficientStock)

	// Both balances stay untouched and no movements are recorded.
	assert.Equal(t, 50, readStock(t, pool, plentifulProduct))
	assert.Equal(t, 3, readStock(t, pool, scarceProduct))
	assert.Equal(t, 0, countMovements(t, pool, plentifulProduct))
	assert.Equal(t, 0, countMovements(t, pool, scarceProduct))
	assert.Equal(t, 0, countRequestMovements(t, pool, req.RequestNumber))

	// The request remains APPROVED so it can be retried after restocking.
	stored, err := repo.GetByID(ctx, req.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.StatusApproved, stored.Status)
}

func TestStockRequestRepository_Issue_InactiveProductRollsBack(t *testing.T) {
	pool := newTestPool(t)
	requireDevIdentities(t, pool)
	repo := postgres.NewStockRequestRepository(pool)
	ctx := context.Background()

	activeProduct := seedProduct(t, pool, true, 20)
	productToDeactivate := seedProduct(t, pool, true, 20)

	req := createStockRequest(t, repo, pool, []requestItemSpec{
		{ProductID: activeProduct, Quantity: 5},
		{ProductID: productToDeactivate, Quantity: 5},
	})
	_, err := repo.Approve(ctx, req.ID, testReviewerID)
	require.NoError(t, err)

	// Deactivate after approval: issuance must fail without touching any balance.
	_, err = pool.Exec(ctx, `UPDATE products SET is_active = false WHERE id = $1`, productToDeactivate)
	require.NoError(t, err)

	_, err = repo.Issue(ctx, req.ID, testReviewerID, "")
	require.ErrorIs(t, err, domain.ErrProductInactive)

	assert.Equal(t, 20, readStock(t, pool, activeProduct))
	assert.Equal(t, 20, readStock(t, pool, productToDeactivate))
	assert.Equal(t, 0, countMovements(t, pool, activeProduct))
	assert.Equal(t, 0, countMovements(t, pool, productToDeactivate))
	assert.Equal(t, 0, countRequestMovements(t, pool, req.RequestNumber))
}

func TestStockRequestRepository_Issue_MovementInsertFailureRollsBackBalancesAndStatus(t *testing.T) {
	pool := newTestPool(t)
	requireDevIdentities(t, pool)
	repo := postgres.NewStockRequestRepository(pool)
	ctx := context.Background()

	product := seedProduct(t, pool, true, 12)
	req := createStockRequest(t, repo, pool, []requestItemSpec{{ProductID: product, Quantity: 4}})
	_, err := repo.Approve(ctx, req.ID, testReviewerID)
	require.NoError(t, err)

	// An unprovisioned actor id violates stock_movements.actor_id -> users(id), which fails
	// the ledger insert after the balance update has already been applied in the transaction.
	_, err = repo.Issue(ctx, req.ID, uuid.New(), "")
	require.Error(t, err)
	require.NotErrorIs(t, err, domain.ErrInsufficientStock)
	require.NotErrorIs(t, err, domain.ErrCannotIssue)

	assert.Equal(t, 12, readStock(t, pool, product), "balance update must be rolled back when the movement insert fails")
	assert.Equal(t, 0, countMovements(t, pool, product))
	assert.Equal(t, 0, countRequestMovements(t, pool, req.RequestNumber))
	assert.Equal(t, 0, countIssuances(t, pool, req.ID), "no fulfillment record may survive a failed issuance")

	stored, err := repo.GetByID(ctx, req.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.StatusApproved, stored.Status, "status must stay APPROVED after a rolled-back issuance")
}

func TestStockRequestRepository_Issue_ConcurrentAttemptsIssueExactlyOnce(t *testing.T) {
	pool := newTestPool(t)
	requireDevIdentities(t, pool)
	repo := postgres.NewStockRequestRepository(pool)
	ctx := context.Background()

	const workers = 8
	const requestedQuantity = 4
	const initialStock = 10

	product := seedProduct(t, pool, true, initialStock)
	req := createStockRequest(t, repo, pool, []requestItemSpec{{ProductID: product, Quantity: requestedQuantity}})
	_, err := repo.Approve(ctx, req.ID, testReviewerID)
	require.NoError(t, err)

	start := make(chan struct{})
	results := make(chan error, workers)
	var wg sync.WaitGroup

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start

			issueCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			_, err := repo.Issue(issueCtx, req.ID, testReviewerID, "")
			results <- err
		}()
	}

	close(start)
	wg.Wait()
	close(results)

	succeeded := 0
	conflicted := 0
	for err := range results {
		switch {
		case err == nil:
			succeeded++
		case errors.Is(err, domain.ErrCannotIssue):
			conflicted++
		default:
			require.Failf(t, "unexpected issuance error", "error: %v", err)
		}
	}

	assert.Equal(t, 1, succeeded, "exactly one concurrent issuance may succeed")
	assert.Equal(t, workers-1, conflicted, "all other concurrent attempts must report a state conflict")

	// Stock is deducted exactly once and exactly one movement exists.
	assert.Equal(t, initialStock-requestedQuantity, readStock(t, pool, product))
	assert.Equal(t, 1, countMovements(t, pool, product))
	assert.Equal(t, 1, countRequestMovements(t, pool, req.RequestNumber))

	stored, err := repo.GetByID(ctx, req.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.StatusIssued, stored.Status)
}

func TestStockRequestRepository_Approve_ConcurrentAttemptsApproveExactlyOnce(t *testing.T) {
	pool := newTestPool(t)
	requireDevIdentities(t, pool)
	repo := postgres.NewStockRequestRepository(pool)

	const workers = 8

	product := seedProduct(t, pool, true, 5)
	req := createStockRequest(t, repo, pool, []requestItemSpec{{ProductID: product, Quantity: 1}})

	start := make(chan struct{})
	results := make(chan error, workers)
	var wg sync.WaitGroup

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start

			approveCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			_, err := repo.Approve(approveCtx, req.ID, testReviewerID)
			results <- err
		}()
	}

	close(start)
	wg.Wait()
	close(results)

	succeeded := 0
	for err := range results {
		if err == nil {
			succeeded++
			continue
		}
		assert.ErrorIs(t, err, domain.ErrCannotReview)
	}
	assert.Equal(t, 1, succeeded, "only one concurrent approval may succeed")
}

func TestStockRequestRepository_List_PaginationAndStatusFilter(t *testing.T) {
	pool := newTestPool(t)
	requireDevIdentities(t, pool)
	repo := postgres.NewStockRequestRepository(pool)
	ctx := context.Background()

	var totalBefore, pendingBefore int
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM stock_requests`).Scan(&totalBefore))
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM stock_requests WHERE status = 'PENDING'`).Scan(&pendingBefore))

	product := seedProduct(t, pool, true, 100)
	first := createStockRequest(t, repo, pool, []requestItemSpec{{ProductID: product, Quantity: 1}})
	second := createStockRequest(t, repo, pool, []requestItemSpec{{ProductID: product, Quantity: 2}})
	third := createStockRequest(t, repo, pool, []requestItemSpec{{ProductID: product, Quantity: 3}})

	// Newest first with deterministic ordering.
	page1, total, err := repo.List(ctx, domain.ListStockRequestFilter{Limit: 2, Offset: 0})
	require.NoError(t, err)
	assert.Equal(t, int64(totalBefore+3), total)
	require.Len(t, page1, 2)
	assert.Equal(t, third.ID, page1[0].ID)
	assert.Equal(t, second.ID, page1[1].ID)

	page2, total, err := repo.List(ctx, domain.ListStockRequestFilter{Limit: 2, Offset: 2})
	require.NoError(t, err)
	assert.Equal(t, int64(totalBefore+3), total)
	require.Len(t, page2, 1)
	assert.Equal(t, first.ID, page2[0].ID)

	// Approve one request so the status filter can be verified, and check items are loaded.
	_, err = repo.Approve(ctx, first.ID, testReviewerID)
	require.NoError(t, err)

	approvedStatus := domain.StatusApproved
	approvedPage, approvedTotal, err := repo.List(ctx, domain.ListStockRequestFilter{Status: &approvedStatus, Limit: 100})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, approvedTotal, int64(1))

	found := false
	for _, item := range approvedPage {
		if item.ID == first.ID {
			found = true
			assert.Equal(t, domain.StatusApproved, item.Status)
			require.Len(t, item.Items, 1)
			assert.Equal(t, 1, item.Items[0].RequestedQuantity)
		}
	}
	assert.True(t, found, "the approved request must appear in the APPROVED filter")

	// Two of the created requests are still PENDING.
	pendingStatus := domain.StatusPending
	_, pendingTotal, err := repo.List(ctx, domain.ListStockRequestFilter{Status: &pendingStatus, Limit: 1})
	require.NoError(t, err)
	assert.Equal(t, int64(pendingBefore+2), pendingTotal)

	// An offset past the end yields an empty, non-nil page.
	emptyPage, _, err := repo.List(ctx, domain.ListStockRequestFilter{Limit: 2, Offset: 100000})
	require.NoError(t, err)
	assert.Empty(t, emptyPage)
	assert.NotNil(t, emptyPage)
}

func TestStockRequestRepository_Cancel_WithoutStockChange(t *testing.T) {
	pool := newTestPool(t)
	requireDevIdentities(t, pool)
	repo := postgres.NewStockRequestRepository(pool)
	ctx := context.Background()

	product := seedProduct(t, pool, true, 12)

	// PENDING -> CANCELLED, nothing else changes.
	pendingReq := createStockRequest(t, repo, pool, []requestItemSpec{{ProductID: product, Quantity: 3}})
	cancelled, err := repo.Cancel(ctx, pendingReq.ID, testRequesterID, "no longer needed")
	require.NoError(t, err)
	assert.Equal(t, domain.StatusCancelled, cancelled.Status)
	assert.Len(t, cancelled.Items, 1)
	assert.Equal(t, 12, readStock(t, pool, product))
	assert.Equal(t, 0, countMovements(t, pool, product))
	assert.Equal(t, 0, countRequestMovements(t, pool, pendingReq.RequestNumber))
	assert.Equal(t, 0, countIssuances(t, pool, pendingReq.ID))

	// Cancelling twice is a state conflict.
	_, err = repo.Cancel(ctx, pendingReq.ID, testRequesterID, "")
	require.ErrorIs(t, err, domain.ErrCannotCancel)

	// APPROVED -> CANCELLED, and the request can no longer be issued.
	approvedReq := createStockRequest(t, repo, pool, []requestItemSpec{{ProductID: product, Quantity: 2}})
	_, err = repo.Approve(ctx, approvedReq.ID, testReviewerID)
	require.NoError(t, err)

	cancelled, err = repo.Cancel(ctx, approvedReq.ID, testReviewerID, "")
	require.NoError(t, err)
	assert.Equal(t, domain.StatusCancelled, cancelled.Status)
	assert.Equal(t, 12, readStock(t, pool, product))

	_, err = repo.Issue(ctx, approvedReq.ID, testReviewerID, "")
	require.ErrorIs(t, err, domain.ErrCannotIssue)
	assert.Equal(t, 12, readStock(t, pool, product))
	assert.Equal(t, 0, countIssuances(t, pool, approvedReq.ID))

	// Unknown request.
	_, err = repo.Cancel(ctx, uuid.New(), testRequesterID, "")
	require.ErrorIs(t, err, domain.ErrStockRequestNotFound)
}

func TestStockRequestRepository_Cancel_IssuedRequestIsRejected(t *testing.T) {
	pool := newTestPool(t)
	requireDevIdentities(t, pool)
	repo := postgres.NewStockRequestRepository(pool)
	ctx := context.Background()

	product := seedProduct(t, pool, true, 10)
	req := createStockRequest(t, repo, pool, []requestItemSpec{{ProductID: product, Quantity: 4}})
	_, err := repo.Approve(ctx, req.ID, testReviewerID)
	require.NoError(t, err)
	_, err = repo.Issue(ctx, req.ID, testReviewerID, "")
	require.NoError(t, err)

	_, err = repo.Cancel(ctx, req.ID, testReviewerID, "")
	require.ErrorIs(t, err, domain.ErrCannotCancel)

	var conflict *domain.StateConflictError
	require.True(t, errors.As(err, &conflict))
	assert.Equal(t, domain.StatusIssued, conflict.CurrentStatus)
	require.NotNil(t, conflict.IssuanceID)

	// Fulfillment is untouched by the rejected cancellation.
	assert.Equal(t, 6, readStock(t, pool, product))
	assert.Equal(t, 1, countMovements(t, pool, product))
	assert.Equal(t, 1, countIssuances(t, pool, req.ID))
}

func TestStockRequestRepository_Issue_ReplaysWithSameIdempotencyKey(t *testing.T) {
	pool := newTestPool(t)
	requireDevIdentities(t, pool)
	repo := postgres.NewStockRequestRepository(pool)
	ctx := context.Background()

	product := seedProduct(t, pool, true, 20)
	req := createStockRequest(t, repo, pool, []requestItemSpec{{ProductID: product, Quantity: 6}})
	_, err := repo.Approve(ctx, req.ID, testReviewerID)
	require.NoError(t, err)

	const key = "checkout-key-0001"

	first, err := repo.Issue(ctx, req.ID, testReviewerID, key)
	require.NoError(t, err)
	require.NotNil(t, first.Issuance)
	assert.False(t, first.IdempotentReplay)
	assert.Equal(t, key, first.Issuance.IdempotencyKey)
	require.Len(t, first.Movements, 1)

	// Retry with the same key: replay the original result, no second deduction.
	second, err := repo.Issue(ctx, req.ID, testReviewerID, key)
	require.NoError(t, err)
	assert.True(t, second.IdempotentReplay)
	require.NotNil(t, second.Issuance)
	assert.Equal(t, first.Issuance.ID, second.Issuance.ID)
	require.Len(t, second.Movements, 1)
	assert.Equal(t, first.Movements[0].ID, second.Movements[0].ID)
	assert.Equal(t, domain.StatusIssued, second.Request.Status)

	assert.Equal(t, 14, readStock(t, pool, product), "stock must be deducted exactly once")
	assert.Equal(t, 1, countMovements(t, pool, product))
	assert.Equal(t, 1, countIssuances(t, pool, req.ID))

	// A different key on an issued request is a conflict, not a replay.
	_, err = repo.Issue(ctx, req.ID, testReviewerID, "checkout-key-0002")
	require.ErrorIs(t, err, domain.ErrCannotIssue)

	var conflict *domain.StateConflictError
	require.True(t, errors.As(err, &conflict))
	require.NotNil(t, conflict.IssuanceID)

	// The detail read exposes the fulfillment record.
	stored, err := repo.GetByID(ctx, req.ID)
	require.NoError(t, err)
	require.NotNil(t, stored.Issuance)
	assert.Equal(t, first.Issuance.ID, stored.Issuance.ID)
	assert.Equal(t, 6, stored.Issuance.TotalQuantity)
	assert.Equal(t, 1, stored.Issuance.ItemCount)
}

func TestStockRequestRepository_Issue_RejectsKeyReusedByAnotherRequest(t *testing.T) {
	pool := newTestPool(t)
	requireDevIdentities(t, pool)
	repo := postgres.NewStockRequestRepository(pool)
	ctx := context.Background()

	const key = "checkout-key-reuse-0001"
	product := seedProduct(t, pool, true, 30)

	first := createStockRequest(t, repo, pool, []requestItemSpec{{ProductID: product, Quantity: 3}})
	_, err := repo.Approve(ctx, first.ID, testReviewerID)
	require.NoError(t, err)
	_, err = repo.Issue(ctx, first.ID, testReviewerID, key)
	require.NoError(t, err)
	assert.Equal(t, 27, readStock(t, pool, product))

	second := createStockRequest(t, repo, pool, []requestItemSpec{{ProductID: product, Quantity: 2}})
	_, err = repo.Approve(ctx, second.ID, testReviewerID)
	require.NoError(t, err)

	_, err = repo.Issue(ctx, second.ID, testReviewerID, key)
	require.ErrorIs(t, err, domain.ErrIdempotencyKeyReused)

	// The second request is untouched.
	assert.Equal(t, 27, readStock(t, pool, product))
	assert.Equal(t, 0, countRequestMovements(t, pool, second.RequestNumber))
	assert.Equal(t, 0, countIssuances(t, pool, second.ID))

	stored, err := repo.GetByID(ctx, second.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.StatusApproved, stored.Status)
}

func TestStockRequestRepository_Issue_ReportsEveryShortageWithoutChanges(t *testing.T) {
	pool := newTestPool(t)
	requireDevIdentities(t, pool)
	repo := postgres.NewStockRequestRepository(pool)
	ctx := context.Background()

	enoughProduct := seedProduct(t, pool, true, 50)
	scarceProduct := seedProduct(t, pool, true, 3)
	veryscarceProduct := seedProduct(t, pool, true, 1)

	req := createStockRequest(t, repo, pool, []requestItemSpec{
		{ProductID: enoughProduct, Quantity: 10},
		{ProductID: scarceProduct, Quantity: 5},
		{ProductID: veryscarceProduct, Quantity: 4},
	})
	_, err := repo.Approve(ctx, req.ID, testReviewerID)
	require.NoError(t, err)

	_, err = repo.Issue(ctx, req.ID, testReviewerID, "")
	require.ErrorIs(t, err, domain.ErrInsufficientStock)

	var shortage *domain.InsufficientStockError
	require.True(t, errors.As(err, &shortage))
	require.Len(t, shortage.Items, 2, "both short items must be reported in one response")

	byProduct := map[uuid.UUID]domain.StockShortage{}
	for _, item := range shortage.Items {
		byProduct[item.ProductID] = item
	}
	assert.Equal(t, domain.StockShortage{ProductID: scarceProduct, Requested: 5, Available: 3, Shortfall: 2}, byProduct[scarceProduct])
	assert.Equal(t, domain.StockShortage{ProductID: veryscarceProduct, Requested: 4, Available: 1, Shortfall: 3}, byProduct[veryscarceProduct])

	// Nothing may be deducted or recorded.
	assert.Equal(t, 50, readStock(t, pool, enoughProduct))
	assert.Equal(t, 3, readStock(t, pool, scarceProduct))
	assert.Equal(t, 1, readStock(t, pool, veryscarceProduct))
	for _, id := range []uuid.UUID{enoughProduct, scarceProduct, veryscarceProduct} {
		assert.Equal(t, 0, countMovements(t, pool, id))
	}
	assert.Equal(t, 0, countIssuances(t, pool, req.ID))

	stored, err := repo.GetByID(ctx, req.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.StatusApproved, stored.Status)
}

func TestStockRequestRepository_Issue_ConcurrentSameKeyIssuesOnceAndReplays(t *testing.T) {
	pool := newTestPool(t)
	requireDevIdentities(t, pool)
	repo := postgres.NewStockRequestRepository(pool)

	const workers = 8
	const quantity = 4
	const initialStock = 20
	const key = "concurrent-key-0001"

	product := seedProduct(t, pool, true, initialStock)
	req := createStockRequest(t, repo, pool, []requestItemSpec{{ProductID: product, Quantity: quantity}})
	_, err := repo.Approve(context.Background(), req.ID, testReviewerID)
	require.NoError(t, err)

	start := make(chan struct{})
	results := make(chan *domain.StockIssueResult, workers)
	errs := make(chan error, workers)
	var wg sync.WaitGroup

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start

			issueCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			result, err := repo.Issue(issueCtx, req.ID, testReviewerID, key)
			results <- result
			errs <- err
		}()
	}

	close(start)
	wg.Wait()
	close(results)
	close(errs)

	for err := range errs {
		require.NoError(t, err, "every caller must succeed through a real issuance or a replay")
	}

	movementIDs := map[int64]struct{}{}
	realIssuances := 0
	for result := range results {
		require.NotNil(t, result)
		require.Len(t, result.Movements, 1)
		movementIDs[result.Movements[0].ID] = struct{}{}
		if !result.IdempotentReplay {
			realIssuances++
		}
	}

	assert.Equal(t, 1, realIssuances, "only one caller may perform the real issuance")
	assert.Len(t, movementIDs, 1, "every caller must observe the same ledger row")
	assert.Equal(t, initialStock-quantity, readStock(t, pool, product))
	assert.Equal(t, 1, countMovements(t, pool, product))
	assert.Equal(t, 1, countIssuances(t, pool, req.ID))
}
