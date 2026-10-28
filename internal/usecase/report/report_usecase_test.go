package report_test

import (
	"context"
	"testing"
	"time"

	"github.com/BreakDown-CS/StockFlowLite/internal/domain"
	reportuc "github.com/BreakDown-CS/StockFlowLite/internal/usecase/report"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type mockReportRepository struct{ mock.Mock }

func (m *mockReportRepository) Inventory(ctx context.Context, filter domain.InventoryReportFilter) ([]domain.InventoryReportRow, int64, domain.InventorySummary, error) {
	args := m.Called(ctx, filter)
	items, _ := args.Get(0).([]domain.InventoryReportRow)
	return items, args.Get(1).(int64), args.Get(2).(domain.InventorySummary), args.Error(3)
}

func (m *mockReportRepository) Movements(ctx context.Context, filter domain.MovementReportFilter) ([]domain.MovementReportRow, int64, domain.MovementSummary, error) {
	args := m.Called(ctx, filter)
	items, _ := args.Get(0).([]domain.MovementReportRow)
	return items, args.Get(1).(int64), args.Get(2).(domain.MovementSummary), args.Error(3)
}

func (m *mockReportRepository) Requests(ctx context.Context, filter domain.RequestReportFilter) ([]domain.RequestReportRow, int64, domain.RequestSummary, error) {
	args := m.Called(ctx, filter)
	items, _ := args.Get(0).([]domain.RequestReportRow)
	return items, args.Get(1).(int64), args.Get(2).(domain.RequestSummary), args.Error(3)
}

func (m *mockReportRepository) StockIntegrity(ctx context.Context, filter domain.StockIntegrityFilter) ([]domain.StockIntegrityRow, int64, domain.StockIntegritySummary, error) {
	args := m.Called(ctx, filter)
	items, _ := args.Get(0).([]domain.StockIntegrityRow)
	return items, args.Get(1).(int64), args.Get(2).(domain.StockIntegritySummary), args.Error(3)
}

func TestReportUsecase_Inventory_ClampsPagination(t *testing.T) {
	repo := new(mockReportRepository)
	uc := reportuc.NewUsecase(repo)

	summary := domain.InventorySummary{TotalProducts: 1, ActiveProducts: 1}
	items := []domain.InventoryReportRow{{SKU: "SKU-1"}}

	repo.On("Inventory", mock.Anything, mock.MatchedBy(func(f domain.InventoryReportFilter) bool {
		return f.Limit == reportuc.DefaultPageSize && f.Offset == 0
	})).Return(items, int64(1), summary, nil).Once()

	report, total, err := uc.Inventory(context.Background(), reportuc.InventoryInput{})
	require.NoError(t, err)
	assert.EqualValues(t, 1, total)
	require.NotNil(t, report)
	assert.Equal(t, summary, report.Summary)
	assert.Len(t, report.Items, 1)

	repo.On("Inventory", mock.Anything, mock.MatchedBy(func(f domain.InventoryReportFilter) bool {
		return f.Limit == reportuc.MaxPageSize && f.Offset == 0
	})).Return([]domain.InventoryReportRow{}, int64(0), domain.InventorySummary{}, nil).Once()

	_, _, err = uc.Inventory(context.Background(), reportuc.InventoryInput{Limit: 500, Offset: -10})
	require.NoError(t, err)

	repo.AssertExpectations(t)
}

func TestReportUsecase_Movements_ValidatesFilters(t *testing.T) {
	repo := new(mockReportRepository)
	uc := reportuc.NewUsecase(repo)

	invalidType := domain.MovementType("TRANSFER")
	_, _, err := uc.Movements(context.Background(), reportuc.MovementInput{MovementType: &invalidType})
	require.ErrorIs(t, err, domain.ErrInvalidMovementType)
	repo.AssertNotCalled(t, "Movements", mock.Anything, mock.Anything)

	from := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	to := from.Add(-time.Hour)
	_, _, err = uc.Movements(context.Background(), reportuc.MovementInput{From: &from, To: &to})
	require.ErrorIs(t, err, domain.ErrInvalidDateRange)
	repo.AssertNotCalled(t, "Movements", mock.Anything, mock.Anything)

	movementType := domain.MovementReceipt
	productID := uuid.New()
	repo.On("Movements", mock.Anything, mock.MatchedBy(func(f domain.MovementReportFilter) bool {
		return f.ProductID != nil && *f.ProductID == productID &&
			f.MovementType != nil && *f.MovementType == movementType &&
			f.From != nil && f.To != nil && f.Limit == reportuc.DefaultPageSize
	})).Return([]domain.MovementReportRow{}, int64(0), domain.MovementSummary{NetChange: 3}, nil)

	report, _, err := uc.Movements(context.Background(), reportuc.MovementInput{
		ProductID:    &productID,
		MovementType: &movementType,
		From:         &to,
		To:           &from,
	})
	require.NoError(t, err)
	assert.EqualValues(t, 3, report.Summary.NetChange)
	repo.AssertExpectations(t)
}

func TestReportUsecase_Requests_ScopesRequestersToThemselves(t *testing.T) {
	requesterID := uuid.New()
	otherID := uuid.New()

	repo := new(mockReportRepository)
	uc := reportuc.NewUsecase(repo)

	// A requester asking for somebody else's data is silently scoped to their own.
	requesterCtx := domain.WithActor(context.Background(), domain.Actor{
		ID: requesterID, Username: "req", Role: domain.RoleRequester,
	})
	repo.On("Requests", mock.Anything, mock.MatchedBy(func(f domain.RequestReportFilter) bool {
		return f.RequesterID != nil && *f.RequesterID == requesterID
	})).Return([]domain.RequestReportRow{}, int64(0), domain.RequestSummary{}, nil)

	_, _, err := uc.Requests(requesterCtx, reportuc.RequestInput{RequesterID: &otherID})
	require.NoError(t, err)

	// Staff may filter by any requester.
	staffCtx := domain.WithActor(context.Background(), domain.Actor{
		ID: uuid.New(), Username: "keeper", Role: domain.RoleStorekeeper,
	})
	repo.On("Requests", mock.Anything, mock.MatchedBy(func(f domain.RequestReportFilter) bool {
		return f.RequesterID != nil && *f.RequesterID == otherID
	})).Return([]domain.RequestReportRow{}, int64(0), domain.RequestSummary{}, nil)

	_, _, err = uc.Requests(staffCtx, reportuc.RequestInput{RequesterID: &otherID})
	require.NoError(t, err)

	repo.AssertExpectations(t)
}

func TestReportUsecase_StockIntegrity_ForwardsFilters(t *testing.T) {
	repo := new(mockReportRepository)
	uc := reportuc.NewUsecase(repo)

	summary := domain.StockIntegritySummary{TotalProducts: 2, DriftedProducts: 1, AbsoluteDrift: 3}
	repo.On("StockIntegrity", mock.Anything, mock.MatchedBy(func(f domain.StockIntegrityFilter) bool {
		return f.OnlyDrifted && f.Limit == reportuc.DefaultPageSize && f.Offset == 0
	})).Return([]domain.StockIntegrityRow{{SKU: "A", Drift: -3}}, int64(1), summary, nil)

	report, total, err := uc.StockIntegrity(context.Background(), reportuc.StockIntegrityInput{OnlyDrifted: true})
	require.NoError(t, err)
	assert.EqualValues(t, 1, total)
	require.NotNil(t, report)
	assert.EqualValues(t, 1, report.Summary.DriftedProducts)
	require.Len(t, report.Items, 1)
	repo.AssertExpectations(t)
}

func TestReportUsecase_StockIntegrity_ClampsPagination(t *testing.T) {
	repo := new(mockReportRepository)
	uc := reportuc.NewUsecase(repo)

	repo.On("StockIntegrity", mock.Anything, mock.MatchedBy(func(f domain.StockIntegrityFilter) bool {
		return f.Limit == reportuc.MaxPageSize && f.Offset == 0
	})).Return([]domain.StockIntegrityRow{}, int64(0), domain.StockIntegritySummary{}, nil)

	_, _, err := uc.StockIntegrity(context.Background(), reportuc.StockIntegrityInput{Limit: 1000, Offset: -5})
	require.NoError(t, err)
	repo.AssertExpectations(t)
}

func TestReportUsecase_Requests_RejectsBadFiltersAndAnonymous(t *testing.T) {
	repo := new(mockReportRepository)
	uc := reportuc.NewUsecase(repo)

	_, _, err := uc.Requests(context.Background(), reportuc.RequestInput{})
	require.ErrorIs(t, err, domain.ErrUnauthenticated)

	staffCtx := domain.WithActor(context.Background(), domain.Actor{
		ID: uuid.New(), Username: "admin", Role: domain.RoleAdmin,
	})

	invalidStatus := domain.RequestStatus("DELIVERED")
	_, _, err = uc.Requests(staffCtx, reportuc.RequestInput{Status: &invalidStatus})
	require.ErrorIs(t, err, domain.ErrInvalidStatus)

	repo.AssertNotCalled(t, "Requests", mock.Anything, mock.Anything)
}
