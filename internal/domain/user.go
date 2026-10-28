package domain

import (
	"strings"
	"time"

	"github.com/google/uuid"
)

// UserRole represents the authorized system roles.
type UserRole string

const (
	RoleAdmin       UserRole = "ADMIN"
	RoleStorekeeper UserRole = "STOREKEEPER"
	RoleRequester   UserRole = "REQUESTER"
)

// ValidRoles lists all accepted user roles.
var ValidRoles = []UserRole{RoleAdmin, RoleStorekeeper, RoleRequester}

// User represents an actor in the system.
type User struct {
	ID           uuid.UUID `json:"id"`
	Username     string    `json:"username"`
	PasswordHash string    `json:"-"`
	Role         UserRole  `json:"role"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// NewUser creates a validated User instance with a generated UUID and timestamps.
func NewUser(username, passwordHash string, role UserRole) (*User, error) {
	now := time.Now().UTC()
	user := &User{
		ID:           uuid.New(),
		Username:     strings.TrimSpace(username),
		PasswordHash: passwordHash,
		Role:         role,
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	if err := user.Validate(); err != nil {
		return nil, err
	}
	return user, nil
}

// Validate verifies internal domain invariants for a User.
func (u *User) Validate() error {
	if strings.TrimSpace(u.Username) == "" {
		return ErrEmptyUsername
	}
	if len(strings.TrimSpace(u.Username)) < 3 {
		return ErrUsernameTooShort
	}
	if strings.TrimSpace(u.PasswordHash) == "" {
		return ErrEmptyPasswordHash
	}
	if !u.IsValidRole() {
		return ErrInvalidRole
	}
	return nil
}

// IsValidRole checks if the assigned role is supported.
func (u *User) IsValidRole() bool {
	for _, r := range ValidRoles {
		if u.Role == r {
			return true
		}
	}
	return false
}
