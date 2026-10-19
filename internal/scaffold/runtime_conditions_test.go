package scaffold

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/naoray/anvil/internal/config"
)

func runtimeGuard(name, value string) config.ConditionHolder {
	return config.ConditionHolder{Condition: map[string]any{"context_var": map[string]any{"name": name, "value": value}}}
}

func TestRuntimeConditionsObserveStoreAsAndUpdates(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "source"), []byte("sentinel"), 0600))
	cfg := &config.Config{}
	cfg.Scaffold.Steps = []config.StepConfig{
		{Name: "command.run", Command: "echo true", StoreAs: "Decision"},
		{Name: "file.copy", From: "source", To: "matched", ConditionHolder: runtimeGuard("Decision", "true")},
		{Name: "file.copy", From: "source", To: "wrong", ConditionHolder: runtimeGuard("Decision", "false")},
		{Name: "command.run", Command: "echo false", StoreAs: "Decision"},
		{Name: "file.copy", From: "source", To: "updated", ConditionHolder: runtimeGuard("Decision", "false")},
		{Name: "file.copy", From: "source", To: "stale", ConditionHolder: runtimeGuard("Decision", "true")},
	}
	require.NoError(t, NewScaffoldManager().RunScaffold(dir, "branch", "repo", "site", "", nil, cfg, false, false, true))
	for _, name := range []string{"matched", "updated"} {
		data, err := os.ReadFile(filepath.Join(dir, name))
		require.NoError(t, err)
		require.Equal(t, "sentinel", string(data))
	}
	for _, name := range []string{"wrong", "stale"} {
		require.NoFileExists(t, filepath.Join(dir, name))
	}
}

func TestRuntimeConditionsRejectLaterGuardBeforeAnyMutation(t *testing.T) {
	disabled := false
	for _, cleanup := range []bool{false, true} {
		t.Run(map[bool]string{false: "scaffold", true: "cleanup"}[cleanup], func(t *testing.T) {
			dir := t.TempDir()
			legacy := []byte("db_suffix: legacy\n")
			require.NoError(t, os.WriteFile(filepath.Join(dir, "anvil.yaml"), legacy, 0600))
			cfg := &config.Config{}
			invalid := config.ConditionHolder{Condition: map[string]any{"context_vra": map[string]any{"name": "Decision", "value": "true"}}}
			cfg.Scaffold.Steps = []config.StepConfig{
				{Name: "command.run", Command: "echo side-effect > effect"},
				{Name: "command.run", Command: "echo skipped", Enabled: &disabled, ConditionHolder: invalid},
			}
			var err error
			if cleanup {
				cfg.Cleanup.Steps = []config.CleanupStep{
					{Name: "command.run", ConditionHolder: config.ConditionHolder{Condition: map[string]any{"command": "echo side-effect > effect"}}},
					{Name: "command.run", ConditionHolder: config.ConditionHolder{Condition: map[string]any{"command": "echo skipped", "context_vra": true}}},
				}
				err = NewScaffoldManager().RunCleanup(dir, "branch", "repo", "site", "", nil, cfg, false, false, true)
			} else {
				err = NewScaffoldManager().RunScaffold(dir, "branch", "repo", "site", "", nil, cfg, false, false, true)
			}
			require.ErrorContains(t, err, "context_vra")
			require.NoFileExists(t, filepath.Join(dir, "effect"))
			require.NoFileExists(t, filepath.Join(dir, ".anvil.local"))
			data, readErr := os.ReadFile(filepath.Join(dir, "anvil.yaml"))
			require.NoError(t, readErr)
			require.Equal(t, legacy, data)
		})
	}
}

func TestRuntimeConditionsPresenceAndNestedGuardsControlFileEffects(t *testing.T) {
	for _, tc := range []struct {
		name, command string
		condition     map[string]any
		runs          bool
	}{
		{name: "absent", condition: runtimeGuard("Decision", "").Condition},
		{name: "empty", command: "echo", condition: runtimeGuard("Decision", "").Condition, runs: true},
		{name: "false literal", command: "echo false", condition: runtimeGuard("Decision", "false").Condition, runs: true},
		{name: "case mismatch", command: "echo True", condition: runtimeGuard("Decision", "true").Condition},
		{name: "not and sibling", command: "echo true", condition: map[string]any{
			"not":         runtimeGuard("Absent", "").Condition,
			"context_var": map[string]any{"name": "Decision", "value": "false"},
		}},
		{name: "nested conjunction", command: "echo true", condition: map[string]any{
			"not": []any{
				runtimeGuard("Decision", "true").Condition,
				map[string]any{"not": runtimeGuard("Absent", "").Condition},
			},
		}},
		{name: "double negation", command: "echo true", condition: map[string]any{
			"not": map[string]any{"not": runtimeGuard("Decision", "true").Condition},
		}, runs: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(dir, "source"), []byte("sentinel"), 0600))
			cfg := &config.Config{}
			if tc.command != "" {
				cfg.Scaffold.Steps = append(cfg.Scaffold.Steps, config.StepConfig{Name: "command.run", Command: tc.command, StoreAs: "Decision"})
			}
			cfg.Scaffold.Steps = append(cfg.Scaffold.Steps, config.StepConfig{Name: "file.copy", From: "source", To: "effect", ConditionHolder: config.ConditionHolder{Condition: tc.condition}})
			require.NoError(t, NewScaffoldManager().RunScaffold(dir, "branch", "repo", "site", "", nil, cfg, false, false, true))
			if tc.runs {
				require.FileExists(t, filepath.Join(dir, "effect"))
			} else {
				require.NoFileExists(t, filepath.Join(dir, "effect"))
			}
		})
	}
}

func TestRuntimeConditionsMalformedShapesFailBeforeScaffoldWrites(t *testing.T) {
	for _, condition := range []map[string]any{
		{"not": []any{true}},
		{"context_var": map[string]any{"name": "Decision", "value": true}},
		{"context_var": map[string]any{"name": "Decision"}},
		{"file_exists": []any{"missing", 42}},
		{"not": map[string]any{"unknown": true}},
	} {
		t.Run("invalid", func(t *testing.T) {
			dir := t.TempDir()
			cfg := &config.Config{}
			cfg.Scaffold.Steps = []config.StepConfig{
				{Name: "env.write", Key: "SENTINEL", Value: "written"},
				{Name: "command.run", Command: "echo skipped", ConditionHolder: config.ConditionHolder{Condition: condition}},
			}
			require.Error(t, NewScaffoldManager().RunScaffold(dir, "branch", "repo", "site", "", nil, cfg, false, false, true))
			entries, err := os.ReadDir(dir)
			require.NoError(t, err)
			require.Empty(t, entries)
		})
	}
}

func TestRuntimeConditionsPreflightValidationPrecedesStateWrites(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{}
	cfg.Scaffold.PreFlight = &config.PreFlight{Condition: map[string]any{"not": false}}
	cfg.Scaffold.Steps = []config.StepConfig{{Name: "env.write", Key: "SENTINEL", Value: "written"}}
	require.Error(t, NewScaffoldManager().RunScaffold(dir, "branch", "repo", "site", "", nil, cfg, false, false, true))
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Empty(t, entries)
}

func TestRuntimeConditionsCleanupRetainsGuardsAndValidatesSkippedDatabase(t *testing.T) {
	t.Run("guard skips command", func(t *testing.T) {
		dir := t.TempDir()
		cfg := &config.Config{}
		cfg.Cleanup.Steps = []config.CleanupStep{{Name: "command.run", ConditionHolder: config.ConditionHolder{Condition: map[string]any{
			"command":     "echo side-effect > effect",
			"context_var": map[string]any{"name": "Absent", "value": ""},
		}}}}
		require.NoError(t, NewScaffoldManager().RunCleanup(dir, "branch", "repo", "site", "", nil, cfg, false, false, true))
		require.NoFileExists(t, filepath.Join(dir, "effect"))
	})
	t.Run("keep db still validates", func(t *testing.T) {
		dir := t.TempDir()
		cfg := &config.Config{}
		cfg.Cleanup.Steps = []config.CleanupStep{
			{Name: "command.run", ConditionHolder: config.ConditionHolder{Condition: map[string]any{"command": "echo side-effect > effect"}}},
			{Name: "db.destroy", ConditionHolder: config.ConditionHolder{Condition: map[string]any{"unknown": true}}},
		}
		require.ErrorContains(t, NewScaffoldManager().RunCleanupWithOptions(dir, "branch", "repo", "site", "", nil, cfg, CleanupOptions{Quiet: true, SkipDatabaseCleanup: true}), "unknown")
		entries, err := os.ReadDir(dir)
		require.NoError(t, err)
		require.Empty(t, entries)
	})
}

func TestRuntimeConditionsPreserveIntrinsicEligibilityAndDisabledSteps(t *testing.T) {
	dir := t.TempDir()
	disabled := false
	cfg := &config.Config{}
	cfg.Scaffold.Steps = []config.StepConfig{
		{Name: "command.run", Command: "echo true", StoreAs: "Decision"},
		{Name: "file.copy", From: "missing", To: "effect", ConditionHolder: runtimeGuard("Decision", "true")},
		{Name: "env.write", Key: "SENTINEL", Value: "written", Enabled: &disabled, ConditionHolder: runtimeGuard("Decision", "true")},
	}
	require.NoError(t, NewScaffoldManager().RunScaffold(dir, "branch", "repo", "site", "", nil, cfg, false, false, true))
	require.NoFileExists(t, filepath.Join(dir, "effect"))
	require.NoFileExists(t, filepath.Join(dir, ".env"))
}

func TestRuntimeConditionsOnlyValidateEffectiveDefaults(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{}
	cfg.Scaffold.Override = true
	cfg.Scaffold.Steps = []config.StepConfig{{Name: "env.write", Key: "SENTINEL", Value: "written"}}
	defaults := []config.StepConfig{{Name: "command.run", Command: "echo unused", ConditionHolder: config.ConditionHolder{Condition: map[string]any{"unknown": true}}}}
	require.NoError(t, NewScaffoldManager().RunScaffold(dir, "branch", "repo", "site", "", defaults, cfg, false, false, true))
	require.FileExists(t, filepath.Join(dir, ".env"))
}

func TestRuntimeConditionsLoadedYAMLObservesStoreAs(t *testing.T) {
	dir := t.TempDir()
	output, err := exec.Command("git", "init", dir).CombinedOutput()
	require.NoError(t, err, "%s", output)
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".env.example"), []byte("SENTINEL=preserved\n"), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "anvil.yaml"), []byte(`scaffold:
  steps:
    - name: command.run
      command: "git rev-parse --is-inside-work-tree"
      store_as: IsWorktree
    - name: file.copy
      from: .env.example
      to: .env
      condition:
        context_var:
          name: IsWorktree
          value: "true"
`), 0600))
	cfg, err := config.LoadProject(dir)
	require.NoError(t, err)
	require.NoError(t, NewScaffoldManager().RunScaffold(dir, "branch", "repo", "site", "", nil, cfg, false, false, true))
	data, err := os.ReadFile(filepath.Join(dir, ".env"))
	require.NoError(t, err)
	require.Equal(t, "SENTINEL=preserved\n", string(data))
}
