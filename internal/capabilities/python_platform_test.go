package capabilities

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPipPreparationUsesPlatformInterpreter(t *testing.T) {
	for _, platform := range []string{"linux", "darwin", "windows"} {
		t.Run(platform, func(t *testing.T) {
			dir := t.TempDir()
			write(t, dir, "requirements.txt")
			contribution := Contribute("python", dir, platform)
			require.Len(t, contribution.Prepare, 1)
			interpreter := "'.venv/bin/python'"
			if platform == "windows" {
				interpreter = "'.venv/Scripts/python.exe'"
			}
			require.Contains(t, contribution.Prepare[0].Run, " && "+interpreter+" -m pip install")
		})
	}
}
