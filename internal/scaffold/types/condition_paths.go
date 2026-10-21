package types

import (
	"path/filepath"

	"github.com/naoray/anvil/internal/config"
)

// ValidateConditionPaths validates every file operand, even behind a false guard.
func ValidateConditionPaths(worktreePath, root string, conditions map[string]any) error {
	if err := ValidateConditions(conditions); err != nil {
		return err
	}
	if len(conditions) == 0 {
		return nil
	}
	return validateConditionPaths(worktreePath, root, conditions)
}

func validateConditionPaths(worktreePath, root string, condition any) error {
	check := func(path string) error {
		if err := config.ValidateRelativePath(path); err != nil {
			return err
		}
		_, err := config.ResolveContainedPath(worktreePath, filepath.Join(root, path))
		return err
	}
	switch c := condition.(type) {
	case map[string]any:
		for _, key := range conditionKeys(c) {
			value := c[key]
			switch key {
			case "not":
				if err := validateConditionPaths(worktreePath, root, value); err != nil {
					return err
				}
			case "file_has_script":
				if err := check("package.json"); err != nil {
					return err
				}
			case "file_exists", "file_contains", "env_file_contains", "env_file_missing":
				var paths []string
				switch v := value.(type) {
				case string:
					if key == "env_file_contains" || key == "env_file_missing" {
						paths = []string{".env"}
					} else {
						paths = []string{v}
					}
				case []string:
					paths = v
				case []any:
					for _, item := range v {
						paths = append(paths, item.(string))
					}
				case map[string]any:
					paths = []string{v["file"].(string)}
				}
				for _, path := range paths {
					if err := check(path); err != nil {
						return err
					}
				}
			}
		}
	case []any:
		for _, item := range c {
			if err := validateConditionPaths(worktreePath, root, item); err != nil {
				return err
			}
		}
	case []map[string]any:
		for _, item := range c {
			if err := validateConditionPaths(worktreePath, root, item); err != nil {
				return err
			}
		}
	}
	return nil
}
