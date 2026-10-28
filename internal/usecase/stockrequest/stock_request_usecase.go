package stockrequest

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/BreakDown-CS/StockFlowLite/internal/domain"
	"github.com/google/uuid"
)

// MaxRequestItems bounds how many line items a single request may contain.
const MaxRequestItems = 100

// MaxPageSize bounds how many requests a single list page may return.
const MaxPageSize = 100

// Idempotency key bounds for the issuance endpoint (see docs/§2.5).
const (
	MinIdempotencyKeyLen = 8
	MaxIdempotencyKeyLen = 100
)

// ActorIdentity was removed in Phase 7: every operation now derives its actor from
// the authenticated principal stored in the request context (see domain.ActorFromContext).

// ItemInput is a single requested line item supplied by a client.
type ItemInput struct {
	ProductID uuid.UUID
	Quantity  int
}

// CreateStockRequestInput holds validated data for submitting a stock request.
type CreateStockRequestInput struct {
	Items []ItemInput
	Notes string
}

// ListStockRequestsInput defines query constraints for request listings.
type ListStockRequestsInput struct {
	Status *domain.RequestStatus
	Limit  int
	Offset int
}

// RejectStockRequestInput holds review data for rejecting a pending request.
type RejectStockRequestInput struct {
	Reason string
}

// StockRequestUsecase defines business application operations for the request workflow.
type StockRequestUsecase interface {
	CreateRequest(ctx context.Context, input CreateStockRequestInput) (*domain.StockRequest, error)
	GetRequest(ctx context.Context, id uuid.UUID) (*domain.StockRequest, error)
	ListRequests(ctx context.Context, input ListStockRequestsInput) ([]domain.StockRequest, int64, error)
	ApproveRequest(ctx context.Context, id uuid.UUID) (*domain.StockRequest, error)
	RejectRequest(ctx context.Context, id uuid.UUID, input RejectStockRequestInput) (*domain.StockRequest, error)
	CancelRequest(ctx context.Context, id uuid.UUID, reason string) (*domain.StockRequest, error)
	IssueRequest(ctx context.Context, id uuid.UUID, idempotencyKey string) (*domain.StockIssueResult, error)
}

type stockRequestUsecase struct {
	repo domain.StockRequestRepository
}

// NewStockRequestUsecase constructs a StockRequestUsecase. The acting user is taken
// from the authenticated principal carried in the request context.
func NewStockRequestUsecase(repo domain.StockRequestRepository) StockRequestUsecase {
	return &stockRequestUsecase{repo: repo}
}

// currentActor returns the authenticated principal or ErrUnauthenticated.
func currentActor(ctx context.Context) (domain.Actor, error) {
	actor, ok := domain.ActorFromContext(ctx)
	if !ok || actor.IsZero() {
		return domain.Actor{}, domain.ErrUnauthenticated
	}
	return actor, nil
}

// CreateRequest validates input, builds a PENDING request, and persists it with its items.
// Stock balances and movement history are untouched at this stage.
func (uc *stockRequestUsecase) CreateRequest(ctx context.Context, input CreateStockRequestInput) (*domain.StockRequest, error) {
	if len(input.Items) == 0 {
		return nil, domain.ErrEmptyRequestItems
	}
	if len(input.Items) > MaxRequestItems {
		return nil, domain.ErrTooManyRequestItems
	}

	items := make([]domain.StockRequestItem, 0, len(input.Items))
	for _, it := range input.Items {
		items = append(items, domain.StockRequestItem{
			ProductID:         it.ProductID,
			RequestedQuantity: it.Quantity,
		})
	}

	// Domain construction enforces non-empty items, positive quantities, and no duplicate products.
	actor, err := currentActor(ctx)
	if err != nil {
		return nil, err
	}

	req, err := domain.NewStockRequest(generateRequestNumber(time.Now().UTC()), actor.ID, input.Notes, items)
	if err != nil {
		return nil, err
	}

	if err := uc.repo.Create(ctx, req); err != nil {
		return nil, err
	}

	return req, nil
}

// GetRequest retrieves a single request together with its items.
// Requesters may only read their own requests (others appear as not found).
func (uc *stockRequestUsecase) GetRequest(ctx context.Context, id uuid.UUID) (*domain.StockRequest, error) {
	if id == uuid.Nil {
		return nil, domain.ErrInvalidRequestID
	}

	actor, err := currentActor(ctx)
	if err != nil {
		return nil, err
	}

	request, err := uc.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if !canAccessRequest(actor, request) {
		return nil, domain.ErrStockRequestNotFound
	}

	return request, nil
}

// ListRequests applies safe pagination and status filtering.
func (uc *stockRequestUsecase) ListRequests(ctx context.Context, input ListStockRequestsInput) ([]domain.StockRequest, int64, error) {
	limit := input.Limit
	if limit <= 0 {
		limit = 20
	} else if limit > MaxPageSize {
		limit = MaxPageSize
	}

	offset := input.Offset
	if offset < 0 {
		offset = 0
	}

	if input.Status != nil {
		valid := false
		for _, s := range domain.ValidRequestStatuses {
			if *input.Status == s {
				valid = true
				break
			}
		}
		if !valid {
			return nil, 0, domain.ErrInvalidStatus
		}
	}

	actor, err := currentActor(ctx)
	if err != nil {
		return nil, 0, err
	}

	filter := domain.ListStockRequestFilter{
		Status: input.Status,
		Limit:  limit,
		Offset: offset,
	}
	// Requesters only ever see their own requests.
	if actor.Role == domain.RoleRequester {
		requesterID := actor.ID
		filter.RequesterID = &requesterID
	}

	return uc.repo.List(ctx, filter)
}

// canAccessRequest reports whether the actor may read the given request.
// Requesters are scoped to their own requests; storekeepers and admins see everything.
func canAccessRequest(actor domain.Actor, request *domain.StockRequest) bool {
	if actor.Role == domain.RoleAdmin || actor.Role == domain.RoleStorekeeper {
		return true
	}
	return request.RequesterID == actor.ID
}

// ApproveRequest moves a PENDING request to APPROVED. It does not move any stock.
func (uc *stockRequestUsecase) ApproveRequest(ctx context.Context, id uuid.UUID) (*domain.StockRequest, error) {
	if id == uuid.Nil {
		return nil, domain.ErrInvalidRequestID
	}
	actor, err := currentActor(ctx)
	if err != nil {
		return nil, err
	}
	return uc.repo.Approve(ctx, id, actor.ID)
}

// RejectRequest moves a PENDING request to REJECTED, recording the optional reason.
func (uc *stockRequestUsecase) RejectRequest(ctx context.Context, id uuid.UUID, input RejectStockRequestInput) (*domain.StockRequest, error) {
	if id == uuid.Nil {
		return nil, domain.ErrInvalidRequestID
	}
	actor, err := currentActor(ctx)
	if err != nil {
		return nil, err
	}
	return uc.repo.Reject(ctx, id, actor.ID, strings.TrimSpace(input.Reason))
}

// IssueRequest issues every approved item atomically and marks the request ISSUED.
// An optional idempotency key lets a retried call replay the original result.
func (uc *stockRequestUsecase) IssueRequest(ctx context.Context, id uuid.UUID, idempotencyKey string) (*domain.StockIssueResult, error) {
	if id == uuid.Nil {
		return nil, domain.ErrInvalidRequestID
	}

	key, err := normalizeIdempotencyKey(idempotencyKey)
	if err != nil {
		return nil, err
	}

	actor, err := currentActor(ctx)
	if err != nil {
		return nil, err
	}

	return uc.repo.Issue(ctx, id, actor.ID, key)
}

// CancelRequest moves a PENDING or APPROVED request to CANCELLED without touching stock.
func (uc *stockRequestUsecase) CancelRequest(ctx context.Context, id uuid.UUID, reason string) (*domain.StockRequest, error) {
	if id == uuid.Nil {
		return nil, domain.ErrInvalidRequestID
	}

	trimmedReason := strings.TrimSpace(reason)
	if len(trimmedReason) > 1000 {
		return nil, domain.ErrInvalidCancellationReason
	}

	actor, err := currentActor(ctx)
	if err != nil {
		return nil, err
	}

	// Requesters may only cancel their own requests.
	existing, err := uc.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if !canAccessRequest(actor, existing) {
		return nil, domain.ErrStockRequestNotFound
	}

	return uc.repo.Cancel(ctx, id, actor.ID, trimmedReason)
}

// normalizeIdempotencyKey validates the optional client idempotency key.
// An empty key is allowed and means "no replay protection requested".
func normalizeIdempotencyKey(key string) (string, error) {
	normalized := strings.TrimSpace(key)
	if normalized == "" {
		return "", nil
	}

	if len(normalized) < MinIdempotencyKeyLen || len(normalized) > MaxIdempotencyKeyLen {
		return "", domain.ErrInvalidIdempotencyKey
	}

	for _, char := range normalized {
		switch {
		case char >= 'a' && char <= 'z':
		case char >= 'A' && char <= 'Z':
		case char >= '0' && char <= '9':
		case char == '.' || char == '_' || char == ':' || char == '-':
		default:
			return "", domain.ErrInvalidIdempotencyKey
		}
	}

	return normalized, nil
}

// generateRequestNumber builds a unique, human-readable request number (VARCHAR(50) column).
func generateRequestNumber(now time.Time) string {
	suffix := strings.ToUpper(strings.ReplaceAll(uuid.NewString(), "-", "")[:8])
	return fmt.Sprintf("REQ-%s-%s", now.Format("20060102"), suffix)
}
