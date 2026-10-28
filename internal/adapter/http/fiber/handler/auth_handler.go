package handler

import (
	"errors"
	"log/slog"
	"strings"

	"github.com/BreakDown-CS/StockFlowLite/internal/adapter/http/fiber/middleware"
	"github.com/BreakDown-CS/StockFlowLite/internal/domain"
	authusecase "github.com/BreakDown-CS/StockFlowLite/internal/usecase/auth"
	"github.com/BreakDown-CS/StockFlowLite/pkg/response"
	"github.com/gofiber/fiber/v2"
)

// AuthHandler manages authentication endpoints.
type AuthHandler struct {
	usecase authusecase.AuthUsecase
}

// NewAuthHandler constructs an AuthHandler.
func NewAuthHandler(usecase authusecase.AuthUsecase) *AuthHandler {
	return &AuthHandler{usecase: usecase}
}

// LoginRequest is the credential payload.
type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// ChangePasswordRequest is the self-service password change payload.
type ChangePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

// Login handles POST /api/v1/auth/login
func (h *AuthHandler) Login(c *fiber.Ctx) error {
	var req LoginRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, fiber.StatusBadRequest, "BAD_REQUEST", "Malformed request payload")
	}

	username := strings.TrimSpace(req.Username)
	if username == "" || req.Password == "" {
		return response.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "username and password are required")
	}

	result, err := h.usecase.Login(c.UserContext(), authusecase.LoginInput{
		Username:  username,
		Password:  req.Password,
		IPAddress: c.IP(),
		UserAgent: c.Get("User-Agent"),
		RequestID: middleware.RequestID(c),
	})
	if err != nil {
		return respondAuthError(c, err)
	}

	return response.OK(c, result, "Login successful")
}

// Logout handles POST /api/v1/auth/logout
func (h *AuthHandler) Logout(c *fiber.Ctx) error {
	actor, ok := middleware.CurrentActor(c)
	if !ok {
		return response.Error(c, fiber.StatusUnauthorized, "UNAUTHORIZED", "Authentication required")
	}
	session, ok := middleware.CurrentSession(c)
	if !ok {
		return response.Error(c, fiber.StatusUnauthorized, "UNAUTHORIZED", "Authentication required")
	}

	if err := h.usecase.Logout(c.UserContext(), actor, session.ID); err != nil {
		return respondAuthError(c, err)
	}

	return response.OK(c, nil, "Logout successful")
}

// Me handles GET /api/v1/auth/me
func (h *AuthHandler) Me(c *fiber.Ctx) error {
	actor, ok := middleware.CurrentActor(c)
	if !ok {
		return response.Error(c, fiber.StatusUnauthorized, "UNAUTHORIZED", "Authentication required")
	}

	user, err := h.usecase.Me(c.UserContext(), actor)
	if err != nil {
		return respondAuthError(c, err)
	}

	return response.OK(c, user)
}

// ChangePassword handles POST /api/v1/auth/change-password
// All sessions are revoked afterwards, so the client must authenticate again.
func (h *AuthHandler) ChangePassword(c *fiber.Ctx) error {
	actor, ok := middleware.CurrentActor(c)
	if !ok {
		return response.Error(c, fiber.StatusUnauthorized, "UNAUTHORIZED", "Authentication required")
	}

	var req ChangePasswordRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, fiber.StatusBadRequest, "BAD_REQUEST", "Malformed request payload")
	}
	if strings.TrimSpace(req.CurrentPassword) == "" || strings.TrimSpace(req.NewPassword) == "" {
		return response.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "current_password and new_password are required")
	}

	if err := h.usecase.ChangePassword(c.UserContext(), actor, req.CurrentPassword, req.NewPassword); err != nil {
		return respondAuthError(c, err)
	}

	return response.OK(c, nil, "Password changed successfully; please sign in again")
}

// respondAuthError maps authentication failures onto the established envelope.
func respondAuthError(c *fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, domain.ErrInvalidCredentials):
		return response.Error(c, fiber.StatusUnauthorized, "INVALID_CREDENTIALS", "Invalid username or password")
	case errors.Is(err, domain.ErrTooManyAttempts):
		return response.Error(c, fiber.StatusTooManyRequests, "TOO_MANY_ATTEMPTS", domain.ErrTooManyAttempts.Error())
	case errors.Is(err, domain.ErrUnauthenticated), errors.Is(err, domain.ErrUserInactive):
		return response.Error(c, fiber.StatusUnauthorized, "UNAUTHORIZED", "Authentication required")
	case errors.Is(err, domain.ErrWeakPassword):
		return response.Error(c, fiber.StatusBadRequest, "WEAK_PASSWORD", domain.ErrWeakPassword.Error())
	case errors.Is(err, domain.ErrUserNotFound):
		return response.Error(c, fiber.StatusNotFound, "NOT_FOUND", "User not found")
	case errors.Is(err, domain.ErrForbidden):
		return response.Error(c, fiber.StatusForbidden, "FORBIDDEN", domain.ErrForbidden.Error())
	default:
		slog.Error("Authentication operation failed", "error", err)
		return response.Error(c, fiber.StatusInternalServerError, "INTERNAL_ERROR", "Authentication failed")
	}
}
