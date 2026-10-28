package fiber_test

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	httpfiber "github.com/BreakDown-CS/StockFlowLite/internal/adapter/http/fiber"
	"github.com/BreakDown-CS/StockFlowLite/internal/adapter/http/fiber/handler"
	"github.com/BreakDown-CS/StockFlowLite/internal/domain"
	"github.com/BreakDown-CS/StockFlowLite/internal/usecase/stockrequest"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubStockRequestUsecase keeps the router test free of database dependencies.
type stubStockRequestUsecase struct{}

func (stubStockRequestUsecase) CreateRequest(context.Context, stockrequest.CreateStockRequestInput) (*domain.StockRequest, error) {
	return &domain.StockRequest{ID: uuid.New(), Status: domain.StatusPending}, nil
}

func (stubStockRequestUsecase) GetRequest(_ context.Context, id uuid.UUID) (*domain.StockRequest, error) {
	return &domain.StockRequest{ID: id, Status: domain.StatusPending}, nil
}

func (stubStockRequestUsecase) ListRequests(context.Context, stockrequest.ListStockRequestsInput) ([]domain.StockRequest, int64, error) {
	return []domain.StockRequest{}, 0, nil
}

func (stubStockRequestUsecase) ApproveRequest(_ context.Context, id uuid.UUID) (*domain.StockRequest, error) {
	return &domain.StockRequest{ID: id, Status: domain.StatusApproved}, nil
}

func (stubStockRequestUsecase) RejectRequest(_ context.Context, id uuid.UUID, _ stockrequest.RejectStockRequestInput) (*domain.StockRequest, error) {
	return &domain.StockRequest{ID: id, Status: domain.StatusRejected}, nil
}

func (stubStockRequestUsecase) CancelRequest(_ context.Context, id uuid.UUID, _ string) (*domain.StockRequest, error) {
	return &domain.StockRequest{ID: id, Status: domain.StatusCancelled}, nil
}

func (stubStockRequestUsecase) IssueRequest(_ context.Context, id uuid.UUID, _ string) (*domain.StockIssueResult, error) {
	return &domain.StockIssueResult{Request: &domain.StockRequest{ID: id, Status: domain.StatusIssued}}, nil
}

// TestNewRouter_RegistersStockRequestEndpoints proves every Phase 5 route is wired,
// including the trailing-slash variant used by the existing product routes.
func TestNewRouter_RegistersStockRequestEndpoints(t *testing.T) {
	app := httpfiber.NewRouter(httpfiber.RouterConfig{
		StockRequestHandler: handler.NewStockRequestHandler(stubStockRequestUsecase{}),
	})

	requestID := uuid.New()
	createBody := `{"items":[{"product_id":"` + uuid.New().String() + `","quantity":2}]}`

	cases := []struct {
		method string
		path   string
		body   string
		want   int
	}{
		{method: "POST", path: "/api/v1/stock-requests", body: createBody, want: fiber.StatusCreated},
		{method: "POST", path: "/api/v1/stock-requests/", body: createBody, want: fiber.StatusCreated},
		{method: "GET", path: "/api/v1/stock-requests", want: fiber.StatusOK},
		{method: "GET", path: "/api/v1/stock-requests/" + requestID.String(), want: fiber.StatusOK},
		{method: "POST", path: "/api/v1/stock-requests/" + requestID.String() + "/approve", want: fiber.StatusOK},
		{method: "POST", path: "/api/v1/stock-requests/" + requestID.String() + "/reject", want: fiber.StatusOK},
		{method: "POST", path: "/api/v1/stock-requests/" + requestID.String() + "/cancel", want: fiber.StatusOK},
		{method: "POST", path: "/api/v1/stock-requests/" + requestID.String() + "/issue", want: fiber.StatusOK},
	}

	for _, tc := range cases {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			if tc.body != "" {
				req.Header.Set("Content-Type", "application/json")
			}

			resp, err := app.Test(req, -1)
			require.NoError(t, err)
			defer resp.Body.Close()

			assert.Equal(t, tc.want, resp.StatusCode)
		})
	}
}
