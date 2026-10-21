package capabilities

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// yarnOutputs follows the selected Berry linker rather than assuming node_modules.
func yarnOutputs(dir string) ([]string, error) {
	data, err := os.ReadFile(filepath.Join(dir, ".yarnrc.yml"))
	if err != nil {
		return nil, fmt.Errorf("reading Yarn configuration: %w", err)
	}
	var settings struct {
		NodeLinker string `yaml:"nodeLinker"`
		ESMLoader  bool   `yaml:"pnpEnableEsmLoader"`
		Inline     *bool  `yaml:"pnpEnableInlining"`
	}
	if err := yaml.Unmarshal(data, &settings); err != nil {
		return nil, fmt.Errorf("parsing Yarn configuration: %w", err)
	}
	linker := os.ExpandEnv(settings.NodeLinker)
	if override := os.Getenv("YARN_NODE_LINKER"); override != "" {
		linker = override
	}
	switch linker {
	case "node-modules", "pnpm":
		return []string{"node_modules"}, nil
	case "", "pnp":
		outputs := []string{".pnp.cjs"}
		if settings.ESMLoader {
			outputs = append(outputs, ".pnp.loader.mjs")
		}
		if settings.Inline != nil && !*settings.Inline {
			outputs = append(outputs, ".pnp.data.json")
		}
		return outputs, nil
	default:
		return nil, fmt.Errorf("unsupported Yarn nodeLinker %q", linker)
	}
}
