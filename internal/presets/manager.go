package presets

import (
	"fmt"
	"io"
	"strings"

	"github.com/naoray/anvil/internal/config"
	"github.com/naoray/anvil/internal/ui"
)

type Manager struct {
	presets     map[string]Preset
	presetOrder []Preset
}

func NewManager(siteDrivers ...config.SiteDriver) *Manager {
	m := &Manager{
		presets:     make(map[string]Preset),
		presetOrder: make([]Preset, 0),
	}
	for _, p := range builtInPresets(resolveSiteDriver(siteDrivers)) {
		m.Register(p)
	}
	return m
}

func (m *Manager) Register(preset Preset) {
	if preset == nil {
		panic("cannot register a nil preset")
	}

	name := preset.Name()
	if _, exists := m.presets[name]; exists {
		panic(fmt.Sprintf("preset %q is already registered", name))
	}

	m.presets[name] = preset
	m.presetOrder = append(m.presetOrder, preset)
}

func (m *Manager) Get(name string) (Preset, bool) {
	preset, ok := m.presets[name]
	return preset, ok
}

// builtInPresets lists all available presets in priority order (most specific first).
// IMPORTANT: Order matters! More specific presets (e.g., Laravel) must come before
// generic ones (e.g., PHP) to ensure correct detection. When adding new presets,
// place them according to specificity (e.g., Next.js before React before JavaScript).
func builtInPresets(siteDriver config.SiteDriver) []Preset {
	return []Preset{
		NewLaravelSharedDB(siteDriver), // shared-database Laravel app (config-only, no auto-detect)
		NewLaravel(siteDriver),
		NewPHP(),
	}
}

func (m *Manager) Detect(path string) string {
	// Iterate in priority order (most specific first) using the ordered slice
	// instead of the map to ensure deterministic detection.
	// builtInPresets is ordered from most specific (Laravel) to least specific (PHP).
	for _, preset := range m.presetOrder {
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

// Resolve selects one preset definition for a run. Explicit selection wins
// over project configuration, which wins over filesystem detection.
func (m *Manager) Resolve(explicit, configured, path string) ResolvedPreset {
	name := explicit
	if name == "" {
		name = configured
	}
	if name == "" {
		name = m.Detect(path)
	}

	resolved := ResolvedPreset{name: name}
	if preset, ok := m.Get(name); ok {
		resolved.defaultSteps = preset.DefaultSteps()
		resolved.cleanupSteps = preset.CleanupSteps()
	}
	return resolved
}

func (m *Manager) Available() []string {
	names := make([]string, 0, len(m.presetOrder))
	for _, preset := range m.presetOrder {
		names = append(names, preset.Name())
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
