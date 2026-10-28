package domain

import "errors"

// Common domain sentinel errors
var (
	// User domain errors
	ErrEmptyUsername     = errors.New("username cannot be empty")
	ErrUsernameTooShort  = errors.New("username must be at least 3 characters")
	ErrUsernameTooLong   = errors.New("username cannot exceed 50 characters")
	ErrEmptyPasswordHash = errors.New("password hash cannot be empty")
	ErrInvalidRole       = errors.New("invalid user role; must be ADMIN, STOREKEEPER, or REQUESTER")

	// Authentication & authorization errors
	ErrInvalidCredentials        = errors.New("invalid username or password")
	ErrUserInactive              = errors.New("user account is disabled")
	ErrUserNotFound              = errors.New("user not found")
	ErrDuplicateUsername         = errors.New("username is already taken")
	ErrWeakPassword              = errors.New("password must be at least 8 characters and must not equal the username")
	ErrUnauthenticated           = errors.New("authentication required")
	ErrForbidden                 = errors.New("insufficient permissions for this operation")
	ErrSelfApproval              = errors.New("requesters cannot approve or issue their own stock request")
	ErrLastAdmin                 = errors.New("cannot deactivate or demote the last active administrator")
	ErrTooManyAttempts           = errors.New("too many login attempts; please try again later")
	ErrInvalidCancellationReason = errors.New("cancellation reason must not exceed 1000 characters")

	// Product domain errors
	ErrProductNotFound    = errors.New("product not found")
	ErrProductInactive    = errors.New("cannot perform stock operation on inactive product")
	ErrDuplicateSKU       = errors.New("product with this SKU already exists")
	ErrInvalidProductID   = errors.New("invalid product ID")
	ErrEmptySKU           = errors.New("product SKU cannot be empty")
	ErrSKUTooLong         = errors.New("product SKU cannot exceed 50 characters")
	ErrEmptyProductName   = errors.New("product name cannot be empty")
	ErrProductNameTooLong = errors.New("product name cannot exceed 200 characters")
	ErrEmptyUnit          = errors.New("product unit cannot be empty")
	ErrUnitTooLong        = errors.New("product unit cannot exceed 20 characters")
	ErrDescriptionTooLong = errors.New("product description cannot exceed 1000 characters")
	ErrNegativeStock      = errors.New("stock quantity cannot be negative")
	ErrNegativeMinStock   = errors.New("minimum stock quantity cannot be negative")
	ErrInsufficientStock  = errors.New("insufficient stock for requested operation")
	ErrStockOverflow      = errors.New("stock quantity exceeds maximum supported limit")

	// Stock Request domain errors
	ErrEmptyRequestNumber = errors.New("request number cannot be empty")
	ErrMissingRequester   = errors.New("requester ID is required")
	ErrInvalidStatus      = errors.New("invalid request status")
	ErrEmptyRequestItems  = errors.New("stock request must contain at least one item")
	ErrInvalidQuantity    = errors.New("quantity must be strictly positive")
	ErrMissingProductID   = errors.New("product ID is required")
	ErrDuplicateItem      = errors.New("duplicate product in stock request items")
	ErrCannotReview       = errors.New("stock request cannot be reviewed in its current status")
	ErrCannotCancel       = errors.New("stock request cannot be cancelled in its current status")
	ErrCannotIssue        = errors.New("stock request cannot be issued unless approved")

	// Stock Request workflow errors
	ErrInvalidRequestID       = errors.New("invalid stock request ID")
	ErrStockRequestNotFound   = errors.New("stock request not found")
	ErrMissingReviewer        = errors.New("reviewer ID is required")
	ErrDuplicateRequestNumber = errors.New("stock request number already exists")
	ErrTooManyRequestItems    = errors.New("stock request cannot contain more than 100 items")

	// Fulfillment (issuance) errors
	ErrInvalidIdempotencyKey = errors.New("idempotency key must be 8-100 characters using A-Z a-z 0-9 . _ : -")
	ErrIdempotencyKeyReused  = errors.New("idempotency key was already used for another stock request")

	// Stock Movement domain errors
	ErrInvalidMovementType = errors.New("invalid movement type; must be RECEIPT, ISSUE, or ADJUSTMENT")
	ErrEmptyReferenceType  = errors.New("movement reference type cannot be empty")
)
