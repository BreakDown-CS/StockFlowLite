package fiber_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	httpfiber "github.com/BreakDown-CS/StockFlowLite/internal/adapter/http/fiber"
	"github.com/BreakDown-CS/StockFlowLite/internal/adapter/http/fiber/handler"
	"github.com/BreakDown-CS/StockFlowLite/internal/domain"
	authusecase "github.com/BreakDown-CS/StockFlowLite/internal/usecase/auth"
	reportusecase "github.com/BreakDown-CS/StockFlowLite/internal/usecase/report"
	"github.com/BreakDown-CS/StockFlowLite/internal/usecase/stockmovement"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubAuthUsecase resolves a fixed token map to principals (no database required).
type stubAuthUsecase struct {
	actors map[string]domain.Actor
}

func (s stubAuthUsecase) Login(context.Context, authusecase.LoginInput) (*authusecase.LoginResult, error) {
	user := &domain.User{ID: uuid.New(), Username: "login_test", Role: domain.RoleRequester, IsActive: true}
	return &authusecase.LoginResult{Token: "issued-token", TokenType: "Bearer", ExpiresAt: time.Now().Add(time.Hour), User: user}, nil
}

func (s stubAuthUsecase) Logout(context.Context, domain.Actor, uuid.UUID) error { return nil }

func (s stubAuthUsecase) Me(context.Context, domain.Actor) (*domain.User, error) {
	return &domain.User{ID: uuid.New(), Username: "me", Role: domain.RoleRequester, IsActive: true}, nil
}

func (s stubAuthUsecase) ChangePassword(context.Context, domain.Actor, string, string) error {
	return nil
}

func (s stubAuthUsecase) Authenticate(_ context.Context, token string) (domain.Actor, *domain.AuthSession, error) {
	actor, ok := s.actors[token]
	if !ok {
		return domain.Actor{}, nil, domain.ErrUnauthenticated
	}
	return actor, &domain.AuthSession{ID: uuid.New(), UserID: actor.ID}, nil
}

type stubStockMovementUsecase struct{}

func (stubStockMovementUsecase) ReceiveStock(context.Context, stockmovement.ReceiveStockInput) (*domain.StockReceiptResult, error) {
	product := &domain.Product{ID: uuid.New(), SKU: "SKU-STUB", Name: "Stub", Unit: "PCS", IsActive: true}
	return &domain.StockReceiptResult{Product: product, Movement: &domain.StockMovement{ID: 1}}, nil
}

func (stubStockMovementUsecase) ListMovements(context.Context, stockmovement.ListMovementsInput) ([]domain.StockMovement, int64, error) {
	return []domain.StockMovement{}, 0, nil
}

type stubAuditRepository struct{}

func (stubAuditRepository) Record(context.Context, domain.AuditEvent) error { return nil }

func (stubAuditRepository) List(context.Context, domain.ListAuditFilter) ([]domain.AuditEvent, int64, error) {
	return []domain.AuditEvent{}, 0, nil
}

type stubReportUsecase struct{}

func (stubReportUsecase) Inventory(context.Context, reportusecase.InventoryInput) (*reportusecase.InventoryReport, int64, error) {
	return &reportusecase.InventoryReport{Items: []domain.InventoryReportRow{}}, 0, nil
}

func (stubReportUsecase) Movements(context.Context, reportusecase.MovementInput) (*reportusecase.MovementReport, int64, error) {
	return &reportusecase.MovementReport{Items: []domain.MovementReportRow{}}, 0, nil
}

func (stubReportUsecase) Requests(context.Context, reportusecase.RequestInput) (*reportusecase.RequestReport, int64, error) {
	return &reportusecase.RequestReport{Items: []domain.RequestReportRow{}}, 0, nil
}

func (stubReportUsecase) StockIntegrity(context.Context, reportusecase.StockIntegrityInput) (*reportusecase.StockIntegrityReport, int64, error) {
	return &reportusecase.StockIntegrityReport{Items: []domain.StockIntegrityRow{}}, 0, nil
}

var (
	requesterActor = domain.Actor{ID: uuid.New(), Username: "requester", Role: domain.RoleRequester}
	keeperActor    = domain.Actor{ID: uuid.New(), Username: "keeper", Role: domain.RoleStorekeeper}
	adminActor     = domain.Actor{ID: uuid.New(), Username: "admin", Role: domain.RoleAdmin}
)

func authTestApp() *fiber.App {
	authUC := stubAuthUsecase{actors: map[string]domain.Actor{
		"requester-token": requesterActor,
		"keeper-token":    keeperActor,
		"admin-token":     adminActor,
	}}

	return httpfiber.NewRouter(httpfiber.RouterConfig{
		StockRequestHandler:  handler.NewStockRequestHandler(stubStockRequestUsecase{}),
		StockReceiptHandler:  handler.NewStockReceiptHandler(stubStockMovementUsecase{}),
		StockMovementHandler: handler.NewStockMovementHandler(stubStockMovementUsecase{}),
		AuthHandler:          handler.NewAuthHandler(authUC),
		AuditHandler:         handler.NewAuditHandler(stubAuditRepository{}),
		ReportHandler:        handler.NewReportHandler(stubReportUsecase{}),
		AuthUsecase:          authUC,
		AuditRepository:      stubAuditRepository{},
	})
}

func doAuthed(app *fiber.App, method, path, token, body string) (*http.Response, error) {
	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}

	req := httptest.NewRequest(method, path, reader)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	return app.Test(req, -1)
}

func TestNewRouter_RequiresAuthentication(t *testing.T) {
	app := authTestApp()

	cases := []struct {
		name   string
		method string
		path   string
		token  string
		body   string
		want   int
	}{
		{name: "missing token", method: "GET", path: "/api/v1/stock-requests", want: fiber.StatusUnauthorized},
		{name: "unknown token", method: "GET", path: "/api/v1/stock-requests", token: "ghost-token", want: fiber.StatusUnauthorized},
		{name: "requester can read requests", method: "GET", path: "/api/v1/stock-requests", token: "requester-token", want: fiber.StatusOK},
		{name: "storekeeper can read requests", method: "GET", path: "/api/v1/stock-requests", token: "keeper-token", want: fiber.StatusOK},
		{name: "requester cannot receive stock", method: "POST", path: "/api/v1/stock-receipts", token: "requester-token", body: `{"product_id":"` + uuid.New().String() + `","quantity":1}`, want: fiber.StatusForbidden},
		{name: "storekeeper can receive stock", method: "POST", path: "/api/v1/stock-receipts", token: "keeper-token", body: `{"product_id":"` + uuid.New().String() + `","quantity":1}`, want: fiber.StatusCreated},
		{name: "storekeeper cannot read audit trail", method: "GET", path: "/api/v1/audit-events", token: "keeper-token", want: fiber.StatusForbidden},
		{name: "admin can read audit trail", method: "GET", path: "/api/v1/audit-events", token: "admin-token", want: fiber.StatusOK},
		{name: "reports require a token", method: "GET", path: "/api/v1/reports/inventory", want: fiber.StatusUnauthorized},
		{name: "requester can read inventory report", method: "GET", path: "/api/v1/reports/inventory", token: "requester-token", want: fiber.StatusOK},
		{name: "requester can read movement report", method: "GET", path: "/api/v1/reports/stock-movements", token: "requester-token", want: fiber.StatusOK},
		{name: "requester can read request report", method: "GET", path: "/api/v1/reports/stock-requests", token: "requester-token", want: fiber.StatusOK},
		{name: "requester cannot read integrity report", method: "GET", path: "/api/v1/reports/integrity/stock", token: "requester-token", want: fiber.StatusForbidden},
		{name: "admin can read integrity report", method: "GET", path: "/api/v1/reports/integrity/stock", token: "admin-token", want: fiber.StatusOK},
		{name: "login is public", method: "POST", path: "/api/v1/auth/login", body: `{"username":"someone","password":"whatever-pass"}`, want: fiber.StatusOK},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := doAuthed(app, tc.method, tc.path, tc.token, tc.body)
			require.NoError(t, err)
			defer resp.Body.Close()

			assert.Equal(t, tc.want, resp.StatusCode)
		})
	}
}
