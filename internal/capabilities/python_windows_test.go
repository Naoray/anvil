package capabilities

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWindowsPipPreparationRunsWithPOSIXShell(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "requirements.txt"), nil, 0o644))
	contribution := Contribute("python", dir, "windows")
	require.Equal(t, []string{"python"}, contribution.Requires)
	cmd := exec.Command("sh", "-c", contribution.Prepare[0].Run)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	require.FileExists(t, filepath.Join(dir, ".venv", "Scripts", "python.exe"))
}
