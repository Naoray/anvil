package presets

import (
	"os"
	"path/filepath"

	"github.com/artisanexperiences/arbor/internal/config"
)

type PHP struct {
	basePreset
}

func NewPHP() *PHP {
	return &PHP{
		basePreset: basePreset{
			name: "php",
			defaultSteps: []config.StepConfig{
				{Name: "php.composer", Args: []string{"install"}, Condition: map[string]interface{}{"file_exists": "composer.lock"}},
				{Name: "php.composer", Args: []string{"update"}, Condition: map[string]interface{}{"not": map[string]interface{}{"file_exists": "composer.lock"}}},
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
