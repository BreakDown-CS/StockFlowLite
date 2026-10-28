package handler

import (
	"errors"
	"log/slog"
	"strconv"
	"strings"

	"github.com/BreakDown-CS/StockFlowLite/internal/adapter/http/fiber/middleware"
	"github.com/BreakDown-CS/StockFlowLite/internal/domain"
	usermgmt "github.com/BreakDown-CS/StockFlowLite/internal/usecase/user"
	"github.com/BreakDown-CS/StockFlowLite/pkg/response"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

// UserHandler manages administrative account endpoints (ADMIN only).
type UserHandler struct {
	usecase usermgmt.UserUsecase
}

// NewUserHandler constructs a UserHandler.
func NewUserHandler(usecase usermgmt.UserUsecase) *UserHandler {
	return &UserHandler{usecase: usecase}
}

// CreateUserRequest is the payload for provisioning an account.
type CreateUserRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Role     string `json:"role"`
}

// UpdateUserRequest is the payload for changing a role and/or the active flag.
type UpdateUserRequest struct {
	Role     *string `json:"role"`
	IsActive *bool   `json:"is_active"`
}

// ResetPasswordRequest is the payload for an administrative password reset.
type ResetPasswordRequest struct {
	NewPassword string `json:"new_password"`
}

// Create handles POST /api/v1/users
func (h *UserHandler) Create(c *fiber.Ctx) error {
	var req CreateUserRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, fiber.StatusBadRequest, "BAD_REQUEST", "Malformed request payload")
	}

	role := domain.UserRole(strings.ToUpper(strings.TrimSpace(req.Role)))
	valid := false
	for _, candidate := range domain.ValidRoles {
		if role == candidate {
			valid = true
			break
		}
	}
	if !valid {
		return response.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "role must be ADMIN, STOREKEEPER, or REQUESTER")
	}

	user, err := h.usecase.CreateUser(c.UserContext(), usermgmt.CreateUserInput{
		Username: strings.TrimSpace(req.Username),
		Password: req.Password,
		Role:     role,
	})
	if err != nil {
		return respondUserError(c, err)
	}

	return response.Created(c, user, "User created successfully")
}

// List handles GET /api/v1/users
func (h *UserHandler) List(c *fiber.Ctx) error {
	limit := 20
	if limitStr := c.Query("limit"); limitStr != "" {
		parsed, err := strconv.Atoi(limitStr)
		if err != nil || parsed <= 0 {
			return response.Error(c, fiber.StatusBadRequest, "INVALID_PARAMETER", "limit must be a positive integer")
		}
		limit = parsed
	}
	if limit > 100 {
		limit = 100
	}

	offset := 0
	if offsetStr := c.Query("offset"); offsetStr != "" {
		parsed, err := strconv.Atoi(offsetStr)
		if err != nil || parsed < 0 {
			return response.Error(c, fiber.StatusBadRequest, "INVALID_PARAMETER", "offset must be a non-negative integer")
		}
		offset = parsed
	}

	var role *domain.UserRole
	if roleStr := strings.TrimSpace(c.Query("role")); roleStr != "" {
		parsed := domain.UserRole(strings.ToUpper(roleStr))
		valid := false
		for _, candidate := range domain.ValidRoles {
			if parsed == candidate {
				valid = true
				break
			}
		}
		if !valid {
			return response.Error(c, fiber.StatusBadRequest, "INVALID_PARAMETER", "role must be ADMIN, STOREKEEPER, or REQUESTER")
		}
		role = &parsed
	}

	var isActive *bool
	if activeStr := strings.TrimSpace(c.Query("is_active")); activeStr != "" {
		switch strings.ToLower(activeStr) {
		case "true":
			value := true
			isActive = &value
		case "false":
			value := false
			isActive = &value
		default:
			return response.Error(c, fiber.StatusBadRequest, "INVALID_PARAMETER", "is_active must be true or false")
		}
	}

	users, total, err := h.usecase.ListUsers(c.UserContext(), usermgmt.ListUsersInput{
		Search:   c.Query("search"),
		Role:     role,
		IsActive: isActive,
		Limit:    limit,
		Offset:   offset,
	})
	if err != nil {
		return respondUserError(c, err)
	}

	return response.SuccessWithMeta(c, users, response.PageMeta{Limit: limit, Offset: offset, Total: total})
}

// GetByID handles GET /api/v1/users/:id
func (h *UserHandler) GetByID(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return response.Error(c, fiber.StatusBadRequest, "INVALID_ID", "Invalid user ID format")
	}

	user, err := h.usecase.GetUser(c.UserContext(), id)
	if err != nil {
		return respondUserError(c, err)
	}

	return response.OK(c, user)
}

// Update handles PATCH /api/v1/users/:id
func (h *UserHandler) Update(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return response.Error(c, fiber.StatusBadRequest, "INVALID_ID", "Invalid user ID format")
	}

	actor, ok := middleware.CurrentActor(c)
	if !ok {
		return response.Error(c, fiber.StatusUnauthorized, "UNAUTHORIZED", "Authentication required")
	}

	var req UpdateUserRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, fiber.StatusBadRequest, "BAD_REQUEST", "Malformed request payload")
	}
	if req.Role == nil && req.IsActive == nil {
		return response.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "role or is_active is required")
	}

	input := usermgmt.UpdateUserInput{IsActive: req.IsActive}
	if req.Role != nil {
		role := domain.UserRole(strings.ToUpper(strings.TrimSpace(*req.Role)))
		valid := false
		for _, candidate := range domain.ValidRoles {
			if role == candidate {
				valid = true
				break
			}
		}
		if !valid {
			return response.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "role must be ADMIN, STOREKEEPER, or REQUESTER")
		}
		input.Role = &role
	}

	user, err := h.usecase.UpdateUser(c.UserContext(), actor, id, input)
	if err != nil {
		return respondUserError(c, err)
	}

	return response.OK(c, user, "User updated successfully")
}

// ResetPassword handles POST /api/v1/users/:id/reset-password
func (h *UserHandler) ResetPassword(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return response.Error(c, fiber.StatusBadRequest, "INVALID_ID", "Invalid user ID format")
	}

	actor, ok := middleware.CurrentActor(c)
	if !ok {
		return response.Error(c, fiber.StatusUnauthorized, "UNAUTHORIZED", "Authentication required")
	}

	var req ResetPasswordRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, fiber.StatusBadRequest, "BAD_REQUEST", "Malformed request payload")
	}
	if strings.TrimSpace(req.NewPassword) == "" {
		return response.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "new_password is required")
	}

	if err := h.usecase.ResetPassword(c.UserContext(), actor, id, req.NewPassword); err != nil {
		return respondUserError(c, err)
	}

	return response.OK(c, nil, "Password reset successfully; all sessions were revoked")
}

// respondUserError maps account-management failures onto the established envelope.
func respondUserError(c *fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, domain.ErrDuplicateUsername):
		return response.Error(c, fiber.StatusConflict, "DUPLICATE_USERNAME", "Username is already taken")
	case errors.Is(err, domain.ErrUserNotFound):
		return response.Error(c, fiber.StatusNotFound, "NOT_FOUND", "User not found")
	case errors.Is(err, domain.ErrLastAdmin):
		return response.Error(c, fiber.StatusConflict, "LAST_ADMIN", domain.ErrLastAdmin.Error())
	case errors.Is(err, domain.ErrForbidden):
		return response.Error(c, fiber.StatusForbidden, "FORBIDDEN", domain.ErrForbidden.Error())
	case errors.Is(err, domain.ErrWeakPassword):
		return response.Error(c, fiber.StatusBadRequest, "WEAK_PASSWORD", domain.ErrWeakPassword.Error())
	case errors.Is(err, domain.ErrEmptyUsername),
		errors.Is(err, domain.ErrUsernameTooShort),
		errors.Is(err, domain.ErrUsernameTooLong),
		errors.Is(err, domain.ErrInvalidRole):
		return response.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", err.Error())
	default:
		slog.Error("User management operation failed", "error", err)
		return response.Error(c, fiber.StatusInternalServerError, "INTERNAL_ERROR", "User operation failed")
	}
}
