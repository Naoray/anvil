package presets

import (
	"os"
	"path/filepath"

	"github.com/artisanexperiences/arbor/internal/config"
	"github.com/artisanexperiences/arbor/internal/utils"
)

type Laravel struct {
	basePreset
}

func NewLaravel() *Laravel {
	return &Laravel{
		basePreset: basePreset{
			name: "laravel",
			defaultSteps: []config.StepConfig{
				{Name: "php.composer", Args: []string{"install"}, Condition: map[string]interface{}{"file_exists": "composer.lock"}},
				{Name: "php.composer", Args: []string{"update"}, Condition: map[string]interface{}{"not": map[string]interface{}{"file_exists": "composer.lock"}}},
				{Name: "file.copy", From: ".env.example", To: ".env"},
				{Name: "php.laravel", Args: []string{"key:generate", "--show", "--no-interaction"}, StoreAs: "AppKey", Condition: map[string]interface{}{"env_file_missing": "APP_KEY"}},
				{Name: "env.write", Key: "APP_KEY", Value: "{{ .AppKey }}", Condition: map[string]interface{}{"env_file_missing": "APP_KEY"}},
				{Name: "db.create", Condition: map[string]interface{}{"env_file_contains": map[string]interface{}{"file": ".env", "key": "DB_CONNECTION"}}},
				{Name: "env.write", Key: "DB_DATABASE", Value: "{{ .SanitizedSiteName }}_{{ .DbSuffix }}", Condition: map[string]interface{}{"env_file_contains": map[string]interface{}{"file": ".env", "key": "DB_CONNECTION"}}},
				{Name: "node.npm", Args: []string{"ci"}, Condition: map[string]interface{}{"file_exists": "package-lock.json"}},
				{Name: "php.laravel", Args: []string{"migrate:fresh", "--seed", "--no-interaction"}},
				{Name: "node.npm", Args: []string{"run", "build"}, Condition: map[string]interface{}{"file_exists": "package-lock.json"}},
				{Name: "php.laravel", Args: []string{"storage:link", "--no-interaction"}},
				{Name: "herd", Args: []string{"link", "--secure", "{{ .SiteName }}"}},
			},
			cleanupSteps: []config.CleanupStep{
				{Name: "herd", Condition: nil},
				{Name: "db.destroy", Condition: nil},
			},
		},
	}
}

func (p *Laravel) Detect(path string) bool {
	composerPath := filepath.Join(path, "composer.json")
	if _, err := os.Stat(composerPath); err != nil {
		return false
	}

	artisanPath := filepath.Join(path, "artisan")
	if _, err := os.Stat(artisanPath); err != nil {
		return false
	}

	return true
}

func (p *Laravel) Suggest(path string) string {
	env := utils.ReadEnvFile(path, ".env")
	if env["DB_CONNECTION"] != "" {
		return "laravel"
	}
	return ""
}
