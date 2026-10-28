package handler_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/BreakDown-CS/StockFlowLite/internal/adapter/http/fiber/handler"
	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockPinger implements database.Pinger for testing.
type mockPinger struct {
	err error
}

func (m *mockPinger) Ping(ctx context.Context) error {
	return m.err
}

func setupTestApp(h *handler.HealthHandler) *fiber.App {
	app := fiber.New(fiber.Config{
		DisableStartupMessage: true,
	})
	app.Get("/health", h.Health)
	app.Get("/ready", h.Ready)
	return app
}

func TestHealthHandler_Health(t *testing.T) {
	// Health endpoint must succeed even if DB is nil or failing
	h := handler.NewHealthHandler(nil, 1*time.Second)
	app := setupTestApp(h)

	req := httptest.NewRequest("GET", "/health", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, fiber.StatusOK, resp.StatusCode)

	bodyBytes, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	var payload handler.HealthResponse
	err = json.Unmarshal(bodyBytes, &payload)
	require.NoError(t, err)
	assert.Equal(t, "ok", payload.Status)
	assert.Equal(t, "stockflow-lite", payload.Service)
}

func TestHealthHandler_Ready_Success(t *testing.T) {
	mockDB := &mockPinger{err: nil}
	h := handler.NewHealthHandler(mockDB, 1*time.Second)
	app := setupTestApp(h)

	req := httptest.NewRequest("GET", "/ready", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, fiber.StatusOK, resp.StatusCode)

	bodyBytes, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	var payload handler.ReadyResponse
	err = json.Unmarshal(bodyBytes, &payload)
	require.NoError(t, err)
	assert.Equal(t, "ready", payload.Status)
	assert.Equal(t, "up", payload.Database)
}

func TestHealthHandler_Ready_Failure(t *testing.T) {
	mockDB := &mockPinger{err: errors.New("connection refused")}
	h := handler.NewHealthHandler(mockDB, 1*time.Second)
	app := setupTestApp(h)

	req := httptest.NewRequest("GET", "/ready", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, fiber.StatusServiceUnavailable, resp.StatusCode)

	bodyBytes, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	var payload handler.ReadyResponse
	err = json.Unmarshal(bodyBytes, &payload)
	require.NoError(t, err)
	assert.Equal(t, "unavailable", payload.Status)
	assert.Equal(t, "down", payload.Database)
	assert.NotContains(t, string(bodyBytes), "connection refused")
}

func TestHealthHandler_Ready_NilPinger(t *testing.T) {
	h := handler.NewHealthHandler(nil, 1*time.Second)
	app := setupTestApp(h)

	req := httptest.NewRequest("GET", "/ready", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, fiber.StatusServiceUnavailable, resp.StatusCode)

	bodyBytes, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	var payload handler.ReadyResponse
	err = json.Unmarshal(bodyBytes, &payload)
	require.NoError(t, err)
	assert.Equal(t, "unavailable", payload.Status)
	assert.Equal(t, "down", payload.Database)
}
