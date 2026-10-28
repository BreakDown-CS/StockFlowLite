package auth

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"time"

	coreauth "github.com/BreakDown-CS/StockFlowLite/internal/auth"
	"github.com/BreakDown-CS/StockFlowLite/internal/domain"
	"github.com/google/uuid"
)

// LoginInput carries the credentials and request metadata for authentication.
type LoginInput struct {
	Username  string
	Password  string
	IPAddress string
	UserAgent string
	RequestID string
}

// LoginResult is returned once on successful authentication.
// The raw token is never persisted (only its keyed hash is stored).
type LoginResult struct {
	Token     string       `json:"token"`
	TokenType string       `json:"token_type"`
	ExpiresAt time.Time    `json:"expires_at"`
	User      *domain.User `json:"user"`
}

// Config holds authentication policy that can be tuned from the environment.
type Config struct {
	TokenTTL      time.Duration
	Pepper        string
	MaxAttempts   int
	AttemptWindow time.Duration
	// TouchInterval throttles session last_used_at writes (one per interval at most).
	TouchInterval time.Duration
}

// AuthUsecase defines authentication and session operations.
type AuthUsecase interface {
	Login(ctx context.Context, input LoginInput) (*LoginResult, error)
	Logout(ctx context.Context, actor domain.Actor, sessionID uuid.UUID) error
	Me(ctx context.Context, actor domain.Actor) (*domain.User, error)
	ChangePassword(ctx context.Context, actor domain.Actor, currentPassword, newPassword string) error
	Authenticate(ctx context.Context, token string) (domain.Actor, *domain.AuthSession, error)
}

type authUsecase struct {
	users     domain.UserRepository
	sessions  domain.SessionRepository
	audit     domain.AuditRepository
	hasher    *coreauth.Hasher
	cfg       Config
	limiter   *loginLimiter
	dummyHash string
}

// NewAuthUsecase constructs the authentication use case.
func NewAuthUsecase(
	users domain.UserRepository,
	sessions domain.SessionRepository,
	audit domain.AuditRepository,
	hasher *coreauth.Hasher,
	cfg Config,
) AuthUsecase {
	if cfg.TokenTTL <= 0 {
		cfg.TokenTTL = 12 * time.Hour
	}
	if cfg.MaxAttempts <= 0 {
		cfg.MaxAttempts = 8
	}
	if cfg.AttemptWindow <= 0 {
		cfg.AttemptWindow = 15 * time.Minute
	}
	if cfg.TouchInterval <= 0 {
		cfg.TouchInterval = 5 * time.Minute
	}

	// A deterministic hash used to spend the same work when the username does not exist,
	// so login latency cannot be used to enumerate accounts.
	dummyHash, err := hasher.Hash("timing-equaliser-not-a-real-password")
	if err != nil {
		dummyHash = ""
	}

	return &authUsecase{
		users:     users,
		sessions:  sessions,
		audit:     audit,
		hasher:    hasher,
		cfg:       cfg,
		limiter:   newLoginLimiter(cfg.MaxAttempts, cfg.AttemptWindow),
		dummyHash: dummyHash,
	}
}

// Login verifies credentials and issues a new opaque bearer session token.
// Every failure returns the same ErrInvalidCredentials so accounts cannot be enumerated.
func (uc *authUsecase) Login(ctx context.Context, input LoginInput) (*LoginResult, error) {
	username := strings.TrimSpace(input.Username)
	if username == "" || input.Password == "" {
		return nil, domain.ErrInvalidCredentials
	}

	limitKey := loginLimitKey(username, input.IPAddress)
	if !uc.limiter.Allow(limitKey) {
		return nil, domain.ErrTooManyAttempts
	}

	user, err := uc.users.GetByUsername(ctx, username)
	if err != nil {
		if !errors.Is(err, domain.ErrUserNotFound) {
			slog.Error("Failed to load user during login", "error", err)
		}
		// Spend the same hashing work as a real verification so an unknown username
		// cannot be distinguished from a wrong password by response time.
		uc.hasher.Verify(uc.dummyHash, input.Password)
		uc.limiter.RegisterFailure(limitKey)
		uc.recordLoginFailure(ctx, username, input, "unknown-user")
		return nil, domain.ErrInvalidCredentials
	}

	if !uc.hasher.Verify(user.PasswordHash, input.Password) {
		uc.limiter.RegisterFailure(limitKey)
		uc.recordLoginFailure(ctx, username, input, "bad-password")
		return nil, domain.ErrInvalidCredentials
	}

	// The account state is only inspected after the credential check, so inactive
	// accounts cost the same as a wrong password.
	if !user.IsActive {
		uc.limiter.RegisterFailure(limitKey)
		uc.recordLoginFailure(ctx, username, input, "inactive-account")
		return nil, domain.ErrInvalidCredentials
	}

	uc.limiter.Reset(limitKey)

	token, tokenHash, err := coreauth.GenerateToken(uc.cfg.Pepper)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	session := &domain.AuthSession{
		UserID:    user.ID,
		TokenHash: tokenHash,
		CreatedAt: now,
		ExpiresAt: now.Add(uc.cfg.TokenTTL),
		IPAddress: input.IPAddress,
		UserAgent: input.UserAgent,
	}
	if err := uc.sessions.Create(ctx, session); err != nil {
		return nil, err
	}

	if err := uc.users.TouchLastLogin(ctx, user.ID, now); err != nil {
		slog.Warn("Failed to record last login timestamp", "user_id", user.ID.String(), "error", err)
	}

	uc.recordAudit(ctx, domain.AuditEvent{
		EventType:     domain.AuditAuthLoginSucceeded,
		ActorID:       &user.ID,
		ActorUsername: user.Username,
		ActorRole:     user.Role,
		EntityType:    domain.AuditEntityAuth,
		EntityID:      &session.ID,
		RequestID:     input.RequestID,
		IPAddress:     input.IPAddress,
		UserAgent:     input.UserAgent,
	})

	user.PasswordHash = ""
	user.LastLoginAt = &now

	return &LoginResult{
		Token:     token,
		TokenType: "Bearer",
		ExpiresAt: session.ExpiresAt,
		User:      user,
	}, nil
}

// Logout revokes the current session and records the security event.
func (uc *authUsecase) Logout(ctx context.Context, actor domain.Actor, sessionID uuid.UUID) error {
	if sessionID == uuid.Nil {
		return domain.ErrUnauthenticated
	}
	if err := uc.sessions.Revoke(ctx, sessionID, time.Now().UTC()); err != nil {
		return err
	}

	uc.recordAudit(ctx, domain.AuditEvent{
		EventType:     domain.AuditAuthLogout,
		ActorID:       &actor.ID,
		ActorUsername: actor.Username,
		ActorRole:     actor.Role,
		EntityType:    domain.AuditEntityAuth,
		EntityID:      &sessionID,
		RequestID:     actor.RequestID,
		IPAddress:     actor.IPAddress,
		UserAgent:     actor.UserAgent,
	})
	return nil
}

// Me returns the authenticated account without its credential material.
func (uc *authUsecase) Me(ctx context.Context, actor domain.Actor) (*domain.User, error) {
	if actor.IsZero() {
		return nil, domain.ErrUnauthenticated
	}

	user, err := uc.users.GetByID(ctx, actor.ID)
	if err != nil {
		return nil, err
	}
	if !user.IsActive {
		return nil, domain.ErrUserInactive
	}

	user.PasswordHash = ""
	return user, nil
}

// ChangePassword verifies the current password, stores a new hash and revokes all sessions.
func (uc *authUsecase) ChangePassword(ctx context.Context, actor domain.Actor, currentPassword, newPassword string) error {
	if actor.IsZero() {
		return domain.ErrUnauthenticated
	}

	user, err := uc.users.GetByID(ctx, actor.ID)
	if err != nil {
		return err
	}
	if !user.IsActive {
		return domain.ErrUserInactive
	}
	if !uc.hasher.Verify(user.PasswordHash, currentPassword) {
		return domain.ErrInvalidCredentials
	}
	if err := coreauth.ValidatePasswordPolicy(user.Username, newPassword); err != nil {
		return err
	}
	if uc.hasher.Verify(user.PasswordHash, newPassword) {
		return domain.ErrPasswordReused
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
		slog.Warn("Failed to revoke sessions after password change", "user_id", user.ID.String(), "error", err)
	}

	uc.recordAudit(ctx, domain.AuditEvent{
		EventType:     domain.AuditAuthPasswordChanged,
		ActorID:       &user.ID,
		ActorUsername: user.Username,
		ActorRole:     user.Role,
		EntityType:    domain.AuditEntityUser,
		EntityID:      &user.ID,
		RequestID:     actor.RequestID,
		IPAddress:     actor.IPAddress,
		UserAgent:     actor.UserAgent,
	})
	return nil
}

// Authenticate resolves a bearer token into a principal (used by the HTTP middleware).
func (uc *authUsecase) Authenticate(ctx context.Context, token string) (domain.Actor, *domain.AuthSession, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return domain.Actor{}, nil, domain.ErrUnauthenticated
	}

	session, user, err := uc.sessions.GetActiveByTokenHash(ctx, coreauth.HashToken(uc.cfg.Pepper, token))
	if err != nil {
		return domain.Actor{}, nil, err
	}
	if !user.IsActive {
		return domain.Actor{}, nil, domain.ErrUserInactive
	}

	// Throttle last_used_at writes: at most one UPDATE per configured interval per session.
	if session.LastUsedAt == nil || time.Since(*session.LastUsedAt) >= uc.cfg.TouchInterval {
		if err := uc.sessions.TouchLastUsed(ctx, session.ID, time.Now().UTC()); err != nil {
			slog.Debug("Failed to touch auth session", "session_id", session.ID.String(), "error", err)
		}
	}

	return domain.Actor{ID: user.ID, Username: user.Username, Role: user.Role}, session, nil
}

// recordLoginFailure audits a rejected authentication attempt (best effort).
func (uc *authUsecase) recordLoginFailure(ctx context.Context, username string, input LoginInput, reason string) {
	uc.recordAudit(ctx, domain.AuditEvent{
		EventType:     domain.AuditAuthLoginFailed,
		ActorUsername: username,
		ActorRole:     domain.UserRole("UNKNOWN"),
		EntityType:    domain.AuditEntityAuth,
		Reason:        reason,
		Metadata:      map[string]interface{}{"reason": reason},
		RequestID:     input.RequestID,
		IPAddress:     input.IPAddress,
		UserAgent:     input.UserAgent,
	})
}

// recordAudit writes a security event without ever failing the calling request.
func (uc *authUsecase) recordAudit(ctx context.Context, event domain.AuditEvent) {
	if uc.audit == nil {
		return
	}
	if err := uc.audit.Record(ctx, event); err != nil {
		slog.Warn("Failed to record audit event", "event_type", string(event.EventType), "error", err)
	}
}

// loginAttempt tracks consecutive failures for one username+IP pair.
type loginAttempt struct {
	failures     int
	firstFailure time.Time
	blockedUntil time.Time
}

// loginLimiter is a small in-memory brute-force guard for the login endpoint.
// It is intentionally process-local: a multi-instance deployment should move this
// state to a shared store (documented limitation).
type loginLimiter struct {
	mu          sync.Mutex
	attempts    map[string]*loginAttempt
	maxAttempts int
	window      time.Duration
}

func newLoginLimiter(maxAttempts int, window time.Duration) *loginLimiter {
	return &loginLimiter{
		attempts:    make(map[string]*loginAttempt),
		maxAttempts: maxAttempts,
		window:      window,
	}
}

// Allow reports whether a login attempt may proceed.
func (l *loginLimiter) Allow(key string) bool {
	now := time.Now()

	l.mu.Lock()
	defer l.mu.Unlock()

	attempt, ok := l.attempts[key]
	if !ok {
		return true
	}
	if now.Before(attempt.blockedUntil) {
		return false
	}
	if now.Sub(attempt.firstFailure) > l.window {
		delete(l.attempts, key)
	}
	return true
}

// RegisterFailure counts a failed attempt and blocks the key once the limit is reached.
func (l *loginLimiter) RegisterFailure(key string) {
	now := time.Now()

	l.mu.Lock()
	defer l.mu.Unlock()

	attempt, ok := l.attempts[key]
	if !ok || now.Sub(attempt.firstFailure) > l.window {
		l.attempts[key] = &loginAttempt{failures: 1, firstFailure: now}
		return
	}

	attempt.failures++
	if attempt.failures >= l.maxAttempts {
		attempt.blockedUntil = now.Add(l.window)
	}

	// Bound memory under a flood of distinct usernames/IPs.
	if len(l.attempts) > 10_000 {
		for existingKey, existing := range l.attempts {
			if now.Sub(existing.firstFailure) > l.window && now.After(existing.blockedUntil) {
				delete(l.attempts, existingKey)
			}
		}
	}
}

// Reset clears the failure counter after a successful login.
func (l *loginLimiter) Reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.attempts, key)
}

func loginLimitKey(username, ipAddress string) string {
	return strings.ToLower(strings.TrimSpace(username)) + "|" + ipAddress
}
