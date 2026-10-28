package report

import (
	"context"
	"strings"
	"time"

	"github.com/BreakDown-CS/StockFlowLite/internal/domain"
	"github.com/google/uuid"
)

// Pagination bounds for report endpoints.
const (
	DefaultPageSize = 20
	MaxPageSize     = 100
)

// InventoryInput holds validated filters for the inventory report.
type InventoryInput struct {
	Search   string
	IsActive *bool
	LowStock bool
	Limit    int
	Offset   int
}

// MovementInput holds validated filters for the stock movement report.
type MovementInput struct {
	ProductID    *uuid.UUID
	MovementType *domain.MovementType
	From         *time.Time
	To           *time.Time
	Limit        int
	Offset       int
}

// RequestInput holds validated filters for the stock request report.
type RequestInput struct {
	Status      *domain.RequestStatus
	RequesterID *uuid.UUID
	From        *time.Time
	To          *time.Time
	Limit       int
	Offset      int
}

// InventoryReport is the inventory report payload (summary + one page of items).
type InventoryReport struct {
	Summary domain.InventorySummary     `json:"summary"`
	Items   []domain.InventoryReportRow `json:"items"`
}

// MovementReport is the stock movement report payload.
type MovementReport struct {
	Summary domain.MovementSummary     `json:"summary"`
	Items   []domain.MovementReportRow `json:"items"`
}

// RequestReport is the stock request report payload.
type RequestReport struct {
	Summary domain.RequestSummary     `json:"summary"`
	Items   []domain.RequestReportRow `json:"items"`
}

// StockIntegrityInput holds validated filters for the stock integrity report.
type StockIntegrityInput struct {
	OnlyDrifted bool
	Limit       int
	Offset      int
}

// StockIntegrityReport compares stored balances with the movement ledger.
type StockIntegrityReport struct {
	Summary domain.StockIntegritySummary `json:"summary"`
	Items   []domain.StockIntegrityRow   `json:"items"`
}

// Usecase defines the read-only reporting operations.
type Usecase interface {
	Inventory(ctx context.Context, input InventoryInput) (*InventoryReport, int64, error)
	Movements(ctx context.Context, input MovementInput) (*MovementReport, int64, error)
	Requests(ctx context.Context, input RequestInput) (*RequestReport, int64, error)
	StockIntegrity(ctx context.Context, input StockIntegrityInput) (*StockIntegrityReport, int64, error)
}

type usecase struct {
	repo domain.ReportRepository
}

// NewUsecase constructs the reporting use case.
func NewUsecase(repo domain.ReportRepository) Usecase {
	return &usecase{repo: repo}
}

// Inventory returns the filtered stock snapshot with low-stock/out-of-stock flags.
func (uc *usecase) Inventory(ctx context.Context, input InventoryInput) (*InventoryReport, int64, error) {
	limit, offset := clampPagination(input.Limit, input.Offset)

	items, total, summary, err := uc.repo.Inventory(ctx, domain.InventoryReportFilter{
		Search:   strings.TrimSpace(input.Search),
		IsActive: input.IsActive,
		LowStock: input.LowStock,
		Limit:    limit,
		Offset:   offset,
	})
	if err != nil {
		return nil, 0, err
	}

	return &InventoryReport{Summary: summary, Items: items}, total, nil
}

// Movements returns the filtered ledger page with per-type totals.
func (uc *usecase) Movements(ctx context.Context, input MovementInput) (*MovementReport, int64, error) {
	if input.MovementType != nil && !isValidMovementType(*input.MovementType) {
		return nil, 0, domain.ErrInvalidMovementType
	}
	if err := validateRange(input.From, input.To); err != nil {
		return nil, 0, err
	}

	limit, offset := clampPagination(input.Limit, input.Offset)

	items, total, summary, err := uc.repo.Movements(ctx, domain.MovementReportFilter{
		ProductID:    input.ProductID,
		MovementType: input.MovementType,
		From:         input.From,
		To:           input.To,
		Limit:        limit,
		Offset:       offset,
	})
	if err != nil {
		return nil, 0, err
	}

	return &MovementReport{Summary: summary, Items: items}, total, nil
}

// Requests returns the filtered request page with status counts and unit totals.
// Requesters are always scoped to their own requests, whatever they ask for.
func (uc *usecase) Requests(ctx context.Context, input RequestInput) (*RequestReport, int64, error) {
	actor, ok := domain.ActorFromContext(ctx)
	if !ok || actor.IsZero() {
		return nil, 0, domain.ErrUnauthenticated
	}

	if input.Status != nil && !isValidStatus(*input.Status) {
		return nil, 0, domain.ErrInvalidStatus
	}
	if err := validateRange(input.From, input.To); err != nil {
		return nil, 0, err
	}

	requesterID := input.RequesterID
	if actor.Role == domain.RoleRequester {
		self := actor.ID
		requesterID = &self
	}

	limit, offset := clampPagination(input.Limit, input.Offset)

	items, total, summary, err := uc.repo.Requests(ctx, domain.RequestReportFilter{
		Status:      input.Status,
		RequesterID: requesterID,
		From:        input.From,
		To:          input.To,
		Limit:       limit,
		Offset:      offset,
	})
	if err != nil {
		return nil, 0, err
	}

	return &RequestReport{Summary: summary, Items: items}, total, nil
}

// StockIntegrity reports drift between products.current_stock and the movement ledger.
// It never repairs data: remediation stays a manual, audited decision.
func (uc *usecase) StockIntegrity(ctx context.Context, input StockIntegrityInput) (*StockIntegrityReport, int64, error) {
	limit, offset := clampPagination(input.Limit, input.Offset)

	items, total, summary, err := uc.repo.StockIntegrity(ctx, domain.StockIntegrityFilter{
		OnlyDrifted: input.OnlyDrifted,
		Limit:       limit,
		Offset:      offset,
	})
	if err != nil {
		return nil, 0, err
	}

	return &StockIntegrityReport{Summary: summary, Items: items}, total, nil
}

func clampPagination(limit, offset int) (int, int) {
	if limit <= 0 {
		limit = DefaultPageSize
	} else if limit > MaxPageSize {
		limit = MaxPageSize
	}
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}

func validateRange(from, to *time.Time) error {
	if from != nil && to != nil && from.After(*to) {
		return domain.ErrInvalidDateRange
	}
	return nil
}

func isValidMovementType(movementType domain.MovementType) bool {
	for _, candidate := range domain.ValidMovementTypes {
		if movementType == candidate {
			return true
		}
	}
	return false
}

func isValidStatus(status domain.RequestStatus) bool {
	for _, candidate := range domain.ValidRequestStatuses {
		if status == candidate {
			return true
		}
	}
	return false
}
