package presets

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"github.com/michaeldyrynda/arbor/internal/config"
)

type Laravel struct {
	basePreset
}

func NewLaravel() *Laravel {
	return &Laravel{
		basePreset: basePreset{
			name: "laravel",
			defaultSteps: []config.StepConfig{
				{Name: "php.composer", Args: []string{"install"}},
				{Name: "node.npm", Args: []string{"ci"}},
				{Name: "database.create"},
				{Name: "php.laravel.artisan", Args: []string{"key:generate"}, Condition: map[string]interface{}{"env_not_exists": "APP_KEY"}},
				{Name: "file.copy", From: ".env.example", To: ".env"},
				{Name: "php.laravel.artisan", Args: []string{"migrate:fresh", "--seed"}, Condition: map[string]interface{}{"env_exists": "DB_CONNECTION"}},
				{Name: "node.npm", Args: []string{"run", "build"}, Priority: 15},
				{Name: "php.laravel.artisan", Args: []string{"storage:link"}},
				{Name: "herd", Args: []string{"link", "--secure"}},
			},
			cleanupSteps: []config.CleanupStep{
				{Name: "herd", Condition: nil},
				{Name: "bash.run", Condition: map[string]interface{}{
					"command":    "echo \"Consider cleaning up database: {{ .DB_DATABASE }}\"",
					"env_exists": "DB_CONNECTION",
				}},
			},
		},
	}
}

func (p *Laravel) Detect(path string) bool {
	artisanPath := filepath.Join(path, "artisan")
	if _, err := os.Stat(artisanPath); err == nil {
		return true
	}

	composerPath := filepath.Join(path, "composer.json")
	data, err := ioutil.ReadFile(composerPath)
	if err != nil {
		return false
	}

	return strings.Contains(string(data), "laravel/framework")
}
