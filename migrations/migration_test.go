package migrations_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMigrations_StructureAndIntegrity(t *testing.T) {
	upFiles, err := filepath.Glob("*.up.sql")
	require.NoError(t, err)
	require.NotEmpty(t, upFiles, "At least one .up.sql file must exist")

	downFiles, err := filepath.Glob("*.down.sql")
	require.NoError(t, err)
	require.Equal(t, len(upFiles), len(downFiles), "Each .up.sql must have an accompanying .down.sql")

	for _, upFile := range upFiles {
		// 1. Verify corresponding down file exists
		downFile := strings.TrimSuffix(upFile, ".up.sql") + ".down.sql"
		_, err := os.Stat(downFile)
		assert.NoError(t, err, "Expected corresponding down migration: %s", downFile)

		// 2. Verify file is not empty
		content, err := os.ReadFile(upFile)
		require.NoError(t, err)
		assert.NotEmpty(t, strings.TrimSpace(string(content)), "%s cannot be empty", upFile)

		downContent, err := os.ReadFile(downFile)
		require.NoError(t, err)
		assert.NotEmpty(t, strings.TrimSpace(string(downContent)), "%s cannot be empty", downFile)

		// 3. Verify naming convention (e.g., 000001_name.up.sql)
		base := filepath.Base(upFile)
		parts := strings.Split(base, "_")
		require.GreaterOrEqual(t, len(parts), 2, "Migration file must follow convention <version>_<name>.up.sql: %s", base)
		assert.Len(t, parts[0], 6, "Version should be 6-digit zero-padded number: %s", parts[0])
	}
}

func TestMigrations_ConstraintDeclarations(t *testing.T) {
	// Verify that table definitions contain critical constraints
	userMigration, err := os.ReadFile("000001_create_users.up.sql")
	require.NoError(t, err)
	userSQL := string(userMigration)
	assert.Contains(t, userSQL, "CHECK (role IN ('ADMIN', 'STOREKEEPER', 'REQUESTER'))")
	assert.Contains(t, userSQL, "username VARCHAR(50) NOT NULL UNIQUE")

	prodMigration, err := os.ReadFile("000002_create_products.up.sql")
	require.NoError(t, err)
	prodSQL := string(prodMigration)
	assert.Contains(t, prodSQL, "CHECK (current_stock >= 0)")
	assert.Contains(t, prodSQL, "CHECK (min_stock >= 0)")
	assert.Contains(t, prodSQL, "sku VARCHAR(50) NOT NULL UNIQUE")

	reqMigration, err := os.ReadFile("000003_create_stock_requests.up.sql")
	require.NoError(t, err)
	reqSQL := string(reqMigration)
	assert.Contains(t, reqSQL, "CHECK (requested_quantity > 0)")
	assert.Contains(t, reqSQL, "CHECK (status IN ('PENDING', 'APPROVED', 'REJECTED', 'ISSUED', 'CANCELLED'))")

	movMigration, err := os.ReadFile("000004_create_stock_movements.up.sql")
	require.NoError(t, err)
	movSQL := string(movMigration)
	assert.Contains(t, movSQL, "CHECK (movement_type IN ('RECEIPT', 'ISSUE', 'ADJUSTMENT'))")
	assert.Contains(t, movSQL, "CHECK (quantity > 0)")

	issuanceMigration, err := os.ReadFile("000006_create_stock_request_issuances.up.sql")
	require.NoError(t, err)
	issuanceSQL := string(issuanceMigration)
	assert.Contains(t, issuanceSQL, "REFERENCES stock_requests(id)")
	assert.Contains(t, issuanceSQL, "CONSTRAINT uq_stock_request_issuances_request UNIQUE (request_id)")
	assert.Contains(t, issuanceSQL, "CONSTRAINT uq_stock_request_issuances_key UNIQUE (idempotency_key)")
	assert.Contains(t, issuanceSQL, "CHECK (item_count > 0)")
	assert.Contains(t, issuanceSQL, "CHECK (total_quantity > 0)")

	authMigration, err := os.ReadFile("000007_auth_and_audit.up.sql")
	require.NoError(t, err)
	authSQL := string(authMigration)
	assert.Contains(t, authSQL, "ALTER TABLE users ADD COLUMN IF NOT EXISTS is_active BOOLEAN NOT NULL DEFAULT true")
	assert.Contains(t, authSQL, "CREATE TABLE IF NOT EXISTS auth_sessions")
	assert.Contains(t, authSQL, "token_hash CHAR(64) NOT NULL UNIQUE")
	assert.Contains(t, authSQL, "CREATE TABLE IF NOT EXISTS audit_events")
	assert.Contains(t, authSQL, "ALTER TABLE stock_requests ADD COLUMN IF NOT EXISTS cancellation_reason TEXT")
}

// TestMigrations_AuthDownReversesUp guards the Phase 7 rollback path.
func TestMigrations_AuthDownReversesUp(t *testing.T) {
	downMigration, err := os.ReadFile("000007_auth_and_audit.down.sql")
	require.NoError(t, err)

	downSQL := string(downMigration)
	assert.Contains(t, downSQL, "DROP TABLE IF EXISTS audit_events")
	assert.Contains(t, downSQL, "DROP TABLE IF EXISTS auth_sessions")
	// The rollback must never drop Phase 1-6 tables.
	assert.NotContains(t, downSQL, "DROP TABLE IF EXISTS users")
	assert.NotContains(t, downSQL, "DROP TABLE IF EXISTS stock_requests")
}

// TestMigrations_IssuanceDownReversesUp guards the Phase 6 rollback path.
func TestMigrations_IssuanceDownReversesUp(t *testing.T) {
	downMigration, err := os.ReadFile("000006_create_stock_request_issuances.down.sql")
	require.NoError(t, err)

	downSQL := string(downMigration)
	assert.Contains(t, downSQL, "DROP TABLE IF EXISTS stock_request_issuances")
	// The rollback must never touch Phase 1-5 tables.
	assert.NotContains(t, downSQL, "DROP TABLE IF EXISTS stock_requests")
	assert.NotContains(t, downSQL, "DROP TABLE IF EXISTS stock_request_items")
	assert.NotContains(t, downSQL, "DROP TABLE IF EXISTS stock_movements")
}
