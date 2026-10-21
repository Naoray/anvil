package cli

import (
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// exampleFiles loads a synthetic example repository from examples/.
func exampleFiles(t *testing.T, name string) map[string]string {
	t.Helper()
	root := filepath.Join("..", "..", "examples", name)
	files := map[string]string{}
	require.NoError(t, filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		files[filepath.ToSlash(rel)] = string(content)
		return nil
	}))
	return files
}

// fakeToolchain installs logging stand-ins for stack tools. Package managers
// create node_modules with an executable in .bin; swift build creates .build.
func fakeToolchain(t *testing.T, f *agentFixture, tools ...string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("stand-in toolchains are POSIX shell scripts")
	}
	bin := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "tools.log")
	script := `#!/bin/sh
echo "$(basename "$0") $* @ $(pwd)" >> "$ANVIL_TOOL_LOG"
case "$(basename "$0") $1" in
  "pnpm install"|"npm ci"|"npm install"|"bun install"|"yarn install")
    mkdir -p node_modules/.bin
    printf '#!/bin/sh\necho local-tool\n' > node_modules/.bin/local-tool
    chmod +x node_modules/.bin/local-tool ;;
  "swift build") mkdir -p .build ;;
  "uv sync") mkdir -p .venv/bin ;;
esac
`
	for _, tool := range tools {
		require.NoError(t, os.WriteFile(filepath.Join(bin, tool), []byte(script), 0o755))
	}
	// Hermetic PATH: stand-ins plus system directories (git, sh, coreutils),
	// so real toolchains installed elsewhere never leak into the test.
	path := strings.Join([]string{bin, "/usr/bin", "/bin"}, string(os.PathListSeparator))
	f.env = append(f.env, "PATH="+path, "ANVIL_TOOL_LOG="+logPath)
	f.env = subprocessEnvFrom(f.env)
	return logPath
}

// subprocessEnvFrom de-duplicates keys, keeping the last value.
func subprocessEnvFrom(entries []string) []string {
	seen := map[string]int{}
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		key, _, _ := strings.Cut(entry, "=")
		if index, ok := seen[key]; ok {
			out[index] = entry
			continue
		}
		seen[key] = len(out)
		out = append(out, entry)
	}
	return out
}

func readLog(t *testing.T, path string) []string {
	t.Helper()
	content, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	require.NoError(t, err)
	return strings.Split(strings.TrimSpace(string(content)), "\n")
}

func componentStatuses(t *testing.T, envelope map[string]any) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, raw := range field(t, envelope, "data", "components").([]any) {
		out[field(t, raw, "name").(string)] = field(t, raw, "status").(string)
	}
	return out
}

func TestEnsureMixedStackComponents(t *testing.T) {
	f := newAgentFixture(t, "", exampleFiles(t, "mixed-desktop"))
	logPath := fakeToolchain(t, f, "node", "pnpm", "npm", "cargo", "swift")

	envelope, stderr, code := f.runJSON(f.repo, "ensure", "feature/x")
	require.Equal(t, 0, code, stderr)
	wt := f.worktreePath("feature-x")
	statuses := componentStatuses(t, envelope)
	require.Equal(t, "active", statuses["desktop"])
	require.Equal(t, "active", statuses["service"])

	log := strings.Join(readLog(t, logPath), "\n")
	require.Contains(t, log, "pnpm install --frozen-lockfile @ "+filepath.Join(wt, "apps", "desktop"))
	require.Contains(t, log, "cargo fetch @ "+filepath.Join(wt, "apps", "desktop", "src-tauri"))
	require.Contains(t, log, "npm ci @ "+filepath.Join(wt, "service"))
	if runtime.GOOS == "darwin" {
		require.Equal(t, "active", statuses["helper"])
		require.Contains(t, log, "swift package resolve")
		require.Contains(t, log, "swift build")
	} else {
		require.Equal(t, "skipped", statuses["helper"], "Swift helper is macOS-only and never mandatory elsewhere")
		require.NotContains(t, log, "swift")
	}

	before := readLog(t, logPath)
	envelope, _, code = f.runJSON(f.repo, "ensure", "feature-x")
	require.Equal(t, 0, code)
	for _, result := range stepResults(t, envelope) {
		require.Equal(t, "up_to_date", result)
	}
	require.Equal(t, before, readLog(t, logPath), "idempotent: no tool reruns")

	// Lockfile change in one component reinstalls only that component.
	require.NoError(t, os.WriteFile(filepath.Join(wt, "service", "package-lock.json"), []byte(`{"lockfileVersion":3}`), 0o644))
	_, _, code = f.runJSON(f.repo, "ensure", "feature-x")
	require.Equal(t, 0, code)
	after := readLog(t, logPath)
	require.Len(t, after, len(before)+1)
	require.Contains(t, after[len(after)-1], "npm ci @ "+filepath.Join(wt, "service"))
}

func TestEnsureFailsBeforeEffectsWhenRequirementsMissing(t *testing.T) {
	f := newAgentFixture(t, "", exampleFiles(t, "mixed-desktop"))
	logPath := fakeToolchain(t, f, "node", "npm", "cargo") // no pnpm
	envelope, _, code := f.runJSON(f.repo, "ensure", "feature/x")
	require.Equal(t, 6, code)
	require.Equal(t, "prepare_failed", field(t, envelope, "error", "code"))
	require.Contains(t, field(t, envelope, "error", "message"), "pnpm")
	require.Empty(t, readLog(t, logPath), "no component ran")
	require.Equal(t, "skipped", componentStatuses(t, envelope)["desktop"])
}

func TestRunExportsEnvironmentAndPassesExitStatus(t *testing.T) {
	f := newAgentFixture(t, "", exampleFiles(t, "mixed-desktop"))
	fakeToolchain(t, f, "node", "pnpm", "npm", "cargo", "swift")

	// Unprepared: warns but runs; --require-ready refuses.
	_, stderr, code := f.run(f.repo, "ensure", "feature/x", "--skip-prepare")
	require.Equal(t, 0, code, stderr)
	stdout, stderr, code := f.run(f.repo, "run", "feature-x", "--", "sh", "-c", "echo ok")
	require.Equal(t, 0, code, stderr)
	require.Equal(t, "ok\n", stdout)
	require.Contains(t, stderr, "preparation is unprepared")
	_, stderr, code = f.run(f.repo, "run", "feature-x", "--require-ready", "--", "true")
	require.Equal(t, 6, code, stderr)

	_, stderr, code = f.run(f.repo, "ensure", "feature-x")
	require.Equal(t, 0, code, stderr)
	wt := f.worktreePath("feature-x")

	stdout, stderr, code = f.run(f.repo, "run", "feature-x", "--", "sh", "-c", `printf '%s|%s|%s|%s|%s' "$PWD" "$ANVIL_WORKTREE_PATH" "$DESKTOP_INSTANCE" "${DB_DATABASE-unset}" "$RUST_LOG"`)
	require.Equal(t, 0, code, stderr)
	require.NotContains(t, stderr, "warning")
	parts := strings.Split(stdout, "|")
	require.Equal(t, wt, parts[0])
	require.Equal(t, wt, parts[1])
	require.Regexp(t, `^demo-feature-x-[0-9a-f]{6}$`, parts[2])
	require.Equal(t, "unset", parts[3], "run never sets exec's DB_DATABASE")
	require.Equal(t, "info", parts[4])

	stdout, stderr, code = f.run(f.repo, "run", "feature-x", "--component", "desktop", "--", "sh", "-c", `printf '%s|%s|%s' "$PWD" "$ANVIL_COMPONENT" "$(local-tool)"`)
	require.Equal(t, 0, code, stderr)
	require.Equal(t, filepath.Join(wt, "apps", "desktop")+"|desktop|local-tool", stdout)

	// From a subdirectory without WORKTREE: current directory is kept.
	stdout, _, code = f.run(filepath.Join(wt, "service"), "run", "--", "sh", "-c", `printf '%s' "$PWD"`)
	require.Equal(t, 0, code)
	require.Equal(t, filepath.Join(wt, "service"), stdout)

	_, _, code = f.run(f.repo, "run", "feature-x", "--", "sh", "-c", "exit 7")
	require.Equal(t, 7, code)
	_, _, code = f.run(f.repo, "run", "feature-x", "--", "definitely-not-a-command")
	require.Equal(t, 127, code)
	_, stderr, code = f.run(f.repo, "run", "feature-x", "echo")
	require.Equal(t, 2, code)
	require.Contains(t, stderr, "--")
	_, _, code = f.run(f.repo, "run", "nope", "--", "true")
	require.Equal(t, 3, code)
	_, _, code = f.run(f.repo, "run", "feature-x", "--component", "nope", "--", "true")
	require.Equal(t, 3, code)
}

func TestEnsureRustPythonExample(t *testing.T) {
	f := newAgentFixture(t, "", exampleFiles(t, "gaze-like"))
	logPath := fakeToolchain(t, f, "cargo", "uv")
	envelope, stderr, code := f.runJSON(f.repo, "ensure", "feature/eval")
	require.Equal(t, 0, code, stderr)
	require.Equal(t, map[string]string{"detector": "active", "eval": "active"}, componentStatuses(t, envelope))
	wt := f.worktreePath("feature-eval")
	log := strings.Join(readLog(t, logPath), "\n")
	require.Contains(t, log, "cargo fetch --locked @ "+filepath.Join(wt, "crates", "detector"))
	require.Contains(t, log, "uv sync --frozen @ "+filepath.Join(wt, "eval"))

	stdout, stderr, code := f.run(f.repo, "run", "feature-eval", "--component", "eval", "--", "sh", "-c", `printf '%s|%s' "$VIRTUAL_ENV" "${PATH%%:*}"`)
	require.Equal(t, 0, code, stderr)
	venv := filepath.Join(wt, "eval", ".venv")
	require.Equal(t, venv+"|"+filepath.Join(venv, "bin"), stdout)
}

func TestRunResolvesComponentExecutableOnExportedPath(t *testing.T) {
	f := newAgentFixture(t, "components:\n  - name: tool\n    path: tool\n    uses: [node]\n", map[string]string{"tool/package.json": "{}"})
	fakeToolchain(t, f, "node", "npm")
	_, stderr, code := f.runJSON(f.repo, "ensure", "feature/path")
	require.Equal(t, 0, code, stderr)
	wt := f.worktreePath("feature-path")
	stdout, stderr, code := f.run(f.repo, "run", "feature-path", "--component", "tool", "--", "local-tool")
	require.Equal(t, 0, code, stderr)
	require.Equal(t, "local-tool\n", stdout)

	global := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(global, "local-tool"), []byte("#!/bin/sh\necho global\n"), 0o755))
	f.env = subprocessEnvFrom(append(f.env, "PATH="+global+string(os.PathListSeparator)+"/usr/bin:/bin"))
	stdout, stderr, code = f.run(f.repo, "run", "feature-path", "--component", "tool", "--", "local-tool")
	require.Equal(t, 0, code, stderr)
	require.Equal(t, "local-tool\n", stdout)

	require.NoError(t, os.WriteFile(filepath.Join(wt, "anvil.yaml"), []byte("components:\n  - name: tool\n    path: tool\n    env:\n      PATH: node_modules/.bin\n"), 0o644))
	stdout, stderr, code = f.run(f.repo, "run", "feature-path", "--component", "tool", "--", "local-tool")
	require.Equal(t, 0, code, stderr)
	require.Equal(t, "local-tool\n", stdout)
}
