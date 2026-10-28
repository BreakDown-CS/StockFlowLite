package auth_test

import (
	"context"
	"testing"
	"time"

	coreauth "github.com/BreakDown-CS/StockFlowLite/internal/auth"
	"github.com/BreakDown-CS/StockFlowLite/internal/domain"
	authusecase "github.com/BreakDown-CS/StockFlowLite/internal/usecase/auth"
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
	s.ID = uuid.New()
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

const testPepper = "unit-test-pepper-value"

func newTestUsecase(t *testing.T, users *mockUserRepository, sessions *mockSessionRepository, audit *mockAuditRepository) authusecase.AuthUsecase {
	t.Helper()
	return authusecase.NewAuthUsecase(users, sessions, audit, coreauth.NewHasher(coreauth.MinIterations), authusecase.Config{
		TokenTTL:      time.Hour,
		Pepper:        testPepper,
		MaxAttempts:   3,
		AttemptWindow: time.Minute,
	})
}

func activeUser(t *testing.T, username, password string, role domain.UserRole) *domain.User {
	t.Helper()

	hash, err := coreauth.NewHasher(coreauth.MinIterations).Hash(password)
	require.NoError(t, err)

	user, err := domain.NewUser(username, hash, role)
	require.NoError(t, err)
	return user
}

func TestAuthUsecase_Login_Success(t *testing.T) {
	users := new(mockUserRepository)
	sessions := new(mockSessionRepository)
	audit := new(mockAuditRepository)
	uc := newTestUsecase(t, users, sessions, audit)

	user := activeUser(t, "alice", "s3cret-pass", domain.RoleStorekeeper)

	users.On("GetByUsername", mock.Anything, "alice").Return(user, nil)
	sessions.On("Create", mock.Anything, mock.MatchedBy(func(s *domain.AuthSession) bool {
		return s.UserID == user.ID && len(s.TokenHash) == 64 && s.ExpiresAt.After(s.CreatedAt)
	})).Return(nil)
	users.On("TouchLastLogin", mock.Anything, user.ID, mock.Anything).Return(nil)
	audit.On("Record", mock.Anything, mock.MatchedBy(func(e domain.AuditEvent) bool {
		return e.EventType == domain.AuditAuthLoginSucceeded && e.ActorUsername == "alice"
	})).Return(nil)

	result, err := uc.Login(context.Background(), authusecase.LoginInput{
		Username:  "  alice  ",
		Password:  "s3cret-pass",
		IPAddress: "127.0.0.1",
		UserAgent: "go-test",
	})
	require.NoError(t, err)
	assert.NotEmpty(t, result.Token)
	assert.Equal(t, "Bearer", result.TokenType)
	assert.True(t, result.ExpiresAt.After(time.Now()))
	assert.Equal(t, "alice", result.User.Username)
	assert.Empty(t, result.User.PasswordHash, "credential material must never be returned")
	users.AssertExpectations(t)
	sessions.AssertExpectations(t)
	audit.AssertExpectations(t)
}

func TestAuthUsecase_Login_RejectsBadCredentials(t *testing.T) {
	cases := []struct {
		name     string
		user     func(t *testing.T) (*domain.User, string)
		username string
	}{
		{
			name:     "wrong password",
			username: "bob",
			user: func(t *testing.T) (*domain.User, string) {
				return activeUser(t, "bob", "correct-pass", domain.RoleRequester), "wrong-pass"
			},
		},
		{
			name:     "unknown user",
			username: "ghost",
			user:     func(t *testing.T) (*domain.User, string) { return nil, "any-pass" },
		},
		{
			name:     "inactive account",
			username: "disabled",
			user: func(t *testing.T) (*domain.User, string) {
				user := activeUser(t, "disabled", "correct-pass", domain.RoleRequester)
				user.IsActive = false
				return user, "correct-pass"
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			users := new(mockUserRepository)
			sessions := new(mockSessionRepository)
			audit := new(mockAuditRepository)
			uc := newTestUsecase(t, users, sessions, audit)

			user, password := tc.user(t)
			if user == nil {
				users.On("GetByUsername", mock.Anything, tc.username).Return(nil, domain.ErrUserNotFound)
			} else {
				users.On("GetByUsername", mock.Anything, tc.username).Return(user, nil)
			}
			audit.On("Record", mock.Anything, mock.MatchedBy(func(e domain.AuditEvent) bool {
				return e.EventType == domain.AuditAuthLoginFailed
			})).Return(nil)

			_, err := uc.Login(context.Background(), authusecase.LoginInput{
				Username: tc.username,
				Password: password,
			})
			require.ErrorIs(t, err, domain.ErrInvalidCredentials)
			sessions.AssertNotCalled(t, "Create", mock.Anything, mock.Anything)
			audit.AssertExpectations(t)
		})
	}
}

func TestAuthUsecase_Login_RateLimited(t *testing.T) {
	users := new(mockUserRepository)
	sessions := new(mockSessionRepository)
	audit := new(mockAuditRepository)
	uc := newTestUsecase(t, users, sessions, audit)

	user := activeUser(t, "carol", "correct-pass", domain.RoleRequester)
	users.On("GetByUsername", mock.Anything, "carol").Return(user, nil)
	audit.On("Record", mock.Anything, mock.Anything).Return(nil)

	input := authusecase.LoginInput{Username: "carol", Password: "wrong-pass", IPAddress: "10.0.0.1"}
	for i := 0; i < 3; i++ {
		_, err := uc.Login(context.Background(), input)
		require.ErrorIs(t, err, domain.ErrInvalidCredentials)
	}

	_, err := uc.Login(context.Background(), input)
	require.ErrorIs(t, err, domain.ErrTooManyAttempts)
}

func TestAuthUsecase_Authenticate(t *testing.T) {
	users := new(mockUserRepository)
	sessions := new(mockSessionRepository)
	audit := new(mockAuditRepository)
	uc := newTestUsecase(t, users, sessions, audit)

	user := activeUser(t, "dana", "s3cret-pass", domain.RoleStorekeeper)
	session := &domain.AuthSession{ID: uuid.New(), UserID: user.ID, ExpiresAt: time.Now().Add(time.Hour)}

	sessions.On("GetActiveByTokenHash", mock.Anything, coreauth.HashToken(testPepper, "token-abc")).
		Return(session, user, nil)
	sessions.On("TouchLastUsed", mock.Anything, session.ID, mock.Anything).Return(nil)

	actor, resolved, err := uc.Authenticate(context.Background(), " token-abc ")
	require.NoError(t, err)
	assert.Equal(t, user.ID, actor.ID)
	assert.Equal(t, "dana", actor.Username)
	assert.Equal(t, domain.RoleStorekeeper, actor.Role)
	require.NotNil(t, resolved)
	assert.Equal(t, session.ID, resolved.ID)

	// Empty tokens never reach the repository.
	_, _, err = uc.Authenticate(context.Background(), "   ")
	require.ErrorIs(t, err, domain.ErrUnauthenticated)

	// Disabled accounts lose access even with a valid session.
	inactive := *user
	inactive.IsActive = false
	sessions.On("GetActiveByTokenHash", mock.Anything, coreauth.HashToken(testPepper, "token-disabled")).
		Return(session, &inactive, nil)

	_, _, err = uc.Authenticate(context.Background(), "token-disabled")
	require.ErrorIs(t, err, domain.ErrUserInactive)
}

func TestAuthUsecase_LogoutRevokesSession(t *testing.T) {
	users := new(mockUserRepository)
	sessions := new(mockSessionRepository)
	audit := new(mockAuditRepository)
	uc := newTestUsecase(t, users, sessions, audit)

	actor := domain.Actor{ID: uuid.New(), Username: "erin", Role: domain.RoleRequester}
	sessionID := uuid.New()

	sessions.On("Revoke", mock.Anything, sessionID, mock.Anything).Return(nil)
	audit.On("Record", mock.Anything, mock.MatchedBy(func(e domain.AuditEvent) bool {
		return e.EventType == domain.AuditAuthLogout && e.ActorUsername == "erin"
	})).Return(nil)

	require.NoError(t, uc.Logout(context.Background(), actor, sessionID))
	sessions.AssertExpectations(t)
	audit.AssertExpectations(t)

	// A missing session id is rejected.
	require.ErrorIs(t, uc.Logout(context.Background(), actor, uuid.Nil), domain.ErrUnauthenticated)
}

func TestAuthUsecase_ChangePassword_RejectsReuse(t *testing.T) {
	users := new(mockUserRepository)
	sessions := new(mockSessionRepository)
	audit := new(mockAuditRepository)
	uc := newTestUsecase(t, users, sessions, audit)

	user := activeUser(t, "grace", "current-pass", domain.RoleRequester)
	users.On("GetByID", mock.Anything, user.ID).Return(user, nil)

	err := uc.ChangePassword(context.Background(),
		domain.Actor{ID: user.ID, Username: user.Username}, "current-pass", "current-pass")
	require.ErrorIs(t, err, domain.ErrPasswordReused)
	users.AssertNotCalled(t, "UpdatePasswordHash", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

func TestAuthUsecase_Authenticate_ThrottlesLastUsedWrites(t *testing.T) {
	users := new(mockUserRepository)
	sessions := new(mockSessionRepository)
	audit := new(mockAuditRepository)
	uc := newTestUsecase(t, users, sessions, audit)

	user := activeUser(t, "heidi", "s3cret-pass", domain.RoleRequester)
	recent := time.Now().UTC().Add(-time.Minute)
	session := &domain.AuthSession{
		ID:         uuid.New(),
		UserID:     user.ID,
		ExpiresAt:  time.Now().Add(time.Hour),
		LastUsedAt: &recent,
	}

	sessions.On("GetActiveByTokenHash", mock.Anything, coreauth.HashToken(testPepper, "token-recent")).
		Return(session, user, nil)

	_, _, err := uc.Authenticate(context.Background(), "token-recent")
	require.NoError(t, err)
	sessions.AssertNotCalled(t, "TouchLastUsed", mock.Anything, mock.Anything, mock.Anything)
}

func TestAuthUsecase_ChangePassword(t *testing.T) {
	user := activeUser(t, "frank", "current-pass", domain.RoleRequester)

	t.Run("rejects a wrong current password", func(t *testing.T) {
		users := new(mockUserRepository)
		sessions := new(mockSessionRepository)
		audit := new(mockAuditRepository)
		uc := newTestUsecase(t, users, sessions, audit)

		users.On("GetByID", mock.Anything, user.ID).Return(user, nil)

		err := uc.ChangePassword(context.Background(), domain.Actor{ID: user.ID, Username: user.Username}, "wrong-pass", "new-password-1")
		require.ErrorIs(t, err, domain.ErrInvalidCredentials)
		users.AssertNotCalled(t, "UpdatePasswordHash", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	})

	t.Run("enforces the password policy", func(t *testing.T) {
		users := new(mockUserRepository)
		sessions := new(mockSessionRepository)
		audit := new(mockAuditRepository)
		uc := newTestUsecase(t, users, sessions, audit)

		users.On("GetByID", mock.Anything, user.ID).Return(user, nil)

		err := uc.ChangePassword(context.Background(), domain.Actor{ID: user.ID, Username: user.Username}, "current-pass", "short")
		require.ErrorIs(t, err, domain.ErrWeakPassword)
	})

	t.Run("updates the hash and revokes sessions", func(t *testing.T) {
		users := new(mockUserRepository)
		sessions := new(mockSessionRepository)
		audit := new(mockAuditRepository)
		uc := newTestUsecase(t, users, sessions, audit)

		users.On("GetByID", mock.Anything, user.ID).Return(user, nil)
		users.On("UpdatePasswordHash", mock.Anything, user.ID, mock.MatchedBy(func(hash string) bool {
			return coreauth.NewHasher(coreauth.MinIterations).Verify(hash, "new-password-1")
		}), mock.Anything).Return(nil)
		sessions.On("RevokeAllForUser", mock.Anything, user.ID, mock.Anything).Return(nil)
		audit.On("Record", mock.Anything, mock.MatchedBy(func(e domain.AuditEvent) bool {
			return e.EventType == domain.AuditAuthPasswordChanged
		})).Return(nil)

		err := uc.ChangePassword(context.Background(), domain.Actor{ID: user.ID, Username: user.Username}, "current-pass", "new-password-1")
		require.NoError(t, err)
		users.AssertExpectations(t)
		sessions.AssertExpectations(t)
		audit.AssertExpectations(t)
	})
}
