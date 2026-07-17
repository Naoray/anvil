package steps

import (
	"testing"

	anvilexec "github.com/naoray/anvil/internal/exec"
	"github.com/naoray/anvil/internal/scaffold/types"
)

func TestCleanupCapableCommandSteps_DryRunDoesNotExecuteCommands(t *testing.T) {
	tests := []struct {
		name string
		step types.ScaffoldStep
		mock *anvilexec.MockCommander
	}{
		{
			name: "binary",
			mock: anvilexec.NewMockCommander(),
		},
		{
			name: "bash",
			mock: anvilexec.NewMockCommander(),
		},
		{
			name: "command",
			mock: anvilexec.NewMockCommander(),
		},
	}
	tests[0].step = NewBinaryStepWithExecutor("herd", "herd", []string{"unlink"}, "", anvilexec.NewCommandExecutor(tests[0].mock))
	tests[1].step = NewBashRunStepWithExecutor("echo mutate", "", anvilexec.NewCommandExecutor(tests[1].mock))
	tests[2].step = NewCommandRunStepWithExecutor("echo mutate", "", anvilexec.NewCommandExecutor(tests[2].mock))

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.step.Run(&types.ScaffoldContext{WorktreePath: t.TempDir()}, types.StepOptions{DryRun: true})
			if err != nil {
				t.Fatalf("Run() error = %v", err)
			}
			if tt.mock.CallCount() != 0 {
				t.Fatalf("command calls = %d, want 0", tt.mock.CallCount())
			}
		})
	}
}
