//go:build !windows

package cli

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func runWorkflowScript(t *testing.T, step workflowStep, dir string, env ...string) {
	t.Helper()
	cmd := exec.Command("sh", "-eu", "-c", step.Run)
	cmd.Dir = dir
	cmd.Env = subprocessEnv(t, env...)
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "workflow step %q failed: %s", step.Name, output)
}

func TestChangelogWorkflowGuardPreservesHostileTagData(t *testing.T) {
	step := workflowRunStep(t, "changelog.yml", "Check for existing release heading")

	for _, present := range []bool{true, false} {
		name := "absent"
		if present {
			name = "present"
		}
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			marker := filepath.Join(dir, "injected-marker")
			tag := "v1.8.0$(touch " + marker + ");`touch " + marker + "`'\""
			changelog := "# Changelog\n"
			if present {
				changelog += "\n## [" + tag + "](https://example.invalid/compare) - 2026-07-17\n"
			}
			require.NoError(t, os.WriteFile(filepath.Join(dir, "CHANGELOG.md"), []byte(changelog), 0o644))
			githubOutput := filepath.Join(dir, "github-output")

			runWorkflowScript(t, step, dir,
				"RELEASE_TAG="+tag,
				"GITHUB_OUTPUT="+githubOutput,
			)

			output, err := os.ReadFile(githubOutput)
			require.NoError(t, err)
			expected := "exists=false\n"
			if present {
				expected = "exists=true\n"
			}
			assert.Equal(t, expected, string(output))
			assert.NoFileExists(t, marker)
		})
	}
}

func TestReleaseWorkflowBodyPreservesHostileAnnotationExactly(t *testing.T) {
	step := workflowRunStep(t, "release.yml", "Build release body")
	repo := t.TempDir()
	runGitCommand(t, repo, "init", "-b", "main")
	require.NoError(t, os.WriteFile(filepath.Join(repo, "README.md"), []byte("fixture\n"), 0o644))
	runGitCommand(t, repo, "add", "README.md")
	runGitCommand(t, repo, "commit", "-m", "fixture")
	runGitCommand(t, repo, "tag", "--no-sign", "v1.7.1")

	marker := filepath.Join(repo, "injected-marker")
	annotation := "# Release notes\n\n" +
		"`touch " + marker + "`\n" +
		"$(touch " + marker + ")\n" +
		"Quotes: \"double\" and 'single'\n" +
		"EOF\n"
	annotationFile := filepath.Join(repo, "annotation.md")
	require.NoError(t, os.WriteFile(annotationFile, []byte(annotation), 0o644))
	runGitCommand(t, repo, "tag", "--no-sign", "-a", "v1.8.0", "--cleanup=verbatim", "-F", annotationFile)

	direct := exec.Command("git", "cat-file", "tag", "v1.8.0")
	direct.Dir = repo
	tagObject, err := direct.Output()
	require.NoError(t, err)
	_, directAnnotation, found := bytes.Cut(tagObject, []byte("\n\n"))
	require.True(t, found, "annotated tag object has no header separator")
	require.Equal(t, annotation, string(directAnnotation))

	runnerTemp := t.TempDir()
	var bodyPaths []string
	for range 2 {
		githubOutput := filepath.Join(t.TempDir(), "github-output")
		runWorkflowScript(t, step, repo,
			"VERSION=1.8.0",
			"PREVIOUS=v1.7.1",
			"REPOSITORY=naoray/anvil",
			"RUNNER_TEMP="+runnerTemp,
			"GITHUB_OUTPUT="+githubOutput,
		)

		output, readErr := os.ReadFile(githubOutput)
		require.NoError(t, readErr)
		bodyPath, found := strings.CutPrefix(strings.TrimSuffix(string(output), "\n"), "BODY_PATH=")
		require.True(t, found, "unexpected GITHUB_OUTPUT: %q", output)
		assert.Equal(t, runnerTemp, filepath.Dir(bodyPath))
		bodyPaths = append(bodyPaths, bodyPath)

		body, bodyErr := os.ReadFile(bodyPath)
		require.NoError(t, bodyErr)
		link := fmt.Sprintf("\n\n**Full changelog:** [%s..%s](https://github.com/%s/compare/%s..%s)\n",
			"v1.7.1", "v1.8.0", "naoray/anvil", "v1.7.1", "v1.8.0")
		expected := append(bytes.Clone(directAnnotation), link...)
		assert.Equal(t, expected, body)
		assert.Contains(t, string(body), "\nEOF\n")
		assert.NoFileExists(t, marker)
	}
	assert.NotEqual(t, bodyPaths[0], bodyPaths[1], "release body files must be collision-safe")
}
