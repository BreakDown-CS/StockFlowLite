package database

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// MigrationStatus reports the state of a single migration version.
type MigrationStatus struct {
	Version   string     `json:"version"`
	Name      string     `json:"name"`
	Applied   bool       `json:"applied"`
	AppliedAt *time.Time `json:"applied_at,omitempty"`
}

// Migrator manages versioned SQL migrations using pgxpool.
type Migrator struct {
	pool    *pgxpool.Pool
	dirPath string
}

// NewMigrator initializes a new Migrator instance.
func NewMigrator(pool *pgxpool.Pool, dirPath string) *Migrator {
	return &Migrator{
		pool:    pool,
		dirPath: dirPath,
	}
}

// EnsureTrackingTable creates the schema_migrations table if it does not already exist.
func (m *Migrator) EnsureTrackingTable(ctx context.Context) error {
	const query = `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version VARCHAR(50) PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
		);
	`
	_, err := m.pool.Exec(ctx, query)
	if err != nil {
		return fmt.Errorf("failed to create schema_migrations table: %w", err)
	}
	return nil
}

// GetApplied returns a map of applied versions and their application timestamps.
func (m *Migrator) GetApplied(ctx context.Context) (map[string]time.Time, error) {
	if err := m.EnsureTrackingTable(ctx); err != nil {
		return nil, err
	}

	rows, err := m.pool.Query(ctx, "SELECT version, applied_at FROM schema_migrations ORDER BY version ASC")
	if err != nil {
		return nil, fmt.Errorf("failed to query applied migrations: %w", err)
	}
	defer rows.Close()

	applied := make(map[string]time.Time)
	for rows.Next() {
		var version string
		var appliedAt time.Time
		if err := rows.Scan(&version, &appliedAt); err != nil {
			return nil, fmt.Errorf("failed to scan migration record: %w", err)
		}
		applied[version] = appliedAt
	}

	return applied, rows.Err()
}

// Up applies pending migrations in ascending order. If steps <= 0, all pending are applied.
func (m *Migrator) Up(ctx context.Context, steps int) ([]string, error) {
	applied, err := m.GetApplied(ctx)
	if err != nil {
		return nil, err
	}

	upFiles, err := filepath.Glob(filepath.Join(m.dirPath, "*.up.sql"))
	if err != nil {
		return nil, fmt.Errorf("failed to list migration files: %w", err)
	}
	sort.Strings(upFiles)

	var executed []string
	for _, file := range upFiles {
		version, name := parseMigrationFilename(filepath.Base(file), ".up.sql")
		if version == "" {
			continue
		}

		if _, isApplied := applied[version]; isApplied {
			continue // Already applied
		}

		content, err := os.ReadFile(file)
		if err != nil {
			return executed, fmt.Errorf("failed to read migration file %s: %w", file, err)
		}

		err = m.execInTx(ctx, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, string(content)); err != nil {
				return fmt.Errorf("error executing %s: %w", filepath.Base(file), err)
			}
			_, err := tx.Exec(ctx, "INSERT INTO schema_migrations (version, applied_at) VALUES ($1, now())", version)
			if err != nil {
				return fmt.Errorf("error recording migration %s: %w", version, err)
			}
			return nil
		})
		if err != nil {
			return executed, err
		}

		executed = append(executed, fmt.Sprintf("%s (%s)", version, name))
		if steps > 0 && len(executed) >= steps {
			break
		}
	}

	return executed, nil
}

// Down rolls back applied migrations in descending order. If steps <= 0, defaults to 1.
func (m *Migrator) Down(ctx context.Context, steps int) ([]string, error) {
	if steps <= 0 {
		steps = 1
	}

	applied, err := m.GetApplied(ctx)
	if err != nil {
		return nil, err
	}

	var appliedVersions []string
	for v := range applied {
		appliedVersions = append(appliedVersions, v)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(appliedVersions)))

	var rolledBack []string
	for _, version := range appliedVersions {
		pattern := filepath.Join(m.dirPath, version+"_*.down.sql")
		matches, err := filepath.Glob(pattern)
		if err != nil || len(matches) == 0 {
			return rolledBack, fmt.Errorf("down migration file not found for version %s", version)
		}

		file := matches[0]
		content, err := os.ReadFile(file)
		if err != nil {
			return rolledBack, fmt.Errorf("failed to read down migration %s: %w", file, err)
		}

		err = m.execInTx(ctx, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, string(content)); err != nil {
				return fmt.Errorf("error executing %s: %w", filepath.Base(file), err)
			}
			_, err := tx.Exec(ctx, "DELETE FROM schema_migrations WHERE version = $1", version)
			if err != nil {
				return fmt.Errorf("error removing migration record %s: %w", version, err)
			}
			return nil
		})
		if err != nil {
			return rolledBack, err
		}

		rolledBack = append(rolledBack, filepath.Base(file))
		if len(rolledBack) >= steps {
			break
		}
	}

	return rolledBack, nil
}

// Status lists all discovered migration files and indicates whether each is applied.
func (m *Migrator) Status(ctx context.Context) ([]MigrationStatus, error) {
	applied, err := m.GetApplied(ctx)
	if err != nil {
		return nil, err
	}

	upFiles, err := filepath.Glob(filepath.Join(m.dirPath, "*.up.sql"))
	if err != nil {
		return nil, fmt.Errorf("failed to list migration files: %w", err)
	}
	sort.Strings(upFiles)

	var statuses []MigrationStatus
	for _, file := range upFiles {
		version, name := parseMigrationFilename(filepath.Base(file), ".up.sql")
		if version == "" {
			continue
		}

		status := MigrationStatus{
			Version: version,
			Name:    name,
		}

		if appliedAt, ok := applied[version]; ok {
			status.Applied = true
			status.AppliedAt = &appliedAt
		}

		statuses = append(statuses, status)
	}

	return statuses, nil
}

func (m *Migrator) execInTx(ctx context.Context, fn func(tx pgx.Tx) error) error {
	tx, err := m.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	if err := fn(tx); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

func parseMigrationFilename(filename, suffix string) (version, name string) {
	if !strings.HasSuffix(filename, suffix) {
		return "", ""
	}
	base := strings.TrimSuffix(filename, suffix)
	parts := strings.SplitN(base, "_", 2)
	if len(parts) == 0 {
		return "", ""
	}
	version = parts[0]
	if len(parts) > 1 {
		name = parts[1]
	}
	return version, name
}
