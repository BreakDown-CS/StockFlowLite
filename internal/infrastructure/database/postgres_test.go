package database_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/BreakDown-CS/StockFlowLite/internal/infrastructure/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockPinger struct {
	err           error
	sleepDuration time.Duration
}

func (m *mockPinger) Ping(ctx context.Context) error {
	if m.sleepDuration > 0 {
		select {
		case <-time.After(m.sleepDuration):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return m.err
}

func TestCheckReadiness_Success(t *testing.T) {
	p := &mockPinger{err: nil}
	err := database.CheckReadiness(context.Background(), p, 500*time.Millisecond)
	require.NoError(t, err)
}

func TestCheckReadiness_NilPinger(t *testing.T) {
	err := database.CheckReadiness(context.Background(), nil, 500*time.Millisecond)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "database connection is not initialized")
}

func TestCheckReadiness_PingError(t *testing.T) {
	p := &mockPinger{err: errors.New("connection failed")}
	err := database.CheckReadiness(context.Background(), p, 500*time.Millisecond)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "database ping failed: connection failed")
}

func TestCheckReadiness_Timeout(t *testing.T) {
	p := &mockPinger{sleepDuration: 200 * time.Millisecond}
	err := database.CheckReadiness(context.Background(), p, 50*time.Millisecond)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "context deadline exceeded")
}
