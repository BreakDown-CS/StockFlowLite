package main

// Command user provisions the first administrator (or resets an existing password)
// without ever storing or printing the credential.
//
// Usage:
//
//	BOOTSTRAP_ADMIN_USERNAME=admin BOOTSTRAP_ADMIN_PASSWORD='...' go run ./cmd/user -action create
//	BOOTSTRAP_ADMIN_USERNAME=admin BOOTSTRAP_ADMIN_PASSWORD='...' go run ./cmd/user -action reset-password
//
// The password policy (>= 8 characters, different from the username) is enforced.

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/BreakDown-CS/StockFlowLite/internal/adapter/repository/postgres"
	coreauth "github.com/BreakDown-CS/StockFlowLite/internal/auth"
	"github.com/BreakDown-CS/StockFlowLite/internal/config"
	"github.com/BreakDown-CS/StockFlowLite/internal/domain"
	"github.com/BreakDown-CS/StockFlowLite/internal/infrastructure/database"
)

func main() {
	action := flag.String("action", "create", "create | reset-password")
	usernameFlag := flag.String("username", "", "username (defaults to BOOTSTRAP_ADMIN_USERNAME)")
	roleFlag := flag.String("role", "ADMIN", "role for -action=create: ADMIN, STOREKEEPER, REQUESTER")
	flag.Parse()

	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))

	username := strings.TrimSpace(*usernameFlag)
	if username == "" {
		username = strings.TrimSpace(os.Getenv("BOOTSTRAP_ADMIN_USERNAME"))
	}
	password := os.Getenv("BOOTSTRAP_ADMIN_PASSWORD")

	if username == "" {
		exitWith("username is required (-username or BOOTSTRAP_ADMIN_USERNAME)")
	}
	if password == "" {
		exitWith("password is required (BOOTSTRAP_ADMIN_PASSWORD); it is never logged")
	}
	if err := coreauth.ValidatePasswordPolicy(username, password); err != nil {
		exitWith(err.Error())
	}

	cfg, err := config.LoadFromEnv()
	if err != nil {
		exitWith("configuration error: " + err.Error())
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pool, err := database.NewPool(ctx, &cfg.DB)
	if err != nil {
		exitWith("database pool error: " + err.Error())
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		exitWith("database is unreachable: " + err.Error())
	}

	hasher := coreauth.NewHasher(cfg.Auth.PBKDF2Iterations)
	repo := postgres.NewUserRepository(pool)

	// CLI operations are attributed to the system actor.
	ctx = domain.WithActor(ctx, domain.SystemActor())

	switch *action {
	case "create":
		role := domain.UserRole(strings.ToUpper(strings.TrimSpace(*roleFlag)))
		hash, hashErr := hasher.Hash(password)
		if hashErr != nil {
			exitWith("failed to hash password: " + hashErr.Error())
		}

		user, newErr := domain.NewUser(username, hash, role)
		if newErr != nil {
			exitWith(newErr.Error())
		}

		if createErr := repo.Create(ctx, user); createErr != nil {
			exitWith("failed to create user: " + createErr.Error())
		}

		fmt.Printf("created user %s (id=%s, role=%s)\n", user.Username, user.ID, user.Role)

	case "reset-password":
		user, findErr := repo.GetByUsername(ctx, username)
		if findErr != nil {
			exitWith("failed to load user: " + findErr.Error())
		}

		hash, hashErr := hasher.Hash(password)
		if hashErr != nil {
			exitWith("failed to hash password: " + hashErr.Error())
		}

		if updateErr := repo.UpdatePasswordHash(ctx, user.ID, hash, time.Now().UTC()); updateErr != nil {
			exitWith("failed to update password: " + updateErr.Error())
		}

		fmt.Printf("password updated for user %s (id=%s)\n", user.Username, user.ID)

	default:
		exitWith("unknown action; valid actions are 'create' and 'reset-password'")
	}
}

func exitWith(message string) {
	slog.Error(message)
	os.Exit(1)
}
