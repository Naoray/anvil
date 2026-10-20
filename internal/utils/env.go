package utils

import (
	"os"
	"path/filepath"
	"strings"
)

func ReadEnvFile(worktreePath, filename string) map[string]string {
	result := make(map[string]string)

	envPath := filepath.Join(worktreePath, filename)
	data, err := os.ReadFile(envPath)
	if err != nil {
		return result
	}

	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			key := strings.TrimSpace(parts[0])
			value := strings.TrimSpace(parts[1])
			result[key] = value
		}
	}

	return result
}

func EnvExists(env map[string]string, key string) bool {
	_, exists := env[key]
	return exists
}

func EnvNotExists(env map[string]string, key string) bool {
	return !EnvExists(env, key)
}

// ExpandEnv expands ${VAR} and $VAR against primary, then the process
// environment. Unknown variables expand to the empty string, as in a shell.
func ExpandEnv(value string, primary map[string]string) string {
	return os.Expand(value, func(name string) string {
		if v, ok := primary[name]; ok {
			return v
		}
		return os.Getenv(name)
	})
}
