package resources

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/naoray/anvil/internal/config"
	"github.com/naoray/anvil/internal/workenv"
)

// SharedStatus describes one declared shared resource.
type SharedStatus struct {
	Name     string `json:"name"`
	Path     string `json:"path"`
	Exists   bool   `json:"exists"`
	Required bool   `json:"required"`
}

// ExpandHome resolves a leading ~ to the user's home directory.
func ExpandHome(path string) (string, error) {
	if path != "~" && !strings.HasPrefix(path, "~/") {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, strings.TrimPrefix(path, "~")), nil
}

// DescribeShared resolves shared resources without touching them. Missing
// required resources are reported as an error so preparation can refuse.
func DescribeShared(cfg *config.Config) ([]SharedStatus, []workenv.Variable, error) {
	var statuses []SharedStatus
	var vars []workenv.Variable
	var missing []string
	for _, shared := range cfg.Resources.Shared {
		path, err := ExpandHome(os.ExpandEnv(shared.Path))
		if err != nil {
			return nil, nil, err
		}
		if !filepath.IsAbs(path) {
			return nil, nil, fmt.Errorf("shared resource %q: path %q must be absolute (or start with ~)", shared.Name, shared.Path)
		}
		_, statErr := os.Stat(path)
		status := SharedStatus{Name: shared.Name, Path: path, Exists: statErr == nil, Required: shared.Required}
		statuses = append(statuses, status)
		if !status.Exists && shared.Required {
			missing = append(missing, fmt.Sprintf("%s (%s)", shared.Name, path))
		}
		vars = append(vars, workenv.Variable{Name: "ANVIL_SHARED_" + config.ResourceEnvSuffix(shared.Name), Value: path, Source: workenv.SourceResource})
		if shared.Env != "" {
			vars = append(vars, workenv.Variable{Name: shared.Env, Value: path, Source: workenv.SourceResource})
		}
	}
	if len(missing) > 0 {
		return statuses, vars, fmt.Errorf("required shared resources are missing: %s", strings.Join(missing, ", "))
	}
	return statuses, vars, nil
}
