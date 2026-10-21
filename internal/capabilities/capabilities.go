// Package capabilities turns declarative components into ordinary scaffold
// steps. Each stack capability (node, rust, python, go, php, tauri, swift)
// contributes required commands, lockfile-backed default preparation and
// environment; Anvil never reimplements a package manager or build system.
package capabilities

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Contribution is what one capability adds to a component.
type Contribution struct {
	Requires []string
	Prepare  []Command
	// Env values may reference ${ANVIL_COMPONENT_DIR}.
	Env map[string]string
	// PathPrepend lists component-relative directories to put first on PATH.
	PathPrepend []string
	// Platforms restricts the component when it declares none itself.
	Platforms []string
}

// Command is a default preparation command, relative to the component (or
// to Dir inside it).
type Command struct {
	Run     string
	Dir     string
	Inputs  []string
	Outputs []string
}

func exists(dir, name string) bool {
	_, err := os.Stat(filepath.Join(dir, name))
	return err == nil
}

// Contribute returns the contribution of capability name for a component
// rooted at dir. Detection is lockfile-based and conservative: without a
// recognized manifest no default command is added.
func Contribute(name, dir, goos string) Contribution {
	switch name {
	case "node":
		return node(dir)
	case "rust":
		return rust(dir, "")
	case "python":
		return python(dir, goos)
	case "go":
		return golang(dir)
	case "php":
		return php(dir)
	case "tauri":
		return tauri(dir)
	case "swift":
		return swift(dir)
	default:
		return Contribution{}
	}
}

func node(dir string) Contribution {
	c := Contribution{PathPrepend: []string{filepath.Join("node_modules", ".bin")}}
	install := func(manager, run string, inputs, outputs []string) {
		c.Requires = append(c.Requires, manager)
		c.Prepare = append(c.Prepare, Command{
			Run:     run,
			Inputs:  append([]string{"package.json"}, inputs...),
			Outputs: outputs,
		})
	}
	nodeModules := []string{"node_modules"}
	switch {
	case exists(dir, "pnpm-lock.yaml"):
		install("pnpm", "pnpm install --frozen-lockfile", []string{"pnpm-lock.yaml"}, nodeModules)
	case exists(dir, "bun.lock"):
		install("bun", "bun install --frozen-lockfile", []string{"bun.lock"}, nodeModules)
	case exists(dir, "bun.lockb"):
		install("bun", "bun install --frozen-lockfile", []string{"bun.lockb"}, nodeModules)
	case exists(dir, "yarn.lock") && exists(dir, ".yarnrc.yml"):
		install("yarn", "yarn install --immutable", []string{"yarn.lock", ".yarnrc.yml"}, yarnBerryOutputs(dir))
	case exists(dir, "yarn.lock"):
		install("yarn", "yarn install --frozen-lockfile", []string{"yarn.lock"}, nodeModules)
	case exists(dir, "package-lock.json"):
		install("npm", "npm ci", []string{"package-lock.json"}, nodeModules)
	case exists(dir, "package.json"):
		// No lockfile: installs are not reproducible, so only record the manifest.
		c.Requires = append(c.Requires, "npm")
		c.Prepare = append(c.Prepare, Command{Run: "npm install", Inputs: []string{"package.json"}, Outputs: []string{"node_modules"}})
	}
	if len(c.Requires) > 0 && c.Requires[0] != "bun" {
		c.Requires = append([]string{"node"}, c.Requires...)
	}
	return c
}

// yarnBerryOutputs follows .yarnrc.yml's nodeLinker: Plug'n'Play (the
// default) writes .pnp.cjs instead of node_modules. An unreadable file keeps
// the default; yarn itself reports the configuration error.
func yarnBerryOutputs(dir string) []string {
	var settings struct {
		NodeLinker string `yaml:"nodeLinker"`
	}
	if content, err := os.ReadFile(filepath.Join(dir, ".yarnrc.yml")); err == nil {
		if err := yaml.Unmarshal(content, &settings); err != nil {
			settings.NodeLinker = ""
		}
	}
	switch settings.NodeLinker {
	case "node-modules", "pnpm":
		return []string{"node_modules"}
	default:
		return []string{".pnp.cjs"}
	}
}

func rust(dir, sub string) Contribution {
	root := filepath.Join(dir, sub)
	if !exists(root, "Cargo.toml") {
		return Contribution{Requires: []string{"cargo"}}
	}
	inputs := []string{"Cargo.toml", "Cargo.lock", "rust-toolchain", "rust-toolchain.toml"}
	run := "cargo fetch"
	if exists(root, "Cargo.lock") {
		run = "cargo fetch --locked"
	}
	return Contribution{
		Requires: []string{"cargo"},
		Prepare:  []Command{{Run: run, Dir: sub, Inputs: inputs}},
	}
}

func python(dir, goos string) Contribution {
	// Native Windows virtualenvs keep executables in Scripts.
	bin, interpreter := filepath.Join(".venv", "bin"), ".venv/bin/python"
	if goos == "windows" {
		bin, interpreter = filepath.Join(".venv", "Scripts"), ".venv/Scripts/python.exe"
	}
	venv := Contribution{
		Env:         map[string]string{"VIRTUAL_ENV": filepath.Join("${ANVIL_COMPONENT_DIR}", ".venv")},
		PathPrepend: []string{bin},
	}
	switch {
	case exists(dir, "uv.lock"):
		venv.Requires = []string{"uv"}
		venv.Prepare = []Command{{Run: "uv sync --frozen", Inputs: []string{"pyproject.toml", "uv.lock", ".python-version"}, Outputs: []string{".venv"}}}
	case exists(dir, "poetry.lock"):
		venv.Requires = []string{"poetry"}
		venv.Env["POETRY_VIRTUALENVS_IN_PROJECT"] = "true"
		venv.Prepare = []Command{{Run: "poetry install --no-interaction", Inputs: []string{"pyproject.toml", "poetry.lock"}, Outputs: []string{".venv"}}}
	case exists(dir, "requirements.txt"):
		venv.Requires = []string{"python3"}
		venv.Prepare = []Command{{
			Run:     "python3 -m venv .venv && " + shellQuote(interpreter) + " -m pip install --disable-pip-version-check -r requirements.txt",
			Inputs:  []string{"requirements.txt"},
			Outputs: []string{".venv"},
		}}
	default:
		venv.Requires = []string{"python3"}
	}
	return venv
}

func golang(dir string) Contribution {
	c := Contribution{Requires: []string{"go"}}
	if exists(dir, "go.mod") {
		c.Prepare = []Command{{Run: "go mod download", Inputs: []string{"go.mod", "go.sum"}}}
	}
	return c
}

func php(dir string) Contribution {
	c := Contribution{Requires: []string{"php", "composer"}, PathPrepend: []string{filepath.Join("vendor", "bin")}}
	if exists(dir, "composer.lock") {
		c.Prepare = []Command{{Run: "composer install --no-interaction", Inputs: []string{"composer.json", "composer.lock"}, Outputs: []string{"vendor"}}}
	}
	return c
}

// tauri prepares the Rust side in src-tauri when present; the frontend is a
// separate `node` capability so each can be cached independently.
func tauri(dir string) Contribution {
	if exists(dir, filepath.Join("src-tauri", "Cargo.toml")) {
		return rust(dir, "src-tauri")
	}
	return Contribution{Requires: []string{"cargo"}}
}

// swift defaults to macOS so Swift helpers never become a mandatory
// dependency on other platforms; components may override platforms.
func swift(dir string) Contribution {
	c := Contribution{Requires: []string{"swift"}, Platforms: []string{"darwin"}}
	if exists(dir, "Package.swift") {
		c.Prepare = []Command{{Run: "swift package resolve", Inputs: []string{"Package.swift", "Package.resolved"}}}
	}
	return c
}
