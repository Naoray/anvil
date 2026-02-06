package presets

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/naoray/anvil/internal/scaffold"
	"github.com/naoray/anvil/internal/ui"
)

type Manager struct {
	presets map[string]Preset
}

func NewManager() *Manager {
	m := &Manager{
		presets: make(map[string]Preset),
	}
	for _, p := range builtInPresets {
		m.Register(p)
	}
	return m
}

func (m *Manager) Register(preset Preset) {
	m.presets[preset.Name()] = preset
}

func (m *Manager) Get(name string) (Preset, bool) {
	preset, ok := m.presets[name]
	return preset, ok
}

// builtInPresets lists all available presets in priority order (most specific first).
// IMPORTANT: Order matters! More specific presets (e.g., Laravel) must come before
// generic ones (e.g., PHP) to ensure correct detection. When adding new presets,
// place them according to specificity (e.g., Next.js before React before JavaScript).
var builtInPresets = []Preset{
	NewTars(), // TARS: shared-database Laravel app (must come before Laravel)
	NewLaravel(),
	NewPHP(),
}

// RegisterAllWithScaffold registers all built-in presets with a scaffold manager
func RegisterAllWithScaffold(m *scaffold.ScaffoldManager) {
	for _, p := range builtInPresets {
		m.RegisterPreset(p)
	}
}

func (m *Manager) Detect(path string) string {
	// Iterate in priority order (most specific first) using the ordered slice
	// instead of the map to ensure deterministic detection.
	// builtInPresets is ordered from most specific (Laravel) to least specific (PHP).
	for _, preset := range builtInPresets {
		if preset.Detect(path) {
			return preset.Name()
		}
	}
	return ""
}

func (m *Manager) Suggest(path string) string {
	detected := m.Detect(path)
	if detected != "" {
		return detected
	}
	return "php"
}

func (m *Manager) Available() []string {
	names := make([]string, 0, len(m.presets))
	for name := range m.presets {
		names = append(names, name)
	}
	return names
}

func PromptForPreset(m *Manager, suggested string) (string, error) {
	available := m.Available()

	fmt.Printf("Detected preset: %s\n", suggested)
	fmt.Print("Select preset (or press Enter to accept): ")

	var choice string
	_, err := fmt.Scanln(&choice)
	if err != nil && err != io.EOF && !strings.Contains(err.Error(), "unexpected newline") {
		return "", ui.NormalizeAbort(err)
	}
	if err == io.EOF {
		return "", ui.ErrUserAborted
	}

	choice = strings.TrimSpace(choice)
	if choice == "" {
		return suggested, nil
	}

	for _, name := range available {
		if name == choice {
			return choice, nil
		}
	}

	fmt.Printf("Unknown preset: %s. Using suggested: %s\n", choice, suggested)
	return suggested, nil
}

func DirectoryExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func EnsureDirectory(path string) error {
	if !DirectoryExists(path) {
		return os.MkdirAll(path, 0755)
	}
	return nil
}

func JoinPath(base string, parts ...string) string {
	result := base
	for _, part := range parts {
		result = filepath.Join(result, part)
	}
	return result
}
