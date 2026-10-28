package config

import (
	"bufio"
	"log/slog"
	"os"
	"strings"
)

// defaultEnvFile is loaded automatically by LoadFromEnv when it exists.
const defaultEnvFile = ".env"

// EnvFileEnvVar allows overriding which dotenv file is loaded (e.g. ".env.local").
const EnvFileEnvVar = "ENV_FILE"

// loadDotEnvFile loads the dotenv file resolved from ENV_FILE (default ".env").
// Precedence: existing OS environment > dotenv file > built-in defaults.
// A missing file is not an error; the application simply falls back to defaults.
func loadDotEnvFile() {
	path := strings.TrimSpace(os.Getenv(EnvFileEnvVar))
	if path == "" {
		path = defaultEnvFile
	}

	loaded, err := loadDotEnv(path)
	if err != nil {
		slog.Warn("Could not load environment file; falling back to OS environment and defaults",
			"file", path,
			"error", err,
		)
		return
	}

	if loaded {
		slog.Info("Loaded environment file", "file", path)
	}
}

// loadDotEnv parses a dotenv file and sets every KEY=VALUE pair that is not already
// present in the process environment. Blank lines and lines starting with '#' are
// ignored, an optional "export " prefix is tolerated, and surrounding quotes are removed.
// It reports whether the file was found and read.
func loadDotEnv(path string) (bool, error) {
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")

		key, value, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}

		// Values already provided by the OS environment always win.
		if _, exists := os.LookupEnv(key); exists {
			continue
		}

		if err := os.Setenv(key, trimQuotes(strings.TrimSpace(value))); err != nil {
			return true, err
		}
	}

	return true, scanner.Err()
}

func trimQuotes(value string) string {
	if len(value) >= 2 {
		first, last := value[0], value[len(value)-1]
		if (first == '"' && last == '"') || (first == '\'' && last == '\'') {
			return value[1 : len(value)-1]
		}
	}
	return value
}
