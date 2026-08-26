package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/naoray/anvil/internal/config"
)

func TestOpenCommand_RejectsEditorAndBrowserTogether(t *testing.T) {
	err := executeCommandForFlagValidation(t, openCmd, []string{"open", "auth", "--editor", "--browser"}, "editor", "browser")

	assert.EqualError(t, err, "if any flags in the group [editor browser] are set none of the others can be; [browser editor] were all set")
}

func TestOpenCommand_AcceptsEachActionMode(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "both", args: []string{"open", "auth"}},
		{name: "editor", args: []string{"open", "auth", "--editor"}},
		{name: "browser", args: []string{"open", "auth", "--browser"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := executeCommandForFlagValidation(t, openCmd, tt.args, "editor", "browser")

			assert.ErrorIs(t, err, errCommandRun)
		})
	}
}

func TestResolveWorktreeURL_FromAppURL(t *testing.T) {
	dir := t.TempDir()
	envContent := "APP_URL=https://dashboard.test\nAPP_NAME=Dashboard\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".env"), []byte(envContent), 0644))

	url := resolveWorktreeURL(dir)

	assert.Equal(t, "https://dashboard.test", url)
}

func TestResolveWorktreeURL_FallbackToFolderName(t *testing.T) {
	dir := t.TempDir()
	// No .env file

	url := resolveWorktreeURL(dir)

	assert.Equal(t, "https://"+filepath.Base(dir)+".test", url)
}

func TestResolveWorktreeURL_StripsQuotesFromAppURL(t *testing.T) {
	dir := t.TempDir()
	envContent := "APP_URL=\"https://my-feature.test\"\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".env"), []byte(envContent), 0644))

	url := resolveWorktreeURL(dir)

	assert.Equal(t, "https://my-feature.test", url)
}

func TestResolveWorktreeURL_LocalhostFallsBackToFolderName(t *testing.T) {
	dir := t.TempDir()
	envContent := "APP_URL=http://localhost\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".env"), []byte(envContent), 0644))

	url := resolveWorktreeURL(dir)

	assert.Equal(t, "https://"+filepath.Base(dir)+".test", url)
}

func TestResolveWorktreeURL_LocalhostWithPortFallsBack(t *testing.T) {
	dir := t.TempDir()
	envContent := "APP_URL=http://localhost:8000\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".env"), []byte(envContent), 0644))

	url := resolveWorktreeURL(dir)

	assert.Equal(t, "https://"+filepath.Base(dir)+".test", url)
}

func TestResolveEditorCmd_DefaultsToCursor(t *testing.T) {
	pc := &ProjectContext{
		Config:       &config.Config{},
		GlobalConfig: &config.GlobalConfig{},
	}

	result := resolveEditorCmd(pc)

	assert.Equal(t, "cursor", result)
}

func TestResolveEditorCmd_ProjectConfigOverridesGlobal(t *testing.T) {
	pc := &ProjectContext{
		Config:       &config.Config{EditorCmd: "zed"},
		GlobalConfig: &config.GlobalConfig{EditorCmd: "code"},
	}

	result := resolveEditorCmd(pc)

	assert.Equal(t, "zed", result)
}

func TestResolveEditorCmd_GlobalConfigUsedWhenNoProjectConfig(t *testing.T) {
	pc := &ProjectContext{
		Config:       &config.Config{},
		GlobalConfig: &config.GlobalConfig{EditorCmd: "code"},
	}

	result := resolveEditorCmd(pc)

	assert.Equal(t, "code", result)
}
