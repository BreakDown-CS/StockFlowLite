package middleware

import (
	"errors"
	"log/slog"
	"strings"

	"github.com/BreakDown-CS/StockFlowLite/internal/domain"
	authusecase "github.com/BreakDown-CS/StockFlowLite/internal/usecase/auth"
	"github.com/BreakDown-CS/StockFlowLite/pkg/response"
	"github.com/gofiber/fiber/v2"
)

// Locals keys used to share the authenticated principal with handlers.
const (
	AuthContextKey    = "auth.user"
	SessionContextKey = "auth.session"
)

// RequireAuth validates the bearer token, resolves the principal and stores it both in
// Fiber locals (for handlers) and in the request context (for repositories/usecases).
func RequireAuth(uc authusecase.AuthUsecase) fiber.Handler {
	return func(c *fiber.Ctx) error {
		header := strings.TrimSpace(c.Get("Authorization"))
		if header == "" {
			return unauthorized(c)
		}

		parts := strings.Fields(header)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			return unauthorized(c)
		}

		actor, session, err := uc.Authenticate(c.UserContext(), parts[1])
		if err != nil {
			if !errors.Is(err, domain.ErrUnauthenticated) && !errors.Is(err, domain.ErrUserInactive) {
				slog.Error("Authentication lookup failed", "error", err)
			}
			return unauthorized(c)
		}

		actor.RequestID = requestID(c)
		actor.IPAddress = c.IP()
		actor.UserAgent = c.Get("User-Agent")

		c.Locals(AuthContextKey, actor)
		c.Locals(SessionContextKey, session)
		c.SetUserContext(domain.WithActor(c.UserContext(), actor))

		return c.Next()
	}
}

// RequireRoles allows only the given roles. RequireAuth must run before it.
// Denied attempts are audited on a best-effort basis (they never change business state).
func RequireRoles(audit domain.AuditRepository, roles ...domain.UserRole) fiber.Handler {
	return func(c *fiber.Ctx) error {
		actor, ok := CurrentActor(c)
		if !ok {
			return unauthorized(c)
		}
		if actor.HasRole(roles...) {
			return c.Next()
		}

		slog.Warn("Access denied",
			"user_id", actor.ID.String(),
			"role", string(actor.Role),
			"path", c.Path(),
		)

		if audit != nil {
			actorID := actor.ID
			event := domain.AuditEvent{
				EventType:     domain.AuditAccessDenied,
				ActorID:       &actorID,
				ActorUsername: actor.Username,
				ActorRole:     actor.Role,
				EntityType:    domain.AuditEntityAuth,
				Reference:     c.Path(),
				Reason:        "role not permitted",
				RequestID:     actor.RequestID,
				IPAddress:     actor.IPAddress,
				UserAgent:     actor.UserAgent,
			}
			if err := audit.Record(c.UserContext(), event); err != nil {
				slog.Warn("Failed to audit access denial", "error", err)
			}
		}

		return response.Error(c, fiber.StatusForbidden, "FORBIDDEN", "Insufficient permissions for this operation")
	}
}

// CurrentActor returns the principal stored by RequireAuth.
func CurrentActor(c *fiber.Ctx) (domain.Actor, bool) {
	actor, ok := c.Locals(AuthContextKey).(domain.Actor)
	return actor, ok
}

// CurrentSession returns the session stored by RequireAuth.
func CurrentSession(c *fiber.Ctx) (*domain.AuthSession, bool) {
	session, ok := c.Locals(SessionContextKey).(*domain.AuthSession)
	return session, ok
}

func unauthorized(c *fiber.Ctx) error {
	return response.Error(c, fiber.StatusUnauthorized, "UNAUTHORIZED", "Authentication required")
}

// RequestID returns the correlation id assigned by the requestid middleware.
func RequestID(c *fiber.Ctx) string {
	return requestID(c)
}

func requestID(c *fiber.Ctx) string {
	if id, ok := c.Locals("requestid").(string); ok {
		return id
	}
	return ""
}
