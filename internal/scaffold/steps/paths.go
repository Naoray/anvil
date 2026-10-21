package steps

import (
	"fmt"
	"path/filepath"

	"github.com/naoray/anvil/internal/config"
	"github.com/naoray/anvil/internal/scaffold/types"
)

// ValidatePaths checks declared paths before scaffold effects and again at use.
// Missing directories may be created by an earlier step.
func ValidatePaths(worktreePath string, cfg config.StepConfig) error {
	root, err := config.ResolveContainedPath(worktreePath, filepath.Join(worktreePath, cfg.Dir))
	if err != nil {
		return fmt.Errorf("%s: dir: %w", cfg.Name, err)
	}
	for _, pattern := range append(append([]string(nil), cfg.Inputs...), cfg.Outputs...) {
		if err := config.ValidateContainedPattern(worktreePath, root, pattern); err != nil {
			return fmt.Errorf("%s: declared path: %w", cfg.Name, err)
		}
	}
	return types.ValidateConditionPaths(worktreePath, root, cfg.Condition)
}
