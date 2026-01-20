package utils

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestReadEnvFile_ValidFile(t *testing.T) {
	tmpDir := t.TempDir()

	envContent := `DB_HOST=localhost
DB_PORT=5432
API_KEY=secret123
`
	err := os.WriteFile(filepath.Join(tmpDir, ".env"), []byte(envContent), 0644)
	assert.NoError(t, err)

	result := ReadEnvFile(tmpDir, ".env")

	assert.Equal(t, "localhost", result["DB_HOST"])
	assert.Equal(t, "5432", result["DB_PORT"])
	assert.Equal(t, "secret123", result["API_KEY"])
	assert.Len(t, result, 3)
}

func TestReadEnvFile_WithComments(t *testing.T) {
	tmpDir := t.TempDir()

	envContent := `# This is a comment
DB_HOST=localhost
# Another comment
DB_PORT=5432
EMPTY_LINE=

# Trailing comment
`
	err := os.WriteFile(filepath.Join(tmpDir, ".env"), []byte(envContent), 0644)
	assert.NoError(t, err)

	result := ReadEnvFile(tmpDir, ".env")

	assert.Equal(t, "localhost", result["DB_HOST"])
	assert.Equal(t, "5432", result["DB_PORT"])
	assert.Equal(t, "", result["EMPTY_LINE"])
	assert.Len(t, result, 3, "comments and blank lines should be ignored")
}

func TestReadEnvFile_MissingFile(t *testing.T) {
	result := ReadEnvFile("/nonexistent/path", ".env")

	assert.Empty(t, result, "missing file should return empty map")
}

func TestReadEnvFile_MalformedLines(t *testing.T) {
	tmpDir := t.TempDir()

	envContent := `DB_HOST=localhost
MALFORMED_LINE
=VALUE_WITHOUT_KEY
KEY_ONLY
DB_PORT=5432
`
	err := os.WriteFile(filepath.Join(tmpDir, ".env"), []byte(envContent), 0644)
	assert.NoError(t, err)

	result := ReadEnvFile(tmpDir, ".env")

	assert.Equal(t, "localhost", result["DB_HOST"])
	assert.Equal(t, "5432", result["DB_PORT"])
	assert.Equal(t, "VALUE_WITHOUT_KEY", result[""], "empty key before = should be accepted")
	assert.Len(t, result, 3, "malformed lines with = should create empty key entries")
}

func TestReadEnvFile_ValuesWithEquals(t *testing.T) {
	tmpDir := t.TempDir()

	envContent := `URL=http://example.com?param=value
FORMULA=a=b=c
`
	err := os.WriteFile(filepath.Join(tmpDir, ".env"), []byte(envContent), 0644)
	assert.NoError(t, err)

	result := ReadEnvFile(tmpDir, ".env")

	assert.Equal(t, "http://example.com?param=value", result["URL"])
	assert.Equal(t, "a=b=c", result["FORMULA"])
}

func TestReadEnvFile_WhitespaceHandling(t *testing.T) {
	tmpDir := t.TempDir()

	envContent := `  SPACED_KEY = value with spaces  
NORMAL=value
`
	err := os.WriteFile(filepath.Join(tmpDir, ".env"), []byte(envContent), 0644)
	assert.NoError(t, err)

	result := ReadEnvFile(tmpDir, ".env")

	assert.Equal(t, "value with spaces", result["SPACED_KEY"], "whitespace around key and value should be trimmed")
	assert.Equal(t, "value", result["NORMAL"])
}

func TestEnvExists(t *testing.T) {
	env := map[string]string{
		"FOO": "bar",
		"BAZ": "qux",
	}

	assert.True(t, EnvExists(env, "FOO"))
	assert.True(t, EnvExists(env, "BAZ"))
	assert.False(t, EnvExists(env, "MISSING"))
	assert.False(t, EnvExists(env, "foo"), "keys are case-sensitive")
}

func TestEnvNotExists(t *testing.T) {
	env := map[string]string{
		"EXISTING": "value",
	}

	assert.False(t, EnvNotExists(env, "EXISTING"))
	assert.True(t, EnvNotExists(env, "MISSING"))
	assert.True(t, EnvNotExists(env, "existing"), "keys are case-sensitive")
}
