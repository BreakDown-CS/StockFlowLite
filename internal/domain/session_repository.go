package domain

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// SessionRepository defines persistence operations for opaque bearer sessions.
type SessionRepository interface {
	// Create persists a new session (only the keyed token hash is stored).
	Create(ctx context.Context, s *AuthSession) error

	// GetActiveByTokenHash resolves a session and its user in a single round trip.
	// It returns ErrUnauthenticated when no active session matches the hash.
	GetActiveByTokenHash(ctx context.Context, tokenHash string) (*AuthSession, *User, error)

	// Revoke marks a single session as revoked.
	Revoke(ctx context.Context, id uuid.UUID, at time.Time) error

	// RevokeAllForUser revokes every active session of a user, optionally sparing one.
	RevokeAllForUser(ctx context.Context, userID uuid.UUID, at time.Time) error

	// TouchLastUsed records session activity without failing the request.
	TouchLastUsed(ctx context.Context, id uuid.UUID, at time.Time) error
}
