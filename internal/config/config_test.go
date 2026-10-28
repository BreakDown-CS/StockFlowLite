package config_test

import (
	"os"
	"testing"
	"time"

	coreauth "github.com/BreakDown-CS/StockFlowLite/internal/auth"
	"github.com/BreakDown-CS/StockFlowLite/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadFromEnv_Defaults(t *testing.T) {
	// Ensure clean environment for relevant variables
	envKeys := []string{
		"APP_ENV", "APP_PORT", "APP_SHUTDOWN_TIMEOUT",
		"APP_READ_TIMEOUT", "APP_WRITE_TIMEOUT", "APP_IDLE_TIMEOUT", "APP_BODY_LIMIT_BYTES",
		"DB_HOST", "DB_PORT", "DB_USER", "DB_PASSWORD", "DB_NAME",
		"DB_SSLMODE", "DB_MAX_CONNS", "DB_MIN_CONNS",
		"DB_MAX_CONN_LIFETIME", "DB_MAX_CONN_IDLE_TIME", "DB_HEALTH_TIMEOUT", "DB_STATEMENT_TIMEOUT",
		"AUTH_TOKEN_TTL", "AUTH_TOKEN_PEPPER", "AUTH_PBKDF2_ITERATIONS",
		"AUTH_MAX_LOGIN_ATTEMPTS", "AUTH_LOGIN_WINDOW", "AUTH_SESSION_TOUCH_INTERVAL",
	}
	for _, k := range envKeys {
		os.Unsetenv(k)
	}

	cfg, err := config.LoadFromEnv()
	require.NoError(t, err)
	assert.Equal(t, "development", cfg.App.Env)
	assert.Equal(t, 8080, cfg.App.Port)
	assert.Equal(t, 10*time.Second, cfg.App.ShutdownTimeout)
	assert.Equal(t, "127.0.0.1", cfg.DB.Host)
	assert.Equal(t, 5432, cfg.DB.Port)
	assert.Equal(t, "stockflow_user", cfg.DB.User)
	assert.Equal(t, "stockflow_db", cfg.DB.Name)
	assert.Equal(t, int32(25), cfg.DB.MaxConns)
	assert.Equal(t, int32(5), cfg.DB.MinConns)

	// Authentication defaults: a strong work factor and a non-empty pepper.
	assert.Equal(t, 12*time.Hour, cfg.Auth.TokenTTL)
	assert.GreaterOrEqual(t, len(cfg.Auth.TokenPepper), 16)
	assert.GreaterOrEqual(t, cfg.Auth.PBKDF2Iterations, coreauth.MinIterations)
	assert.Equal(t, 8, cfg.Auth.MaxLoginAttempts)
	assert.Equal(t, 15*time.Minute, cfg.Auth.LoginWindow)
	assert.Equal(t, 5*time.Minute, cfg.Auth.SessionTouchInterval)

	// Operational defaults (Phase 9 hardening).
	assert.Equal(t, 15*time.Second, cfg.App.ReadTimeout)
	assert.Equal(t, 30*time.Second, cfg.App.WriteTimeout)
	assert.Equal(t, 60*time.Second, cfg.App.IdleTimeout)
	assert.Equal(t, 1<<20, cfg.App.BodyLimit)
	assert.Equal(t, 15*time.Second, cfg.DB.StatementTimeout)
	assert.True(t, cfg.IsDevelopment())
}

func TestConfig_RejectsDevelopmentSecretsOutsideDevelopment(t *testing.T) {
	cfg, err := config.LoadFromEnv()
	require.NoError(t, err)

	cfg.App.Env = "production"
	// Defaults are still the development values at this point.
	err = cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "DB_PASSWORD must not use the development default")
	assert.Contains(t, err.Error(), "AUTH_TOKEN_PEPPER must not use the development default")

	cfg.DB.Password = "a-real-production-password"
	cfg.Auth.TokenPepper = "a-real-production-pepper-value"
	require.NoError(t, cfg.Validate())
}

func TestConfig_Validation(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(c *config.Config)
		wantErr string
	}{
		{
			name: "invalid app port - negative",
			mutate: func(c *config.Config) {
				c.App.Port = -1
			},
			wantErr: "APP_PORT must be between 1 and 65535",
		},
		{
			name: "invalid app port - too large",
			mutate: func(c *config.Config) {
				c.App.Port = 70000
			},
			wantErr: "APP_PORT must be between 1 and 65535",
		},
		{
			name: "invalid shutdown timeout",
			mutate: func(c *config.Config) {
				c.App.ShutdownTimeout = 0
			},
			wantErr: "APP_SHUTDOWN_TIMEOUT must be positive",
		},
		{
			name: "empty db host",
			mutate: func(c *config.Config) {
				c.DB.Host = ""
			},
			wantErr: "DB_HOST cannot be empty",
		},
		{
			name: "empty db user",
			mutate: func(c *config.Config) {
				c.DB.User = "   "
			},
			wantErr: "DB_USER cannot be empty",
		},
		{
			name: "empty db name",
			mutate: func(c *config.Config) {
				c.DB.Name = ""
			},
			wantErr: "DB_NAME cannot be empty",
		},
		{
			name: "min conns exceeds max conns",
			mutate: func(c *config.Config) {
				c.DB.MinConns = 30
				c.DB.MaxConns = 10
			},
			wantErr: "DB_MIN_CONNS cannot exceed DB_MAX_CONNS",
		},
		{
			name: "invalid db health timeout",
			mutate: func(c *config.Config) {
				c.DB.HealthTimeout = 0
			},
			wantErr: "DB_HEALTH_TIMEOUT must be positive",
		},
		{
			name: "invalid auth token ttl",
			mutate: func(c *config.Config) {
				c.Auth.TokenTTL = 0
			},
			wantErr: "AUTH_TOKEN_TTL must be positive",
		},
		{
			name: "auth token pepper too short",
			mutate: func(c *config.Config) {
				c.Auth.TokenPepper = "too-short"
			},
			wantErr: "AUTH_TOKEN_PEPPER must be at least 16 characters",
		},
		{
			name: "auth pbkdf2 iterations below minimum",
			mutate: func(c *config.Config) {
				c.Auth.PBKDF2Iterations = 1_000
			},
			wantErr: "AUTH_PBKDF2_ITERATIONS must be at least",
		},
		{
			name: "invalid auth max login attempts",
			mutate: func(c *config.Config) {
				c.Auth.MaxLoginAttempts = 0
			},
			wantErr: "AUTH_MAX_LOGIN_ATTEMPTS must be positive",
		},
		{
			name: "invalid auth login window",
			mutate: func(c *config.Config) {
				c.Auth.LoginWindow = 0
			},
			wantErr: "AUTH_LOGIN_WINDOW must be positive",
		},
		{
			name: "invalid app read timeout",
			mutate: func(c *config.Config) {
				c.App.ReadTimeout = 0
			},
			wantErr: "APP_READ_TIMEOUT must be positive",
		},
		{
			name: "invalid app body limit",
			mutate: func(c *config.Config) {
				c.App.BodyLimit = 0
			},
			wantErr: "APP_BODY_LIMIT_BYTES must be positive",
		},
		{
			name: "invalid statement timeout",
			mutate: func(c *config.Config) {
				c.DB.StatementTimeout = 0
			},
			wantErr: "DB_STATEMENT_TIMEOUT must be positive",
		},
		{
			name: "invalid session touch interval",
			mutate: func(c *config.Config) {
				c.Auth.SessionTouchInterval = 0
			},
			wantErr: "AUTH_SESSION_TOUCH_INTERVAL must be positive",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := config.LoadFromEnv()
			require.NoError(t, err)
			tt.mutate(cfg)
			err = cfg.Validate()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestConfig_RedactedConnString(t *testing.T) {
	cfg := &config.DBConfig{
		Host:     "localhost",
		Port:     5432,
		User:     "admin",
		Password: "SuperSecretPassword123",
		Name:     "inventory",
		SSLMode:  "disable",
	}

	redacted := cfg.RedactedConnString()
	assert.NotContains(t, redacted, "SuperSecretPassword123")
	assert.Contains(t, redacted, "postgres://admin:***@localhost:5432/inventory?sslmode=disable")
}
