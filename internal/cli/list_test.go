package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/artisanexperiences/arbor/internal/git"
)

func createTestRepo(t *testing.T) (string, string) {
	tmpDir := t.TempDir()
	repoDir := filepath.Join(tmpDir, "repo")
	barePath := filepath.Join(tmpDir, ".bare")

	if err := os.MkdirAll(repoDir, 0755); err != nil {
		t.Fatalf("creating repo dir: %v", err)
	}

	cmd := exec.Command("git", "init", "-b", "main")
	cmd.Dir = repoDir
	if err := cmd.Run(); err != nil {
		t.Fatalf("initializing git repo: %v", err)
	}

	cmd = exec.Command("git", "config", "user.email", "test@example.com")
	cmd.Dir = repoDir
	if err := cmd.Run(); err != nil {
		t.Fatalf("setting git user.email: %v", err)
	}

	cmd = exec.Command("git", "config", "user.name", "Test User")
	cmd.Dir = repoDir
	if err := cmd.Run(); err != nil {
		t.Fatalf("setting git user.name: %v", err)
	}

	readmePath := filepath.Join(repoDir, "README.md")
	if err := os.WriteFile(readmePath, []byte("test"), 0644); err != nil {
		t.Fatalf("writing README: %v", err)
	}

	cmd = exec.Command("git", "add", ".")
	cmd.Dir = repoDir
	if err := cmd.Run(); err != nil {
		t.Fatalf("staging files: %v", err)
	}

	cmd = exec.Command("git", "commit", "-m", "Initial commit")
	cmd.Dir = repoDir
	if err := cmd.Run(); err != nil {
		t.Fatalf("committing: %v", err)
	}

	cmd = exec.Command("git", "clone", "--bare", repoDir, barePath)
	if err := cmd.Run(); err != nil {
		t.Fatalf("cloning to bare: %v", err)
	}

	return barePath, repoDir
}

func TestPrintTable_Empty(t *testing.T) {
	var buf bytes.Buffer
	err := printTable(&buf, []git.Worktree{})
	if err != nil {
		t.Fatalf("printTable failed: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "No worktrees found") {
		t.Errorf("expected 'No worktrees found' message, got: %s", output)
	}
}

func TestPrintTable_WithWorktrees(t *testing.T) {
	worktrees := []git.Worktree{
		{Path: "/test/main", Branch: "main", IsMain: true, IsCurrent: true, IsMerged: true},
		{Path: "/test/feature", Branch: "feature", IsMain: false, IsCurrent: false, IsMerged: true},
		{Path: "/test/unmerged", Branch: "unmerged", IsMain: false, IsCurrent: false, IsMerged: false},
	}

	var buf bytes.Buffer
	err := printTable(&buf, worktrees)
	if err != nil {
		t.Fatalf("printTable failed: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "main") {
		t.Errorf("output should contain main, got: %s", output)
	}
	if !strings.Contains(output, "feature") {
		t.Errorf("output should contain feature, got: %s", output)
	}
	if !strings.Contains(output, "unmerged") {
		t.Errorf("output should contain unmerged, got: %s", output)
	}
	if !strings.Contains(output, "current") {
		t.Errorf("output should contain current status, got: %s", output)
	}
	if !strings.Contains(output, "main") {
		t.Errorf("output should contain main status, got: %s", output)
	}

	lines := strings.Split(output, "\n")
	var mainLine string
	for _, line := range lines {
		if strings.Contains(line, "main") && strings.Contains(line, "│") {
			mainLine = line
			break
		}
	}
	if mainLine == "" {
		t.Error("could not find main row in output")
	} else if strings.Contains(mainLine, "merged") {
		t.Errorf("main branch should not show merged status, got: %s", mainLine)
	}
	if !strings.Contains(output, "active") && !strings.Contains(output, "merged") {
		t.Errorf("output should contain status for unmerged branch, got: %s", output)
	}
}

func TestPrintJSON(t *testing.T) {
	worktrees := []git.Worktree{
		{Path: "/test/main", Branch: "main", IsMain: true, IsCurrent: true, IsMerged: true},
		{Path: "/test/feature", Branch: "feature", IsMain: false, IsCurrent: false, IsMerged: false},
	}

	var buf bytes.Buffer
	err := printJSON(&buf, worktrees)
	if err != nil {
		t.Fatalf("printJSON failed: %v", err)
	}

	var result []struct {
		Path      string `json:"path"`
		Branch    string `json:"branch"`
		IsMain    bool   `json:"isMain"`
		IsCurrent bool   `json:"isCurrent"`
		IsMerged  bool   `json:"isMerged"`
	}
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("invalid JSON output: %v\n%s", err, buf.String())
	}

	if len(result) != 2 {
		t.Errorf("expected 2 items in JSON array, got %d", len(result))
	}

	for _, wt := range result {
		switch wt.Branch {
		case "main":
			if !wt.IsMain {
				t.Error("main branch should have isMain=true")
			}
			if !wt.IsCurrent {
				t.Error("main branch should have isCurrent=true")
			}
			if !wt.IsMerged {
				t.Error("main branch should have isMerged=true")
			}
		case "feature":
			if wt.IsMain {
				t.Error("feature branch should have isMain=false")
			}
			if wt.IsCurrent {
				t.Error("feature branch should have isCurrent=false")
			}
			if wt.IsMerged {
				t.Error("feature branch should have isMerged=false")
			}
		}
	}
}

func TestPrintPorcelain(t *testing.T) {
	worktrees := []git.Worktree{
		{Path: "/test/main", Branch: "main", IsMain: true, IsCurrent: true, IsMerged: true},
		{Path: "/test/feature", Branch: "feature", IsMain: false, IsCurrent: false, IsMerged: false},
	}

	var buf bytes.Buffer
	err := printPorcelain(&buf, worktrees)
	if err != nil {
		t.Fatalf("printPorcelain failed: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 2 {
		t.Errorf("expected 2 lines, got %d: %s", len(lines), buf.String())
	}

	for _, line := range lines {
		parts := strings.Split(line, " ")
		if len(parts) < 5 {
			t.Fatalf("porcelain line should have 5 fields, got %d: %s", len(parts), line)
		}
	}
}

func TestPrintTable_SingleWorktree(t *testing.T) {
	worktrees := []git.Worktree{
		{Path: "/test/feature", Branch: "feature", IsMain: false, IsCurrent: true, IsMerged: false},
	}

	var buf bytes.Buffer
	err := printTable(&buf, worktrees)
	if err != nil {
		t.Fatalf("printTable failed: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "WORKTREE") {
		t.Errorf("output should contain header, got: %s", output)
	}
	if !strings.Contains(output, "BRANCH") {
		t.Errorf("output should contain header, got: %s", output)
	}
	if !strings.Contains(output, "STATUS") {
		t.Errorf("output should contain header, got: %s", output)
	}
	if !strings.Contains(output, "feature") {
		t.Errorf("output should contain feature, got: %s", output)
	}
	if !strings.Contains(output, "current") {
		t.Errorf("output should contain current status, got: %s", output)
	}
	if strings.Contains(output, "No worktrees found") {
		t.Error("should not show empty message when worktrees exist")
	}
}

func TestPrintPorcelain_Empty(t *testing.T) {
	var buf bytes.Buffer
	err := printPorcelain(&buf, []git.Worktree{})
	if err != nil {
		t.Fatalf("printPorcelain failed: %v", err)
	}
	assert.Equal(t, "", buf.String(), "empty worktree list should produce empty output")
}

func TestPrintJSON_Empty(t *testing.T) {
	var buf bytes.Buffer
	err := printJSON(&buf, []git.Worktree{})
	if err != nil {
		t.Fatalf("printJSON failed: %v", err)
	}
	assert.Equal(t, "[]\n", buf.String(), "empty worktree list should produce empty JSON array")
}

func TestListCommand_Integration(t *testing.T) {
	barePath, _ := createTestRepo(t)
	projectDir := filepath.Dir(barePath)

	mainPath := filepath.Join(projectDir, "main")
	if err := git.CreateWorktree(barePath, mainPath, "main", ""); err != nil {
		t.Fatalf("creating main worktree: %v", err)
	}

	featurePath := filepath.Join(projectDir, "feature")
	if err := git.CreateWorktree(barePath, featurePath, "feature", "main"); err != nil {
		t.Fatalf("creating feature worktree: %v", err)
	}

	cmd := exec.Command("git", "config", "user.email", "test@example.com")
	cmd.Dir = featurePath
	if err := cmd.Run(); err != nil {
		t.Fatalf("setting git user.email: %v", err)
	}

	cmd = exec.Command("git", "config", "user.name", "Test User")
	cmd.Dir = featurePath
	if err := cmd.Run(); err != nil {
		t.Fatalf("setting git user.name: %v", err)
	}

	readmePath := filepath.Join(featurePath, "README.md")
	if err := os.WriteFile(readmePath, []byte("test\nfeature"), 0644); err != nil {
		t.Fatalf("writing README: %v", err)
	}

	cmd = exec.Command("git", "add", ".")
	cmd.Dir = featurePath
	if err := cmd.Run(); err != nil {
		t.Fatalf("staging files: %v", err)
	}

	cmd = exec.Command("git", "commit", "-m", "Feature commit")
	cmd.Dir = featurePath
	if err := cmd.Run(); err != nil {
		t.Fatalf("committing: %v", err)
	}

	worktrees, err := git.ListWorktreesDetailed(barePath, mainPath, "main")
	if err != nil {
		t.Fatalf("listing worktrees: %v", err)
	}

	if len(worktrees) != 2 {
		t.Errorf("expected 2 worktrees, got %d", len(worktrees))
	}

	mainFound := false
	featureFound := false
	for _, wt := range worktrees {
		if wt.Branch == "main" {
			mainFound = true
			if !wt.IsMain {
				t.Error("main worktree should have IsMain=true")
			}
			if !wt.IsCurrent {
				t.Error("main worktree should have IsCurrent=true when cwd is main")
			}
		}
		if wt.Branch == "feature" {
			featureFound = true
			if wt.IsMain {
				t.Error("feature worktree should have IsMain=false")
			}
			if wt.IsCurrent {
				t.Error("feature worktree should have IsCurrent=false when cwd is main")
			}
			// feature at same commit as main should NOT be marked as merged
			if wt.IsMerged {
				t.Error("feature worktree should not be merged (at same commit as main)")
			}
		}
	}

	if !mainFound {
		t.Error("main worktree should be found")
	}
	if !featureFound {
		t.Error("feature worktree should be found")
	}
}

func TestListCommand_FolderNameMatchesArborRemove(t *testing.T) {
	barePath, _ := createTestRepo(t)
	projectDir := filepath.Dir(barePath)

	mainPath := filepath.Join(projectDir, "main")
	if err := git.CreateWorktree(barePath, mainPath, "main", ""); err != nil {
		t.Fatalf("creating main worktree: %v", err)
	}

	featurePath := filepath.Join(projectDir, "my-feature")
	if err := git.CreateWorktree(barePath, featurePath, "feature/my-cool-feature", "main"); err != nil {
		t.Fatalf("creating feature worktree: %v", err)
	}

	worktrees, err := git.ListWorktrees(barePath)
	if err != nil {
		t.Fatalf("listing worktrees: %v", err)
	}

	var myFeatureWorktree *git.Worktree
	for _, wt := range worktrees {
		if filepath.Base(wt.Path) == "my-feature" {
			myFeatureWorktree = &wt
			break
		}
	}

	if myFeatureWorktree == nil {
		t.Fatal("should find my-feature worktree by folder name")
	}

	if myFeatureWorktree.Branch != "feature/my-cool-feature" {
		t.Errorf("expected branch 'feature/my-cool-feature', got '%s'", myFeatureWorktree.Branch)
	}

	featurePathEval, _ := filepath.EvalSymlinks(featurePath)
	wtPathEval, _ := filepath.EvalSymlinks(myFeatureWorktree.Path)
	if wtPathEval != featurePathEval {
		t.Errorf("expected path %s (resolved: %s), got %s (resolved: %s)", featurePath, featurePathEval, myFeatureWorktree.Path, wtPathEval)
	}
}
