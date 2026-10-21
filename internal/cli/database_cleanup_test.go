package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/naoray/anvil/internal/git"
)

func TestSkipMissingRemovalTargets(t *testing.T) {
	root := t.TempDir()
	present := filepath.Join(root, "present")
	require.NoError(t, os.Mkdir(present, 0o755))
	absent := filepath.Join(root, "absent")
	// An ancestor that is a dangling symlink (an unmounted volume) is not proof of absence.
	unmounted := filepath.Join(root, "unmounted")
	require.NoError(t, os.Symlink(filepath.Join(root, "no-volume"), unmounted))

	t.Run("skips an unlocked absent worktree with a warning", func(t *testing.T) {
		var warnings []string
		kept, err := skipMissingRemovalTargets(
			[]git.Worktree{{Path: absent, Branch: "gone"}, {Path: present, Branch: "kept"}},
			func(message string) { warnings = append(warnings, message) },
		)
		require.NoError(t, err)
		require.Equal(t, []git.Worktree{{Path: present, Branch: "kept"}}, kept)
		require.Len(t, warnings, 1)
		require.Contains(t, warnings[0], "registered worktree "+absent+" no longer exists; skipping it")
		require.Contains(t, warnings[0], "git worktree prune")
	})

	t.Run("fails closed for a locked absent worktree", func(t *testing.T) {
		_, err := skipMissingRemovalTargets(
			[]git.Worktree{{Path: absent, Locked: true, LockReason: "usb"}},
			func(message string) { t.Fatalf("unexpected warning: %s", message) },
		)
		require.EqualError(t, err, "registered worktree "+absent+" is locked (usb) but absent; reconnect it, or unlock and prune it")
	})

	t.Run("fails when absence cannot be proven", func(t *testing.T) {
		path := filepath.Join(unmounted, "feature")
		_, err := skipMissingRemovalTargets(
			[]git.Worktree{{Path: path}},
			func(message string) { t.Fatalf("unexpected warning: %s", message) },
		)
		require.ErrorContains(t, err, "inspecting worktree \""+path+"\"")
	})
}
