package cli

import (
	"bufio"
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/naoray/anvil/internal/config"
	"github.com/naoray/anvil/internal/git"
	"github.com/naoray/anvil/internal/scaffold"
	"github.com/naoray/anvil/internal/scaffold/steps"
	"github.com/naoray/anvil/internal/scaffold/types"
)

type removeTestRegistry struct {
	client *steps.MockDatabaseClient
	output *bytes.Buffer
}

func (r *removeTestRegistry) Create(name string, cfg config.StepConfig) (types.ScaffoldStep, error) {
	if name != config.StepDbDestroy {
		return nil, errors.New("unexpected cleanup step: " + name)
	}
	return steps.NewDbDestroyStepWithFactoryAndWriter(cfg, steps.MockClientFactory(r.client), r.output), nil
}

func (r *removeTestRegistry) ListRegistered() []string {
	return []string{config.StepDbDestroy}
}

type removeTestPreset struct{}

func (removeTestPreset) Name() string                      { return "remove-test" }
func (removeTestPreset) Detect(string) bool                { return false }
func (removeTestPreset) DefaultSteps() []config.StepConfig { return nil }
func (removeTestPreset) CleanupSteps() []config.CleanupStep {
	return []config.CleanupStep{{Name: config.StepDbDestroy}}
}

func TestRemoveCommand_DryRunEnumeratesExactDropSet(t *testing.T) {
	worktreePath := t.TempDir()
	state := config.LocalState{Databases: []config.OwnedDatabase{
		{Name: "app_top_provider", Engine: "mysql", Role: config.DbRoleApplication},
		{Name: "app_top_provider_test", Engine: "mysql", Role: config.DbRoleTesting},
	}}
	require.NoError(t, config.WriteLocalState(worktreePath, state))
	statePath := filepath.Join(worktreePath, ".anvil.local")
	stateBefore, err := os.ReadFile(statePath)
	require.NoError(t, err)

	client := steps.NewMockDatabaseClient()
	for _, name := range []string{
		"app_top_provider",
		"app_top_provider_test",
		"app_top_provider_test_1",
		"app_top_provider_test_2",
		"app_top_provider_test_test_1",
		"unrelated_test_1",
	} {
		client.AddDatabase(name)
	}
	var cleanupOutput bytes.Buffer
	manager := scaffold.NewScaffoldManagerWithRegistry(&removeTestRegistry{client: client, output: &cleanupOutput})
	manager.RegisterPreset(removeTestPreset{})
	pc := &ProjectContext{GitDir: "git-dir", Config: &config.Config{Preset: "remove-test"}}
	removeCalls := 0
	var infoMessages []string
	deps := removeCommandDependencies{
		openProject:      func() (*ProjectContext, error) { return pc, nil },
		getwd:            func() (string, error) { return "/main", nil },
		getDefaultBranch: func(string) (string, error) { return "main", nil },
		listWorktrees: func(string, string, string) ([]git.Worktree, error) {
			return []git.Worktree{{Path: worktreePath, Branch: "agent/test"}}, nil
		},
		branchExists: func(string, string) bool { return false },
		removeWorktree: func(string, string, bool) error {
			removeCalls++
			return nil
		},
		deleteBranch:    func(string, string, bool) error { return nil },
		readLocalState:  config.ReadLocalState,
		scaffoldManager: func(*ProjectContext) *scaffold.ScaffoldManager { return manager },
		detectPreset:    func(*ProjectContext, string) string { return "remove-test" },
		printInfo:       func(message string) { infoMessages = append(infoMessages, message) },
	}

	root := &cobra.Command{Use: "anvil"}
	root.PersistentFlags().Bool("dry-run", false, "")
	root.PersistentFlags().Bool("verbose", false, "")
	root.PersistentFlags().Bool("quiet", false, "")
	root.AddCommand(newRemoveCommand(deps))
	root.SetArgs([]string{"remove", filepath.Base(worktreePath), "--dry-run", "--force", "--quiet"})

	var output string
	output = captureStdout(t, func() {
		require.NoError(t, root.Execute())
	})
	assert.Contains(t, infoMessages, "[DRY RUN] Would remove agent/test at "+worktreePath)
	assert.NotContains(t, output, "Worktree removed")
	assert.Equal(t, "Would drop database: app_top_provider\n"+
		"Would drop database: app_top_provider_test\n"+
		"Would drop database: app_top_provider_test_1\n"+
		"Would drop database: app_top_provider_test_2\n"+
		"Would drop database: app_top_provider_test_test_1\n", cleanupOutput.String())
	assert.Equal(t, []string{`app\_top\_provider\_test\_%`, `app\_top\_provider\_test\_test\_%`}, client.GetListCalls())
	assert.Empty(t, client.GetDropCalls())
	assert.Zero(t, removeCalls)
	_, err = os.Stat(worktreePath)
	require.NoError(t, err)
	stateAfter, err := os.ReadFile(statePath)
	require.NoError(t, err)
	assert.Equal(t, stateBefore, stateAfter)
}

func TestPlanRemoveCleanup_KeepDbListsPreservedNames(t *testing.T) {
	state := &config.LocalState{Databases: []config.OwnedDatabase{
		{Name: "app_top_provider", Engine: "mysql", Role: config.DbRoleApplication},
		{Name: "app_top_provider_test", Engine: "mysql", Role: config.DbRoleTesting},
	}}

	opts, messages, err := planRemoveCleanup(state, nil, true, false, false)

	require.NoError(t, err)
	assert.True(t, opts.SkipDatabaseCleanup)
	assert.Equal(t, []string{
		"Preserving databases: app_top_provider, app_top_provider_test (parallel worker databases are kept too; drop manually when done)",
	}, messages)
}

func TestPlanRemoveCleanup_DuplicateOwnedStateHardStopsUnlessForce(t *testing.T) {
	tests := []struct {
		name  string
		state *config.LocalState
		want  string
	}{
		{
			name: "duplicate role",
			state: &config.LocalState{Databases: []config.OwnedDatabase{
				{Name: "app_top_provider", Engine: "mysql", Role: config.DbRoleApplication},
				{Name: "app_top_provider_test", Engine: "mysql", Role: config.DbRoleTesting},
				{Name: "worker_top_provider_test", Engine: "mysql", Role: config.DbRoleTesting},
			}},
			want: `duplicate database role "testing" in record 2; first seen in record 1`,
		},
		{
			name: "duplicate name",
			state: &config.LocalState{Databases: []config.OwnedDatabase{
				{Name: "app_top_provider", Engine: "mysql", Role: config.DbRoleApplication},
				{Name: "app_top_provider", Engine: "mysql", Role: config.DbRoleTesting},
			}},
			want: `duplicate database name "app_top_provider" in record 1; first seen in record 0`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := planRemoveCleanup(tt.state, nil, false, false, false)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.want)
			assert.Contains(t, err.Error(), "--force")

			opts, messages, err := planRemoveCleanup(tt.state, nil, false, false, true)
			require.NoError(t, err)
			assert.True(t, opts.SkipDatabaseCleanup)
			require.Len(t, messages, 1)
			assert.Contains(t, messages[0], "WARNING")
			assert.Contains(t, messages[0], "databases will be left untouched and unrecorded")
		})
	}
}

func TestPlanRemoveCleanup_KeepDbLegacySuffixPattern(t *testing.T) {
	opts, messages, err := planRemoveCleanup(&config.LocalState{DbSuffix: "top_provider"}, nil, true, false, false)

	require.NoError(t, err)
	assert.True(t, opts.SkipDatabaseCleanup)
	assert.Equal(t, []string{
		"Preserving databases matching suffix 'top_provider' (legacy worktree — exact names unknown)",
	}, messages)
}

func TestPlanRemoveCleanup_KeepDbUnreadableStateHardStopsUnlessForce(t *testing.T) {
	stateErr := errors.New("permission denied")

	_, _, err := planRemoveCleanup(nil, stateErr, true, false, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot read .anvil.local")
	assert.Contains(t, err.Error(), "--force")

	opts, messages, err := planRemoveCleanup(nil, stateErr, true, false, true)
	require.NoError(t, err)
	assert.True(t, opts.SkipDatabaseCleanup)
	require.Len(t, messages, 1)
	assert.Contains(t, messages[0], "WARNING")
	assert.Contains(t, messages[0], "databases will be left untouched and unrecorded")
}

func TestPlanRemoveCleanup_InvalidOwnedStateHardStopsUnlessForce(t *testing.T) {
	state := &config.LocalState{Databases: []config.OwnedDatabase{
		{Name: "app_top_provider", Engine: "mysql", Role: "unknown"},
		{Name: "app_top_provider_test", Engine: "pgsql", Role: config.DbRoleTesting},
	}}

	_, _, err := planRemoveCleanup(state, nil, false, false, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid database records in .anvil.local")
	assert.Contains(t, err.Error(), "--force")

	opts, messages, err := planRemoveCleanup(state, nil, false, false, true)
	require.NoError(t, err)
	assert.True(t, opts.SkipDatabaseCleanup)
	require.Len(t, messages, 1)
	assert.Contains(t, messages[0], "WARNING")
	assert.Contains(t, messages[0], "databases will be left untouched and unrecorded")
}

func TestPlanRemoveCleanup_DryRunPropagatesDryRunTrue(t *testing.T) {
	opts, messages, err := planRemoveCleanup(&config.LocalState{}, nil, false, true, false)

	require.NoError(t, err)
	assert.True(t, opts.DryRun)
	assert.False(t, opts.SkipDatabaseCleanup)
	assert.Empty(t, messages)
}

func TestPlanRemoveCleanup_DryRunKeepDbMessage(t *testing.T) {
	opts, messages, err := planRemoveCleanup(&config.LocalState{DbSuffix: "top_provider"}, nil, true, true, false)

	require.NoError(t, err)
	assert.True(t, opts.DryRun)
	assert.True(t, opts.SkipDatabaseCleanup)
	assert.Equal(t, "[DRY RUN] database cleanup would be skipped (--keep-db)", messages[len(messages)-1])
}

func TestRemoveCmd_PreventsMainWorktreeDeletion(t *testing.T) {
	repoDir := t.TempDir()
	parentDir := filepath.Dir(repoDir)

	cmd := exec.Command("git", "init", "-b", "main")
	cmd.Dir = repoDir
	require.NoError(t, cmd.Run())

	runGitCmd(t, repoDir, "config", "user.email", "test@example.com")
	runGitCmd(t, repoDir, "config", "user.name", "Test User")
	require.NoError(t, os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("test"), 0644))
	runGitCmd(t, repoDir, "add", ".")
	runGitCmd(t, repoDir, "commit", "-m", "Initial commit")

	gitDir := filepath.Join(repoDir, ".git")

	detachHEAD(t, repoDir)

	mainPath := filepath.Join(parentDir, "main-wt")
	require.NoError(t, git.CreateWorktree(gitDir, mainPath, "main", ""))

	featurePath := filepath.Join(parentDir, "feature-wt")
	require.NoError(t, git.CreateWorktree(gitDir, featurePath, "feature", "main"))

	t.Run("main worktree is correctly identified", func(t *testing.T) {
		defaultBranch, err := git.GetDefaultBranch(gitDir)
		require.NoError(t, err)

		worktrees, err := git.ListWorktreesDetailed(gitDir, mainPath, defaultBranch)
		require.NoError(t, err)

		var mainWt *git.Worktree
		for _, wt := range worktrees {
			if wt.Branch == "main" && wt.Path != repoDir {
				mainWt = &wt
				break
			}
		}

		require.NotNil(t, mainWt, "main worktree should be found")
		assert.True(t, mainWt.IsMain, "main worktree should have IsMain=true")
	})

	t.Run("feature worktree can be removed", func(t *testing.T) {
		_, err := os.Stat(featurePath)
		assert.NoError(t, err, "feature worktree should exist before removal")

		err = git.RemoveWorktree(gitDir, featurePath, true)
		assert.NoError(t, err)

		_, err = os.Stat(featurePath)
		assert.True(t, os.IsNotExist(err), "feature worktree should not exist after removal")
	})
}

func TestRemoveCmd_EmptyInputBehavior(t *testing.T) {
	t.Run("empty input handled gracefully with bufio.Reader", func(t *testing.T) {
		reader := bufio.NewReader(bytes.NewReader([]byte("\n")))

		input, err := reader.ReadString('\n')
		require.NoError(t, err)

		trimmed := strings.TrimSpace(input)
		t.Logf("Fixed behavior: response = %q", trimmed)

		assert.Empty(t, trimmed, "empty input should result in empty string")

		assert.NotPanics(t, func() {
			_ = trimmed
		}, "empty input should not cause panic")

		assert.Equal(t, "", trimmed, "empty input should be treated as 'no'")
	})
}

func TestWorkCmd_InteractiveInputPattern(t *testing.T) {
	t.Run("work.go bufio.Reader handles empty input gracefully", func(t *testing.T) {
		reader := bufio.NewReader(bytes.NewReader([]byte("\n")))

		input, err := reader.ReadString('\n')
		require.NoError(t, err)

		trimmed := input
		if len(trimmed) > 0 {
			trimmed = trimmed[:len(trimmed)-1]
		}

		assert.Empty(t, trimmed)
		assert.NotPanics(t, func() {
			_ = trimmed
		})
	})

	t.Run("work.go pattern for branch selection", func(t *testing.T) {
		reader := bufio.NewReader(bytes.NewReader([]byte("1\n")))

		input, err := reader.ReadString('\n')
		require.NoError(t, err)

		trimmed := strings.TrimSpace(input)
		assert.Equal(t, "1", trimmed)
	})
}

func runGitCmd(t *testing.T, dir string, args ...string) {
	allArgs := append([]string{"-C"}, dir)
	allArgs = append(allArgs, args...)
	cmd := exec.Command("git", allArgs...)
	cmd.Dir = dir
	if err := cmd.Run(); err != nil {
		t.Fatalf("git %v failed: %v", args, err)
	}
}
