package cli

import (
	"bufio"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/naoray/anvil/internal/procs"
)

const fakeGH = `#!/bin/sh
branch=""
while [ $# -gt 0 ]; do
  if [ "$1" = "--head" ]; then branch="$2"; shift; fi
  shift
done
case "$branch" in
  feature/pr-open) echo '[{"state":"OPEN","number":12,"mergedAt":null,"headRefOid":"0000"}]' ;;
  feature/gh-broken) echo 'gh: not logged in' >&2; exit 1 ;;
  *) echo '[]' ;;
esac
`

// newPruneFixture is an agent fixture with a local bare "origin" holding
// main and a fake gh on PATH, so merge, remote and PR evidence are real.
func newPruneFixture(t *testing.T) *agentFixture {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("uses sh and procfs/lsof")
	}
	f := newAgentFixture(t, "scaffold:\n  override: true\nprune:\n  protected:\n    - config-keep/*\n  allow_ignored:\n    - \"*.count\"\n",
		map[string]string{".gitignore": ".anvil.local\n*.count\n.env\nnode_modules/\nstorage/\n"})
	origin := filepath.Join(evalSymlinks(t.TempDir()), "origin.git")
	gitRun(t, f.repo, "init", "--bare", "-b", "main", origin)
	gitRun(t, f.repo, "remote", "add", "origin", origin)
	gitRun(t, f.repo, "push", "origin", "main")
	bin := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(bin, "gh"), []byte(fakeGH), 0o755))
	return withEnv(f, "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// idleTime is well outside every recent-activity window the tests use.
var idleTime = time.Now().Add(-48 * time.Hour)

// backdate makes every worktree under the fixture's base look idle: file
// mtimes and each worktree's HEAD reflog and index are set to idleTime.
func backdate(t *testing.T, f *agentFixture) {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(f.base, "demo"))
	require.NoError(t, err)
	for _, entry := range entries {
		dir := filepath.Join(f.base, "demo", entry.Name())
		require.NoError(t, filepath.Walk(dir, func(path string, _ os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			return os.Chtimes(path, idleTime, idleTime)
		}))
		gitDir := gitRun(t, dir, "rev-parse", "--absolute-git-dir")
		for _, name := range []string{"logs/HEAD", "index"} {
			if _, err := os.Stat(filepath.Join(gitDir, name)); err == nil {
				require.NoError(t, os.Chtimes(filepath.Join(gitDir, name), idleTime, idleTime))
			}
		}
	}
}

// pruneArgs appends --allow-uninspectable for own processes that already
// existed and cannot be inspected in this environment (for example a
// sandbox's init process), so open-file evidence is decidable here.
func pruneArgs(t *testing.T, args ...string) []string {
	t.Helper()
	snapshot, err := procs.ScanOpenFiles()
	if err != nil {
		t.Skipf("open files cannot be inspected here: %v", err)
	}
	for _, process := range snapshot.UninspectableOwn {
		if process.Command == "" {
			t.Skipf("own process %d cannot be inspected or named", process.PID)
		}
		args = append(args, "--allow-uninspectable", process.Command)
	}
	for _, process := range snapshot.ForeignNamespaceOwn {
		args = append(args, "--allow-uninspectable", process.Command)
	}
	return args
}

func commitFile(t *testing.T, dir, name, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644))
	gitRun(t, dir, "add", name)
	gitRun(t, dir, "commit", "-m", "change "+name)
}

// holdFileOpen starts a helper process whose working directory is elsewhere
// and which holds path open until the test ends.
func holdFileOpen(t *testing.T, path string) int {
	t.Helper()
	cmd := exec.Command(os.Args[0])
	cmd.Dir = t.TempDir()
	cmd.Env = append(os.Environ(), "ANVIL_TEST_HELPER=1", "ANVIL_HELPER_MODE=hold-file", "ANVIL_HELPER_FILE="+path)
	stdin, err := cmd.StdinPipe()
	require.NoError(t, err)
	stdout, err := cmd.StdoutPipe()
	require.NoError(t, err)
	require.NoError(t, cmd.Start())
	t.Cleanup(func() {
		if err := stdin.Close(); err != nil {
			t.Logf("closing helper stdin: %v", err)
		}
		if err := cmd.Wait(); err != nil {
			t.Logf("helper exited: %v", err)
		}
	})
	line, err := bufio.NewReader(stdout).ReadString('\n')
	require.NoError(t, err)
	require.Equal(t, "ready\n", line)
	return cmd.Process.Pid
}

func candidateEntries(t *testing.T, doc any) map[string]map[string]any {
	t.Helper()
	entries := map[string]map[string]any{}
	for _, raw := range field(t, doc, "worktrees").([]any) {
		entry := raw.(map[string]any)
		entries[field(t, entry, "worktree", "name").(string)] = entry
	}
	return entries
}

func requireListContains(t *testing.T, entry map[string]any, key, substring string) {
	t.Helper()
	list, _ := entry[key].([]any)
	for _, item := range list {
		if strings.Contains(item.(string), substring) {
			return
		}
	}
	require.Failf(t, "missing entry", "%s has no %q: %v", key, substring, list)
}

func requireBlocked(t *testing.T, entry map[string]any, substring string) {
	t.Helper()
	require.Equal(t, false, entry["candidate"], "%v", entry["worktree"])
	requireListContains(t, entry, "blockers", substring)
}

func TestPruneCandidatesReportEvidenceForEveryBlocker(t *testing.T) {
	f := newPruneFixture(t)
	merged := f.addGitWorktree("feature/merged", "feature-merged")
	commitFile(t, merged, "merged.txt", "merged\n")
	gitRun(t, merged, "push", "origin", "feature/merged")
	gitRun(t, f.repo, "merge", "--ff-only", "feature/merged")
	gitRun(t, f.repo, "push", "origin", "main")

	unmerged := f.addGitWorktree("feature/unmerged", "feature-unmerged")
	commitFile(t, unmerged, "unmerged.txt", "x\n")
	gitRun(t, unmerged, "push", "origin", "feature/unmerged")
	unpushed := f.addGitWorktree("feature/unpushed", "feature-unpushed")
	commitFile(t, unpushed, "unpushed.txt", "x\n")
	dirty := f.addGitWorktree("feature/dirty", "feature-dirty")
	require.NoError(t, os.WriteFile(filepath.Join(dirty, "README.md"), []byte("changed\n"), 0o644))
	untracked := f.addGitWorktree("feature/untracked", "feature-untracked")
	require.NoError(t, os.WriteFile(filepath.Join(untracked, "notes.txt"), []byte("x"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(untracked, "node_modules", "pkg"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(untracked, "node_modules", "pkg", "index.js"), []byte("x"), 0o644))
	f.addGitWorktree("feature/leased", "feature-leased")
	f.addGitWorktree("keep/flag", "keep-flag")
	f.addGitWorktree("config-keep/x", "config-keep-x")
	held := f.addGitWorktree("feature/held", "feature-held")
	f.addGitWorktree("feature/pr-open", "feature-pr-open")
	f.addGitWorktree("feature/gh-broken", "feature-gh-broken")
	gitRun(t, f.repo, "worktree", "add", "--detach", f.worktreePath("detached"), "main")
	locked := f.addGitWorktree("feature/locked", "feature-locked")
	gitRun(t, f.repo, "worktree", "lock", "--reason", "on usb", locked)

	_, stderr, code := f.runJSON(f.repo, "adopt", "--all")
	require.Equal(t, 0, code, stderr)
	_, stderr, code = f.runJSON(f.repo, "adopt", "feature-leased", "--lease-owner", "codex")
	require.Equal(t, 0, code, stderr)
	touched := f.addGitWorktree("feature/touched", "feature-touched")
	noReflog := f.addGitWorktree("feature/no-reflog", "feature-no-reflog")
	_, stderr, code = f.runJSON(f.repo, "adopt", "feature-touched")
	require.Equal(t, 0, code, stderr)
	_, stderr, code = f.runJSON(f.repo, "adopt", "feature-no-reflog")
	require.Equal(t, 0, code, stderr)
	f.addGitWorktree("feature/unadopted", "feature-unadopted")
	holder := holdFileOpen(t, filepath.Join(held, "README.md"))
	backdate(t, f)
	now := time.Now()
	require.NoError(t, os.Chtimes(filepath.Join(touched, "README.md"), now, now))
	require.NoError(t, os.Remove(filepath.Join(gitRun(t, noReflog, "rev-parse", "--absolute-git-dir"), "logs", "HEAD")))

	envelope, stderr, code := f.runJSON(f.repo, pruneArgs(t, "prune", "candidates", "--recent", "10m", "--protect", "keep/*")...)
	require.Equal(t, 0, code, stderr)
	require.Equal(t, "prune candidates", envelope["command"])
	require.Equal(t, false, envelope["mutating"])
	doc := envelope["data"]
	require.Equal(t, "anvil.prune-candidates/v1", field(t, doc, "schema"))
	require.NotEmpty(t, field(t, doc, "generated_at"))
	require.Equal(t, filepath.Join(f.repo, ".git"), field(t, doc, "repository", "common_dir"))
	require.Equal(t, "demo", field(t, doc, "repository", "project"))
	entries := candidateEntries(t, doc)

	ok := entries["feature-merged"]
	require.Equal(t, true, ok["candidate"], "%v", ok["blockers"])
	require.Empty(t, ok["blockers"])
	requireListContains(t, ok, "reasons", "merged into origin/main")
	require.Equal(t, "merged", field(t, ok, "evidence", "merge", "state"))
	require.Equal(t, "pushed", field(t, ok, "evidence", "remote", "state"))
	require.Equal(t, "none", field(t, ok, "evidence", "pr", "state"))
	require.Equal(t, "gh", field(t, ok, "evidence", "pr", "source"))
	require.Equal(t, "idle", field(t, ok, "evidence", "activity", "state"))

	requireBlocked(t, entries["feature-unmerged"], "not merged into origin/main")
	require.Equal(t, "pushed", field(t, entries["feature-unmerged"], "evidence", "remote", "state"))
	requireBlocked(t, entries["feature-unpushed"], "1 commit(s) not on any remote")
	require.EqualValues(t, 1, field(t, entries["feature-unpushed"], "evidence", "remote", "unpushed_commits"))
	requireBlocked(t, entries["feature-dirty"], "1 tracked change(s)")
	requireBlocked(t, entries["feature-untracked"], "1 untracked file(s)")
	require.Equal(t, []any{"notes.txt"}, field(t, entries["feature-untracked"], "evidence", "untracked", "sample"))
	requireBlocked(t, entries["feature-leased"], `active lease by "codex"`)
	requireBlocked(t, entries["keep-flag"], `protected by pattern "keep/*"`)
	requireBlocked(t, entries["config-keep-x"], `protected by pattern "config-keep/*"`)
	requireBlocked(t, entries["feature-held"], "hold files open beneath the worktree")
	require.EqualValues(t, holder, field(t, entries["feature-held"], "evidence", "open_files", "holders", 0, "pid"))
	requireBlocked(t, entries["feature-pr-open"], "pull request #12 is open")
	gh := entries["feature-gh-broken"]
	require.Equal(t, true, gh["candidate"], "an unavailable PR lookup is not a blocker: %v", gh["blockers"])
	require.Equal(t, "unavailable", field(t, gh, "evidence", "pr", "state"))
	requireBlocked(t, entries["feature-unadopted"], "no Anvil identity")
	requireBlocked(t, entries["detached"], "detached HEAD")
	require.Equal(t, "detached", entries["detached"]["kind"])
	requireBlocked(t, entries["feature-locked"], "locked worktree (on usb)")
	main := entries[filepath.Base(f.repo)]
	requireBlocked(t, main, "main checkout")
	require.Equal(t, "main", main["kind"])
	requireBlocked(t, entries["feature-touched"], "recent activity: files")
	require.Equal(t, "recent", field(t, entries["feature-touched"], "evidence", "activity", "state"))
	noReflogEntry := entries["feature-no-reflog"]
	require.Equal(t, true, noReflogEntry["candidate"], "%v", noReflogEntry["blockers"])
	requireListContains(t, noReflogEntry, "warnings", "reflog unavailable")

	// A window longer than the backdated idle time sees the activity.
	envelope, stderr, code = f.runJSON(f.repo, pruneArgs(t, "prune", "candidates", "--no-pr", "--recent", "72h")...)
	require.Equal(t, 0, code, stderr)
	entries = candidateEntries(t, envelope["data"])
	requireBlocked(t, entries["feature-merged"], "recent activity")
	require.Equal(t, "recent", field(t, entries["feature-merged"], "evidence", "activity", "state"))
	require.Equal(t, "unavailable", field(t, entries["feature-merged"], "evidence", "pr", "state"))
}

func TestPruneCandidatesMissingRemoteRefIsABlocker(t *testing.T) {
	f := newAgentFixture(t, "scaffold:\n  override: true\n", nil)
	f.addGitWorktree("feature/x", "feature-x")
	_, stderr, code := f.runJSON(f.repo, "adopt", "--all")
	require.Equal(t, 0, code, stderr)
	backdate(t, f)
	envelope, stderr, code := f.runJSON(f.repo, pruneArgs(t, "prune", "candidates", "--recent", "10m", "--no-pr")...)
	require.Equal(t, 0, code, stderr)
	entry := candidateEntries(t, envelope["data"])["feature-x"]
	requireBlocked(t, entry, "evidence unavailable: merge state")
	require.Equal(t, "unknown", field(t, entry, "evidence", "merge", "state"))
}

// generateCandidates writes a candidate file for every fresh, adopted worktree.
func generateCandidates(t *testing.T, f *agentFixture, extra ...string) (string, map[string]any) {
	t.Helper()
	file := filepath.Join(t.TempDir(), "candidates.json")
	backdate(t, f)
	args := pruneArgs(t, append([]string{"prune", "candidates", "--recent", "10m", "--output", file}, extra...)...)
	_, stderr, code := f.runJSON(f.repo, args...)
	require.Equal(t, 0, code, stderr)
	content, err := os.ReadFile(file)
	require.NoError(t, err)
	var doc map[string]any
	require.NoError(t, json.Unmarshal(content, &doc))
	require.Equal(t, "anvil.prune-candidates/v1", doc["schema"])
	return file, doc
}

func writeCandidates(t *testing.T, file string, doc map[string]any) {
	t.Helper()
	content, err := json.Marshal(doc)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(file, content, 0o644))
}

func applyResults(t *testing.T, envelope map[string]any) map[string]map[string]any {
	t.Helper()
	results := map[string]map[string]any{}
	for _, raw := range field(t, envelope, "data", "results").([]any) {
		entry := raw.(map[string]any)
		results[field(t, entry, "worktree", "name").(string)] = entry
	}
	return results
}

func TestPruneApplyRemovesOnlyStillSafeCandidates(t *testing.T) {
	f := newPruneFixture(t)
	paths := map[string]string{}
	for _, name := range []string{"a", "b", "c", "d", "e"} {
		paths[name] = f.addGitWorktree("feature/"+name, "feature-"+name)
	}
	protected := f.addGitWorktree("keep/f", "keep-f")
	_, stderr, code := f.runJSON(f.repo, "adopt", "--all")
	require.Equal(t, 0, code, stderr)
	file, doc := generateCandidates(t, f, "--protect", "keep/*")
	entries := candidateEntries(t, doc)
	for _, name := range []string{"a", "b", "c", "d", "e"} {
		require.Equal(t, true, entries["feature-"+name]["candidate"], "%s: %v", name, entries["feature-"+name]["blockers"])
	}
	require.Equal(t, false, entries["keep-f"]["candidate"])

	// After generation: a lease, an open file, a new commit, a dirty file.
	_, stderr, code = f.runJSON(f.repo, "adopt", "feature-b", "--lease-owner", "codex")
	require.Equal(t, 0, code, stderr)
	holdFileOpen(t, filepath.Join(paths["c"], "README.md"))
	commitFile(t, paths["d"], "new.txt", "new\n")
	require.NoError(t, os.WriteFile(filepath.Join(paths["e"], "README.md"), []byte("dirty\n"), 0o644))

	envelope, stderr, code := f.runJSON(f.repo, pruneArgs(t, "prune", "apply", "--from", file, "--recent", "10m", "--force")...)
	require.Equal(t, 0, code, stderr)
	require.Equal(t, true, envelope["mutating"])
	results := applyResults(t, envelope)
	require.Equal(t, "removed", results["feature-a"]["outcome"], "%v", results["feature-a"])
	require.NoDirExists(t, paths["a"])
	// Git for Windows lists C:/... paths; compare in native form so the
	// assertion cannot pass vacuously there.
	require.NotContains(t, filepath.FromSlash(gitRun(t, f.repo, "worktree", "list")), paths["a"])

	requireListContains(t, results["feature-b"], "blockers", `active lease by "codex"`)
	requireListContains(t, results["feature-c"], "blockers", "hold files open")
	require.Contains(t, results["feature-d"]["reason"], "HEAD changed")
	requireListContains(t, results["feature-e"], "blockers", "tracked change")
	for _, name := range []string{"b", "c", "d", "e"} {
		require.Equal(t, "skipped", results["feature-"+name]["outcome"], name)
		require.DirExists(t, paths[name])
	}
	require.Equal(t, "skipped", results["keep-f"]["outcome"])
	require.Equal(t, "not_candidate", results["keep-f"]["code"])
	require.DirExists(t, protected)
}

func TestPruneApplyDryRunMutatesNothingAndCurrentThresholdsWin(t *testing.T) {
	f := newPruneFixture(t)
	path := f.addGitWorktree("feature/a", "feature-a")
	_, stderr, code := f.runJSON(f.repo, "adopt", "--all")
	require.Equal(t, 0, code, stderr)
	file, _ := generateCandidates(t, f)
	state, err := os.ReadFile(filepath.Join(path, ".anvil.local"))
	require.NoError(t, err)

	envelope, stderr, code := f.runJSON(f.repo, pruneArgs(t, "prune", "apply", "--from", file, "--recent", "10m", "--dry-run")...)
	require.Equal(t, 0, code, stderr)
	require.Equal(t, true, field(t, envelope, "data", "dry_run"))
	require.Equal(t, "would_remove", applyResults(t, envelope)["feature-a"]["outcome"])
	require.DirExists(t, path)
	after, err := os.ReadFile(filepath.Join(path, ".anvil.local"))
	require.NoError(t, err)
	require.Equal(t, string(state), string(after))

	// The file was generated with --recent 10m; apply's own larger window
	// applies, so activity 48h ago now blocks.
	envelope, stderr, code = f.runJSON(f.repo, pruneArgs(t, "prune", "apply", "--from", file, "--recent", "72h", "--force")...)
	require.Equal(t, 0, code, stderr)
	result := applyResults(t, envelope)["feature-a"]
	require.Equal(t, "skipped", result["outcome"])
	requireListContains(t, result, "blockers", "recent activity")
	require.DirExists(t, path)
}

func TestPruneApplyRefusesTamperedFiles(t *testing.T) {
	f := newPruneFixture(t)
	paths := map[string]string{}
	for _, name := range []string{"x", "y", "z", "w", "v"} {
		paths[name] = f.addGitWorktree("feature/"+name, "feature-"+name)
	}
	_, stderr, code := f.runJSON(f.repo, "adopt", "--all")
	require.Equal(t, 0, code, stderr)

	other := newAgentFixture(t, "scaffold:\n  override: true\n", nil)
	otherPath := other.addGitWorktree("feature/other", "feature-other")
	envelope, stderr, code := other.runJSON(other.repo, "adopt", "feature-other")
	require.Equal(t, 0, code, stderr)
	otherID := field(t, adoptEntries(t, envelope)["feature-other"], "worktree", "id")
	outside := filepath.Join(evalSymlinks(t.TempDir()), "feature-z")
	require.NoError(t, os.MkdirAll(outside, 0o755))

	file, doc := generateCandidates(t, f)
	for _, raw := range doc["worktrees"].([]any) {
		entry := raw.(map[string]any)
		worktree := entry["worktree"].(map[string]any)
		switch worktree["name"] {
		case "feature-x": // a worktree of another repository
			worktree["path"], worktree["id"] = otherPath, otherID
		case "feature-y":
			worktree["id"] = "wt_0000000000000000"
		case "feature-z":
			worktree["path"] = outside
		case "feature-w":
			worktree["path"] = f.base + "/demo/../demo/feature-w"
		case "feature-v":
			entry["candidate"] = false
		}
	}
	writeCandidates(t, file, doc)

	envelope, stderr, code = f.runJSON(f.repo, pruneArgs(t, "prune", "apply", "--from", file, "--recent", "10m", "--force")...)
	require.Equal(t, 0, code, stderr)
	results := applyResults(t, envelope)
	for _, name := range []string{"x", "y", "z", "w"} {
		result := results["feature-"+name]
		require.Equal(t, "skipped", result["outcome"], name)
		require.Equal(t, "target_mismatch", result["code"], "%s: %v", name, result)
	}
	require.Equal(t, "not_candidate", results["feature-v"]["code"])
	for _, path := range paths {
		require.DirExists(t, path)
	}
	require.DirExists(t, otherPath)
	require.DirExists(t, outside)

	doc["repository"].(map[string]any)["common_dir"] = filepath.Join(other.repo, ".git")
	writeCandidates(t, file, doc)
	envelope, _, code = f.runJSON(f.repo, pruneArgs(t, "prune", "apply", "--from", file, "--recent", "10m", "--force")...)
	require.Equal(t, 2, code)
	require.Equal(t, "invalid_arguments", field(t, envelope, "error", "code"))

	doc["repository"].(map[string]any)["common_dir"] = filepath.Join(f.repo, ".git")
	worktrees := doc["worktrees"].([]any)
	doc["worktrees"] = append(worktrees, worktrees[len(worktrees)-1])
	writeCandidates(t, file, doc)
	envelope, _, code = f.runJSON(f.repo, pruneArgs(t, "prune", "apply", "--from", file, "--recent", "10m", "--force")...)
	require.Equal(t, 2, code, "duplicate entries are refused")
	require.Contains(t, field(t, envelope, "error", "message"), "duplicate")

	doc["schema"] = "anvil.prune-candidates/v0"
	writeCandidates(t, file, doc)
	_, _, code = f.runJSON(f.repo, pruneArgs(t, "prune", "apply", "--from", file)...)
	require.Equal(t, 2, code)
	for _, path := range paths {
		require.DirExists(t, path)
	}
}

func TestPruneRecentWindowHasAMinimum(t *testing.T) {
	f := newPruneFixture(t)
	for _, command := range [][]string{{"prune", "candidates"}, {"prune", "apply", "--from", "unused.json"}} {
		for _, window := range []string{"0s", "5m"} {
			envelope, _, code := f.runJSON(f.repo, append(command, "--recent", window)...)
			require.Equal(t, 2, code, "%v --recent %s", command, window)
			require.Equal(t, "invalid_arguments", field(t, envelope, "error", "code"))
		}
	}
}

func TestPruneCandidatesIgnoredFilesBlockUnlessAllowed(t *testing.T) {
	f := newPruneFixture(t)
	env := f.addGitWorktree("feature/env", "feature-env")
	require.NoError(t, os.WriteFile(filepath.Join(env, ".env"), []byte("SECRET=1\n"), 0o644))
	storage := f.addGitWorktree("feature/storage", "feature-storage")
	require.NoError(t, os.MkdirAll(filepath.Join(storage, "storage", "logs"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(storage, "storage", "logs", "a.log"), []byte("x"), 0o644))
	modules := f.addGitWorktree("feature/modules", "feature-modules")
	require.NoError(t, os.MkdirAll(filepath.Join(modules, "node_modules", "pkg"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(modules, "node_modules", "pkg", "index.js"), []byte("x"), 0o644))
	counted := f.addGitWorktree("feature/counted", "feature-counted")
	require.NoError(t, os.WriteFile(filepath.Join(counted, "runs.count"), []byte("1"), 0o644))
	_, stderr, code := f.runJSON(f.repo, "adopt", "--all")
	require.Equal(t, 0, code, stderr)
	backdate(t, f)

	envelope, stderr, code := f.runJSON(f.repo, pruneArgs(t, "prune", "candidates", "--recent", "10m")...)
	require.Equal(t, 0, code, stderr)
	entries := candidateEntries(t, envelope["data"])
	requireBlocked(t, entries["feature-env"], "1 ignored path(s) would be deleted")
	require.Equal(t, []any{".env"}, field(t, entries["feature-env"], "evidence", "ignored", "sample"))
	requireBlocked(t, entries["feature-storage"], "ignored path(s) would be deleted")
	require.Equal(t, []any{"storage/"}, field(t, entries["feature-storage"], "evidence", "ignored", "sample"))
	for _, name := range []string{"feature-modules", "feature-counted"} {
		require.Equal(t, true, entries[name]["candidate"], "%s: %v (node_modules, .anvil.local and config-allowed files never block)", name, entries[name]["blockers"])
	}
	require.EqualValues(t, 1, field(t, entries["feature-counted"], "evidence", "ignored", "allowed"))

	envelope, stderr, code = f.runJSON(f.repo, pruneArgs(t, "prune", "candidates", "--recent", "10m", "--allow-ignored", ".env", "--allow-ignored", "storage")...)
	require.Equal(t, 0, code, stderr)
	entries = candidateEntries(t, envelope["data"])
	require.Equal(t, true, entries["feature-env"]["candidate"], "%v", entries["feature-env"]["blockers"])
	require.Equal(t, true, entries["feature-storage"]["candidate"], "%v", entries["feature-storage"]["blockers"])

	// Ignored files count as activity even when allowed.
	now := time.Now()
	require.NoError(t, os.Chtimes(filepath.Join(env, ".env"), now, now))
	envelope, stderr, code = f.runJSON(f.repo, pruneArgs(t, "prune", "candidates", "--recent", "10m", "--allow-ignored", ".env")...)
	require.Equal(t, 0, code, stderr)
	requireBlocked(t, candidateEntries(t, envelope["data"])["feature-env"], "recent activity: ignored_files")
}

func TestPruneApplyNeverAllowsMoreIgnoredFilesThanTheInvocation(t *testing.T) {
	f := newPruneFixture(t)
	env := f.addGitWorktree("feature/env", "feature-env")
	require.NoError(t, os.WriteFile(filepath.Join(env, ".env"), []byte("SECRET=1\n"), 0o644))
	late := f.addGitWorktree("feature/late", "feature-late")
	_, stderr, code := f.runJSON(f.repo, "adopt", "--all")
	require.Equal(t, 0, code, stderr)
	file, doc := generateCandidates(t, f, "--allow-ignored", ".env")
	require.Equal(t, true, candidateEntries(t, doc)["feature-env"]["candidate"])
	require.Equal(t, true, candidateEntries(t, doc)["feature-late"]["candidate"])
	// An ignored file appears after generation.
	require.NoError(t, os.MkdirAll(filepath.Join(late, "storage"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(late, "storage", "db.sqlite"), []byte("x"), 0o644))
	require.NoError(t, os.Chtimes(filepath.Join(late, "storage", "db.sqlite"), idleTime, idleTime))

	envelope, stderr, code := f.runJSON(f.repo, pruneArgs(t, "prune", "apply", "--from", file, "--recent", "10m", "--force")...)
	require.Equal(t, 0, code, stderr)
	results := applyResults(t, envelope)
	requireListContains(t, results["feature-env"], "blockers", "ignored path(s) would be deleted")
	requireListContains(t, results["feature-late"], "blockers", "ignored path(s) would be deleted")
	require.DirExists(t, env)
	require.DirExists(t, late)

	envelope, stderr, code = f.runJSON(f.repo, pruneArgs(t, "prune", "apply", "--from", file, "--recent", "10m", "--allow-ignored", ".env", "--force")...)
	require.Equal(t, 0, code, stderr)
	results = applyResults(t, envelope)
	require.Equal(t, "removed", results["feature-env"]["outcome"], "%v", results["feature-env"])
	require.Equal(t, "skipped", results["feature-late"]["outcome"])
	require.NoDirExists(t, env)
}

func TestPruneCandidatesNeverRewriteTheIndex(t *testing.T) {
	f := newPruneFixture(t)
	path := f.addGitWorktree("feature/index", "feature-index")
	_, stderr, code := f.runJSON(f.repo, "adopt", "--all")
	require.Equal(t, 0, code, stderr)
	// Backdating tracked files makes their cached stat data stale, which an
	// ordinary 'git status' would refresh by rewriting the index.
	backdate(t, f)
	index := filepath.Join(gitRun(t, path, "rev-parse", "--absolute-git-dir"), "index")
	before, err := os.Stat(index)
	require.NoError(t, err)
	content, err := os.ReadFile(index)
	require.NoError(t, err)

	_, stderr, code = f.runJSON(f.repo, pruneArgs(t, "prune", "candidates", "--recent", "10m")...)
	require.Equal(t, 0, code, stderr)
	after, err := os.Stat(index)
	require.NoError(t, err)
	require.Equal(t, before.ModTime(), after.ModTime(), "read-only evidence must not rewrite the index")
	afterContent, err := os.ReadFile(index)
	require.NoError(t, err)
	require.Equal(t, content, afterContent)
}

func TestPruneApplyRequiresForceUnlessDryRun(t *testing.T) {
	f := newPruneFixture(t)
	path := f.addGitWorktree("feature/a", "feature-a")
	_, stderr, code := f.runJSON(f.repo, "adopt", "--all")
	require.Equal(t, 0, code, stderr)
	file, _ := generateCandidates(t, f)

	envelope, _, code := f.runJSON(f.repo, pruneArgs(t, "prune", "apply", "--from", file, "--recent", "10m")...)
	require.Equal(t, 2, code)
	require.Equal(t, "invalid_arguments", field(t, envelope, "error", "code"))
	require.Contains(t, field(t, envelope, "error", "message"), "--force")
	_, stderr, code = f.run(f.repo, pruneArgs(t, "prune", "apply", "--from", file, "--recent", "10m")...)
	require.Equal(t, 2, code, "human mode refuses too")
	require.Contains(t, stderr, "--force")
	require.DirExists(t, path)
}

func TestPruneApplySurfacesEvaluationWarnings(t *testing.T) {
	f := newPruneFixture(t)
	path := f.addGitWorktree("feature/a", "feature-a")
	_, stderr, code := f.runJSON(f.repo, "adopt", "--all")
	require.Equal(t, 0, code, stderr)
	file, _ := generateCandidates(t, f)
	require.NoError(t, os.Remove(filepath.Join(gitRun(t, path, "rev-parse", "--absolute-git-dir"), "logs", "HEAD")))
	envelope, stderr, code := f.runJSON(f.repo, pruneArgs(t, "prune", "apply", "--from", file, "--recent", "10m", "--dry-run")...)
	require.Equal(t, 0, code, stderr)
	result := applyResults(t, envelope)["feature-a"]
	require.Equal(t, "would_remove", result["outcome"])
	requireListContains(t, result, "warnings", "reflog unavailable")
}

func TestPruneCandidatesPreparationIsRecentActivity(t *testing.T) {
	f := newPruneFixture(t)
	f.addGitWorktree("feature/prepared", "feature-prepared")
	// ensure records a preparation (finished now) without touching files.
	_, stderr, code := f.runJSON(f.repo, "ensure", "feature-prepared")
	require.Equal(t, 0, code, stderr)
	backdate(t, f)
	envelope, stderr, code := f.runJSON(f.repo, pruneArgs(t, "prune", "candidates", "--recent", "10m")...)
	require.Equal(t, 0, code, stderr)
	requireBlocked(t, candidateEntries(t, envelope["data"])["feature-prepared"], "recent activity: preparation")
}
