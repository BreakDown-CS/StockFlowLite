package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// unsetForEnv removes a variable for the duration of the test and restores it afterwards,
// so these tests never leak configuration into the rest of the suite.
func unsetForEnv(t *testing.T, key string) {
	t.Helper()

	original, existed := os.LookupEnv(key)
	require.NoError(t, os.Unsetenv(key))
	t.Cleanup(func() {
		if existed {
			_ = os.Setenv(key, original)
			return
		}
		_ = os.Unsetenv(key)
	})
}

func writeEnvFile(t *testing.T, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), ".env")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

func TestLoadDotEnv_SetsMissingKeysAndKeepsExisting(t *testing.T) {
	unsetForEnv(t, "SFL_TEST_FROM_FILE")
	unsetForEnv(t, "SFL_TEST_QUOTED")
	t.Setenv("SFL_TEST_EXISTING", "from_os")

	path := writeEnvFile(t, `
# comment line
SFL_TEST_FROM_FILE=from_file
export SFL_TEST_QUOTED="hello world"
SFL_TEST_EXISTING=from_file
LINE_WITHOUT_EQUALS_IS_IGNORED
`)
	loaded, err := loadDotEnv(path)
	require.NoError(t, err)
	assert.True(t, loaded)

	assert.Equal(t, "from_file", os.Getenv("SFL_TEST_FROM_FILE"))
	assert.Equal(t, "hello world", os.Getenv("SFL_TEST_QUOTED"), "surrounding quotes must be stripped")
	assert.Equal(t, "from_os", os.Getenv("SFL_TEST_EXISTING"), "OS environment must win over the file")
}

func TestLoadDotEnv_MissingFileIsNotAnError(t *testing.T) {
	loaded, err := loadDotEnv(filepath.Join(t.TempDir(), "does-not-exist.env"))
	require.NoError(t, err)
	assert.False(t, loaded)
}

func TestLoadDotEnvFile_UsesEnvFileVariable(t *testing.T) {
	unsetForEnv(t, "SFL_TEST_ENV_FILE")

	path := writeEnvFile(t, "SFL_TEST_ENV_FILE=from_custom_path\n")
	t.Setenv(EnvFileEnvVar, path)

	loadDotEnvFile()
	assert.Equal(t, "from_custom_path", os.Getenv("SFL_TEST_ENV_FILE"))
}
