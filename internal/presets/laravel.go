package presets

import (
	"os"
	"path/filepath"

	"github.com/naoray/anvil/internal/config"
	"github.com/naoray/anvil/internal/utils"
)

type Laravel struct {
	basePreset
}

func NewLaravel() *Laravel {
	return &Laravel{
		basePreset: basePreset{
			name: "laravel",
			defaultSteps: []config.StepConfig{
				{Name: "php.composer", Args: []string{"install"}, ConditionHolder: config.ConditionHolder{Condition: map[string]any{"file_exists": "composer.lock"}}},
				{Name: "php.composer", Args: []string{"update"}, ConditionHolder: config.ConditionHolder{Condition: map[string]any{"not": map[string]any{"file_exists": "composer.lock"}}}},
				{Name: config.StepFileCopy, From: ".env.example", To: ".env"},
				{Name: "php.laravel", Args: []string{"key:generate", "--show", "--no-interaction", "--no-ansi"}, StoreAs: "AppKey", ConditionHolder: config.ConditionHolder{Condition: map[string]any{"env_file_missing": "APP_KEY"}}},
				{Name: config.StepEnvWrite, Key: "APP_KEY", Value: "{{ .AppKey }}", ConditionHolder: config.ConditionHolder{Condition: map[string]any{"env_file_missing": "APP_KEY"}}},
				{Name: config.StepDbCreate, ConditionHolder: config.ConditionHolder{Condition: map[string]any{"env_file_contains": map[string]any{"file": ".env", "key": "DB_CONNECTION"}}}},
				{Name: config.StepEnvWrite, Key: "DB_DATABASE", Value: "{{ .DatabaseName }}", ConditionHolder: config.ConditionHolder{Condition: map[string]any{"env_file_contains": map[string]any{"file": ".env", "key": "DB_CONNECTION"}}}},
				{Name: "node.npm", Args: []string{"ci"}, ConditionHolder: config.ConditionHolder{Condition: map[string]any{"file_exists": "package-lock.json"}}},
				{Name: "php.laravel", Args: []string{"migrate:fresh", "--seed", "--no-interaction"}},
				{Name: "node.npm", Args: []string{"run", "build"}, ConditionHolder: config.ConditionHolder{Condition: map[string]any{"file_exists": "package-lock.json"}}},
				{Name: "php.laravel", Args: []string{"storage:link", "--no-interaction"}},
				{Name: "herd", Args: []string{"link", "--secure", "{{ .SiteName }}"}},
			},
			cleanupSteps: []config.CleanupStep{
				{Name: "herd"},
				{Name: config.StepDbDestroy},
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
