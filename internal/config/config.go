package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	coreauth "github.com/BreakDown-CS/StockFlowLite/internal/auth"
)

// Config aggregates all application configuration values.
type Config struct {
	App  AppConfig
	DB   DBConfig
	Auth AuthConfig
}

// AppConfig defines HTTP server and general application settings.
type AppConfig struct {
	Env             string
	Port            int
	ShutdownTimeout time.Duration
}

// AuthConfig defines authentication policy. Secrets are never logged.
type AuthConfig struct {
	TokenTTL         time.Duration
	TokenPepper      string
	PBKDF2Iterations int
	MaxLoginAttempts int
	LoginWindow      time.Duration
}

// DBConfig defines PostgreSQL connection and pool parameters.
type DBConfig struct {
	Host            string
	Port            int
	User            string
	Password        string
	Name            string
	SSLMode         string
	MaxConns        int32
	MinConns        int32
	MaxConnLifetime time.Duration
	MaxConnIdleTime time.Duration
	HealthTimeout   time.Duration
}

// LoadFromEnv loads configuration from a dotenv file (optional) and environment
// variables with safe defaults. Precedence: OS environment > dotenv > defaults.
func LoadFromEnv() (*Config, error) {
	loadDotEnvFile()

	cfg := &Config{
		App: AppConfig{
			Env:             getEnvString("APP_ENV", "development"),
			Port:            getEnvInt("APP_PORT", 8080),
			ShutdownTimeout: getEnvDuration("APP_SHUTDOWN_TIMEOUT", 10*time.Second),
		},
		DB: DBConfig{
			Host:            getEnvString("DB_HOST", "127.0.0.1"),
			Port:            getEnvInt("DB_PORT", 5432),
			User:            getEnvString("DB_USER", "stockflow_user"),
			Password:        getEnvString("DB_PASSWORD", "stockflow_dev_password"),
			Name:            getEnvString("DB_NAME", "stockflow_db"),
			SSLMode:         getEnvString("DB_SSLMODE", "disable"),
			MaxConns:        int32(getEnvInt("DB_MAX_CONNS", 25)),
			MinConns:        int32(getEnvInt("DB_MIN_CONNS", 5)),
			MaxConnLifetime: getEnvDuration("DB_MAX_CONN_LIFETIME", 1*time.Hour),
			MaxConnIdleTime: getEnvDuration("DB_MAX_CONN_IDLE_TIME", 30*time.Minute),
			HealthTimeout:   getEnvDuration("DB_HEALTH_TIMEOUT", 3*time.Second),
		},
		Auth: AuthConfig{
			TokenTTL:         getEnvDuration("AUTH_TOKEN_TTL", 12*time.Hour),
			TokenPepper:      getEnvString("AUTH_TOKEN_PEPPER", "stockflow-development-pepper-change-me"),
			PBKDF2Iterations: getEnvInt("AUTH_PBKDF2_ITERATIONS", coreauth.DefaultIterations),
			MaxLoginAttempts: getEnvInt("AUTH_MAX_LOGIN_ATTEMPTS", 8),
			LoginWindow:      getEnvDuration("AUTH_LOGIN_WINDOW", 15*time.Minute),
		},
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("configuration validation failed: %w", err)
	}

	return cfg, nil
}

// Validate ensures required configuration fields are set and within safe boundaries.
func (c *Config) Validate() error {
	var errs []string

	// Validate App Config
	if c.App.Port < 1 || c.App.Port > 65535 {
		errs = append(errs, "APP_PORT must be between 1 and 65535")
	}
	if c.App.ShutdownTimeout <= 0 {
		errs = append(errs, "APP_SHUTDOWN_TIMEOUT must be positive")
	}

	// Validate DB Config
	if strings.TrimSpace(c.DB.Host) == "" {
		errs = append(errs, "DB_HOST cannot be empty")
	}
	if c.DB.Port < 1 || c.DB.Port > 65535 {
		errs = append(errs, "DB_PORT must be between 1 and 65535")
	}
	if strings.TrimSpace(c.DB.User) == "" {
		errs = append(errs, "DB_USER cannot be empty")
	}
	if strings.TrimSpace(c.DB.Name) == "" {
		errs = append(errs, "DB_NAME cannot be empty")
	}
	if c.DB.MaxConns <= 0 {
		errs = append(errs, "DB_MAX_CONNS must be greater than 0")
	}
	if c.DB.MinConns < 0 {
		errs = append(errs, "DB_MIN_CONNS must be non-negative")
	}
	if c.DB.MinConns > c.DB.MaxConns {
		errs = append(errs, "DB_MIN_CONNS cannot exceed DB_MAX_CONNS")
	}
	if c.DB.HealthTimeout <= 0 {
		errs = append(errs, "DB_HEALTH_TIMEOUT must be positive")
	}

	// Validate Auth Config
	if c.Auth.TokenTTL <= 0 {
		errs = append(errs, "AUTH_TOKEN_TTL must be positive")
	}
	if len(strings.TrimSpace(c.Auth.TokenPepper)) < 16 {
		errs = append(errs, "AUTH_TOKEN_PEPPER must be at least 16 characters")
	}
	if c.Auth.PBKDF2Iterations < coreauth.MinIterations {
		errs = append(errs, fmt.Sprintf("AUTH_PBKDF2_ITERATIONS must be at least %d", coreauth.MinIterations))
	}
	if c.Auth.MaxLoginAttempts <= 0 {
		errs = append(errs, "AUTH_MAX_LOGIN_ATTEMPTS must be positive")
	}
	if c.Auth.LoginWindow <= 0 {
		errs = append(errs, "AUTH_LOGIN_WINDOW must be positive")
	}

	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

// ConnString constructs the connection string for pgxpool.
func (d *DBConfig) ConnString() string {
	encodedUser := url.QueryEscape(d.User)
	encodedPass := url.QueryEscape(d.Password)
	sslMode := d.SSLMode
	if sslMode == "" {
		sslMode = "disable"
	}
	return fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=%s",
		encodedUser, encodedPass, d.Host, d.Port, d.Name, sslMode)
}

// RedactedConnString returns a safe representation of connection details omitting secrets.
func (d *DBConfig) RedactedConnString() string {
	sslMode := d.SSLMode
	if sslMode == "" {
		sslMode = "disable"
	}
	return fmt.Sprintf("postgres://%s:***@%s:%d/%s?sslmode=%s",
		d.User, d.Host, d.Port, d.Name, sslMode)
}

func getEnvString(key, fallback string) string {
	if val, ok := os.LookupEnv(key); ok && strings.TrimSpace(val) != "" {
		return strings.TrimSpace(val)
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if val, ok := os.LookupEnv(key); ok && strings.TrimSpace(val) != "" {
		if intVal, err := strconv.Atoi(strings.TrimSpace(val)); err == nil {
			return intVal
		}
	}
	return fallback
}

func getEnvDuration(key string, fallback time.Duration) time.Duration {
	if val, ok := os.LookupEnv(key); ok && strings.TrimSpace(val) != "" {
		if durationVal, err := time.ParseDuration(strings.TrimSpace(val)); err == nil {
			return durationVal
		}
	}
	return fallback
}
