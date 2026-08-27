package ui

import (
	"strings"
	"testing"

	"github.com/naoray/anvil/internal/git"
)

func TestWorktreeRemovalOption_UsesPathAndStateLabels(t *testing.T) {
	detached := git.Worktree{Path: "/worktrees/detached-a", Detached: true}
	locked := git.Worktree{Path: "/worktrees/locked", Branch: "feature/locked", Locked: true, LockReason: "keep for review"}
	bare := git.Worktree{Path: "/worktrees/bare", Bare: true}

	detachedLabel, detachedKey := worktreeRemovalOption(detached)
	lockedLabel, lockedKey := worktreeRemovalOption(locked)
	bareLabel, bareKey := worktreeRemovalOption(bare)

	if detachedKey != detached.Path || lockedKey != locked.Path || bareKey != bare.Path {
		t.Fatalf("removal keys must use unique worktree paths: %q, %q, %q", detachedKey, lockedKey, bareKey)
	}
	for _, expected := range []string{"detached", detached.Path} {
		if !strings.Contains(detachedLabel, expected) {
			t.Errorf("detached label should contain %q, got %q", expected, detachedLabel)
		}
	}
	for _, expected := range []string{"locked", "keep for review", locked.Path} {
		if !strings.Contains(lockedLabel, expected) {
			t.Errorf("locked label should contain %q, got %q", expected, lockedLabel)
		}
	}
	for _, expected := range []string{"bare", bare.Path} {
		if !strings.Contains(bareLabel, expected) {
			t.Errorf("bare label should contain %q, got %q", expected, bareLabel)
		}
	}
}

func TestFindWorktreeForRemoval_DistinguishesDetachedPaths(t *testing.T) {
	worktrees := []git.Worktree{
		{Path: "/worktrees/detached-a", Detached: true},
		{Path: "/worktrees/detached-b", Detached: true},
	}

	selected, err := findWorktreeForRemoval(worktrees, "/worktrees/detached-b")
	if err != nil {
		t.Fatalf("finding detached worktree: %v", err)
	}
	if selected == nil || selected.Path != "/worktrees/detached-b" {
		t.Fatalf("expected detached-b selection, got %+v", selected)
	}
}
