package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func stateRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-b", "main"}, {"config", "user.email", "t@e.st"}, {"config", "user.name", "T"},
	} {
		out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
		require.NoError(t, err, string(out))
	}
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("ignored/\n"), 0o644))
	for _, args := range [][]string{{"add", "."}, {"commit", "-m", "init"}} {
		out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
		require.NoError(t, err, string(out))
	}
	return dir
}

func TestWorkingStateDistinguishesDirtyFromHead(t *testing.T) {
	dir := stateRepo(t)
	clean, err := ReadWorkingState(dir)
	require.NoError(t, err)
	require.False(t, clean.Dirty)
	require.Equal(t, "main", clean.Branch)
	require.Len(t, clean.Head, 40)

	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.txt"), []byte("two"), 0o644))
	dirty, err := ReadWorkingState(dir)
	require.NoError(t, err)
	require.True(t, dirty.Dirty)
	require.Equal(t, clean.Head, dirty.Head)
	require.NotEqual(t, clean.Digest, dirty.Digest, "dirty tree is not its HEAD")

	again, err := ReadWorkingState(dir)
	require.NoError(t, err)
	require.Equal(t, dirty.Digest, again.Digest, "deterministic")

	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.txt"), []byte("three"), 0o644))
	other, err := ReadWorkingState(dir)
	require.NoError(t, err)
	require.NotEqual(t, dirty.Digest, other.Digest, "different dirty contents differ")

	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one"), 0o644))
	restored, err := ReadWorkingState(dir)
	require.NoError(t, err)
	require.Equal(t, clean.Digest, restored.Digest)

	require.NoError(t, os.WriteFile(filepath.Join(dir, "new.txt"), []byte("u"), 0o644))
	untracked, err := ReadWorkingState(dir)
	require.NoError(t, err)
	require.True(t, untracked.Dirty, "untracked files count")
	require.NoError(t, os.Remove(filepath.Join(dir, "new.txt")))

	require.NoError(t, os.MkdirAll(filepath.Join(dir, "ignored"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "ignored", "x"), []byte("i"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".anvil.local"), []byte("x: 1"), 0o644))
	ignored, err := ReadWorkingState(dir)
	require.NoError(t, err)
	require.Equal(t, clean.Digest, ignored.Digest, "ignored files and .anvil.local are excluded")

	require.NoError(t, os.Remove(filepath.Join(dir, "a.txt")))
	deleted, err := ReadWorkingState(dir)
	require.NoError(t, err)
	require.True(t, deleted.Dirty)
}

func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir, "-c", "protocol.file.allow=always", "-c", "user.email=t@e.st", "-c", "user.name=T"}, args...)...).CombinedOutput()
	require.NoError(t, err, string(out))
}

func TestWorkingStateDistinguishesDirtySubmoduleContents(t *testing.T) {
	sub := stateRepo(t)
	require.NoError(t, os.MkdirAll(filepath.Join(sub, "src"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(sub, "src", "lib.txt"), []byte("v1"), 0o644))
	gitIn(t, sub, "add", ".")
	gitIn(t, sub, "commit", "-m", "lib")

	parent := stateRepo(t)
	gitIn(t, parent, "submodule", "add", sub, "sub")
	gitIn(t, parent, "commit", "-m", "add sub")
	clean, err := ReadWorkingState(parent)
	require.NoError(t, err)
	require.False(t, clean.Dirty)

	lib := filepath.Join(parent, "sub", "src", "lib.txt")
	require.NoError(t, os.WriteFile(lib, []byte("dirty A"), 0o644))
	_, err = ReadWorkingState(parent)
	require.ErrorContains(t, err, "dirty submodule or nested repository")
	require.NoError(t, os.WriteFile(lib, []byte("dirty B"), 0o644))
	_, err = ReadWorkingState(parent)
	require.ErrorContains(t, err, "dirty submodule or nested repository", "two dirty states cannot share a reusable identity")

	// A new commit inside the submodule changes the parent state too.
	gitIn(t, filepath.Join(parent, "sub"), "commit", "-am", "move")
	_, err = ReadWorkingState(parent)
	require.ErrorContains(t, err, "dirty submodule or nested repository", "a gitlink differing from the parent is also rejected")
}
