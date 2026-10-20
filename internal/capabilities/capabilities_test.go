package capabilities

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/naoray/anvil/internal/config"
)

func write(t *testing.T, dir string, files ...string) {
	t.Helper()
	for _, file := range files {
		path := filepath.Join(dir, file)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte("x"), 0o644))
	}
}

func TestNodePackageManagerDetection(t *testing.T) {
	cases := map[string][]string{
		"pnpm install --frozen-lockfile": {"package.json", "pnpm-lock.yaml"},
		"bun install --frozen-lockfile":  {"package.json", "bun.lock"},
		"yarn install --immutable":       {"package.json", "yarn.lock", ".yarnrc.yml"},
		"yarn install --frozen-lockfile": {"package.json", "yarn.lock"},
		"npm ci":                         {"package.json", "package-lock.json"},
		"npm install":                    {"package.json"},
	}
	for want, files := range cases {
		dir := t.TempDir()
		write(t, dir, files...)
		c := Contribute("node", dir, "linux")
		require.Len(t, c.Prepare, 1, want)
		require.Equal(t, want, c.Prepare[0].Run)
		require.Equal(t, []string{"node_modules"}, c.Prepare[0].Outputs)
		require.Contains(t, c.Prepare[0].Inputs, "package.json")
	}
	require.Empty(t, Contribute("node", t.TempDir(), "linux").Prepare, "no manifest, no default command")
}

func TestStackDetection(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "Cargo.toml", "Cargo.lock", "uv.lock", "pyproject.toml", "go.mod", "composer.lock", "Package.swift", "src-tauri/Cargo.toml")
	require.Equal(t, "cargo fetch --locked", Contribute("rust", dir, "linux").Prepare[0].Run)
	python := Contribute("python", dir, "linux")
	require.Equal(t, "uv sync --frozen", python.Prepare[0].Run)
	require.Equal(t, []string{"uv"}, python.Requires)
	require.Equal(t, filepath.Join(".venv", "bin"), python.PathPrepend[0])
	require.Equal(t, filepath.Join(".venv", "Scripts"), Contribute("python", dir, "windows").PathPrepend[0])
	require.Equal(t, "go mod download", Contribute("go", dir, "linux").Prepare[0].Run)
	require.Equal(t, "composer install --no-interaction", Contribute("php", dir, "linux").Prepare[0].Run)
	tauri := Contribute("tauri", dir, "linux")
	require.Equal(t, "src-tauri", tauri.Prepare[0].Dir)
	require.Equal(t, "cargo fetch", tauri.Prepare[0].Run, "no src-tauri/Cargo.lock")
	swift := Contribute("swift", dir, "linux")
	require.Equal(t, []string{"darwin"}, swift.Platforms)
	require.Equal(t, "swift package resolve", swift.Prepare[0].Run)
}

func lookPath(available ...string) func(string) (string, error) {
	return func(name string) (string, error) {
		for _, candidate := range available {
			if candidate == name {
				return "/bin/" + name, nil
			}
		}
		return "", errors.New("not found")
	}
}

func TestPlanMixedStack(t *testing.T) {
	root := t.TempDir()
	write(t, root, "apps/desktop/package.json", "apps/desktop/pnpm-lock.yaml", "apps/desktop/src-tauri/Cargo.toml", "helpers/mac/Package.swift", "service/package.json", "service/package-lock.json")
	cfg := &config.Config{Components: []config.Component{
		{Name: "desktop", Path: "apps/desktop", Uses: []string{"node", "tauri"}, Env: map[string]string{"VITE_MODE": "dev"},
			Prepare: []config.PrepareCommand{{Run: "pnpm tauri info", Always: true, Platforms: []string{"darwin", "linux"}}}},
		{Name: "service", Path: "service", Uses: []string{"node"}},
		{Name: "helper", Path: "helpers/mac", Uses: []string{"swift"}},
		{Name: "bench", Path: ".", OptIn: true, Prepare: []config.PrepareCommand{{Run: "true"}}},
		{Name: "optional-tool", Path: ".", Requires: []string{"missing-tool"}, OnMissing: config.OnMissingSkip},
	}}
	require.NoError(t, cfg.ValidateEnvironment())
	plans, err := Plan(root, cfg, Options{GOOS: "linux", LookPath: lookPath("node", "pnpm", "npm", "cargo")})
	require.NoError(t, err)
	byName := map[string]ComponentPlan{}
	for _, plan := range plans {
		byName[plan.Name] = plan
	}

	desktop := byName["desktop"]
	require.Equal(t, StatusActive, desktop.Status)
	require.Equal(t, []string{"cargo", "node", "pnpm"}, desktop.Requires)
	require.Len(t, desktop.Steps, 3)
	require.Equal(t, "pnpm install --frozen-lockfile", desktop.Steps[0].Command)
	require.Equal(t, "apps/desktop", desktop.Steps[0].Dir)
	require.Equal(t, "cd 'src-tauri' && cargo fetch", desktop.Steps[1].Command)
	require.Contains(t, desktop.Steps[1].Inputs, "src-tauri/Cargo.toml")
	require.True(t, desktop.Steps[2].Always)
	require.Equal(t, []any{"darwin", "linux"}, desktop.Steps[2].Condition["os"])
	require.Equal(t, "dev", desktop.Steps[0].Env["VITE_MODE"])
	require.Contains(t, desktop.Env["PATH"], filepath.Join(root, "apps/desktop", "node_modules", ".bin"))
	require.Equal(t, filepath.Join(root, "apps/desktop"), desktop.Env["ANVIL_COMPONENT_DIR"])

	require.Equal(t, StatusSkipped, byName["helper"].Status, "swift defaults to darwin")
	require.Contains(t, byName["helper"].Reason, "platform linux")
	require.Equal(t, StatusSkipped, byName["bench"].Status)
	require.Equal(t, StatusSkipped, byName["optional-tool"].Status)
	require.Equal(t, []string{"missing-tool"}, byName["optional-tool"].Missing)
	require.Len(t, Steps(plans), 4, "desktop (3) + service (1)")

	darwin, err := Plan(root, cfg, Options{GOOS: "darwin", LookPath: lookPath("node", "pnpm", "npm", "cargo")})
	require.Error(t, err, "helper requires swift on darwin")
	require.ErrorContains(t, err, `component "helper": missing required commands: swift`)
	require.NotEmpty(t, darwin)

	selected, err := Plan(root, cfg, Options{GOOS: "linux", Selected: []string{"bench"}, LookPath: lookPath("node", "pnpm", "npm", "cargo")})
	require.NoError(t, err)
	require.Equal(t, StatusActive, selected[3].Status)

	_, err = Plan(root, cfg, Options{GOOS: "linux", Selected: []string{"optional-tool"}, LookPath: lookPath("node", "pnpm", "npm", "cargo")})
	require.Error(t, err, "explicitly requested components must have their requirements")

	_, err = Plan(root, cfg, Options{GOOS: "linux", Selected: []string{"nope"}, LookPath: lookPath()})
	var unknown *UnknownComponentError
	require.ErrorAs(t, err, &unknown)
}

func TestPlanRejectsMissingDirectoryAndConflictingConditions(t *testing.T) {
	root := t.TempDir()
	cfg := &config.Config{Components: []config.Component{{Name: "gone", Path: "nope"}}}
	_, err := Plan(root, cfg, Options{GOOS: "linux", LookPath: lookPath()})
	require.ErrorContains(t, err, "does not exist")
	cfg.Components[0].OnMissing = config.OnMissingSkip
	_, err = Plan(root, cfg, Options{GOOS: "linux", LookPath: lookPath()})
	require.NoError(t, err)

	cfg = &config.Config{Components: []config.Component{{Name: "c", When: map[string]any{"file_exists": "a"},
		Prepare: []config.PrepareCommand{{Run: "true", When: map[string]any{"file_exists": "b"}}}}}}
	_, err = Plan(root, cfg, Options{GOOS: "linux", LookPath: lookPath()})
	require.ErrorContains(t, err, "both the component and its prepare command")
}

func TestValidateEnvironment(t *testing.T) {
	bad := &config.Config{
		Env:       map[string]string{"bad-name": "x"},
		SecretEnv: []string{"1X"},
		Components: []config.Component{
			{Name: "Upper"},
			{Name: "dup"}, {Name: "dup"},
			{Name: "p", Path: "../escape", Uses: []string{"cobol"}, Platforms: []string{"plan9"}, OnMissing: "maybe",
				Prepare: []config.PrepareCommand{{Run: " ", Inputs: []string{"/abs"}}}},
		},
	}
	err := bad.ValidateEnvironment()
	require.Error(t, err)
	for _, fragment := range []string{"bad-name", "1X", "invalid name \"Upper\"", "duplicate", "leaves the worktree", "cobol", "plan9", "on_missing", "run is required", "must be relative"} {
		require.ErrorContains(t, err, fragment)
	}
}
