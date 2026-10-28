package user

import (
	"context"
	"log/slog"
	"strings"
	"time"

	coreauth "github.com/BreakDown-CS/StockFlowLite/internal/auth"
	"github.com/BreakDown-CS/StockFlowLite/internal/domain"
	"github.com/google/uuid"
)

// CreateUserInput holds the data for provisioning a new account.
type CreateUserInput struct {
	Username string
	Password string
	Role     domain.UserRole
}

// UpdateUserInput holds the mutable attributes of an account.
type UpdateUserInput struct {
	Role     *domain.UserRole
	IsActive *bool
}

// ListUsersInput defines query constraints for user listings.
type ListUsersInput struct {
	Search   string
	Role     *domain.UserRole
	IsActive *bool
	Limit    int
	Offset   int
}

// UserUsecase defines administrative account operations (ADMIN only).
type UserUsecase interface {
	CreateUser(ctx context.Context, input CreateUserInput) (*domain.User, error)
	GetUser(ctx context.Context, id uuid.UUID) (*domain.User, error)
	ListUsers(ctx context.Context, input ListUsersInput) ([]domain.User, int64, error)
	UpdateUser(ctx context.Context, actor domain.Actor, id uuid.UUID, input UpdateUserInput) (*domain.User, error)
	ResetPassword(ctx context.Context, actor domain.Actor, id uuid.UUID, newPassword string) error
}

type userUsecase struct {
	users    domain.UserRepository
	sessions domain.SessionRepository
	audit    domain.AuditRepository
	hasher   *coreauth.Hasher
}

// NewUserUsecase constructs the administrative user use case.
func NewUserUsecase(
	users domain.UserRepository,
	sessions domain.SessionRepository,
	audit domain.AuditRepository,
	hasher *coreauth.Hasher,
) UserUsecase {
	return &userUsecase{users: users, sessions: sessions, audit: audit, hasher: hasher}
}

// CreateUser hashes the password and persists a new active account.
func (uc *userUsecase) CreateUser(ctx context.Context, input CreateUserInput) (*domain.User, error) {
	username := strings.TrimSpace(input.Username)
	if input.Role == "" {
		return nil, domain.ErrInvalidRole
	}
	if err := coreauth.ValidatePasswordPolicy(username, input.Password); err != nil {
		return nil, err
	}

	hash, err := uc.hasher.Hash(input.Password)
	if err != nil {
		return nil, err
	}

	user, err := domain.NewUser(username, hash, input.Role)
	if err != nil {
		return nil, err
	}

	if err := uc.users.Create(ctx, user); err != nil {
		return nil, err
	}

	uc.recordAudit(ctx, domain.AuditEvent{
		EventType:  domain.AuditUserCreated,
		EntityType: domain.AuditEntityUser,
		EntityID:   &user.ID,
		Reference:  user.Username,
		ToStatus:   string(user.Role),
	})

	user.PasswordHash = ""
	return user, nil
}

// GetUser loads a single account.
func (uc *userUsecase) GetUser(ctx context.Context, id uuid.UUID) (*domain.User, error) {
	if id == uuid.Nil {
		return nil, domain.ErrUserNotFound
	}
	user, err := uc.users.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	user.PasswordHash = ""
	return user, nil
}

// ListUsers applies safe pagination and returns accounts without credential material.
func (uc *userUsecase) ListUsers(ctx context.Context, input ListUsersInput) ([]domain.User, int64, error) {
	limit := input.Limit
	if limit <= 0 {
		limit = 20
	} else if limit > 100 {
		limit = 100
	}
	offset := input.Offset
	if offset < 0 {
		offset = 0
	}

	users, total, err := uc.users.List(ctx, domain.ListUserFilter{
		Search:   strings.TrimSpace(input.Search),
		Role:     input.Role,
		IsActive: input.IsActive,
		Limit:    limit,
		Offset:   offset,
	})
	if err != nil {
		return nil, 0, err
	}

	for i := range users {
		users[i].PasswordHash = ""
	}
	return users, total, nil
}

// UpdateUser changes the role and/or the active flag, protecting the last administrator
// and preventing an administrator from disabling their own account.
func (uc *userUsecase) UpdateUser(ctx context.Context, actor domain.Actor, id uuid.UUID, input UpdateUserInput) (*domain.User, error) {
	user, err := uc.users.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	demotingAdmin := false
	if input.Role != nil && *input.Role != user.Role && user.Role == domain.RoleAdmin && *input.Role != domain.RoleAdmin {
		demotingAdmin = true
	}
	deactivatingAdmin := false
	if input.IsActive != nil && !*input.IsActive && user.IsActive && user.Role == domain.RoleAdmin {
		deactivatingAdmin = true
	}

	if deactivatingAdmin && actor.ID == user.ID {
		return nil, domain.ErrForbidden
	}

	if demotingAdmin || deactivatingAdmin {
		activeAdmins, err := uc.users.CountActiveAdmins(ctx)
		if err != nil {
			return nil, err
		}
		if activeAdmins <= 1 {
			return nil, domain.ErrLastAdmin
		}
	}

	previousRole := user.Role
	previousActive := user.IsActive

	if input.Role != nil && *input.Role != user.Role {
		if err := uc.users.UpdateRole(ctx, user.ID, *input.Role); err != nil {
			return nil, err
		}
		user.Role = *input.Role
		uc.recordAudit(ctx, domain.AuditEvent{
			EventType:  domain.AuditUserRoleChanged,
			EntityType: domain.AuditEntityUser,
			EntityID:   &user.ID,
			Reference:  user.Username,
			FromStatus: string(previousRole),
			ToStatus:   string(user.Role),
		})
	}

	if input.IsActive != nil && *input.IsActive != user.IsActive {
		if err := uc.users.SetActive(ctx, user.ID, *input.IsActive); err != nil {
			return nil, err
		}
		user.IsActive = *input.IsActive
		user.UpdatedAt = time.Now().UTC()

		eventType := domain.AuditUserDeactivated
		if user.IsActive {
			eventType = domain.AuditUserActivated
		}
		uc.recordAudit(ctx, domain.AuditEvent{
			EventType:  eventType,
			EntityType: domain.AuditEntityUser,
			EntityID:   &user.ID,
			Reference:  user.Username,
			FromStatus: boolStatus(previousActive),
			ToStatus:   boolStatus(user.IsActive),
		})

		// Disabled accounts must lose access immediately.
		if !user.IsActive {
			if err := uc.sessions.RevokeAllForUser(ctx, user.ID, time.Now().UTC()); err != nil {
				slog.Warn("Failed to revoke sessions of disabled user", "user_id", user.ID.String(), "error", err)
			}
		}
	}

	user.PasswordHash = ""
	return user, nil
}

// ResetPassword replaces an account password (ADMIN) and revokes its sessions.
func (uc *userUsecase) ResetPassword(ctx context.Context, actor domain.Actor, id uuid.UUID, newPassword string) error {
	user, err := uc.users.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if err := coreauth.ValidatePasswordPolicy(user.Username, newPassword); err != nil {
		return err
	}

	hash, err := uc.hasher.Hash(newPassword)
	if err != nil {
		return err
	}

	now := time.Now().UTC()
	if err := uc.users.UpdatePasswordHash(ctx, user.ID, hash, now); err != nil {
		return err
	}
	if err := uc.sessions.RevokeAllForUser(ctx, user.ID, now); err != nil {
		slog.Warn("Failed to revoke sessions after admin password reset", "user_id", user.ID.String(), "error", err)
	}

	uc.recordAudit(ctx, domain.AuditEvent{
		EventType:  domain.AuditUserPasswordReset,
		EntityType: domain.AuditEntityUser,
		EntityID:   &user.ID,
		Reference:  user.Username,
	})

	return nil
}

func (uc *userUsecase) recordAudit(ctx context.Context, event domain.AuditEvent) {
	if actor, ok := domain.ActorFromContext(ctx); ok {
		actorID := actor.ID
		event.ActorID = &actorID
		event.ActorUsername = actor.Username
		event.ActorRole = actor.Role
		event.RequestID = actor.RequestID
		event.IPAddress = actor.IPAddress
		event.UserAgent = actor.UserAgent
	}
	if uc.audit == nil {
		return
	}
	if err := uc.audit.Record(ctx, event); err != nil {
		slog.Warn("Failed to record audit event", "event_type", string(event.EventType), "error", err)
	}
}

func boolStatus(active bool) string {
	if active {
		return "ACTIVE"
	}
	return "INACTIVE"
}
