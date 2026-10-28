package domain_test

import (
	"testing"

	"github.com/BreakDown-CS/StockFlowLite/internal/domain"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewUser_Success(t *testing.T) {
	u, err := domain.NewUser("admin_john", "hashed_secret_pw", domain.RoleAdmin)
	require.NoError(t, err)
	assert.NotEqual(t, uuid.Nil, u.ID)
	assert.Equal(t, "admin_john", u.Username)
	assert.Equal(t, domain.RoleAdmin, u.Role)
	assert.False(t, u.CreatedAt.IsZero())
	assert.False(t, u.UpdatedAt.IsZero())
}

func TestUser_Validate(t *testing.T) {
	tests := []struct {
		name     string
		username string
		hash     string
		role     domain.UserRole
		wantErr  error
	}{
		{
			name:     "valid storekeeper",
			username: "keeper1",
			hash:     "hash123",
			role:     domain.RoleStorekeeper,
			wantErr:  nil,
		},
		{
			name:     "valid requester",
			username: "req_finance",
			hash:     "hash123",
			role:     domain.RoleRequester,
			wantErr:  nil,
		},
		{
			name:     "empty username",
			username: "   ",
			hash:     "hash123",
			role:     domain.RoleAdmin,
			wantErr:  domain.ErrEmptyUsername,
		},
		{
			name:     "username too short",
			username: "ab",
			hash:     "hash123",
			role:     domain.RoleAdmin,
			wantErr:  domain.ErrUsernameTooShort,
		},
		{
			name:     "empty password hash",
			username: "admin_user",
			hash:     "   ",
			role:     domain.RoleAdmin,
			wantErr:  domain.ErrEmptyPasswordHash,
		},
		{
			name:     "invalid role",
			username: "super_root",
			hash:     "hash123",
			role:     domain.UserRole("SUPERADMIN"),
			wantErr:  domain.ErrInvalidRole,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := domain.NewUser(tt.username, tt.hash, tt.role)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
