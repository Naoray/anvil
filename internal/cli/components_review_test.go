package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func readFile(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(content)
}

func TestRunResolvesCommandAgainstChildPath(t *testing.T) {
	f := newAgentFixture(t, "", exampleFiles(t, "mixed-desktop"))
	fakeToolchain(t, f, "node", "pnpm", "npm", "cargo", "swift")
	// A same-named global executable must lose to the component-local one.
	global := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(global, "local-tool"), []byte("#!/bin/sh\necho global-tool\n"), 0o755))
	for i, entry := range f.env {
		if strings.HasPrefix(entry, "PATH=") {
			f.env[i] = entry + string(os.PathListSeparator) + global
		}
	}
	_, stderr, code := f.run(f.repo, "ensure", "feature/x")
	require.Equal(t, 0, code, stderr)

	stdout, stderr, code := f.run(f.repo, "run", "feature-x", "--component", "desktop", "--", "local-tool")
	require.Equal(t, 0, code, stderr)
	require.Equal(t, "local-tool\n", stdout)

	// Without the component, the global one is found on the exported PATH.
	stdout, stderr, code = f.run(f.repo, "run", "feature-x", "--", "local-tool")
	require.Equal(t, 0, code, stderr)
	require.Equal(t, "global-tool\n", stdout)

	// Relative paths resolve against the child's directory.
	stdout, stderr, code = f.run(f.repo, "run", "feature-x", "--component", "desktop", "--", "./node_modules/.bin/local-tool")
	require.Equal(t, 0, code, stderr)
	require.Equal(t, "local-tool\n", stdout)
}

const optInComponentsYAML = `components:
  - name: app
    path: app
    prepare:
      - run: echo app >> "$ANVIL_TOOL_LOG"
  - name: extras
    path: extras
    opt_in: true
    requires: [extras-tool]
    prepare:
      - run: echo extras >> "$ANVIL_TOOL_LOG" && touch built
        outputs: [built]
`

func TestReviewRunRequireReadyChecksSelectedComponent(t *testing.T) {
	f := newAgentFixture(t, optInComponentsYAML, map[string]string{"app/.keep": "", "extras/.keep": ""})
	logPath := fakeToolchain(t, f, "extras-tool")
	_, stderr, code := f.run(f.repo, "ensure", "feature/x")
	require.Equal(t, 0, code, stderr)
	require.Equal(t, []string{"app"}, readLog(t, logPath))

	stdout, stderr, code := f.run(f.repo, "run", "feature-x", "--component", "extras", "--require-ready", "--", "echo", "ran")
	require.Equal(t, 6, code, stderr)
	require.Empty(t, stdout, "no child execution")
	require.Contains(t, stderr, "extras")

	// The worktree-wide gate still passes for the root and prepared components.
	_, stderr, code = f.run(f.repo, "run", "feature-x", "--component", "app", "--require-ready", "--", "true")
	require.Equal(t, 0, code, stderr)

	_, stderr, code = f.run(f.repo, "ensure", "feature-x", "--component", "extras")
	require.Equal(t, 0, code, stderr)
	stdout, stderr, code = f.run(f.repo, "run", "feature-x", "--component", "extras", "--require-ready", "--", "echo", "ran")
	require.Equal(t, 0, code, stderr)
	require.Equal(t, "ran\n", stdout)

	// A deleted output makes the component stale again.
	wt := f.worktreePath("feature-x")
	require.NoError(t, os.Remove(filepath.Join(wt, "extras", "built")))
	_, _, code = f.run(f.repo, "run", "feature-x", "--component", "extras", "--require-ready", "--", "true")
	require.Equal(t, 6, code)
	_, stderr, code = f.run(f.repo, "ensure", "feature-x", "--component", "extras")
	require.Equal(t, 0, code, stderr)

	// A newly missing requirement refuses.
	for i, entry := range f.env {
		if strings.HasPrefix(entry, "PATH=") {
			f.env[i] = "PATH=/usr/bin:/bin"
		}
	}
	stdout, stderr, code = f.run(f.repo, "run", "feature-x", "--component", "extras", "--require-ready", "--", "echo", "ran")
	require.Equal(t, 6, code, stderr)
	require.Empty(t, stdout)
	require.Contains(t, stderr, "extras-tool")
}

func TestEnsureRerunsWhenDeclaredEnvChanges(t *testing.T) {
	yaml := `env:
  MODE: one
components:
  - name: app
    path: app
    prepare:
      - run: printf '%s' "$MODE" > prepared.txt
        outputs: [prepared.txt]
`
	f := newAgentFixture(t, yaml, map[string]string{"app/.keep": ""})
	fakeToolchain(t, f)
	_, stderr, code := f.run(f.repo, "ensure", "feature/x")
	require.Equal(t, 0, code, stderr)
	wt := f.worktreePath("feature-x")
	require.Equal(t, "one", readFile(t, filepath.Join(wt, "app", "prepared.txt")))

	envelope, stderr, code := f.runJSON(f.repo, "ensure", "feature-x")
	require.Equal(t, 0, code, stderr)
	require.Equal(t, []string{"up_to_date"}, stepResults(t, envelope))

	require.NoError(t, os.WriteFile(filepath.Join(wt, "anvil.yaml"), []byte(strings.Replace(yaml, "MODE: one", "MODE: zz-declared-two-zz", 1)), 0o644))
	envelope, stderr, code = f.runJSON(f.repo, "ensure", "feature-x")
	require.Equal(t, 0, code, stderr)
	require.Equal(t, []string{"ran"}, stepResults(t, envelope))
	require.Equal(t, "zz-declared-two-zz", readFile(t, filepath.Join(wt, "app", "prepared.txt")))
	// A distinctive value: generated names (db_suffix words) can contain short words like "two".
	require.NotContains(t, readFile(t, filepath.Join(wt, ".anvil.local")), "zz-declared-two-zz", "only a digest of the environment is stored")
}

func TestComponentEnvCanReferenceComponentVariables(t *testing.T) {
	yaml := `components:
  - name: app
    path: app
    env:
      OBSERVED: ${ANVIL_COMPONENT_DIR}/cache
      WHO: ${ANVIL_COMPONENT}
    prepare:
      - run: printf '%s|%s|%s' "$OBSERVED" "$WHO" "$CMD_OBSERVED" > observed.txt
        env:
          CMD_OBSERVED: ${ANVIL_COMPONENT_DIR}/cmd
`
	f := newAgentFixture(t, yaml, map[string]string{"app/.keep": ""})
	fakeToolchain(t, f)
	_, stderr, code := f.run(f.repo, "ensure", "feature/x")
	require.Equal(t, 0, code, stderr)
	dir := filepath.Join(f.worktreePath("feature-x"), "app")
	require.Equal(t, dir+"/cache|app|"+dir+"/cmd", readFile(t, filepath.Join(dir, "observed.txt")))

	stdout, stderr, code := f.run(f.repo, "run", "feature-x", "--component", "app", "--", "sh", "-c", `printf '%s|%s' "$OBSERVED" "$WHO"`)
	require.Equal(t, 0, code, stderr)
	require.Equal(t, dir+"/cache|app", stdout)
}

func TestEnsureValidatesConditionsOfSkippedComponents(t *testing.T) {
	cases := map[string]string{
		"opt-in component": `  - name: extra
    path: app
    opt_in: true
    when: {bogus_condition: x}
`,
		"unsupported platform": `  - name: extra
    path: app
    platforms: [windows]
    when: {bogus_condition: x}
`,
		"missing requirement with on_missing skip": `  - name: extra
    path: app
    requires: [never-installed-tool]
    on_missing: skip
    prepare:
      - run: "true"
        when: {bogus_condition: x}
`,
		"skipped component with conflicting conditions": `  - name: extra
    path: app
    opt_in: true
    when: {os: linux}
    prepare:
      - run: "true"
        platforms: [linux]
`,
	}
	for name, extra := range cases {
		t.Run(name, func(t *testing.T) {
			yaml := "components:\n  - name: app\n    path: app\n    prepare:\n      - run: echo app >> \"$ANVIL_TOOL_LOG\"\n" + extra
			f := newAgentFixture(t, yaml, map[string]string{"app/.keep": ""})
			logPath := fakeToolchain(t, f)
			envelope, stderr, code := f.runJSON(f.repo, "ensure", "feature/x")
			require.Equal(t, "config_invalid", field(t, envelope, "error", "code"), stderr)
			require.NotEqual(t, 0, code)
			require.Empty(t, readLog(t, logPath), "no command ran")
			_, err := os.Stat(filepath.Join(f.worktreePath("feature-x"), ".anvil.local"))
			require.True(t, os.IsNotExist(err), "no identity or state was written")
		})
	}
}

func TestEnsureYarnPnPIsIdempotent(t *testing.T) {
	yaml := "components:\n  - name: web\n    path: web\n    uses: [node]\n"
	f := newAgentFixture(t, yaml, map[string]string{"web/package.json": "{}", "web/yarn.lock": "", "web/.yarnrc.yml": "nodeLinker: pnp\n"})
	logPath := fakeToolchain(t, f, "node")
	// A PnP-shaped yarn: writes .pnp.cjs, or node_modules with the
	// node-modules linker.
	bin := t.TempDir()
	yarn := `#!/bin/sh
echo "yarn $*" >> "$ANVIL_TOOL_LOG"
if grep -q 'nodeLinker: node-modules' .yarnrc.yml; then mkdir -p node_modules; else : > .pnp.cjs; fi
`
	require.NoError(t, os.WriteFile(filepath.Join(bin, "yarn"), []byte(yarn), 0o755))
	for i, entry := range f.env {
		if strings.HasPrefix(entry, "PATH=") {
			f.env[i] = "PATH=" + bin + string(os.PathListSeparator) + strings.TrimPrefix(entry, "PATH=")
		}
	}
	_, stderr, code := f.run(f.repo, "ensure", "feature/x")
	require.Equal(t, 0, code, stderr)
	require.Len(t, readLog(t, logPath), 1)

	envelope, stderr, code := f.runJSON(f.repo, "ensure", "feature-x")
	require.Equal(t, 0, code, stderr)
	require.Equal(t, []string{"up_to_date"}, stepResults(t, envelope))
	require.Len(t, readLog(t, logPath), 1, "unchanged PnP install does not rerun")

	wt := f.worktreePath("feature-x")
	require.NoError(t, os.Remove(filepath.Join(wt, "web", ".pnp.cjs")))
	envelope, stderr, code = f.runJSON(f.repo, "ensure", "feature-x")
	require.Equal(t, 0, code, stderr)
	require.Equal(t, []string{"ran"}, stepResults(t, envelope))

	require.NoError(t, os.WriteFile(filepath.Join(wt, "web", ".yarnrc.yml"), []byte("nodeLinker: node-modules\n"), 0o644))
	envelope, stderr, code = f.runJSON(f.repo, "ensure", "feature-x")
	require.Equal(t, 0, code, stderr)
	require.Equal(t, []string{"ran"}, stepResults(t, envelope), "linker change invalidates")
	require.DirExists(t, filepath.Join(wt, "web", "node_modules"))
	envelope, _, code = f.runJSON(f.repo, "ensure", "feature-x")
	require.Equal(t, 0, code)
	require.Equal(t, []string{"up_to_date"}, stepResults(t, envelope))
}
