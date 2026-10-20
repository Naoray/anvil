package warm

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"github.com/naoray/anvil/internal/config"
)

func stateMap(root string) (map[string]any, error) {
	path := filepath.Join(root, ".anvil.local")
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]any{}, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("local state must be a regular file")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	value := map[string]any{}
	if err = yaml.Unmarshal(data, &value); err != nil {
		return nil, err
	}
	if value == nil {
		value = map[string]any{}
	}
	return value, nil
}
func readStamp(root, tree string) (stamp, error) {
	value, err := stateMap(root)
	if err != nil {
		return stamp{}, err
	}
	data, err := yaml.Marshal(value["dependency_installs"])
	if err != nil {
		return stamp{}, err
	}
	records := map[string]stamp{}
	if err = yaml.Unmarshal(data, &records); err != nil {
		return stamp{}, err
	}
	return records[tree], nil
}
func writeStamp(root, tree string, record stamp) error {
	// stateMap enforces the regular-file requirement before the shared writer runs.
	if _, err := stateMap(root); err != nil {
		return err
	}
	return config.UpdateLocalState(root, func(value map[string]any) error {
		records := map[string]any{}
		if existing, ok := value["dependency_installs"]; ok {
			var valid bool
			records, valid = existing.(map[string]any)
			if !valid {
				return fmt.Errorf("malformed dependency provenance")
			}
		}
		if record.Inputs == "" {
			delete(records, tree)
		} else {
			records[tree] = record
		}
		value["dependency_installs"] = records
		return nil
	})
}
