package stockrequest_test

import (
	"context"
	"strings"
	"testing"

	"github.com/BreakDown-CS/StockFlowLite/internal/domain"
	"github.com/BreakDown-CS/StockFlowLite/internal/usecase/stockrequest"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type mockStockRequestRepository struct {
	mock.Mock
}

func (m *mockStockRequestRepository) Create(ctx context.Context, req *domain.StockRequest) error {
	return m.Called(ctx, req).Error(0)
}

func (m *mockStockRequestRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.StockRequest, error) {
	args := m.Called(ctx, id)
	if res := args.Get(0); res != nil {
		return res.(*domain.StockRequest), args.Error(1)
	}
	return nil, args.Error(1)
}

func (m *mockStockRequestRepository) List(ctx context.Context, filter domain.ListStockRequestFilter) ([]domain.StockRequest, int64, error) {
	args := m.Called(ctx, filter)
	return args.Get(0).([]domain.StockRequest), args.Get(1).(int64), args.Error(2)
}

func (m *mockStockRequestRepository) Approve(ctx context.Context, id uuid.UUID, reviewerID uuid.UUID) (*domain.StockRequest, error) {
	args := m.Called(ctx, id, reviewerID)
	if res := args.Get(0); res != nil {
		return res.(*domain.StockRequest), args.Error(1)
	}
	return nil, args.Error(1)
}

func (m *mockStockRequestRepository) Reject(ctx context.Context, id uuid.UUID, reviewerID uuid.UUID, reason string) (*domain.StockRequest, error) {
	args := m.Called(ctx, id, reviewerID, reason)
	if res := args.Get(0); res != nil {
		return res.(*domain.StockRequest), args.Error(1)
	}
	return nil, args.Error(1)
}

func (m *mockStockRequestRepository) Cancel(ctx context.Context, id uuid.UUID, cancelledBy uuid.UUID, reason string) (*domain.StockRequest, error) {
	args := m.Called(ctx, id, cancelledBy, reason)
	if res := args.Get(0); res != nil {
		return res.(*domain.StockRequest), args.Error(1)
	}
	return nil, args.Error(1)
}

func (m *mockStockRequestRepository) Issue(ctx context.Context, id uuid.UUID, actorID uuid.UUID, idempotencyKey string) (*domain.StockIssueResult, error) {
	args := m.Called(ctx, id, actorID, idempotencyKey)
	if res := args.Get(0); res != nil {
		return res.(*domain.StockIssueResult), args.Error(1)
	}
	return nil, args.Error(1)
}

var (
	testRequesterID = uuid.MustParse("11111111-1111-4111-8111-111111111111")
	testOtherID     = uuid.MustParse("33333333-3333-4333-8333-333333333333")
	testReviewerID  = uuid.MustParse("22222222-2222-4222-8222-222222222222")
)

func requesterCtx() context.Context {
	return domain.WithActor(context.Background(), domain.Actor{
		ID: testRequesterID, Username: "requester", Role: domain.RoleRequester,
	})
}

func otherRequesterCtx() context.Context {
	return domain.WithActor(context.Background(), domain.Actor{
		ID: testOtherID, Username: "other", Role: domain.RoleRequester,
	})
}

func reviewerCtx() context.Context {
	return domain.WithActor(context.Background(), domain.Actor{
		ID: testReviewerID, Username: "storekeeper", Role: domain.RoleStorekeeper,
	})
}

func newUsecase(repo *mockStockRequestRepository) stockrequest.StockRequestUsecase {
	return stockrequest.NewStockRequestUsecase(repo)
}

func TestStockRequestUsecase_CreateRequest_Success(t *testing.T) {
	repo := new(mockStockRequestRepository)
	uc := newUsecase(repo)

	productID := uuid.New()
	repo.On("Create", mock.Anything, mock.MatchedBy(func(req *domain.StockRequest) bool {
		return req.Status == domain.StatusPending &&
			req.RequesterID == testRequesterID &&
			strings.HasPrefix(req.RequestNumber, "REQ-") &&
			len(req.Items) == 1 &&
			req.Items[0].ProductID == productID
	})).Return(nil)

	created, err := uc.CreateRequest(requesterCtx(), stockrequest.CreateStockRequestInput{
		Items: []stockrequest.ItemInput{{ProductID: productID, Quantity: 4}},
		Notes: "  monthly  ",
	})
	require.NoError(t, err)
	assert.Equal(t, domain.StatusPending, created.Status)
	assert.Equal(t, testRequesterID, created.RequesterID)
	assert.Equal(t, "monthly", created.Notes)
	repo.AssertExpectations(t)
}

func TestStockRequestUsecase_CreateRequest_RequiresAuthentication(t *testing.T) {
	repo := new(mockStockRequestRepository)
	uc := newUsecase(repo)

	_, err := uc.CreateRequest(context.Background(), stockrequest.CreateStockRequestInput{
		Items: []stockrequest.ItemInput{{ProductID: uuid.New(), Quantity: 1}},
	})
	require.ErrorIs(t, err, domain.ErrUnauthenticated)
	repo.AssertNotCalled(t, "Create", mock.Anything, mock.Anything)
}

func TestStockRequestUsecase_CreateRequest_ValidationErrors(t *testing.T) {
	productID := uuid.New()
	tooMany := make([]stockrequest.ItemInput, 0, stockrequest.MaxRequestItems+1)
	for i := 0; i <= stockrequest.MaxRequestItems; i++ {
		tooMany = append(tooMany, stockrequest.ItemInput{ProductID: uuid.New(), Quantity: 1})
	}

	cases := []struct {
		name    string
		input   stockrequest.CreateStockRequestInput
		wantErr error
	}{
		{"empty items", stockrequest.CreateStockRequestInput{}, domain.ErrEmptyRequestItems},
		{"zero quantity", stockrequest.CreateStockRequestInput{Items: []stockrequest.ItemInput{{ProductID: productID, Quantity: 0}}}, domain.ErrInvalidQuantity},
		{"negative quantity", stockrequest.CreateStockRequestInput{Items: []stockrequest.ItemInput{{ProductID: productID, Quantity: -2}}}, domain.ErrInvalidQuantity},
		{"missing product", stockrequest.CreateStockRequestInput{Items: []stockrequest.ItemInput{{ProductID: uuid.Nil, Quantity: 1}}}, domain.ErrMissingProductID},
		{"duplicate products", stockrequest.CreateStockRequestInput{Items: []stockrequest.ItemInput{
			{ProductID: productID, Quantity: 1}, {ProductID: productID, Quantity: 2},
		}}, domain.ErrDuplicateItem},
		{"too many items", stockrequest.CreateStockRequestInput{Items: tooMany}, domain.ErrTooManyRequestItems},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := new(mockStockRequestRepository)
			uc := newUsecase(repo)

			_, err := uc.CreateRequest(requesterCtx(), tc.input)
			require.ErrorIs(t, err, tc.wantErr)
			repo.AssertNotCalled(t, "Create", mock.Anything, mock.Anything)
		})
	}
}

func TestStockRequestUsecase_CreateRequest_ProductStateErrors(t *testing.T) {
	for _, repoErr := range []error{domain.ErrProductNotFound, domain.ErrProductInactive} {
		repo := new(mockStockRequestRepository)
		uc := newUsecase(repo)
		repo.On("Create", mock.Anything, mock.Anything).Return(repoErr)

		_, err := uc.CreateRequest(requesterCtx(), stockrequest.CreateStockRequestInput{
			Items: []stockrequest.ItemInput{{ProductID: uuid.New(), Quantity: 3}},
		})
		require.ErrorIs(t, err, repoErr)
	}
}

func TestStockRequestUsecase_ListRequests_ScopedByRole(t *testing.T) {
	repo := new(mockStockRequestRepository)
	uc := newUsecase(repo)

	// Storekeepers see every request.
	repo.On("List", mock.Anything, mock.MatchedBy(func(f domain.ListStockRequestFilter) bool {
		return f.Limit == 20 && f.Offset == 0 && f.Status == nil && f.RequesterID == nil
	})).Return([]domain.StockRequest{}, int64(0), nil)

	_, _, err := uc.ListRequests(reviewerCtx(), stockrequest.ListStockRequestsInput{})
	require.NoError(t, err)

	// Requesters are scoped to their own requests (and bounds are clamped).
	repo.On("List", mock.Anything, mock.MatchedBy(func(f domain.ListStockRequestFilter) bool {
		return f.RequesterID != nil && *f.RequesterID == testRequesterID && f.Limit == stockrequest.MaxPageSize
	})).Return([]domain.StockRequest{}, int64(0), nil)

	_, _, err = uc.ListRequests(requesterCtx(), stockrequest.ListStockRequestsInput{Limit: 500, Offset: -5})
	require.NoError(t, err)

	// Unauthenticated listing is rejected.
	_, _, err = uc.ListRequests(context.Background(), stockrequest.ListStockRequestsInput{})
	require.ErrorIs(t, err, domain.ErrUnauthenticated)

	repo.AssertExpectations(t)
}

func TestStockRequestUsecase_ListRequests_InvalidStatus(t *testing.T) {
	repo := new(mockStockRequestRepository)
	uc := newUsecase(repo)

	invalid := domain.RequestStatus("DELIVERED")
	_, _, err := uc.ListRequests(reviewerCtx(), stockrequest.ListStockRequestsInput{Status: &invalid})
	require.ErrorIs(t, err, domain.ErrInvalidStatus)
	repo.AssertNotCalled(t, "List", mock.Anything, mock.Anything)
}

func TestStockRequestUsecase_GetRequest_Ownership(t *testing.T) {
	repo := new(mockStockRequestRepository)
	uc := newUsecase(repo)

	ownID := uuid.New()
	repo.On("GetByID", mock.Anything, ownID).Return(&domain.StockRequest{
		ID: ownID, RequesterID: testRequesterID, Status: domain.StatusPending,
	}, nil)

	got, err := uc.GetRequest(requesterCtx(), ownID)
	require.NoError(t, err)
	assert.Equal(t, ownID, got.ID)

	foreignID := uuid.New()
	repo.On("GetByID", mock.Anything, foreignID).Return(&domain.StockRequest{
		ID: foreignID, RequesterID: testOtherID, Status: domain.StatusPending,
	}, nil)

	// A requester must not discover another requester's request.
	_, err = uc.GetRequest(requesterCtx(), foreignID)
	require.ErrorIs(t, err, domain.ErrStockRequestNotFound)

	// Other requesters are also rejected.
	_, err = uc.GetRequest(otherRequesterCtx(), ownID)
	require.ErrorIs(t, err, domain.ErrStockRequestNotFound)

	// Storekeepers may read any request.
	_, err = uc.GetRequest(reviewerCtx(), foreignID)
	require.NoError(t, err)

	repo.AssertExpectations(t)
}

func TestStockRequestUsecase_ApproveRequest_UsesAuthenticatedActor(t *testing.T) {
	repo := new(mockStockRequestRepository)
	uc := newUsecase(repo)

	_, err := uc.ApproveRequest(context.Background(), uuid.New())
	require.ErrorIs(t, err, domain.ErrUnauthenticated)

	requestID := uuid.New()
	repo.On("Approve", mock.Anything, requestID, testReviewerID).
		Return(&domain.StockRequest{ID: requestID, Status: domain.StatusApproved}, nil)

	approved, err := uc.ApproveRequest(reviewerCtx(), requestID)
	require.NoError(t, err)
	assert.Equal(t, domain.StatusApproved, approved.Status)
	repo.AssertExpectations(t)
}

func TestStockRequestUsecase_RejectRequest_TrimsReason(t *testing.T) {
	repo := new(mockStockRequestRepository)
	uc := newUsecase(repo)

	requestID := uuid.New()
	repo.On("Reject", mock.Anything, requestID, testReviewerID, "no budget").
		Return(&domain.StockRequest{ID: requestID, Status: domain.StatusRejected}, nil)

	_, err := uc.RejectRequest(reviewerCtx(), requestID, stockrequest.RejectStockRequestInput{
		Reason: "   no budget  ",
	})
	require.NoError(t, err)
	repo.AssertExpectations(t)
}

func TestStockRequestUsecase_CancelRequest(t *testing.T) {
	requestID := uuid.New()

	t.Run("stores the trimmed reason and the actor", func(t *testing.T) {
		repo := new(mockStockRequestRepository)
		uc := newUsecase(repo)

		repo.On("GetByID", mock.Anything, requestID).Return(&domain.StockRequest{
			ID: requestID, RequesterID: testRequesterID, Status: domain.StatusPending,
		}, nil)
		repo.On("Cancel", mock.Anything, requestID, testRequesterID, "changed my mind").
			Return(&domain.StockRequest{ID: requestID, Status: domain.StatusCancelled}, nil)

		cancelled, err := uc.CancelRequest(requesterCtx(), requestID, "  changed my mind  ")
		require.NoError(t, err)
		assert.Equal(t, domain.StatusCancelled, cancelled.Status)
		repo.AssertExpectations(t)
	})

	t.Run("cannot cancel another requester's request", func(t *testing.T) {
		repo := new(mockStockRequestRepository)
		uc := newUsecase(repo)

		repo.On("GetByID", mock.Anything, requestID).Return(&domain.StockRequest{
			ID: requestID, RequesterID: testOtherID, Status: domain.StatusPending,
		}, nil)

		_, err := uc.CancelRequest(requesterCtx(), requestID, "")
		require.ErrorIs(t, err, domain.ErrStockRequestNotFound)
		repo.AssertNotCalled(t, "Cancel", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	})

	t.Run("rejects an over-long reason", func(t *testing.T) {
		repo := new(mockStockRequestRepository)
		uc := newUsecase(repo)

		_, err := uc.CancelRequest(requesterCtx(), requestID, strings.Repeat("x", 1001))
		require.ErrorIs(t, err, domain.ErrInvalidCancellationReason)
	})
}

func TestStockRequestUsecase_IssueRequest_UsesAuthenticatedActorAndKey(t *testing.T) {
	repo := new(mockStockRequestRepository)
	uc := newUsecase(repo)

	_, err := uc.IssueRequest(context.Background(), uuid.New(), "")
	require.ErrorIs(t, err, domain.ErrUnauthenticated)

	requestID := uuid.New()
	repo.On("Issue", mock.Anything, requestID, testReviewerID, "checkout-abc123").
		Return(&domain.StockIssueResult{
			Request:  &domain.StockRequest{ID: requestID, Status: domain.StatusIssued},
			Issuance: &domain.StockRequestIssuance{ID: uuid.New(), RequestID: requestID},
		}, nil)

	result, err := uc.IssueRequest(reviewerCtx(), requestID, "  checkout-abc123  ")
	require.NoError(t, err)
	assert.Equal(t, domain.StatusIssued, result.Request.Status)
	require.NotNil(t, result.Issuance)
	repo.AssertExpectations(t)
}

func TestStockRequestUsecase_IssueRequest_RejectsInvalidIdempotencyKeys(t *testing.T) {
	requestID := uuid.New()

	invalidKeys := []string{
		"short",
		"has spaces in the key",
		"<script>alert(1)</script>",
		strings.Repeat("a", stockrequest.MaxIdempotencyKeyLen+1),
	}

	for _, key := range invalidKeys {
		repo := new(mockStockRequestRepository)
		uc := newUsecase(repo)

		_, err := uc.IssueRequest(reviewerCtx(), requestID, key)
		require.ErrorIs(t, err, domain.ErrInvalidIdempotencyKey)
		repo.AssertNotCalled(t, "Issue", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	}
}
