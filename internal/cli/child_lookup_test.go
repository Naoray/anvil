package cli

import (
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLookPathInUsesChildPathAndDirectory(t *testing.T) {
	name := "tool"
	content, mode := []byte("#!/bin/sh\n"), os.FileMode(0o755)
	if runtime.GOOS == "windows" {
		name = "tool.exe"
	}
	first, second, cwd := t.TempDir(), t.TempDir(), t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(second, name), content, mode))
	require.NoError(t, os.WriteFile(filepath.Join(cwd, name), content, mode))
	path := "PATH=" + first + string(os.PathListSeparator) + "relative" + string(os.PathListSeparator) + second
	env := []string{path, "PATHEXT=.EXE"}

	found, err := lookPathIn("tool", cwd, env)
	require.NoError(t, err)
	require.Equal(t, filepath.Join(second, name), found, "PATH order wins, current directory is never searched")

	found, err = lookPathIn("."+string(filepath.Separator)+name, cwd, env)
	require.NoError(t, err)
	require.Equal(t, filepath.Join(cwd, name), found, "explicit relative paths resolve against the child directory")

	_, err = lookPathIn("missing", cwd, env)
	require.True(t, errors.Is(err, exec.ErrNotFound))
	require.Equal(t, 127, childLookupError("missing", err).(*ChildExitError).Code)
}

func TestLookPathInReportsNonExecutableFiles(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows has no execute bit")
	}
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "tool"), []byte("x"), 0o644))
	_, err := lookPathIn("tool", dir, []string{"PATH=" + dir})
	require.True(t, errors.Is(err, fs.ErrPermission))
	require.Equal(t, 126, childLookupError("tool", err).(*ChildExitError).Code)
}
