package config

import (
	"bytes"
	"errors"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// WarmConfig declares opt-in relaxations of warm dependency reuse
// (`anvil work --warm-from`). The zero value keeps the conservative default.
// Observed provenance lives in .anvil.local, never here.
type WarmConfig struct {
	NodeModules WarmNodeModulesConfig `yaml:"node_modules"`
}

// WarmNodeModulesConfig trusts npm lifecycle scripts and native addons for
// node_modules reuse.
type WarmNodeModulesConfig struct {
	// TrustInstallScripts allows reusing trees whose root or dependencies
	// have install/postinstall/prepare scripts or native (.node) addons,
	// provided every script identity still matches.
	TrustInstallScripts bool `yaml:"trust_install_scripts"`
	// Rebuild is an optional shell command run in the destination worktree
	// after a warm hit (for example "npm rebuild"). It also permits files
	// that embed the source worktree path, which the rebuild must replace,
	// and is required when the root package.json has lifecycle scripts:
	// warm hits never re-run them, so the rebuild must reproduce them.
	Rebuild string `yaml:"rebuild"`
}

// decodeWarm strictly decodes the warm section: unknown keys and wrong
// types are configuration errors rather than silently ignored opt-ins.
func decodeWarm(content []byte) (WarmConfig, error) {
	var document struct {
		Warm yaml.Node `yaml:"warm"`
	}
	if err := yaml.Unmarshal(content, &document); err != nil {
		return WarmConfig{}, err
	}
	var warm WarmConfig
	if document.Warm.Kind == 0 {
		return warm, nil
	}
	section, err := yaml.Marshal(&document.Warm)
	if err != nil {
		return warm, err
	}
	decoder := yaml.NewDecoder(bytes.NewReader(section))
	decoder.KnownFields(true)
	if err := decoder.Decode(&warm); err != nil {
		return WarmConfig{}, err
	}
	return warm, nil
}

// ValidateWarm reports inconsistent warm declarations.
func (c *Config) ValidateWarm() error {
	var problems []error
	nodeModules := c.Warm.NodeModules
	if nodeModules.Rebuild != "" {
		if strings.TrimSpace(nodeModules.Rebuild) == "" {
			problems = append(problems, fmt.Errorf("warm.node_modules.rebuild: must not be blank"))
		}
		if !nodeModules.TrustInstallScripts {
			problems = append(problems, fmt.Errorf("warm.node_modules.rebuild requires trust_install_scripts: true"))
		}
	}
	return errors.Join(problems...)
}
