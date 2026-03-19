package presets

import (
	"os"
	"path/filepath"

	"github.com/naoray/anvil/internal/config"
)

type PHP struct {
	basePreset
}

func NewPHP() *PHP {
	return &PHP{
		basePreset: basePreset{
			name: "php",
			defaultSteps: []config.StepConfig{
				{Name: "php.composer", Args: []string{"install"}, ConditionHolder: config.ConditionHolder{Condition: map[string]any{"file_exists": "composer.lock"}}},
				{Name: "php.composer", Args: []string{"update"}, ConditionHolder: config.ConditionHolder{Condition: map[string]any{"not": map[string]any{"file_exists": "composer.lock"}}}},
			},
			cleanupSteps: nil,
		},
	}
}

func (p *PHP) Detect(path string) bool {
	composerPath := filepath.Join(path, "composer.json")
	_, err := os.Stat(composerPath)
	return err == nil
}
