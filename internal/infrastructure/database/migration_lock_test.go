package database_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/BreakDown-CS/StockFlowLite/internal/config"
	"github.com/BreakDown-CS/StockFlowLite/internal/infrastructure/database"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMigrator_Up_ConcurrentRunsSerialize verifies that the advisory lock keeps
// several migration runners from racing against each other.
func TestMigrator_Up_ConcurrentRunsSerialize(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("TEST_DATABASE_URL"))
	if dsn == "" {
		cfg, err := config.LoadFromEnv()
		require.NoError(t, err)
		dsn = cfg.DB.ConnString()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Skipf("skipping migration lock test: %v", err)
	}
	t.Cleanup(pool.Close)

	if err := pool.Ping(ctx); err != nil {
		t.Skipf("skipping migration lock test: database unreachable (%v)", err)
	}

	migrationsDir, err := filepath.Abs("../../../migrations")
	require.NoError(t, err)
	require.DirExists(t, migrationsDir, "migrations directory must exist for this test")

	migrator := database.NewMigrator(pool, migrationsDir)

	const workers = 5
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	start := make(chan struct{})

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start

			runCtx, runCancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer runCancel()

			_, runErr := migrator.Up(runCtx, 0)
			errs <- runErr
		}()
	}

	close(start)
	wg.Wait()
	close(errs)

	for runErr := range errs {
		require.NoError(t, runErr, "concurrent migration runs must serialize instead of conflicting")
	}

	var applied int
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(DISTINCT version) FROM schema_migrations`).Scan(&applied))
	assert.GreaterOrEqual(t, applied, 1, "schema_migrations must still be consistent")
}
