package cli

import (
	"testing"

	"github.com/artisanexperiences/arbor/internal/git"
)

func TestSortWorktreesForDestroy(t *testing.T) {
	tests := []struct {
		name          string
		worktrees     []git.Worktree
		defaultBranch string
		expectedOrder []string
	}{
		{
			name: "features alphabetically then main",
			worktrees: []git.Worktree{
				{Branch: "main"},
				{Branch: "feature-a"},
				{Branch: "feature-b"},
			},
			defaultBranch: "main",
			expectedOrder: []string{"feature-a", "feature-b", "main"},
		},
		{
			name: "all features, no main",
			worktrees: []git.Worktree{
				{Branch: "feature-c"},
				{Branch: "feature-a"},
				{Branch: "feature-b"},
			},
			defaultBranch: "main",
			expectedOrder: []string{"feature-a", "feature-b", "feature-c"},
		},
		{
			name: "only main",
			worktrees: []git.Worktree{
				{Branch: "main"},
			},
			defaultBranch: "main",
			expectedOrder: []string{"main"},
		},
		{
			name: "master as default",
			worktrees: []git.Worktree{
				{Branch: "master"},
				{Branch: "feature-a"},
			},
			defaultBranch: "master",
			expectedOrder: []string{"feature-a", "master"},
		},
		{
			name: "mixed feature types",
			worktrees: []git.Worktree{
				{Branch: "main"},
				{Branch: "hotfix-a"},
				{Branch: "feature-z"},
				{Branch: "feature-a"},
				{Branch: "hotfix-z"},
			},
			defaultBranch: "main",
			expectedOrder: []string{"feature-a", "feature-z", "hotfix-a", "hotfix-z", "main"},
		},
		{
			name:          "empty worktrees",
			worktrees:     []git.Worktree{},
			defaultBranch: "main",
			expectedOrder: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := sortWorktreesForDestroy(tt.worktrees, tt.defaultBranch)
			if len(result) != len(tt.expectedOrder) {
				t.Errorf("got %d worktrees, want %d", len(result), len(tt.expectedOrder))
				return
			}

			for i, expected := range tt.expectedOrder {
				if i >= len(result) {
					t.Errorf("missing expected branch at index %d: %s", i, expected)
					continue
				}
				if result[i].Branch != expected {
					t.Errorf("worktree at index %d = %s, want %s", i, result[i].Branch, expected)
				}
			}
		})
	}
}

func TestSortWorktreesForDestroy_Stability(t *testing.T) {
	worktrees := []git.Worktree{
		{Branch: "feature-a", Path: "/path/feature-a"},
		{Branch: "feature-b", Path: "/path/feature-b"},
		{Branch: "main", Path: "/path/main"},
		{Branch: "feature-c", Path: "/path/feature-c"},
	}

	defaultBranch := "main"
	result1 := sortWorktreesForDestroy(worktrees, defaultBranch)
	result2 := sortWorktreesForDestroy(worktrees, defaultBranch)

	if len(result1) != len(result2) {
		t.Error("multiple calls produced different number of worktrees")
	}

	for i := 0; i < len(result1); i++ {
		if result1[i].Branch != result2[i].Branch || result1[i].Path != result2[i].Path {
			t.Errorf("sort is not stable: index %d differs", i)
		}
	}
}
