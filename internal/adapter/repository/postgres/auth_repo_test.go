package postgres_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/BreakDown-CS/StockFlowLite/internal/adapter/repository/postgres"
	coreauth "github.com/BreakDown-CS/StockFlowLite/internal/auth"
	"github.com/BreakDown-CS/StockFlowLite/internal/domain"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// createTestUser inserts an isolated account and registers cleanup (sessions + audit first,
// because audit_events.actor_id is ON DELETE RESTRICT).
func createTestUser(t *testing.T, repo *postgres.UserRepository, pool *pgxpool.Pool, role domain.UserRole) (*domain.User, string) {
	t.Helper()

	const password = "integration-password-123"
	hasher := coreauth.NewHasher(coreauth.MinIterations)

	hash, err := hasher.Hash(password)
	require.NoError(t, err)

	username := "tst_" + strings.ToLower(strings.ReplaceAll(uuid.NewString(), "-", "")[:12])
	user, err := domain.NewUser(username, hash, role)
	require.NoError(t, err)
	require.NoError(t, repo.Create(context.Background(), user))

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, _ = pool.Exec(ctx, `DELETE FROM auth_sessions WHERE user_id = $1`, user.ID)
		_, _ = pool.Exec(ctx, `DELETE FROM audit_events WHERE actor_id = $1`, user.ID)
		_, _ = pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, user.ID)
	})

	return user, password
}

func TestUserRepository_CreateAndFetch(t *testing.T) {
	pool := newTestPool(t)
	repo := postgres.NewUserRepository(pool)
	ctx := context.Background()

	user, password := createTestUser(t, repo, pool, domain.RoleStorekeeper)

	byID, err := repo.GetByID(ctx, user.ID)
	require.NoError(t, err)
	assert.Equal(t, user.Username, byID.Username)
	assert.Equal(t, domain.RoleStorekeeper, byID.Role)
	assert.True(t, byID.IsActive)

	byName, err := repo.GetByUsername(ctx, strings.ToUpper(user.Username))
	require.NoError(t, err, "username lookup must be case-insensitive")
	assert.Equal(t, user.ID, byName.ID)

	// The stored credential is a one-way hash, never the plaintext password.
	assert.NotContains(t, byName.PasswordHash, password)
	assert.True(t, coreauth.NewHasher(coreauth.MinIterations).Verify(byName.PasswordHash, password))

	// Duplicate usernames are rejected by the unique index.
	duplicate, err := domain.NewUser(user.Username, user.PasswordHash, domain.RoleRequester)
	require.NoError(t, err)
	require.ErrorIs(t, repo.Create(ctx, duplicate), domain.ErrDuplicateUsername)

	_, err = repo.GetByUsername(ctx, "missing_"+uuid.NewString())
	require.ErrorIs(t, err, domain.ErrUserNotFound)
}

func TestSessionRepository_Lifecycle(t *testing.T) {
	pool := newTestPool(t)
	users := postgres.NewUserRepository(pool)
	sessions := postgres.NewSessionRepository(pool)
	ctx := context.Background()

	user, _ := createTestUser(t, users, pool, domain.RoleStorekeeper)

	const pepper = "integration-test-pepper"
	token, tokenHash, err := coreauth.GenerateToken(pepper)
	require.NoError(t, err)

	now := time.Now().UTC()
	session := &domain.AuthSession{
		UserID:    user.ID,
		TokenHash: tokenHash,
		CreatedAt: now,
		ExpiresAt: now.Add(time.Hour),
	}
	require.NoError(t, sessions.Create(ctx, session))
	assert.NotEqual(t, uuid.Nil, session.ID)

	// The raw token must never be persisted.
	var rawMatches int
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM auth_sessions WHERE token_hash = $1`, token).Scan(&rawMatches))
	assert.Zero(t, rawMatches)

	resolvedSession, resolvedUser, err := sessions.GetActiveByTokenHash(ctx, coreauth.HashToken(pepper, token))
	require.NoError(t, err)
	assert.Equal(t, session.ID, resolvedSession.ID)
	assert.Equal(t, user.ID, resolvedUser.ID)
	assert.True(t, resolvedUser.IsActive)

	// Revoked sessions are no longer usable.
	require.NoError(t, sessions.Revoke(ctx, session.ID, time.Now().UTC()))
	_, _, err = sessions.GetActiveByTokenHash(ctx, tokenHash)
	require.ErrorIs(t, err, domain.ErrUnauthenticated)

	// Expired sessions are rejected (created in the past to satisfy the CHECK constraint).
	_, expiredHash, err := coreauth.GenerateToken(pepper)
	require.NoError(t, err)
	expired := &domain.AuthSession{
		UserID:    user.ID,
		TokenHash: expiredHash,
		CreatedAt: now.Add(-2 * time.Hour),
		ExpiresAt: now.Add(-time.Hour),
	}
	require.NoError(t, sessions.Create(ctx, expired))

	_, _, err = sessions.GetActiveByTokenHash(ctx, expiredHash)
	require.ErrorIs(t, err, domain.ErrUnauthenticated)
}

func TestAuditRepository_RecordAndList(t *testing.T) {
	pool := newTestPool(t)
	repo := postgres.NewAuditRepository(pool)
	ctx := context.Background()

	entityID := uuid.New()
	event := domain.AuditEvent{
		EventType:     domain.AuditRequestCancelled,
		ActorUsername: "audit_tester",
		ActorRole:     domain.RoleStorekeeper,
		EntityType:    domain.AuditEntityStockRequest,
		EntityID:      &entityID,
		Reference:     "REQ-AUDIT-0001",
		FromStatus:    string(domain.StatusApproved),
		ToStatus:      string(domain.StatusCancelled),
		Reason:        "integration test",
		Metadata:      map[string]interface{}{"note": "recorded"},
	}
	require.NoError(t, repo.Record(ctx, event))

	eventType := domain.AuditRequestCancelled
	events, total, err := repo.List(ctx, domain.ListAuditFilter{EventType: &eventType, EntityID: &entityID, Limit: 10})
	require.NoError(t, err)
	assert.EqualValues(t, 1, total)
	require.Len(t, events, 1)

	assert.Equal(t, domain.AuditRequestCancelled, events[0].EventType)
	assert.Equal(t, "audit_tester", events[0].ActorUsername)
	assert.Equal(t, "integration test", events[0].Reason)
	assert.Equal(t, string(domain.StatusApproved), events[0].FromStatus)
	assert.Equal(t, string(domain.StatusCancelled), events[0].ToStatus)
	assert.Equal(t, "recorded", events[0].Metadata["note"])

	// Filtering by an unrelated actor yields nothing.
	otherActor := uuid.New()
	events, total, err = repo.List(ctx, domain.ListAuditFilter{ActorID: &otherActor, EntityID: &entityID})
	require.NoError(t, err)
	assert.EqualValues(t, 0, total)
	assert.Empty(t, events)

	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM audit_events WHERE entity_id = $1`, entityID)
	})
}

func countAudit(t *testing.T, pool *pgxpool.Pool, eventType domain.AuditEventType, entityID *uuid.UUID) int {
	t.Helper()

	var count int
	require.NoError(t, pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM audit_events WHERE event_type = $1 AND entity_id = $2`,
		string(eventType), *entityID).Scan(&count))
	return count
}

func auditTypePtr(eventType domain.AuditEventType) *domain.AuditEventType {
	return &eventType
}

func TestStockRequestRepository_WritesAuditAndBlocksSelfApproval(t *testing.T) {
	pool := newTestPool(t)
	requireDevIdentities(t, pool)
	repo := postgres.NewStockRequestRepository(pool)

	requesterActor := domain.Actor{ID: testRequesterID, Username: "dev_requester", Role: domain.RoleRequester}
	reviewerActor := domain.Actor{ID: testReviewerID, Username: "dev_storekeeper", Role: domain.RoleStorekeeper}
	requesterCtx := domain.WithActor(context.Background(), requesterActor)

	product := seedProduct(t, pool, true, 10)
	req := createStockRequest(t, repo, pool, []requestItemSpec{{ProductID: product, Quantity: 2}})

	// Creation is audited atomically with the request.
	assert.Equal(t, 1, countAudit(t, pool, domain.AuditRequestCreated, &req.ID))

	// Four-eyes rule: the requester cannot approve their own request.
	_, err := repo.Approve(requesterCtx, req.ID, testRequesterID)
	require.ErrorIs(t, err, domain.ErrSelfApproval)
	assert.Equal(t, 1, countAudit(t, pool, domain.AuditSelfApprovalBlocked, &req.ID))

	stored, err := repo.GetByID(requesterCtx, req.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.StatusPending, stored.Status, "a blocked approval must not change the status")

	// A different reviewer may approve; the audit row commits with the state change.
	_, err = repo.Approve(domain.WithActor(context.Background(), reviewerActor), req.ID, testReviewerID)
	require.NoError(t, err)
	assert.Equal(t, 1, countAudit(t, pool, domain.AuditRequestApproved, &req.ID))
}

func TestStockRequestRepository_CancelStoresReasonAndAudit(t *testing.T) {
	pool := newTestPool(t)
	requireDevIdentities(t, pool)
	repo := postgres.NewStockRequestRepository(pool)
	auditRepo := postgres.NewAuditRepository(pool)

	requesterCtx := domain.WithActor(context.Background(), domain.Actor{
		ID: testRequesterID, Username: "dev_requester", Role: domain.RoleRequester,
	})

	product := seedProduct(t, pool, true, 5)
	req := createStockRequest(t, repo, pool, []requestItemSpec{{ProductID: product, Quantity: 1}})

	_, err := repo.Cancel(requesterCtx, req.ID, testRequesterID, "no longer needed")
	require.NoError(t, err)

	var reason string
	require.NoError(t, pool.QueryRow(context.Background(),
		`SELECT COALESCE(cancellation_reason, '') FROM stock_requests WHERE id = $1`, req.ID).Scan(&reason))
	assert.Equal(t, "no longer needed", reason)

	events, total, err := auditRepo.List(context.Background(), domain.ListAuditFilter{
		EventType: auditTypePtr(domain.AuditRequestCancelled),
		EntityID:  &req.ID,
	})
	require.NoError(t, err)
	assert.EqualValues(t, 1, total)
	require.Len(t, events, 1)
	assert.Equal(t, "no longer needed", events[0].Reason)
	assert.Equal(t, string(domain.StatusPending), events[0].FromStatus)
	assert.Equal(t, string(domain.StatusCancelled), events[0].ToStatus)
	assert.Equal(t, "dev_requester", events[0].ActorUsername)
}
