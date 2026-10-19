package warm

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
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
func writeStamp(root, tree string, record stamp) (err error) {
	value, err := stateMap(root)
	if err != nil {
		return err
	}
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
	data, err := yaml.Marshal(value)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(root, ".anvil-warm-state-")
	if err != nil {
		return err
	}
	path := f.Name()
	defer func() {
		if e := os.Remove(path); e != nil && !errors.Is(e, os.ErrNotExist) {
			err = errors.Join(err, e)
		}
	}()
	_, writeErr := f.Write(data)
	if err = errors.Join(writeErr, f.Close()); err != nil {
		return err
	}
	return os.Rename(path, filepath.Join(root, ".anvil.local"))
}
