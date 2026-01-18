package presets

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"github.com/michaeldyrynda/arbor/internal/config"
	"github.com/michaeldyrynda/arbor/internal/utils"
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
				{Name: "file.copy", From: ".env.example", To: ".env", Priority: 5},
				{Name: "database.create", Condition: map[string]interface{}{"env_file_contains": map[string]interface{}{"file": ".env", "key": "DB_CONNECTION"}}},
				{Name: "node.npm", Args: []string{"ci"}, Condition: map[string]interface{}{"file_exists": "package-lock.json"}},
				{Name: "php.laravel.artisan", Args: []string{"key:generate", "--no-interaction"}, Condition: map[string]interface{}{"env_file_missing": "APP_KEY"}},
				{Name: "php.laravel.artisan", Args: []string{"migrate:fresh", "--seed", "--no-interaction"}},
				{Name: "node.npm", Args: []string{"run", "build"}, Priority: 15, Condition: map[string]interface{}{"file_exists": "package-lock.json"}},
				{Name: "php.laravel.artisan", Args: []string{"storage:link", "--no-interaction"}},
				{Name: "herd", Args: []string{"link", "--secure", "{{ .RepoName }}"}},
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

func (p *Laravel) Suggest(path string) string {
	env := utils.ReadEnvFile(path, ".env")
	if env["DB_CONNECTION"] != "" {
		return "laravel"
	}
	return ""
}
