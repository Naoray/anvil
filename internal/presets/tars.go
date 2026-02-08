package presets

import (
	"github.com/naoray/anvil/internal/config"
)

// LaravelSharedDB is a preset for Laravel projects where all worktrees
// share a single database instead of each having their own.
// Useful for projects where parallel agents/worktrees need access to the same data.
type LaravelSharedDB struct {
	basePreset
}

func NewLaravelSharedDB() *LaravelSharedDB {
	return &LaravelSharedDB{
		basePreset: basePreset{
			name: "laravel-shared-db",
			defaultSteps: []config.StepConfig{
				{Name: "php.composer", Args: []string{"install"}, Condition: map[string]interface{}{"file_exists": "composer.lock"}},
				{Name: "php.composer", Args: []string{"update"}, Condition: map[string]interface{}{"not": map[string]interface{}{"file_exists": "composer.lock"}}},
				{Name: "file.copy", From: ".env.example", To: ".env"},
				{Name: "php.laravel.artisan", Args: []string{"key:generate", "--no-interaction"}, Condition: map[string]interface{}{"env_file_missing": "APP_KEY"}},
				// NO db.create - shared database across all worktrees
				// NO env.write for DB_DATABASE - preserve the shared database name
				{Name: "node.npm", Args: []string{"ci"}, Condition: map[string]interface{}{"file_exists": "package-lock.json"}},
				// NO migrate:fresh - database already exists with shared data
				{Name: "node.npm", Args: []string{"run", "build"}, Condition: map[string]interface{}{"file_exists": "package-lock.json"}},
				{Name: "php.laravel.artisan", Args: []string{"storage:link", "--no-interaction"}},
				{Name: "herd", Args: []string{"link", "--secure", "{{ .SiteName }}"}},
			},
			cleanupSteps: []config.CleanupStep{
				{Name: "herd", Condition: nil},
				// NO db.destroy - don't delete the shared database
			},
		},
	}
}

// Detect always returns false — this preset is activated only via anvil.yaml config.
func (p *LaravelSharedDB) Detect(path string) bool {
	return false
}

func (p *LaravelSharedDB) Suggest(path string) string {
	return ""
}
