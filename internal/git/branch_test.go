package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSetBranchUpstream(t *testing.T) {
	repoDir := createTestRepo(t)
	gitDir := filepath.Join(repoDir, ".git")

	// Set up remote origin first
	err := ConfigureFetchRefspec(gitDir, "git@github.com:test/repo.git")
	assert.NoError(t, err)

	// Set up tracking for main branch
	err = SetBranchUpstream(gitDir, "main", "origin")
	assert.NoError(t, err)

	// Verify remote is set
	cmd := exec.Command("git", "-C", repoDir, "config", "--get", "branch.main.remote")
	output, err := cmd.Output()
	assert.NoError(t, err)
	assert.Equal(t, "origin", strings.TrimSpace(string(output)))

	// Verify merge is set
	cmd = exec.Command("git", "-C", repoDir, "config", "--get", "branch.main.merge")
	output, err = cmd.Output()
	assert.NoError(t, err)
	assert.Equal(t, "refs/heads/main", strings.TrimSpace(string(output)))
}

func TestSetBranchUpstream_Idempotent(t *testing.T) {
	repoDir := createTestRepo(t)
	gitDir := filepath.Join(repoDir, ".git")

	// Set up remote origin
	err := ConfigureFetchRefspec(gitDir, "git@github.com:test/repo.git")
	assert.NoError(t, err)

	// Set up tracking first time
	err = SetBranchUpstream(gitDir, "main", "origin")
	assert.NoError(t, err)

	// Set up tracking second time - should not error
	err = SetBranchUpstream(gitDir, "main", "origin")
	assert.NoError(t, err)

	// Verify still correct
	hasTracking, err := HasBranchTracking(gitDir, "main")
	assert.NoError(t, err)
	assert.True(t, hasTracking)
}

func TestHasBranchTracking(t *testing.T) {
	repoDir := createTestRepo(t)
	gitDir := filepath.Join(repoDir, ".git")

	// Initially no tracking
	has, err := HasBranchTracking(gitDir, "main")
	assert.NoError(t, err)
	assert.False(t, has)

	// Set up remote origin and tracking
	err = ConfigureFetchRefspec(gitDir, "git@github.com:test/repo.git")
	assert.NoError(t, err)

	err = SetBranchUpstream(gitDir, "main", "origin")
	assert.NoError(t, err)

	// Now should have tracking
	has, err = HasBranchTracking(gitDir, "main")
	assert.NoError(t, err)
	assert.True(t, has)
}

func TestGetBranchRefs(t *testing.T) {
	repoDir := createTestRepo(t)
	gitDir := filepath.Join(repoDir, ".git")

	// Get branches
	local, remote, err := GetBranchRefs(gitDir)
	assert.NoError(t, err)

	// Should have at least main branch
	assert.Contains(t, local, "main")
	// No remotes configured yet
	assert.Empty(t, remote)
}

func TestGetBranchRefs_PreservesBranchInventory(t *testing.T) {
	repoDir := createTestRepo(t)
	gitDir := filepath.Join(repoDir, ".git")
	commit := runGitOutput(t, repoDir, "rev-parse", "HEAD")

	for _, branch := range []string{"feature/nested/z", "feature/checked-out", "release"} {
		runGit(t, repoDir, "branch", branch)
	}

	runGit(t, repoDir, "remote", "add", "origin", "https://example.test/origin.git")
	runGit(t, repoDir, "remote", "add", "upstream", "https://example.test/upstream.git")
	for _, ref := range []string{
		"refs/remotes/origin/main",
		"refs/remotes/origin/feature/nested/z",
		"refs/remotes/upstream/main",
		"refs/remotes/upstream/release",
	} {
		runGit(t, repoDir, "update-ref", ref, commit)
	}
	runGit(t, repoDir, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/main")
	runGit(t, repoDir, "symbolic-ref", "refs/remotes/upstream/HEAD", "refs/remotes/upstream/main")

	checkedOutPath := filepath.Join(t.TempDir(), "checked-out")
	if err := CreateWorktree(gitDir, checkedOutPath, "feature/checked-out", "main"); err != nil {
		t.Fatalf("creating checked-out worktree: %v", err)
	}

	local, remote, err := GetBranchRefs(gitDir)
	assert.NoError(t, err)
	assert.Equal(t, []string{"feature/checked-out", "feature/nested/z", "main", "release"}, local)
	assert.Equal(t, []string{
		"origin/feature/nested/z",
		"origin/main",
		"upstream/main",
		"upstream/release",
	}, remote)
}

func TestGetBranchRefs_EmptyInventory(t *testing.T) {
	repoDir := filepath.Join(t.TempDir(), "empty")
	if err := os.MkdirAll(repoDir, 0755); err != nil {
		t.Fatalf("creating repository directory: %v", err)
	}
	runGit(t, repoDir, "init", "-b", "main")

	local, remote, err := GetBranchRefs(filepath.Join(repoDir, ".git"))
	assert.NoError(t, err)
	assert.Empty(t, local)
	assert.Empty(t, remote)
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
	}
}

func runGitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %s: %v", strings.Join(args, " "), err)
	}
	return strings.TrimSpace(string(output))
}

func TestResolveWorkBranch(t *testing.T) {
	repo := createTestRepo(t)
	gitDir := filepath.Join(repo, ".git")
	run := func(args ...string) {
		t.Helper()
		out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput()
		require.NoError(t, err, string(out))
	}
	run("remote", "add", "company", "https://example.test/company.git")
	run("remote", "add", "origin", "https://example.test/origin.git")
	run("branch", "origin/feature/nested")
	run("update-ref", "refs/remotes/origin/feature/nested", "HEAD")
	run("update-ref", "refs/remotes/company/feature/nested", "HEAD")
	run("update-ref", "refs/remotes/company/unique", "HEAD")
	run("update-ref", "refs/remotes/removed/stale", "HEAD")
	for _, tc := range []struct {
		selected string
		want     WorkBranch
		wantErr  string
	}{
		{selected: "origin/feature/nested", want: WorkBranch{Branch: "origin/feature/nested", Existing: true}},
		{selected: "refs/remotes/origin/feature/nested", want: WorkBranch{Branch: "feature/nested", Base: "refs/remotes/origin/feature/nested", Remote: "origin"}},
		{selected: "company/feature/nested", want: WorkBranch{Branch: "feature/nested", Base: "refs/remotes/company/feature/nested", Remote: "company"}},
		{selected: "unique", want: WorkBranch{Branch: "unique", Base: "refs/remotes/company/unique", Remote: "company"}},
		{selected: "origin/new", want: WorkBranch{Branch: "origin/new"}},
		{selected: "upstream/new", want: WorkBranch{Branch: "upstream/new"}},
		{selected: "removed/stale", want: WorkBranch{Branch: "removed/stale"}},
		{selected: "new", want: WorkBranch{Branch: "new"}},
		{selected: "feature/nested", wantErr: "ambiguous"},
		{selected: "refs/remotes/origin/missing", wantErr: "not found"},
	} {
		t.Run(tc.selected, func(t *testing.T) {
			got, err := ResolveWorkBranch(gitDir, tc.selected)
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}
