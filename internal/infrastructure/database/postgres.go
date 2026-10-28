package database

import (
	"context"
	"fmt"
	"time"

	"github.com/BreakDown-CS/StockFlowLite/internal/config"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Pinger defines the minimal contract needed to check database reachability.
type Pinger interface {
	Ping(ctx context.Context) error
}

// NewPool initializes and configures a pgxpool.Pool from DBConfig.
func NewPool(ctx context.Context, cfg *config.DBConfig) (*pgxpool.Pool, error) {
	connStr := cfg.ConnString()

	poolCfg, err := pgxpool.ParseConfig(connStr)
	if err != nil {
		return nil, fmt.Errorf("failed to parse database configuration: %w", err)
	}

	poolCfg.MaxConns = cfg.MaxConns
	poolCfg.MinConns = cfg.MinConns
	poolCfg.MaxConnLifetime = cfg.MaxConnLifetime
	poolCfg.MaxConnIdleTime = cfg.MaxConnIdleTime

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create database connection pool: %w", err)
	}

	return pool, nil
}

// CheckReadiness executes a bounded ping against the database.
func CheckReadiness(ctx context.Context, pinger Pinger, timeout time.Duration) error {
	if pinger == nil {
		return fmt.Errorf("database connection is not initialized")
	}

	pingCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	if err := pinger.Ping(pingCtx); err != nil {
		return fmt.Errorf("database ping failed: %w", err)
	}

	return nil
}
