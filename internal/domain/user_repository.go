package domain

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// ListUserFilter holds filtering and pagination parameters for user listings.
type ListUserFilter struct {
	Search   string
	Role     *UserRole
	IsActive *bool
	Limit    int
	Offset   int
}

// UserRepository defines persistence operations for accounts and their credentials.
type UserRepository interface {
	Create(ctx context.Context, u *User) error
	GetByID(ctx context.Context, id uuid.UUID) (*User, error)
	GetByUsername(ctx context.Context, username string) (*User, error)
	List(ctx context.Context, filter ListUserFilter) ([]User, int64, error)
	UpdateRole(ctx context.Context, id uuid.UUID, role UserRole) error
	SetActive(ctx context.Context, id uuid.UUID, isActive bool) error
	UpdatePasswordHash(ctx context.Context, id uuid.UUID, passwordHash string, changedAt time.Time) error
	TouchLastLogin(ctx context.Context, id uuid.UUID, at time.Time) error
	CountActiveAdmins(ctx context.Context) (int, error)
}
