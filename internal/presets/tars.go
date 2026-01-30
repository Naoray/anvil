package presets

import (
	"os"
	"path/filepath"

	"github.com/michaeldyrynda/arbor/internal/config"
)

// Tars is a preset for TARS (memory system for AI assistants).
// Unlike standard Laravel projects, TARS worktrees share a single database
// to maintain unified memory across all parallel agents.
type Tars struct {
	basePreset
}

func NewTars() *Tars {
	return &Tars{
		basePreset: basePreset{
			name: "tars",
			defaultSteps: []config.StepConfig{
				{Name: "php.composer", Args: []string{"install"}, Condition: map[string]interface{}{"file_exists": "composer.lock"}},
				{Name: "php.composer", Args: []string{"update"}, Condition: map[string]interface{}{"not": map[string]interface{}{"file_exists": "composer.lock"}}},
				{Name: "file.copy", From: ".env.example", To: ".env"},
				{Name: "php.laravel.artisan", Args: []string{"key:generate", "--no-interaction"}, Condition: map[string]interface{}{"env_file_missing": "APP_KEY"}},
				// NO db.create - TARS shares a single database across all worktrees
				// NO env.write for DB_DATABASE - preserve the centralized database path
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

func (p *Tars) Detect(path string) bool {
	// Must be a Laravel app
	artisanPath := filepath.Join(path, "artisan")
	if _, err := os.Stat(artisanPath); err != nil {
		return false
	}

	// Must have TARS config file
	tarsConfigPath := filepath.Join(path, "config", "tars.php")
	if _, err := os.Stat(tarsConfigPath); err != nil {
		return false
	}

	return true
}

func (p *Tars) Suggest(path string) string {
	if p.Detect(path) {
		return "tars"
	}
	return ""
}
