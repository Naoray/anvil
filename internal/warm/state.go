package warm

import (
	"fmt"

	"gopkg.in/yaml.v3"

	"github.com/naoray/anvil/internal/config"
)

func readStamp(root, tree string) (stamp, error) {
	value, err := config.ReadLocalStateMap(root)
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
