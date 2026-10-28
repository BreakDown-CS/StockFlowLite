package config_test

import (
	"os"
	"testing"
	"time"

	"github.com/BreakDown-CS/StockFlowLite/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadFromEnv_Defaults(t *testing.T) {
	// Ensure clean environment for relevant variables
	envKeys := []string{
		"APP_ENV", "APP_PORT", "APP_SHUTDOWN_TIMEOUT",
		"DB_HOST", "DB_PORT", "DB_USER", "DB_PASSWORD", "DB_NAME",
		"DB_SSLMODE", "DB_MAX_CONNS", "DB_MIN_CONNS",
		"DB_MAX_CONN_LIFETIME", "DB_MAX_CONN_IDLE_TIME", "DB_HEALTH_TIMEOUT",
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
