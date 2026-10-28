package user_test

import (
	"context"
	"testing"
	"time"

	coreauth "github.com/BreakDown-CS/StockFlowLite/internal/auth"
	"github.com/BreakDown-CS/StockFlowLite/internal/domain"
	usermgmt "github.com/BreakDown-CS/StockFlowLite/internal/usecase/user"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type mockUserRepository struct{ mock.Mock }

func (m *mockUserRepository) Create(ctx context.Context, u *domain.User) error {
	return m.Called(ctx, u).Error(0)
}

func (m *mockUserRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.User, error) {
	args := m.Called(ctx, id)
	if res := args.Get(0); res != nil {
		return res.(*domain.User), args.Error(1)
	}
	return nil, args.Error(1)
}

func (m *mockUserRepository) GetByUsername(ctx context.Context, username string) (*domain.User, error) {
	args := m.Called(ctx, username)
	if res := args.Get(0); res != nil {
		return res.(*domain.User), args.Error(1)
	}
	return nil, args.Error(1)
}

func (m *mockUserRepository) List(ctx context.Context, filter domain.ListUserFilter) ([]domain.User, int64, error) {
	args := m.Called(ctx, filter)
	return args.Get(0).([]domain.User), args.Get(1).(int64), args.Error(2)
}

func (m *mockUserRepository) UpdateRole(ctx context.Context, id uuid.UUID, role domain.UserRole) error {
	return m.Called(ctx, id, role).Error(0)
}

func (m *mockUserRepository) SetActive(ctx context.Context, id uuid.UUID, isActive bool) error {
	return m.Called(ctx, id, isActive).Error(0)
}

func (m *mockUserRepository) UpdatePasswordHash(ctx context.Context, id uuid.UUID, passwordHash string, changedAt time.Time) error {
	return m.Called(ctx, id, passwordHash, changedAt).Error(0)
}

func (m *mockUserRepository) TouchLastLogin(ctx context.Context, id uuid.UUID, at time.Time) error {
	return m.Called(ctx, id, at).Error(0)
}

func (m *mockUserRepository) CountActiveAdmins(ctx context.Context) (int, error) {
	args := m.Called(ctx)
	return args.Int(0), args.Error(1)
}

type mockSessionRepository struct{ mock.Mock }

func (m *mockSessionRepository) Create(ctx context.Context, s *domain.AuthSession) error {
	return m.Called(ctx, s).Error(0)
}

func (m *mockSessionRepository) GetActiveByTokenHash(ctx context.Context, tokenHash string) (*domain.AuthSession, *domain.User, error) {
	args := m.Called(ctx, tokenHash)
	session, _ := args.Get(0).(*domain.AuthSession)
	user, _ := args.Get(1).(*domain.User)
	return session, user, args.Error(2)
}

func (m *mockSessionRepository) Revoke(ctx context.Context, id uuid.UUID, at time.Time) error {
	return m.Called(ctx, id, at).Error(0)
}

func (m *mockSessionRepository) RevokeAllForUser(ctx context.Context, userID uuid.UUID, at time.Time) error {
	return m.Called(ctx, userID, at).Error(0)
}

func (m *mockSessionRepository) TouchLastUsed(ctx context.Context, id uuid.UUID, at time.Time) error {
	return m.Called(ctx, id, at).Error(0)
}

func (m *mockSessionRepository) PurgeInactive(ctx context.Context, before time.Time) (int64, error) {
	args := m.Called(ctx, before)
	return args.Get(0).(int64), args.Error(1)
}

type mockAuditRepository struct{ mock.Mock }

func (m *mockAuditRepository) Record(ctx context.Context, event domain.AuditEvent) error {
	return m.Called(ctx, event).Error(0)
}

func (m *mockAuditRepository) List(ctx context.Context, filter domain.ListAuditFilter) ([]domain.AuditEvent, int64, error) {
	args := m.Called(ctx, filter)
	return args.Get(0).([]domain.AuditEvent), args.Get(1).(int64), args.Error(2)
}

func newUsecase(users *mockUserRepository, sessions *mockSessionRepository, audit *mockAuditRepository) usermgmt.UserUsecase {
	return usermgmt.NewUserUsecase(users, sessions, audit, coreauth.NewHasher(coreauth.MinIterations))
}

func existingUser(t *testing.T, username string, role domain.UserRole, active bool) *domain.User {
	t.Helper()

	hash, err := coreauth.NewHasher(coreauth.MinIterations).Hash("existing-pass")
	require.NoError(t, err)

	user, err := domain.NewUser(username, hash, role)
	require.NoError(t, err)
	user.IsActive = active
	return user
}

func TestUserUsecase_CreateUser_HashesPasswordAndAudits(t *testing.T) {
	users := new(mockUserRepository)
	sessions := new(mockSessionRepository)
	audit := new(mockAuditRepository)
	uc := newUsecase(users, sessions, audit)

	users.On("Create", mock.Anything, mock.MatchedBy(func(u *domain.User) bool {
		return u.Username == "new_keeper" &&
			u.Role == domain.RoleStorekeeper &&
			u.IsActive &&
			coreauth.NewHasher(coreauth.MinIterations).Verify(u.PasswordHash, "s3cret-pass")
	})).Return(nil)
	audit.On("Record", mock.Anything, mock.MatchedBy(func(e domain.AuditEvent) bool {
		return e.EventType == domain.AuditUserCreated && e.Reference == "new_keeper"
	})).Return(nil)

	created, err := uc.CreateUser(context.Background(), usermgmt.CreateUserInput{
		Username: "  new_keeper  ",
		Password: "s3cret-pass",
		Role:     domain.RoleStorekeeper,
	})
	require.NoError(t, err)
	assert.Equal(t, "new_keeper", created.Username)
	assert.Empty(t, created.PasswordHash, "hashes must never be returned")
	users.AssertExpectations(t)
	audit.AssertExpectations(t)
}

func TestUserUsecase_CreateUser_Validation(t *testing.T) {
	users := new(mockUserRepository)
	uc := newUsecase(users, new(mockSessionRepository), new(mockAuditRepository))

	_, err := uc.CreateUser(context.Background(), usermgmt.CreateUserInput{
		Username: "weak", Password: "short", Role: domain.RoleRequester,
	})
	require.ErrorIs(t, err, domain.ErrWeakPassword)

	_, err = uc.CreateUser(context.Background(), usermgmt.CreateUserInput{
		Username: "weak", Password: "long-enough-pass", Role: domain.UserRole("OWNER"),
	})
	require.ErrorIs(t, err, domain.ErrInvalidRole)

	users.AssertNotCalled(t, "Create", mock.Anything, mock.Anything)
}

func TestUserUsecase_ListUsers_StripsHashes(t *testing.T) {
	users := new(mockUserRepository)
	uc := newUsecase(users, new(mockSessionRepository), new(mockAuditRepository))

	stored := []domain.User{
		*existingUser(t, "alpha", domain.RoleRequester, true),
		*existingUser(t, "beta", domain.RoleStorekeeper, true),
	}
	users.On("List", mock.Anything, mock.MatchedBy(func(f domain.ListUserFilter) bool {
		return f.Limit == 20 && f.Offset == 0
	})).Return(stored, int64(2), nil)

	list, total, err := uc.ListUsers(context.Background(), usermgmt.ListUsersInput{})
	require.NoError(t, err)
	assert.EqualValues(t, 2, total)
	for _, user := range list {
		assert.Empty(t, user.PasswordHash)
	}
}

func TestUserUsecase_UpdateUser_GuardsAdministrators(t *testing.T) {
	requesterRole := domain.RoleRequester
	deactivate := false
	adminActor := domain.Actor{ID: uuid.New(), Username: "root", Role: domain.RoleAdmin}

	t.Run("cannot demote the last active administrator", func(t *testing.T) {
		users := new(mockUserRepository)
		uc := newUsecase(users, new(mockSessionRepository), new(mockAuditRepository))

		admin := existingUser(t, "root", domain.RoleAdmin, true)
		users.On("GetByID", mock.Anything, admin.ID).Return(admin, nil)
		users.On("CountActiveAdmins", mock.Anything).Return(1, nil)

		_, err := uc.UpdateUser(context.Background(), adminActor, admin.ID,
			usermgmt.UpdateUserInput{Role: &requesterRole})
		require.ErrorIs(t, err, domain.ErrLastAdmin)
		users.AssertNotCalled(t, "UpdateRole", mock.Anything, mock.Anything, mock.Anything)
	})

	t.Run("cannot deactivate itself", func(t *testing.T) {
		users := new(mockUserRepository)
		uc := newUsecase(users, new(mockSessionRepository), new(mockAuditRepository))

		admin := existingUser(t, "root", domain.RoleAdmin, true)
		users.On("GetByID", mock.Anything, admin.ID).Return(admin, nil)
		users.On("CountActiveAdmins", mock.Anything).Return(2, nil)

		_, err := uc.UpdateUser(context.Background(),
			domain.Actor{ID: admin.ID, Username: "root", Role: domain.RoleAdmin},
			admin.ID, usermgmt.UpdateUserInput{IsActive: &deactivate})
		require.ErrorIs(t, err, domain.ErrForbidden)
	})

	t.Run("role change is audited", func(t *testing.T) {
		users := new(mockUserRepository)
		audit := new(mockAuditRepository)
		uc := newUsecase(users, new(mockSessionRepository), audit)

		user := existingUser(t, "promote_me", domain.RoleRequester, true)
		adminRole := domain.RoleAdmin

		users.On("GetByID", mock.Anything, user.ID).Return(user, nil)
		users.On("UpdateRole", mock.Anything, user.ID, adminRole).Return(nil)
		audit.On("Record", mock.Anything, mock.MatchedBy(func(e domain.AuditEvent) bool {
			return e.EventType == domain.AuditUserRoleChanged && e.ToStatus == string(domain.RoleAdmin)
		})).Return(nil)

		updated, err := uc.UpdateUser(context.Background(), adminActor, user.ID,
			usermgmt.UpdateUserInput{Role: &adminRole})
		require.NoError(t, err)
		assert.Equal(t, domain.RoleAdmin, updated.Role)
		users.AssertExpectations(t)
		audit.AssertExpectations(t)
	})

	t.Run("deactivation revokes sessions", func(t *testing.T) {
		users := new(mockUserRepository)
		sessions := new(mockSessionRepository)
		audit := new(mockAuditRepository)
		uc := newUsecase(users, sessions, audit)

		user := existingUser(t, "leaver", domain.RoleStorekeeper, true)

		users.On("GetByID", mock.Anything, user.ID).Return(user, nil)
		users.On("SetActive", mock.Anything, user.ID, false).Return(nil)
		sessions.On("RevokeAllForUser", mock.Anything, user.ID, mock.Anything).Return(nil)
		audit.On("Record", mock.Anything, mock.MatchedBy(func(e domain.AuditEvent) bool {
			return e.EventType == domain.AuditUserDeactivated
		})).Return(nil)

		updated, err := uc.UpdateUser(context.Background(), adminActor, user.ID,
			usermgmt.UpdateUserInput{IsActive: &deactivate})
		require.NoError(t, err)
		assert.False(t, updated.IsActive)
		sessions.AssertExpectations(t)
	})
}

func TestUserUsecase_ResetPassword(t *testing.T) {
	users := new(mockUserRepository)
	sessions := new(mockSessionRepository)
	audit := new(mockAuditRepository)
	uc := newUsecase(users, sessions, audit)

	user := existingUser(t, "forgetful", domain.RoleRequester, true)

	users.On("GetByID", mock.Anything, user.ID).Return(user, nil)
	users.On("UpdatePasswordHash", mock.Anything, user.ID, mock.MatchedBy(func(hash string) bool {
		return coreauth.NewHasher(coreauth.MinIterations).Verify(hash, "brand-new-pass")
	}), mock.Anything).Return(nil)
	sessions.On("RevokeAllForUser", mock.Anything, user.ID, mock.Anything).Return(nil)
	audit.On("Record", mock.Anything, mock.MatchedBy(func(e domain.AuditEvent) bool {
		return e.EventType == domain.AuditUserPasswordReset
	})).Return(nil)

	require.NoError(t, uc.ResetPassword(context.Background(), domain.Actor{ID: uuid.New()}, user.ID, "brand-new-pass"))

	// The password policy still applies to administrative resets.
	require.ErrorIs(t, uc.ResetPassword(context.Background(), domain.Actor{ID: uuid.New()}, user.ID, "short"), domain.ErrWeakPassword)

	users.AssertExpectations(t)
	sessions.AssertExpectations(t)
	audit.AssertExpectations(t)
}
