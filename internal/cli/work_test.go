package cli

import (
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

func TestWorkCommand_SetsUpBranchTracking(t *testing.T) {
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

	// Create initial commit
	readmePath := filepath.Join(sourceDir, "README.md")
	requireNoError(t, os.WriteFile(readmePath, []byte("test"), 0644))

	cmd = exec.Command("git", "add", ".")
	cmd.Dir = sourceDir
	requireNoError(t, cmd.Run())

	cmd = exec.Command("git", "commit", "-m", "Initial commit")
	cmd.Dir = sourceDir
	requireNoError(t, cmd.Run())

	// Use the source repo's .git as gitDir
	gitDir := filepath.Join(sourceDir, ".git")
	parentDir := filepath.Dir(sourceDir)

	// Add a remote so tracking can be set
	cmd = exec.Command("git", "-C", sourceDir, "remote", "add", "origin", sourceDir)
	requireNoError(t, cmd.Run())

	// Configure fetch refspec
	requireNoError(t, git.ConfigureFetchRefspec(gitDir, sourceDir))

	detachHEAD(t, sourceDir)

	// Create main worktree
	mainPath := filepath.Join(parentDir, "main-wt")
	requireNoError(t, git.CreateWorktree(gitDir, mainPath, "main", ""))

	// Set up tracking for main
	requireNoError(t, git.SetBranchUpstream(gitDir, "main", "origin"))

	// Verify main has tracking
	hasTracking, err := git.HasBranchTracking(gitDir, "main")
	assert.NoError(t, err)
	assert.True(t, hasTracking)

	// Create feature branch worktree
	featurePath := filepath.Join(parentDir, "feature-wt")
	requireNoError(t, git.CreateWorktree(gitDir, featurePath, "feature", "main"))

	// Set up tracking for feature
	requireNoError(t, git.SetBranchUpstream(gitDir, "feature", "origin"))

	// Verify feature has tracking
	hasTracking, err = git.HasBranchTracking(gitDir, "feature")
	assert.NoError(t, err)
	assert.True(t, hasTracking)

	// Check the tracking config
	cmd = exec.Command("git", "-C", sourceDir, "config", "--get", "branch.feature.remote")
	output, err := cmd.Output()
	assert.NoError(t, err)
	assert.Equal(t, "origin", strings.TrimSpace(string(output)))

	cmd = exec.Command("git", "-C", sourceDir, "config", "--get", "branch.feature.merge")
	output, err = cmd.Output()
	assert.NoError(t, err)
	assert.Equal(t, "refs/heads/feature", strings.TrimSpace(string(output)))
}

func TestWorkBranchSelectionUsesUnifiedRefInventory(t *testing.T) {
	gitDir, repoDir := createTestRepo(t)
	commit := runBranchSelectionGitOutput(t, repoDir, "rev-parse", "HEAD")

	for _, branch := range []string{"feature/nested", "release"} {
		runBranchSelectionGit(t, repoDir, "branch", branch)
	}
	runBranchSelectionGit(t, repoDir, "remote", "add", "origin", "https://example.test/origin.git")
	runBranchSelectionGit(t, repoDir, "update-ref", "refs/remotes/origin/main", commit)

	local, remote, err := branchRefsForSelection(gitDir)
	assert.NoError(t, err)
	assert.Equal(t, []string{"feature/nested", "main", "release"}, local)
	assert.Equal(t, []string{"origin/main"}, remote)
}

func runBranchSelectionGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
	}
}

func runBranchSelectionGitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %s: %v", strings.Join(args, " "), err)
	}
	return strings.TrimSpace(string(output))
}

func TestWorkCommandRemoteRef(t *testing.T) {
	binary := os.Getenv("ANVIL_TEST_BIN")
	require.NotEmpty(t, binary)
	for _, tc := range []struct {
		name, selected, remote, base, local, upstream string
		noTrack, ambiguous, existing                  bool
	}{
		{name: "origin qualified", selected: "origin/feature/nested", remote: "origin", upstream: "origin/feature/nested"},
		{name: "custom qualified", selected: "company/feature/nested", remote: "company", upstream: "company/feature/nested"},
		{name: "origin unqualified", selected: "feature/nested", remote: "origin", upstream: "origin/feature/nested"},
		{name: "custom unqualified", selected: "feature/nested", remote: "company", upstream: "company/feature/nested"},
		{name: "picker canonical ref", selected: "refs/remotes/company/feature/nested", remote: "company", upstream: "company/feature/nested"},
		{name: "no track", selected: "origin/feature/nested", remote: "origin", noTrack: true},
		{name: "custom no track", selected: "company/feature/nested", remote: "company", noTrack: true},
		{name: "explicit base", selected: "company/feature/nested", remote: "company", base: "main", upstream: "company/feature/nested"},
		{name: "literal origin prefix", selected: "origin/literal", remote: "origin", local: "origin/literal", existing: true, upstream: "company/feature/nested"},
		{name: "local and remote ref collision", selected: "origin/feature/nested", remote: "origin", local: "origin/feature/nested", existing: true, upstream: "company/feature/nested"},
		{name: "literal upstream prefix", selected: "upstream/literal", remote: "origin", local: "upstream/literal", existing: true, upstream: "company/feature/nested"},
		{name: "existing local", selected: "company/feature/nested", remote: "company", local: "feature/nested", existing: true, upstream: "origin/feature/nested"},
		{name: "existing local explicit base", selected: "company/feature/nested", remote: "company", local: "feature/nested", existing: true, base: "company/feature/nested", upstream: "origin/feature/nested"},
		{name: "ambiguous", selected: "feature/nested", remote: "company", ambiguous: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, repo := createTestRepo(t)
			mainSHA := runBranchSelectionGitOutput(t, repo, "rev-parse", "HEAD")
			runBranchSelectionGit(t, repo, "commit", "--allow-empty", "-m", "remote feature")
			featureSHA := runBranchSelectionGitOutput(t, repo, "rev-parse", "HEAD")
			require.NotEqual(t, mainSHA, featureSHA)
			runBranchSelectionGit(t, repo, "reset", "--hard", mainSHA)
			for _, remote := range []string{"origin", "company"} {
				runBranchSelectionGit(t, repo, "remote", "add", remote, "https://example.test/"+remote+".git")
				if remote == tc.remote || tc.ambiguous || tc.existing {
					runBranchSelectionGit(t, repo, "update-ref", "refs/remotes/"+remote+"/feature/nested", featureSHA)
				}
			}
			// Even aggressive Git defaults must not defeat --no-track.
			runBranchSelectionGit(t, repo, "config", "branch.autoSetupMerge", "always")
			if tc.existing {
				runBranchSelectionGit(t, repo, "branch", "--no-track", tc.local, "main")
				runBranchSelectionGit(t, repo, "branch", "--set-upstream-to="+tc.upstream, tc.local)
			}
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())
			require.NoError(t, config.SaveGlobalConfig(&config.GlobalConfig{Projects: map[string]*config.ProjectInfo{"test": {Path: repo, DefaultBranch: "main"}}}))
			target := filepath.Join(t.TempDir(), "worktree")
			args := []string{"work", tc.selected, target, "--skip-scaffold"}
			if tc.noTrack {
				args = append(args, "--no-track")
			}
			if tc.base != "" {
				args = append(args, "--base", tc.base)
			}
			cmd := exec.Command(binary, args...)
			cmd.Dir = repo
			output, err := cmd.CombinedOutput()
			if tc.ambiguous {
				require.Error(t, err, string(output))
				assert.Contains(t, string(output), "ambiguous")
				_, statErr := os.Stat(target)
				assert.True(t, os.IsNotExist(statErr))
				return
			}
			require.NoError(t, err, string(output))
			wantSHA := featureSHA
			if tc.base == "main" || tc.existing {
				wantSHA = mainSHA
			}
			assert.Equal(t, wantSHA, runBranchSelectionGitOutput(t, target, "rev-parse", "HEAD"))
			branch := "feature/nested"
			if tc.local != "" {
				branch = tc.local
			}
			assert.Equal(t, "refs/heads/"+branch, runBranchSelectionGitOutput(t, target, "symbolic-ref", "HEAD"))
			if tc.noTrack {
				out := runBranchSelectionGitOutput(t, repo, "for-each-ref", "--format=%(upstream)", "refs/heads/"+branch)
				assert.Empty(t, out)
			} else {
				assert.Equal(t, tc.upstream, runBranchSelectionGitOutput(t, target, "rev-parse", "--abbrev-ref", "@{upstream}"))
			}
		})
	}
}
