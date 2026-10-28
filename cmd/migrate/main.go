package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/BreakDown-CS/StockFlowLite/internal/config"
	"github.com/BreakDown-CS/StockFlowLite/internal/infrastructure/database"
)

func main() {
	var (
		action = flag.String("action", "up", "Migration action: up, down, status")
		steps  = flag.Int("steps", 0, "Number of migration steps (0 applies all for 'up')")
		dir    = flag.String("dir", "migrations", "Directory containing versioned SQL migrations")
	)
	flag.Parse()

	// 1. Configure logger
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	// 2. Load configuration
	cfg, err := config.LoadFromEnv()
	if err != nil {
		slog.Error("Failed to load environment configuration", "error", err)
		os.Exit(1)
	}

	// 3. Connect to PostgreSQL
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	slog.Info("Connecting to database", "target", cfg.DB.RedactedConnString())
	pool, err := database.NewPool(ctx, &cfg.DB)
	if err != nil {
		slog.Error("Database connection configuration failed", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		slog.Error("Database is unreachable; cannot execute migrations",
			"target", cfg.DB.RedactedConnString(),
			"error", err,
		)
		os.Exit(1)
	}

	migrator := database.NewMigrator(pool, *dir)

	// 4. Execute requested action
	switch *action {
	case "up":
		slog.Info("Applying pending migrations...", "dir", *dir, "steps", *steps)
		applied, err := migrator.Up(ctx, *steps)
		if err != nil {
			slog.Error("Migration 'up' encountered an error", "error", err)
			os.Exit(1)
		}
		if len(applied) == 0 {
			slog.Info("Database schema is already up to date; no pending migrations")
		} else {
			for _, m := range applied {
				slog.Info("Applied migration successfully", "migration", m)
			}
		}

	case "down":
		slog.Warn("Rolling back migration(s)...", "dir", *dir, "steps", *steps)
		rolledBack, err := migrator.Down(ctx, *steps)
		if err != nil {
			slog.Error("Migration 'down' encountered an error", "error", err)
			os.Exit(1)
		}
		if len(rolledBack) == 0 {
			slog.Info("No applied migrations found to roll back")
		} else {
			for _, m := range rolledBack {
				slog.Info("Rolled back migration successfully", "migration", m)
			}
		}

	case "status":
		statuses, err := migrator.Status(ctx)
		if err != nil {
			slog.Error("Failed to inspect migration status", "error", err)
			os.Exit(1)
		}
		fmt.Printf("\nMigration Status (%s):\n", *dir)
		fmt.Println("--------------------------------------------------------------------------------")
		fmt.Printf("%-10s %-35s %-10s %-25s\n", "Version", "Name", "Applied", "Applied At")
		fmt.Println("--------------------------------------------------------------------------------")
		for _, s := range statuses {
			appliedStr := "NO"
			appliedAtStr := "-"
			if s.Applied {
				appliedStr = "YES"
				if s.AppliedAt != nil {
					appliedAtStr = s.AppliedAt.Format("2006-01-02 15:04:05 UTC")
				}
			}
			fmt.Printf("%-10s %-35s %-10s %-25s\n", s.Version, s.Name, appliedStr, appliedAtStr)
		}
		fmt.Println("--------------------------------------------------------------------------------")
		fmt.Println()

	default:
		slog.Error("Unknown action; valid actions are 'up', 'down', 'status'", "action", *action)
		os.Exit(1)
	}
}
