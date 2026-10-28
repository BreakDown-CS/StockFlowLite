package domain

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// Actor is the authenticated principal resolved from a validated session token.
// It is the only trusted source of identity for attribution and authorization;
// client-supplied identifiers are never used.
type Actor struct {
	ID        uuid.UUID
	Username  string
	Role      UserRole
	RequestID string
	IPAddress string
	UserAgent string
}

// SystemActor is used by internal tooling and CLI commands without an HTTP principal.
func SystemActor() Actor {
	return Actor{Username: "system", Role: RoleAdmin}
}

// HasRole reports whether the actor holds one of the given roles.
func (a Actor) HasRole(roles ...UserRole) bool {
	for _, role := range roles {
		if a.Role == role {
			return true
		}
	}
	return false
}

// IsZero reports whether the actor carries no identity.
func (a Actor) IsZero() bool {
	return a.ID == uuid.Nil
}

type actorContextKey struct{}

// WithActor stores the authenticated principal in the context for downstream layers.
func WithActor(ctx context.Context, actor Actor) context.Context {
	return context.WithValue(ctx, actorContextKey{}, actor)
}

// ActorFromContext returns the authenticated principal stored by the auth middleware.
func ActorFromContext(ctx context.Context) (Actor, bool) {
	if ctx == nil {
		return Actor{}, false
	}
	actor, ok := ctx.Value(actorContextKey{}).(Actor)
	return actor, ok
}

// AuthSession is the server-side session backing an opaque bearer token.
// Only a keyed hash of the token is persisted.
type AuthSession struct {
	ID         uuid.UUID  `json:"id"`
	UserID     uuid.UUID  `json:"user_id"`
	TokenHash  string     `json:"-"`
	CreatedAt  time.Time  `json:"created_at"`
	ExpiresAt  time.Time  `json:"expires_at"`
	RevokedAt  *time.Time `json:"revoked_at,omitempty"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	IPAddress  string     `json:"ip_address,omitempty"`
	UserAgent  string     `json:"user_agent,omitempty"`
}

// IsActive reports whether the session may still be used at the given time.
func (s *AuthSession) IsActive(now time.Time) bool {
	if s.RevokedAt != nil {
		return false
	}
	return now.Before(s.ExpiresAt)
}
