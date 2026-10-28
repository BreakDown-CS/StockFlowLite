package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/BreakDown-CS/StockFlowLite/internal/domain"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// SessionRepository implements domain.SessionRepository using pgxpool.
type SessionRepository struct {
	pool *pgxpool.Pool
}

// NewSessionRepository constructs a SessionRepository.
func NewSessionRepository(pool *pgxpool.Pool) *SessionRepository {
	return &SessionRepository{pool: pool}
}

// Create persists a session. Only the keyed token hash is stored.
func (r *SessionRepository) Create(ctx context.Context, s *domain.AuthSession) error {
	const query = `
		INSERT INTO auth_sessions (user_id, token_hash, created_at, expires_at, ip_address, user_agent)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id;
	`

	err := r.pool.QueryRow(ctx, query,
		s.UserID,
		s.TokenHash,
		s.CreatedAt,
		s.ExpiresAt,
		nullableText(s.IPAddress),
		nullableText(s.UserAgent),
	).Scan(&s.ID)
	if err != nil {
		return fmt.Errorf("failed to create auth session: %w", err)
	}

	return nil
}

// GetActiveByTokenHash resolves an active (not revoked, not expired) session and
// its user in a single round trip. Missing sessions map to ErrUnauthenticated.
func (r *SessionRepository) GetActiveByTokenHash(ctx context.Context, tokenHash string) (*domain.AuthSession, *domain.User, error) {
	const query = `
		SELECT s.id, s.user_id, s.created_at, s.expires_at, s.revoked_at, s.last_used_at,
		       COALESCE(s.ip_address, ''), COALESCE(s.user_agent, ''),
		       u.id, u.username, u.password_hash, u.role, u.is_active,
		       u.last_login_at, u.password_changed_at, u.created_at, u.updated_at
		FROM auth_sessions s
		JOIN users u ON u.id = s.user_id
		WHERE s.token_hash = $1
		  AND s.revoked_at IS NULL
		  AND s.expires_at > now();
	`

	var session domain.AuthSession
	var user domain.User
	var role string

	err := r.pool.QueryRow(ctx, query, tokenHash).Scan(
		&session.ID,
		&session.UserID,
		&session.CreatedAt,
		&session.ExpiresAt,
		&session.RevokedAt,
		&session.LastUsedAt,
		&session.IPAddress,
		&session.UserAgent,
		&user.ID,
		&user.Username,
		&user.PasswordHash,
		&role,
		&user.IsActive,
		&user.LastLoginAt,
		&user.PasswordChangedAt,
		&user.CreatedAt,
		&user.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil, domain.ErrUnauthenticated
		}
		return nil, nil, fmt.Errorf("failed to resolve auth session: %w", err)
	}

	session.TokenHash = tokenHash
	user.Role = domain.UserRole(role)

	return &session, &user, nil
}

// Revoke invalidates a single session (logout).
func (r *SessionRepository) Revoke(ctx context.Context, id uuid.UUID, at time.Time) error {
	const query = `UPDATE auth_sessions SET revoked_at = $2 WHERE id = $1 AND revoked_at IS NULL;`

	if _, err := r.pool.Exec(ctx, query, id, at); err != nil {
		return fmt.Errorf("failed to revoke auth session: %w", err)
	}
	return nil
}

// RevokeAllForUser invalidates every active session of an account
// (used when a password changes or an account is disabled).
func (r *SessionRepository) RevokeAllForUser(ctx context.Context, userID uuid.UUID, at time.Time) error {
	const query = `UPDATE auth_sessions SET revoked_at = $2 WHERE user_id = $1 AND revoked_at IS NULL;`

	if _, err := r.pool.Exec(ctx, query, userID, at); err != nil {
		return fmt.Errorf("failed to revoke user sessions: %w", err)
	}
	return nil
}

// TouchLastUsed records session activity. Failures are non-fatal for the caller.
func (r *SessionRepository) TouchLastUsed(ctx context.Context, id uuid.UUID, at time.Time) error {
	const query = `UPDATE auth_sessions SET last_used_at = $2 WHERE id = $1;`

	if _, err := r.pool.Exec(ctx, query, id, at); err != nil {
		return fmt.Errorf("failed to touch auth session: %w", err)
	}
	return nil
}
