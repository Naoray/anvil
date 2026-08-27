package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

type workflowDefinition struct {
	Jobs map[string]workflowJob `yaml:"jobs"`
}

type workflowJob struct {
	Steps []workflowStep `yaml:"steps"`
}

type workflowStep struct {
	Name string `yaml:"name"`
	Run  string `yaml:"run"`
}

func loadWorkflow(t *testing.T, name string) workflowDefinition {
	t.Helper()
	path := filepath.Join("..", "..", ".github", "workflows", name)
	contents, err := os.ReadFile(path)
	require.NoError(t, err)
	var workflow workflowDefinition
	require.NoError(t, yaml.Unmarshal(contents, &workflow))
	return workflow
}

func workflowRunStep(t *testing.T, workflowName, stepName string) workflowStep {
	t.Helper()
	workflow := loadWorkflow(t, workflowName)
	for _, job := range workflow.Jobs {
		for _, step := range job.Steps {
			if step.Name == stepName {
				require.NotEmpty(t, step.Run, "%s step %q must have a run script", workflowName, stepName)
				return step
			}
		}
	}
	t.Fatalf("%s has no step named %q", workflowName, stepName)
	return workflowStep{}
}

func TestWorkflowRunScriptsContainNoGitHubExpressions(t *testing.T) {
	for _, workflowName := range []string{"release.yml", "changelog.yml"} {
		t.Run(workflowName, func(t *testing.T) {
			workflow := loadWorkflow(t, workflowName)
			for jobName, job := range workflow.Jobs {
				for _, step := range job.Steps {
					assert.False(t, strings.Contains(step.Run, "${{"),
						"%s job %q step %q interpolates a GitHub expression into run source:\n%s",
						workflowName, jobName, step.Name, step.Run)
				}
			}
		})
	}
}

func TestRepositoryHasNoParallelGoReleaserSpecification(t *testing.T) {
	_, err := os.Stat(filepath.Join("..", "..", ".goreleaser.yml"))
	require.ErrorIs(t, err, os.ErrNotExist)
}
