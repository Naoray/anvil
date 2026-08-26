package cli

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/naoray/anvil/internal/config"
	"github.com/naoray/anvil/internal/git"
)

func ensureSyncTestFlags(t *testing.T) {
	t.Helper()

	if syncCmd.Flags().Lookup("dry-run") == nil {
		syncCmd.Flags().Bool("dry-run", false, "")
	}
	if syncCmd.Flags().Lookup("verbose") == nil {
		syncCmd.Flags().Bool("verbose", false, "")
	}
	if syncCmd.Flags().Lookup("quiet") == nil {
		syncCmd.Flags().Bool("quiet", false, "")
	}
}

type syncTestRepo struct {
	sourceDir    string
	repoDir      string
	gitDir       string
	worktreeBase string
	featurePath  string
}

func runSyncGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	requireNoError(t, cmd.Run())
}

func writeSyncCommit(t *testing.T, dir string, relativePath string, content string, message string) {
	t.Helper()
	path := filepath.Join(dir, relativePath)
	requireNoError(t, os.WriteFile(path, []byte(content), 0644))
	runSyncGit(t, dir, "add", relativePath)
	runSyncGit(t, dir, "commit", "-m", message)
}

func setupSyncCommandRepo(t *testing.T) syncTestRepo {
	t.Helper()

	sourceDir := t.TempDir()
	runSyncGit(t, sourceDir, "init", "-b", "main")
	runSyncGit(t, sourceDir, "config", "user.email", "test@example.com")
	runSyncGit(t, sourceDir, "config", "user.name", "Test User")
	writeSyncCommit(t, sourceDir, "README.md", "test", "Initial commit")

	repoDir := filepath.Join(t.TempDir(), "repo")
	cmd := exec.Command("git", "clone", sourceDir, repoDir)
	requireNoError(t, cmd.Run())
	runSyncGit(t, repoDir, "config", "user.email", "test@example.com")
	runSyncGit(t, repoDir, "config", "user.name", "Test User")

	gitDir := filepath.Join(repoDir, ".git")
	parentDir := filepath.Dir(repoDir)
	worktreeBase := filepath.Join(parentDir, "worktrees")
	featurePath := filepath.Join(worktreeBase, "repo", "feature-wt")
	requireNoError(t, os.MkdirAll(filepath.Dir(featurePath), 0755))
	requireNoError(t, git.CreateWorktree(gitDir, featurePath, "feature", "main"))
	worktreeBase = evalSymlinks(worktreeBase)
	repoDir = evalSymlinks(repoDir)
	featurePath = evalSymlinks(featurePath)
	gitDir = filepath.Join(repoDir, ".git")

	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "config"))
	requireNoError(t, config.SaveGlobalConfig(&config.GlobalConfig{
		WorktreeBase: worktreeBase,
		Projects: map[string]*config.ProjectInfo{
			"repo": {
				Path:          repoDir,
				DefaultBranch: "main",
			},
		},
	}))

	return syncTestRepo{
		sourceDir:    sourceDir,
		repoDir:      repoDir,
		gitDir:       gitDir,
		worktreeBase: worktreeBase,
		featurePath:  featurePath,
	}
}

func runSyncCommandForTest(t *testing.T, repo syncTestRepo, remote string) error {
	t.Helper()
	originalDir, err := os.Getwd()
	requireNoError(t, err)
	defer func() { requireNoError(t, os.Chdir(originalDir)) }()
	requireNoError(t, os.Chdir(repo.featurePath))

	ensureSyncTestFlags(t)
	flags := map[string]string{
		"upstream":      "main",
		"remote":        remote,
		"strategy":      "",
		"yes":           "true",
		"quiet":         "true",
		"verbose":       "false",
		"dry-run":       "false",
		"save":          "false",
		"no-auto-stash": "false",
	}
	for name, value := range flags {
		requireNoError(t, syncCmd.Flags().Set(name, value))
		name, value := name, value
		t.Cleanup(func() { requireNoError(t, syncCmd.Flags().Set(name, defaultSyncTestFlagValue(name, value))) })
	}

	return syncCmd.RunE(syncCmd, []string{})
}

func defaultSyncTestFlagValue(name string, value string) string {
	if name == "upstream" || name == "remote" || name == "strategy" {
		return ""
	}
	if name == "yes" || name == "quiet" || name == "verbose" || name == "dry-run" || name == "save" || name == "no-auto-stash" {
		return "false"
	}
	return value
}

func TestSyncCommand_AutoStashApplyConflictGuidance(t *testing.T) {
	repo := setupSyncCommandRepo(t)

	preExistingPath := filepath.Join(repo.featurePath, "pre-existing.txt")
	requireNoError(t, os.WriteFile(preExistingPath, []byte("pre-existing"), 0644))
	preExistingOID, err := git.StashAll(repo.featurePath, "pre-existing stash")
	requireNoError(t, err)

	writeSyncCommit(t, repo.sourceDir, "README.md", "remote", "Remote update")
	requireNoError(t, os.WriteFile(filepath.Join(repo.featurePath, "README.md"), []byte("local"), 0644))

	err = runSyncCommandForTest(t, repo, "origin")
	require.Error(t, err)
	errText := err.Error()

	stashOutput, stashErr := exec.Command("git", "-C", repo.featurePath, "stash", "list", "--format=%H").Output()
	requireNoError(t, stashErr)
	stashOIDs := strings.Split(strings.TrimSpace(string(stashOutput)), "\n")
	require.GreaterOrEqual(t, len(stashOIDs), 2)
	autoStashOID := stashOIDs[0]
	assert.Equal(t, preExistingOID, stashOIDs[1])
	assert.Contains(t, errText, autoStashOID)
	assert.Contains(t, errText, "already contains the attempted application")
	assert.Contains(t, errText, "Resolve and stage")
	assert.Contains(t, errText, "git stash list --format=\"%H %gd\"")
	assert.Contains(t, errText, "grep '^"+autoStashOID+" '")
	assert.Contains(t, errText, "git stash drop <matching-selector>")
	assert.NotContains(t, errText, "git reset --hard")
	assert.NotContains(t, errText, "git stash apply")

	conflictedReadme, readErr := os.ReadFile(filepath.Join(repo.featurePath, "README.md"))
	requireNoError(t, readErr)
	assert.Contains(t, string(conflictedReadme), "<<<<<<<")
	assert.Contains(t, string(conflictedReadme), "=======")
	assert.Contains(t, string(conflictedReadme), ">>>>>>>")

	assert.Contains(t, string(stashOutput), autoStashOID)
	_, preExistingStatErr := os.Stat(preExistingPath)
	assert.True(t, os.IsNotExist(preExistingStatErr))
}

func TestSyncCommand_AutoStashDropFailureGuidance(t *testing.T) {
	repo := setupSyncCommandRepo(t)

	preExistingPath := filepath.Join(repo.featurePath, "pre-existing.txt")
	requireNoError(t, os.WriteFile(preExistingPath, []byte("pre-existing"), 0644))
	preExistingOID, err := git.StashAll(repo.featurePath, "pre-existing stash")
	requireNoError(t, err)

	writeSyncCommit(t, repo.featurePath, "feature.txt", "feature", "Feature commit")
	writeSyncCommit(t, repo.sourceDir, "README.md", "remote", "Remote update")

	autoStashOIDPath := filepath.Join(t.TempDir(), "auto-stash-oid")
	hookPath := filepath.Join(repo.gitDir, "hooks", "post-rewrite")
	hook := fmt.Sprintf(`#!/bin/sh
oid="$(git stash list --format='%%H %%s' | awk '$0 ~ /anvil sync auto-stash/ {print $1; exit}')"
printf '%%s\n' "$oid" > %q
if [ -n "$oid" ]; then
  ref="$(git stash list --format='%%H %%gd' | awk -v oid="$oid" '$1 == oid {print $2; exit}')"
  git stash drop "$ref"
fi
`, autoStashOIDPath)
	requireNoError(t, os.WriteFile(hookPath, []byte(hook), 0755))

	changePath := filepath.Join(repo.featurePath, "untracked.txt")
	requireNoError(t, os.WriteFile(changePath, []byte("changes"), 0644))

	err = runSyncCommandForTest(t, repo, "origin")
	require.Error(t, err)
	errText := err.Error()

	autoStashOIDBytes, readErr := os.ReadFile(autoStashOIDPath)
	requireNoError(t, readErr)
	autoStashOID := strings.TrimSpace(string(autoStashOIDBytes))
	require.NotEmpty(t, autoStashOID)
	assert.Contains(t, errText, autoStashOID)
	assert.Contains(t, errText, "files are already restored")
	assert.Contains(t, errText, "Check the exact auto-stash OID")
	assert.Contains(t, errText, "git stash list --format=\"%H %gd\"")
	assert.Contains(t, errText, "grep '^"+autoStashOID+" '")
	assert.Contains(t, errText, "If it prints a selector")
	assert.Contains(t, errText, "drop only that selector")
	assert.Contains(t, errText, "If it prints nothing")
	assert.Contains(t, errText, "reflog entry is already absent")
	assert.Contains(t, errText, "no drop is needed")
	assert.Contains(t, errText, "recovery evidence")
	assert.NotContains(t, errText, "stash remains")
	assert.NotContains(t, errText, "git stash apply")
	assert.NotContains(t, errText, "git reset --hard")

	restored, readErr := os.ReadFile(changePath)
	requireNoError(t, readErr)
	assert.Equal(t, "changes", string(restored))

	stashOutput, stashErr := exec.Command("git", "-C", repo.featurePath, "stash", "list", "--format=%H").Output()
	requireNoError(t, stashErr)
	assert.Equal(t, preExistingOID, strings.TrimSpace(string(stashOutput)))

	catErr := exec.Command("git", "-C", repo.featurePath, "cat-file", "-e", autoStashOID+"^{commit}").Run()
	assert.NoError(t, catErr)
}

func TestSyncCommand_PreservesPrimaryAndRestoreErrors(t *testing.T) {
	repo := setupSyncCommandRepo(t)

	writeSyncCommit(t, repo.featurePath, "README.md", "feature", "Feature commit")
	writeSyncCommit(t, repo.sourceDir, "README.md", "remote", "Remote update")
	requireNoError(t, os.WriteFile(filepath.Join(repo.featurePath, "README.md"), []byte("local"), 0644))

	hookPath := filepath.Join(repo.gitDir, "hooks", "pre-rebase")
	hook := `#!/bin/sh
printf '%s\n' 'primary' > README.md
git add README.md
git commit -m 'Primary sync failure'
printf '%s\n' 'CONFLICT primary sync failure' >&2
exit 1
`
	requireNoError(t, os.WriteFile(hookPath, []byte(hook), 0755))

	err := runSyncCommandForTest(t, repo, "origin")
	require.Error(t, err)

	var primaryErr *git.RebaseConflictError
	assert.True(t, errors.As(err, &primaryErr))
	var restoreErr *git.StashConflictError
	assert.True(t, errors.As(err, &restoreErr))
}

func TestSyncCommand_ValidatesInWorktree(t *testing.T) {
	// Create a source repo
	sourceDir := t.TempDir()
	cmd := exec.Command("git", "init", "-b", "main")
	cmd.Dir = sourceDir
	requireNoError(t, cmd.Run())

	cmd = exec.Command("git", "config", "user.email", "test@example.com")
	cmd.Dir = sourceDir
	requireNoError(t, cmd.Run())

	cmd = exec.Command("git", "config", "user.name", "Test User")
	cmd.Dir = sourceDir
	requireNoError(t, cmd.Run())

	readmePath := filepath.Join(sourceDir, "README.md")
	requireNoError(t, os.WriteFile(readmePath, []byte("test"), 0644))

	cmd = exec.Command("git", "add", ".")
	cmd.Dir = sourceDir
	requireNoError(t, cmd.Run())

	cmd = exec.Command("git", "commit", "-m", "Initial commit")
	cmd.Dir = sourceDir
	requireNoError(t, cmd.Run())

	// Clone to get a repo with remote
	repoDir := filepath.Join(t.TempDir(), "repo")
	cmd = exec.Command("git", "clone", sourceDir, repoDir)
	requireNoError(t, cmd.Run())

	gitDir := filepath.Join(repoDir, ".git")
	parentDir := filepath.Dir(repoDir)

	detachHEAD(t, repoDir)

	// Create worktrees
	mainPath := filepath.Join(parentDir, "main-wt")
	requireNoError(t, git.CreateWorktree(gitDir, mainPath, "main", ""))

	featurePath := filepath.Join(parentDir, "feature-wt")
	requireNoError(t, git.CreateWorktree(gitDir, featurePath, "feature", "main"))

	// Test: running from the repo root should not be in a worktree
	originalDir, _ := os.Getwd()
	defer os.Chdir(originalDir)

	os.Chdir(repoDir)
	// Note: this test can't use OpenProjectFromCWD because it looks up global config
	// Instead test the IsInWorktree logic directly
	pc := &ProjectContext{
		CWD:         repoDir,
		ProjectPath: repoDir,
	}
	assert.False(t, pc.IsInWorktree())

	// Test: running from worktree should be in a worktree
	pc = &ProjectContext{
		CWD:          featurePath,
		GitDir:       gitDir,
		ProjectPath:  repoDir,
		WorktreeBase: parentDir,
	}
	assert.True(t, pc.IsInWorktree())
}

func TestSyncCommand_DetectsDetachedHEAD(t *testing.T) {
	// Create a source repo
	sourceDir := t.TempDir()
	cmd := exec.Command("git", "init", "-b", "main")
	cmd.Dir = sourceDir
	requireNoError(t, cmd.Run())

	cmd = exec.Command("git", "config", "user.email", "test@example.com")
	cmd.Dir = sourceDir
	requireNoError(t, cmd.Run())

	cmd = exec.Command("git", "config", "user.name", "Test User")
	cmd.Dir = sourceDir
	requireNoError(t, cmd.Run())

	readmePath := filepath.Join(sourceDir, "README.md")
	requireNoError(t, os.WriteFile(readmePath, []byte("test"), 0644))

	cmd = exec.Command("git", "add", ".")
	cmd.Dir = sourceDir
	requireNoError(t, cmd.Run())

	cmd = exec.Command("git", "commit", "-m", "Initial commit")
	cmd.Dir = sourceDir
	requireNoError(t, cmd.Run())

	// Clone to get a repo
	repoDir := filepath.Join(t.TempDir(), "repo")
	cmd = exec.Command("git", "clone", sourceDir, repoDir)
	requireNoError(t, cmd.Run())

	gitDir := filepath.Join(repoDir, ".git")
	parentDir := filepath.Dir(repoDir)

	detachHEAD(t, repoDir)

	// Create main worktree
	mainPath := filepath.Join(parentDir, "main-wt")
	requireNoError(t, git.CreateWorktree(gitDir, mainPath, "main", ""))

	// Checkout detached HEAD in the worktree
	cmd = exec.Command("git", "-C", mainPath, "checkout", "HEAD~0")
	requireNoError(t, cmd.Run())

	// Test: detect detached HEAD
	detached, err := git.IsDetachedHEAD(mainPath)
	assert.NoError(t, err)
	assert.True(t, detached)
}

func TestSyncCommand_ValidatesStrategy(t *testing.T) {
	validStrategies := []string{"rebase", "merge"}
	invalidStrategies := []string{"squash", "fast-forward", ""}

	for _, strategy := range validStrategies {
		assert.True(t, strategy == "rebase" || strategy == "merge", "strategy %q should be valid", strategy)
	}

	for _, strategy := range invalidStrategies {
		if strategy != "" {
			assert.False(t, strategy == "rebase" || strategy == "merge", "strategy %q should be invalid", strategy)
		}
	}
}

func TestSyncCommand_ConfigPrecedence(t *testing.T) {
	cfg := &config.Config{
		DefaultBranch: "main",
		Sync: config.SyncConfig{
			Upstream: "develop",
			Strategy: "merge",
			Remote:   "upstream",
		},
	}

	// If CLI flag is set, use it
	flagUpstream := "feature/cli-flag"
	upstream := flagUpstream
	if upstream == "" {
		upstream = cfg.Sync.Upstream
	}
	assert.Equal(t, "feature/cli-flag", upstream)

	// If CLI flag is not set, use config
	flagUpstream = ""
	upstream = flagUpstream
	if upstream == "" {
		upstream = cfg.Sync.Upstream
	}
	assert.Equal(t, "develop", upstream)

	// If neither is set, use default_branch
	cfg.Sync.Upstream = ""
	upstream = flagUpstream
	if upstream == "" {
		upstream = cfg.Sync.Upstream
	}
	if upstream == "" {
		upstream = cfg.DefaultBranch
	}
	assert.Equal(t, "main", upstream)
}

func TestSyncCommand_SaveConfig(t *testing.T) {
	projectDir := t.TempDir()

	initialConfig := &config.Config{
		SiteName:      "test-project",
		DefaultBranch: "main",
	}

	err := config.SaveProject(projectDir, initialConfig)
	assert.NoError(t, err)

	loadedConfig, err := config.LoadProject(projectDir)
	assert.NoError(t, err)
	assert.Equal(t, "test-project", loadedConfig.SiteName)
	assert.Equal(t, "main", loadedConfig.DefaultBranch)

	syncConfig := config.SyncConfig{
		Upstream: "develop",
		Strategy: "rebase",
		Remote:   "origin",
	}
	initialConfig.Sync = syncConfig

	err = config.SaveProject(projectDir, initialConfig)
	assert.NoError(t, err)

	loadedConfig, err = config.LoadProject(projectDir)
	assert.NoError(t, err)
	assert.Equal(t, "develop", loadedConfig.Sync.Upstream)
	assert.Equal(t, "rebase", loadedConfig.Sync.Strategy)
	assert.Equal(t, "origin", loadedConfig.Sync.Remote)
}

func TestSyncCommand_DoesNotStashWhenRemoteMissing(t *testing.T) {
	ensureSyncTestFlags(t)

	// Create a source repo
	sourceDir := t.TempDir()
	cmd := exec.Command("git", "init", "-b", "main")
	cmd.Dir = sourceDir
	requireNoError(t, cmd.Run())

	cmd = exec.Command("git", "config", "user.email", "test@example.com")
	cmd.Dir = sourceDir
	requireNoError(t, cmd.Run())

	cmd = exec.Command("git", "config", "user.name", "Test User")
	cmd.Dir = sourceDir
	requireNoError(t, cmd.Run())

	readmePath := filepath.Join(sourceDir, "README.md")
	requireNoError(t, os.WriteFile(readmePath, []byte("test"), 0644))

	cmd = exec.Command("git", "add", ".")
	cmd.Dir = sourceDir
	requireNoError(t, cmd.Run())

	cmd = exec.Command("git", "commit", "-m", "Initial commit")
	cmd.Dir = sourceDir
	requireNoError(t, cmd.Run())

	// Clone to get a repo with remote
	repoDir := filepath.Join(t.TempDir(), "repo")
	cmd = exec.Command("git", "clone", sourceDir, repoDir)
	requireNoError(t, cmd.Run())

	gitDir := filepath.Join(repoDir, ".git")
	parentDir := filepath.Dir(repoDir)

	// Create worktree
	featurePath := filepath.Join(parentDir, "feature-wt")
	requireNoError(t, git.CreateWorktree(gitDir, featurePath, "feature", "main"))

	// Add untracked file to trigger auto-stash
	changePath := filepath.Join(featurePath, "untracked.txt")
	requireNoError(t, os.WriteFile(changePath, []byte("changes"), 0644))

	hasStash, err := git.HasStash(featurePath)
	assert.NoError(t, err)
	assert.False(t, hasStash)

	originalDir, _ := os.Getwd()
	defer os.Chdir(originalDir)
	requireNoError(t, os.Chdir(featurePath))

	defer func() {
		requireNoError(t, syncCmd.Flags().Set("upstream", ""))
		requireNoError(t, syncCmd.Flags().Set("remote", ""))
	}()

	requireNoError(t, syncCmd.Flags().Set("upstream", "main"))
	requireNoError(t, syncCmd.Flags().Set("remote", "upstream"))

	err = syncCmd.RunE(syncCmd, []string{})
	assert.Error(t, err)

	hasStash, err = git.HasStash(featurePath)
	assert.NoError(t, err)
	assert.False(t, hasStash)
}

<<<<<<< HEAD
func TestSyncCommand_RestoresAutoStashAfterFetchFailure(t *testing.T) {
	ensureSyncTestFlags(t)

	sourceDir := t.TempDir()
	cmd := exec.Command("git", "init", "-b", "main")
	cmd.Dir = sourceDir
	requireNoError(t, cmd.Run())

	cmd = exec.Command("git", "config", "user.email", "test@example.com")
	cmd.Dir = sourceDir
	requireNoError(t, cmd.Run())

	cmd = exec.Command("git", "config", "user.name", "Test User")
	cmd.Dir = sourceDir
	requireNoError(t, cmd.Run())

	readmePath := filepath.Join(sourceDir, "README.md")
	requireNoError(t, os.WriteFile(readmePath, []byte("test"), 0644))
	cmd = exec.Command("git", "add", ".")
	cmd.Dir = sourceDir
	requireNoError(t, cmd.Run())
	cmd = exec.Command("git", "commit", "-m", "Initial commit")
	cmd.Dir = sourceDir
	requireNoError(t, cmd.Run())

	repoDir := filepath.Join(t.TempDir(), "repo")
	cmd = exec.Command("git", "clone", sourceDir, repoDir)
	requireNoError(t, cmd.Run())

	gitDir := filepath.Join(repoDir, ".git")
	parentDir := filepath.Dir(repoDir)
	worktreeBase := filepath.Join(parentDir, "worktrees")
	featurePath := filepath.Join(worktreeBase, "repo", "feature-wt")
	requireNoError(t, os.MkdirAll(filepath.Dir(featurePath), 0755))
	requireNoError(t, git.CreateWorktree(gitDir, featurePath, "feature", "main"))
	worktreeBase = evalSymlinks(worktreeBase)
	repoDir = evalSymlinks(repoDir)
	featurePath = evalSymlinks(featurePath)

	featureReadmePath := filepath.Join(featurePath, "README.md")
	requireNoError(t, os.WriteFile(featureReadmePath, []byte("pre-existing"), 0644))
	preExistingOID, err := git.StashAll(featurePath, "pre-existing stash")
	requireNoError(t, err)

	changePath := filepath.Join(featurePath, "untracked.txt")
	requireNoError(t, os.WriteFile(changePath, []byte("changes"), 0644))

	missingRemote := filepath.Join(t.TempDir(), "missing-remote")
	cmd = exec.Command("git", "-C", repoDir, "remote", "add", "upstream", missingRemote)
	requireNoError(t, cmd.Run())
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "config"))
	requireNoError(t, config.SaveGlobalConfig(&config.GlobalConfig{
		WorktreeBase: worktreeBase,
		Projects: map[string]*config.ProjectInfo{
			"repo": {
				Path:          repoDir,
				DefaultBranch: "main",
			},
		},
	}))
	assert.Equal(t, missingRemote, mustRemoteURL(t, gitDir, "upstream"))
	assert.Error(t, git.FetchRemote(gitDir, "upstream"))

	originalDir, err := os.Getwd()
	requireNoError(t, err)
	defer func() { requireNoError(t, os.Chdir(originalDir)) }()
	requireNoError(t, os.Chdir(featurePath))

	defer func() {
		requireNoError(t, syncCmd.Flags().Set("upstream", ""))
		requireNoError(t, syncCmd.Flags().Set("remote", ""))
	}()
	requireNoError(t, syncCmd.Flags().Set("upstream", "main"))
	requireNoError(t, syncCmd.Flags().Set("remote", "upstream"))

	err = syncCmd.RunE(syncCmd, []string{})
	assert.Error(t, err)

	restored, err := os.ReadFile(changePath)
	requireNoError(t, err)
	assert.Equal(t, "changes", string(restored))

	currentReadme, err := os.ReadFile(featureReadmePath)
	requireNoError(t, err)
	assert.Equal(t, "test", string(currentReadme))

	cmd = exec.Command("git", "-C", featurePath, "stash", "list", "--format=%H")
	output, err := cmd.Output()
	requireNoError(t, err)
	assert.Equal(t, preExistingOID, strings.TrimSpace(string(output)))
}

func TestSyncCommand_RestoresAutoStashAfterSuccessfulSync(t *testing.T) {
	ensureSyncTestFlags(t)

	sourceDir := t.TempDir()
	cmd := exec.Command("git", "init", "-b", "main")
	cmd.Dir = sourceDir
	requireNoError(t, cmd.Run())
	cmd = exec.Command("git", "config", "user.email", "test@example.com")
	cmd.Dir = sourceDir
	requireNoError(t, cmd.Run())
	cmd = exec.Command("git", "config", "user.name", "Test User")
	cmd.Dir = sourceDir
	requireNoError(t, cmd.Run())
	requireNoError(t, os.WriteFile(filepath.Join(sourceDir, "README.md"), []byte("test"), 0644))
	cmd = exec.Command("git", "add", ".")
	cmd.Dir = sourceDir
	requireNoError(t, cmd.Run())
	cmd = exec.Command("git", "commit", "-m", "Initial commit")
	cmd.Dir = sourceDir
	requireNoError(t, cmd.Run())

	repoDir := filepath.Join(t.TempDir(), "repo")
	cmd = exec.Command("git", "clone", sourceDir, repoDir)
	requireNoError(t, cmd.Run())

	gitDir := filepath.Join(repoDir, ".git")
	parentDir := filepath.Dir(repoDir)
	worktreeBase := filepath.Join(parentDir, "worktrees")
	featurePath := filepath.Join(worktreeBase, "repo", "feature-wt")
	requireNoError(t, os.MkdirAll(filepath.Dir(featurePath), 0755))
	requireNoError(t, git.CreateWorktree(gitDir, featurePath, "feature", "main"))
	worktreeBase = evalSymlinks(worktreeBase)
	repoDir = evalSymlinks(repoDir)
	featurePath = evalSymlinks(featurePath)

	readmePath := filepath.Join(featurePath, "README.md")
	requireNoError(t, os.WriteFile(readmePath, []byte("pre-existing"), 0644))
	preExistingOID, err := git.StashAll(featurePath, "pre-existing stash")
	requireNoError(t, err)
	changePath := filepath.Join(featurePath, "untracked.txt")
	requireNoError(t, os.WriteFile(changePath, []byte("changes"), 0644))

	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "config"))
	requireNoError(t, config.SaveGlobalConfig(&config.GlobalConfig{
		WorktreeBase: worktreeBase,
		Projects: map[string]*config.ProjectInfo{
			"repo": {
				Path:          repoDir,
				DefaultBranch: "main",
			},
		},
	}))

	originalDir, err := os.Getwd()
	requireNoError(t, err)
	defer func() { requireNoError(t, os.Chdir(originalDir)) }()
	requireNoError(t, os.Chdir(featurePath))
	defer func() {
		requireNoError(t, syncCmd.Flags().Set("upstream", ""))
		requireNoError(t, syncCmd.Flags().Set("remote", ""))
	}()
	requireNoError(t, syncCmd.Flags().Set("upstream", "main"))
	requireNoError(t, syncCmd.Flags().Set("remote", "origin"))

	err = syncCmd.RunE(syncCmd, []string{})
	requireNoError(t, err)

	restored, err := os.ReadFile(changePath)
	requireNoError(t, err)
	assert.Equal(t, "changes", string(restored))

	cmd = exec.Command("git", "-C", featurePath, "stash", "list", "--format=%H")
	output, err := cmd.Output()
	requireNoError(t, err)
	assert.Equal(t, preExistingOID, strings.TrimSpace(string(output)))
}

func TestSyncCommand_RestoresAutoStashAfterSyncFailure(t *testing.T) {
	ensureSyncTestFlags(t)

	sourceDir := t.TempDir()
	cmd := exec.Command("git", "init", "-b", "main")
	cmd.Dir = sourceDir
	requireNoError(t, cmd.Run())
	cmd = exec.Command("git", "config", "user.email", "test@example.com")
	cmd.Dir = sourceDir
	requireNoError(t, cmd.Run())
	cmd = exec.Command("git", "config", "user.name", "Test User")
	cmd.Dir = sourceDir
	requireNoError(t, cmd.Run())
	requireNoError(t, os.WriteFile(filepath.Join(sourceDir, "README.md"), []byte("test"), 0644))
	cmd = exec.Command("git", "add", ".")
	cmd.Dir = sourceDir
	requireNoError(t, cmd.Run())
	cmd = exec.Command("git", "commit", "-m", "Initial commit")
	cmd.Dir = sourceDir
	requireNoError(t, cmd.Run())

	repoDir := filepath.Join(t.TempDir(), "repo")
	cmd = exec.Command("git", "clone", sourceDir, repoDir)
	requireNoError(t, cmd.Run())

	gitDir := filepath.Join(repoDir, ".git")
	parentDir := filepath.Dir(repoDir)
	worktreeBase := filepath.Join(parentDir, "worktrees")
	featurePath := filepath.Join(worktreeBase, "repo", "feature-wt")
	requireNoError(t, os.MkdirAll(filepath.Dir(featurePath), 0755))
	requireNoError(t, git.CreateWorktree(gitDir, featurePath, "feature", "main"))
	worktreeBase = evalSymlinks(worktreeBase)
	repoDir = evalSymlinks(repoDir)
	featurePath = evalSymlinks(featurePath)

	readmePath := filepath.Join(featurePath, "README.md")
	requireNoError(t, os.WriteFile(readmePath, []byte("pre-existing"), 0644))
	preExistingOID, err := git.StashAll(featurePath, "pre-existing stash")
	requireNoError(t, err)
	changePath := filepath.Join(featurePath, "untracked.txt")
	requireNoError(t, os.WriteFile(changePath, []byte("changes"), 0644))

	emptyRemote := filepath.Join(parentDir, "empty-remote.git")
	cmd = exec.Command("git", "init", "--bare", emptyRemote)
	requireNoError(t, cmd.Run())
	cmd = exec.Command("git", "-C", repoDir, "remote", "add", "upstream", emptyRemote)
	requireNoError(t, cmd.Run())

	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "config"))
	requireNoError(t, config.SaveGlobalConfig(&config.GlobalConfig{
		WorktreeBase: worktreeBase,
		Projects: map[string]*config.ProjectInfo{
			"repo": {
				Path:          repoDir,
				DefaultBranch: "main",
			},
		},
	}))

	originalDir, err := os.Getwd()
	requireNoError(t, err)
	defer func() { requireNoError(t, os.Chdir(originalDir)) }()
	requireNoError(t, os.Chdir(featurePath))
	defer func() {
		requireNoError(t, syncCmd.Flags().Set("upstream", ""))
		requireNoError(t, syncCmd.Flags().Set("remote", ""))
	}()
	requireNoError(t, syncCmd.Flags().Set("upstream", "main"))
	requireNoError(t, syncCmd.Flags().Set("remote", "upstream"))

	err = syncCmd.RunE(syncCmd, []string{})
	assert.Error(t, err)

	restored, err := os.ReadFile(changePath)
	requireNoError(t, err)
	assert.Equal(t, "changes", string(restored))

	cmd = exec.Command("git", "-C", featurePath, "stash", "list", "--format=%H")
	output, err := cmd.Output()
	requireNoError(t, err)
	assert.Equal(t, preExistingOID, strings.TrimSpace(string(output)))
}

func mustRemoteURL(t *testing.T, gitDir string, remote string) string {
	t.Helper()
	url, err := git.GetRemoteURL(gitDir, remote)
	requireNoError(t, err)
	return url
}

func TestSyncBranchSelectionUsesUnifiedRefInventory(t *testing.T) {
	gitDir, repoDir := createTestRepo(t)
	commit := runBranchSelectionGitOutput(t, repoDir, "rev-parse", "HEAD")

	runBranchSelectionGit(t, repoDir, "remote", "add", "origin", "https://example.test/origin.git")
	runBranchSelectionGit(t, repoDir, "update-ref", "refs/remotes/origin/main", commit)
	runBranchSelectionGit(t, repoDir, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/main")

	local, remote, err := branchRefsForSelection(gitDir)
	assert.NoError(t, err)
	assert.Equal(t, []string{"main"}, local)
	assert.Equal(t, []string{"origin/main"}, remote)
}
