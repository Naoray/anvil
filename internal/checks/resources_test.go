package checks

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/naoray/anvil/internal/config"
)

func (f fixture) resourceRequest(check config.CheckConfig, shared ...SharedResource) IdentityRequest {
	return IdentityRequest{WorktreePath: f.worktree, RuntimeDir: f.runtime, RuntimeRoot: f.root, Check: check, Env: f.env(), Resources: shared}
}

func (f fixture) resourceIdentity(t *testing.T, check config.CheckConfig, shared ...SharedResource) Identity {
	t.Helper()
	identity, err := ComputeIdentity(context.Background(), f.resourceRequest(check, shared...))
	require.NoError(t, err)
	return identity
}

// recorded wraps an identity as a passing stored result.
func recorded(identity Identity) *Result {
	return &Result{Status: StatusPassed, Identity: identity}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}

func TestSharedResourceFileIdentityMakesCheckStale(t *testing.T) {
	f := newFixture(t)
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "SHA256SUMS"), "abc  model.bin\n")
	models := SharedResource{Name: "models", Path: dir, Identity: config.SharedIdentity{File: "SHA256SUMS"}}
	check := config.CheckConfig{Name: "fast", Commands: cmds("true"), Resources: []string{"models"}}

	before := f.resourceIdentity(t, check, models)
	require.Len(t, before.Resources, 1)
	entry := before.Resources[0]
	require.Equal(t, "models", entry.Name)
	require.Equal(t, ResourcePresent, entry.State)
	require.Equal(t, "file", entry.Method)
	require.Equal(t, dir, entry.Path)
	require.NotEmpty(t, entry.Digest)
	require.NotEmpty(t, before.ResourcesDigest)
	require.True(t, Assess(recorded(before), f.resourceIdentity(t, check, models)).PassingAndCurrent)

	// Unrelated content inside the resource is not part of a file identity.
	writeFile(t, filepath.Join(dir, "model.bin"), "weights")
	require.True(t, Assess(recorded(before), f.resourceIdentity(t, check, models)).Current)

	writeFile(t, filepath.Join(dir, "SHA256SUMS"), "def  model.bin\n")
	assessment := Assess(recorded(before), f.resourceIdentity(t, check, models))
	require.False(t, assessment.Current)
	require.Equal(t, []string{`shared resource "models" changed`}, assessment.Reasons)
}

func TestSharedResourceUndeclaredNeverAffectsIdentity(t *testing.T) {
	f := newFixture(t)
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "SUMS"), "1")
	broken := SharedResource{Name: "broken", Path: t.TempDir(), Identity: config.SharedIdentity{Command: "exit 3"}}
	other := SharedResource{Name: "other", Path: dir, Identity: config.SharedIdentity{File: "SUMS"}}
	check := config.CheckConfig{Name: "fast", Commands: cmds("true")}

	before := f.resourceIdentity(t, check, broken, other)
	require.Empty(t, before.Resources)
	require.Empty(t, before.ResourcesDigest)
	writeFile(t, filepath.Join(dir, "SUMS"), "2")
	require.True(t, Assess(recorded(before), f.resourceIdentity(t, check, broken, other)).Current)

	// A check without declared resources keeps the pre-resource digest, so
	// results recorded by earlier versions stay current.
	legacy := digestFields("anvil-check-identity-v1", before.Git.Digest, before.DefinitionDigest, before.ToolchainDigest,
		before.EnvDigest, before.InputsDigest, before.Platform.OS, before.Platform.Arch)
	require.Equal(t, legacy, before.Digest)

	// Only the declared resource is identified; the failing one is ignored.
	check.Resources = []string{"other"}
	withOther := f.resourceIdentity(t, check, broken, other)
	require.Len(t, withOther.Resources, 1)
	require.NotEqual(t, legacy, withOther.Digest)
}

func TestSharedResourceCommandIdentity(t *testing.T) {
	f := newFixture(t)
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "version"), "v1")
	fixtures := SharedResource{Name: "fixtures", Path: dir, Identity: config.SharedIdentity{Command: "cat version; echo noise-$$ >&2"}}
	check := config.CheckConfig{Name: "fast", Commands: cmds("true"), Resources: []string{"fixtures"}}

	before := f.resourceIdentity(t, check, fixtures)
	require.Equal(t, "command", before.Resources[0].Method)
	require.True(t, Assess(recorded(before), f.resourceIdentity(t, check, fixtures)).Current, "stderr is not hashed")

	writeFile(t, filepath.Join(dir, "version"), "v2")
	require.Equal(t, []string{`shared resource "fixtures" changed`}, Assess(recorded(before), f.resourceIdentity(t, check, fixtures)).Reasons)
}

func TestSharedResourceIdentityFailuresAreErrors(t *testing.T) {
	f := newFixture(t)
	dir := t.TempDir()
	// A directory where the identity file should be: unreadable even as root.
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "SUMS"), 0o755))
	writeFile(t, filepath.Join(dir, "inner", "x"), "x")
	for name, tc := range map[string]struct {
		identity config.SharedIdentity
		want     string
	}{
		"failing command": {config.SharedIdentity{Command: "echo bad >&2; exit 3"}, "exit status 3"},
		"unreadable file": {config.SharedIdentity{File: "SUMS"}, "SUMS"},
		"missing file":    {config.SharedIdentity{File: "MISSING"}, "MISSING"},
		"escaping link":   {config.SharedIdentity{File: "link"}, "outside the resource"},
	} {
		t.Run(name, func(t *testing.T) {
			if name == "escaping link" {
				require.NoError(t, os.Symlink("/etc/hostname", filepath.Join(dir, "link")))
			}
			models := SharedResource{Name: "models", Path: dir, Identity: tc.identity}
			check := config.CheckConfig{Name: "fast", Commands: cmds("touch ran.marker"), Resources: []string{"models"}}
			_, err := ComputeIdentity(context.Background(), f.resourceRequest(check, models))
			require.Error(t, err)
			require.Contains(t, err.Error(), `shared resource "models"`)
			require.Contains(t, err.Error(), tc.want)

			// Execution refuses before running anything, and nothing is reused.
			req := f.request(check)
			req.Resources = []SharedResource{models}
			result, err := Run(context.Background(), req)
			require.Error(t, err)
			require.Nil(t, result)
			require.NoFileExists(t, filepath.Join(f.worktree, "ran.marker"))
		})
	}
}

func TestSharedResourceCommandTimeoutIsError(t *testing.T) {
	f := newFixture(t)
	old := probeTimeout
	probeTimeout = 200 * time.Millisecond
	t.Cleanup(func() { probeTimeout = old })
	slow := SharedResource{Name: "slow", Path: t.TempDir(), Identity: config.SharedIdentity{Command: "sleep 30"}}
	start := time.Now()
	_, err := ComputeIdentity(context.Background(), f.resourceRequest(config.CheckConfig{Name: "fast", Commands: cmds("true"), Resources: []string{"slow"}}, slow))
	require.ErrorContains(t, err, `shared resource "slow"`)
	require.ErrorContains(t, err, "timed out")
	require.Less(t, time.Since(start), 5*time.Second)
}

func TestSharedResourceAbsentPresentTransitions(t *testing.T) {
	f := newFixture(t)
	dir := filepath.Join(t.TempDir(), "models")
	models := SharedResource{Name: "models", Path: dir, Identity: config.SharedIdentity{Directory: true}}
	check := config.CheckConfig{Name: "fast", Commands: cmds("true"), Resources: []string{"models"}}

	absent := f.resourceIdentity(t, check, models)
	require.Equal(t, ResourceAbsent, absent.Resources[0].State)
	require.Empty(t, absent.Resources[0].Digest)
	require.True(t, Assess(recorded(absent), f.resourceIdentity(t, check, models)).Current, "absence is a valid identity")

	writeFile(t, filepath.Join(dir, "a.bin"), "a")
	present := f.resourceIdentity(t, check, models)
	require.Equal(t, []string{`shared resource "models" appeared`}, Assess(recorded(absent), present).Reasons)

	require.NoError(t, os.RemoveAll(dir))
	require.Equal(t, []string{`shared resource "models" disappeared`}, Assess(recorded(present), f.resourceIdentity(t, check, models)).Reasons)

	models.Required = true
	_, err := ComputeIdentity(context.Background(), f.resourceRequest(check, models))
	require.ErrorContains(t, err, `shared resource "models": required but missing`)

	_, err = ComputeIdentity(context.Background(), f.resourceRequest(check))
	require.ErrorContains(t, err, `shared resource "models": not declared under resources.shared`)
}

func TestSharedResourceDirectoryIdentityMakesCheckStale(t *testing.T) {
	f := newFixture(t)
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a", "b.bin"), "bbbb")
	models := SharedResource{Name: "models", Path: dir, Identity: config.SharedIdentity{Directory: true}}
	check := config.CheckConfig{Name: "fast", Commands: cmds("true"), Resources: []string{"models"}}
	withoutRacyMargin(t)
	req := f.request(check)
	req.Resources = []SharedResource{models}
	run, err := Run(context.Background(), req)
	require.NoError(t, err)
	before := run.Identity
	entries, err := os.ReadDir(filepath.Join(f.root, "cache", "resource-identity"))
	require.NoError(t, err)
	require.Len(t, entries, 1, "execution writes the stat cache under the runtime root")
	require.True(t, Assess(recorded(before), f.resourceIdentity(t, check, models)).Current)
	writeFile(t, filepath.Join(dir, "a", "b.bin"), "BBBB")
	require.Equal(t, []string{`shared resource "models" changed`}, Assess(recorded(before), f.resourceIdentity(t, check, models)).Reasons)
}

// countingDigester counts file reads so cache use is observable.
type countingDigester struct {
	mu    sync.Mutex
	reads map[string]int
}

func (c *countingDigester) digester(cacheDir string, now func() time.Time) treeDigester {
	return treeDigester{cacheDir: cacheDir, write: true, now: now, hash: func(path string) (string, error) {
		c.mu.Lock()
		c.reads[filepath.Base(path)]++
		c.mu.Unlock()
		sum, _, err := fileDigest(path)
		return sum, err
	}}
}

func (c *countingDigester) total() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for _, count := range c.reads {
		n += count
	}
	return n
}

func (c *countingDigester) reset() {
	c.mu.Lock()
	c.reads = map[string]int{}
	c.mu.Unlock()
}

// later pretends the scan runs well after every file was written, so no
// entry is racy.
func later() time.Time { return time.Now().Add(time.Hour) }

func TestTreeDigestCache(t *testing.T) {
	skipWithoutStatCache(t)
	dir := t.TempDir()
	cacheDir := filepath.Join(t.TempDir(), "cache")
	writeFile(t, filepath.Join(dir, "a.bin"), "aaaa")
	writeFile(t, filepath.Join(dir, "sub", "b.bin"), "bbbb")
	counter := &countingDigester{reads: map[string]int{}}
	d := counter.digester(cacheDir, later)

	first, err := d.digest(context.Background(), dir)
	require.NoError(t, err)
	require.Equal(t, 2, counter.total())

	counter.reset()
	second, err := d.digest(context.Background(), dir)
	require.NoError(t, err)
	require.Equal(t, first, second)
	require.Zero(t, counter.total(), "unchanged files are not re-read")

	// Same size, mtime restored: ctime still changes, so it is rehashed.
	info, err := os.Stat(filepath.Join(dir, "a.bin"))
	require.NoError(t, err)
	writeFile(t, filepath.Join(dir, "a.bin"), "AAAA")
	require.NoError(t, os.Chtimes(filepath.Join(dir, "a.bin"), info.ModTime(), info.ModTime()))
	counter.reset()
	third, err := d.digest(context.Background(), dir)
	require.NoError(t, err)
	require.NotEqual(t, first, third)
	require.Equal(t, map[string]int{"a.bin": 1}, counter.reads)

	// Adding and deleting files changes the digest.
	writeFile(t, filepath.Join(dir, "sub", "c.bin"), "")
	added, err := d.digest(context.Background(), dir)
	require.NoError(t, err)
	require.NotEqual(t, third, added)
	require.NoError(t, os.Remove(filepath.Join(dir, "sub", "c.bin")))
	removed, err := d.digest(context.Background(), dir)
	require.NoError(t, err)
	require.Equal(t, third, removed)
	// An empty directory is part of the tree.
	require.NoError(t, os.Mkdir(filepath.Join(dir, "empty"), 0o755))
	withDir, err := d.digest(context.Background(), dir)
	require.NoError(t, err)
	require.NotEqual(t, removed, withDir)
	require.NoError(t, os.Remove(filepath.Join(dir, "empty")))

	// Mode changes are part of the identity.
	require.NoError(t, os.Chmod(filepath.Join(dir, "a.bin"), 0o755))
	chmodded, err := d.digest(context.Background(), dir)
	require.NoError(t, err)
	require.NotEqual(t, removed, chmodded)
	require.NoError(t, os.Chmod(filepath.Join(dir, "a.bin"), 0o644))

	// A corrupt cache is ignored and rebuilt, never trusted.
	cacheFiles, err := filepath.Glob(filepath.Join(cacheDir, "*.json"))
	require.NoError(t, err)
	require.Len(t, cacheFiles, 1)
	require.NoError(t, os.WriteFile(cacheFiles[0], []byte("{not json"), 0o600))
	counter.reset()
	rebuilt, err := d.digest(context.Background(), dir)
	require.NoError(t, err)
	require.Equal(t, removed, rebuilt)
	require.Equal(t, 2, counter.total())
	content, err := os.ReadFile(cacheFiles[0])
	require.NoError(t, err)
	require.True(t, json.Valid(content), "the cache was rewritten")

	// A cache that can be neither read nor replaced (a directory in its
	// place) is best-effort: the digest is still computed, with a warning.
	require.NoError(t, os.Remove(cacheFiles[0]))
	require.NoError(t, os.Mkdir(cacheFiles[0], 0o700))
	var warnings []string
	d.warn = func(message string) { warnings = append(warnings, message) }
	unreadable, err := d.digest(context.Background(), dir)
	require.NoError(t, err)
	require.Equal(t, removed, unreadable)
	require.Len(t, warnings, 1)
	require.Contains(t, warnings[0], "resource identity cache")
}

func TestTreeDigestCacheSkipsRacyEntries(t *testing.T) {
	skipWithoutStatCache(t)
	dir := t.TempDir()
	cacheDir := filepath.Join(t.TempDir(), "cache")
	counter := &countingDigester{reads: map[string]int{}}
	d := counter.digester(cacheDir, time.Now)
	writeFile(t, filepath.Join(dir, "a.bin"), "aaaa")
	first, err := d.digest(context.Background(), dir)
	require.NoError(t, err)

	// Written within the timestamp granularity of the scan: the entry must not
	// be trusted, so a same-second, same-size edit is still seen.
	counter.reset()
	writeFile(t, filepath.Join(dir, "a.bin"), "AAAA")
	second, err := d.digest(context.Background(), dir)
	require.NoError(t, err)
	require.NotEqual(t, first, second)
	require.Equal(t, 1, counter.total())
	counter.reset()
	_, err = d.digest(context.Background(), dir)
	require.NoError(t, err)
	require.Equal(t, 1, counter.total(), "racy entries are rehashed every time")
}

func TestTreeDigestLinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs privileges on Windows")
	}
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "blobs", "x"), "x")
	writeFile(t, filepath.Join(dir, "blobs", "y"), "y")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "snapshots"), 0o755))
	link := filepath.Join(dir, "snapshots", "current")
	require.NoError(t, os.Symlink("../blobs/x", link))
	d := newTreeDigester("", false, nil)

	first, err := d.digest(context.Background(), dir)
	require.NoError(t, err)
	require.NoError(t, os.Remove(link))
	require.NoError(t, os.Symlink("../blobs/y", link))
	second, err := d.digest(context.Background(), dir)
	require.NoError(t, err)
	require.NotEqual(t, first, second, "link targets are part of the tree")

	require.NoError(t, os.Remove(link))
	require.NoError(t, os.Symlink("../..", link))
	_, err = d.digest(context.Background(), dir)
	require.ErrorContains(t, err, "points outside the resource")
	require.NoError(t, os.Remove(link))
	require.NoError(t, os.Symlink("/etc/hostname", link))
	_, err = d.digest(context.Background(), dir)
	require.ErrorContains(t, err, "points outside the resource")
	require.NoError(t, os.Remove(link))

	// Lexically inside, really outside: s -> . and t -> s/.. resolves to the
	// resource's parent.
	require.NoError(t, os.Symlink(".", filepath.Join(dir, "s")))
	require.NoError(t, os.Symlink("s/..", filepath.Join(dir, "t")))
	_, err = d.digest(context.Background(), dir)
	require.ErrorContains(t, err, "points outside the resource")
	require.NoError(t, os.Remove(filepath.Join(dir, "t")))
	_, err = d.digest(context.Background(), dir)
	require.NoError(t, err, "a link to the resource root itself stays inside")
	require.NoError(t, os.Remove(filepath.Join(dir, "s")))

	require.NoError(t, os.Symlink("missing", link))
	_, err = d.digest(context.Background(), dir)
	require.ErrorContains(t, err, "dangling")
}

func TestTreeDigestConcurrentWritersKeepCacheValid(t *testing.T) {
	skipWithoutStatCache(t)
	dir := t.TempDir()
	cacheDir := filepath.Join(t.TempDir(), "cache")
	for _, name := range []string{"a", "b", "c", "d"} {
		writeFile(t, filepath.Join(dir, name), name)
	}
	counter := &countingDigester{reads: map[string]int{}}
	d := counter.digester(cacheDir, later)
	want, err := d.digest(context.Background(), dir)
	require.NoError(t, err)
	var wg sync.WaitGroup
	errs := make([]error, 8)
	sums := make([]string, 8)
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			sums[i], errs[i] = d.digest(context.Background(), dir)
		}(i)
	}
	wg.Wait()
	for i := range errs {
		require.NoError(t, errs[i])
		require.Equal(t, want, sums[i])
	}
	files, err := os.ReadDir(cacheDir)
	require.NoError(t, err)
	require.Len(t, files, 1, "no temporary files are left behind")
	content, err := os.ReadFile(filepath.Join(cacheDir, files[0].Name()))
	require.NoError(t, err)
	require.True(t, json.Valid(content))
}

// withoutRacyMargin lets files written by the test be cached immediately.
func withoutRacyMargin(t *testing.T) {
	t.Helper()
	old := racyMargin
	racyMargin = 0
	t.Cleanup(func() { racyMargin = old })
}

func skipWithoutStatCache(t *testing.T) {
	t.Helper()
	if !statCacheSupported {
		t.Skip("the directory identity cache needs ctime and inode, unavailable on " + runtime.GOOS)
	}
}

func TestSharedResourceCacheIsBestEffortAndReadOnlyByDefault(t *testing.T) {
	f := newFixture(t)
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a.bin"), "a")
	models := SharedResource{Name: "models", Path: dir, Identity: config.SharedIdentity{Directory: true}}
	check := config.CheckConfig{Name: "fast", Commands: cmds("true"), Resources: []string{"models"}}

	// Read paths never create the cache, even with cacheable entries.
	withoutRacyMargin(t)
	f.resourceIdentity(t, check, models)
	require.NoDirExists(t, filepath.Join(f.root, "cache"))

	// A regular file where the cache directory belongs: identity still works,
	// the check still runs, and its result is current.
	writeFile(t, filepath.Join(f.root, "cache", "resource-identity"), "not a directory")
	var warnings []string
	req := f.request(check)
	req.Resources = []SharedResource{models}
	req.Warn = func(message string) { warnings = append(warnings, message) }
	result, err := Run(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, StatusPassed, result.Status)
	require.NotEmpty(t, warnings)
	require.Contains(t, warnings[0], "resource identity cache")
	require.True(t, Assess(result, f.resourceIdentity(t, check, models)).PassingAndCurrent)
}

func TestTreeDigestCacheMaintenance(t *testing.T) {
	skipWithoutStatCache(t)
	dir := t.TempDir()
	cacheDir := filepath.Join(t.TempDir(), "cache")
	writeFile(t, filepath.Join(dir, "a.bin"), "a")
	stale := filepath.Join(cacheDir, "old.json.tmp-123")
	fresh := filepath.Join(cacheDir, "inflight.json.tmp-456")
	writeFile(t, stale, "partial")
	writeFile(t, fresh, "partial")
	old := time.Now().Add(-2 * time.Hour)
	require.NoError(t, os.Chtimes(stale, old, old))
	d := newTreeDigester(cacheDir, true, nil)
	d.now = later
	_, err := d.digest(context.Background(), dir)
	require.NoError(t, err)
	require.NoFileExists(t, stale, "abandoned temporary files are removed")
	require.FileExists(t, fresh, "a concurrent writer's temporary file is left alone")

	// Nothing changed: the cache is not rewritten.
	files, err := filepath.Glob(filepath.Join(cacheDir, "*.json"))
	require.NoError(t, err)
	require.Len(t, files, 1)
	before, err := os.Stat(files[0])
	require.NoError(t, err)
	_, err = d.digest(context.Background(), dir)
	require.NoError(t, err)
	after, err := os.Stat(files[0])
	require.NoError(t, err)
	require.True(t, os.SameFile(before, after), "an unchanged cache is not replaced")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = d.digest(ctx, dir)
	require.ErrorIs(t, err, context.Canceled)
}

func TestSharedResourceIdentityIsMachineWide(t *testing.T) {
	first, second := newFixture(t), newFixture(t)
	dir := t.TempDir()
	// The command sees only the inherited environment, never worktree values.
	fixtures := SharedResource{Name: "fixtures", Path: dir, Identity: config.SharedIdentity{Command: `echo "rt=$ANVIL_RUNTIME_DIR token=$API_TOKEN"`}}
	check := config.CheckConfig{Name: "fast", Commands: cmds("true"), Resources: []string{"fixtures"}}
	a := first.resourceIdentity(t, check, fixtures)
	b := second.resourceIdentity(t, check, fixtures)
	require.Equal(t, a.Resources, b.Resources)
	require.Equal(t, a.ResourcesDigest, b.ResourcesDigest)
}

// An anvil process started from inside another worktree (anvil run) inherits
// that worktree's ANVIL_* values; they must not leak into resource identity.
func TestSharedResourceIdentityIgnoresInheritedWorktreeVariables(t *testing.T) {
	f := newFixture(t)
	dir := t.TempDir()
	fixtures := SharedResource{Name: "fixtures", Path: dir, Identity: config.SharedIdentity{Command: `echo "id=$ANVIL_WORKTREE_ID port=$ANVIL_PORT_WEB"`}}
	check := config.CheckConfig{Name: "fast", Commands: cmds("true"), Resources: []string{"fixtures"}}
	t.Setenv("ANVIL_WORKTREE_ID", "wt_1111111111111111")
	t.Setenv("ANVIL_PORT_WEB", "8001")
	a := f.resourceIdentity(t, check, fixtures)
	t.Setenv("ANVIL_WORKTREE_ID", "wt_2222222222222222")
	t.Setenv("ANVIL_PORT_WEB", "8002")
	b := f.resourceIdentity(t, check, fixtures)
	require.Equal(t, a.ResourcesDigest, b.ResourcesDigest)
}

func TestSharedResourceMemoIdentifiesOncePerInvocation(t *testing.T) {
	f := newFixture(t)
	dir := t.TempDir()
	counter := filepath.Join(t.TempDir(), "count")
	fixtures := SharedResource{Name: "fixtures", Path: dir, Identity: config.SharedIdentity{Command: "echo x >> '" + counter + "'; echo same"}}
	memo := NewResourceMemo()
	for _, name := range []string{"one", "two"} {
		req := f.resourceRequest(config.CheckConfig{Name: name, Commands: cmds("true"), Resources: []string{"fixtures"}}, fixtures)
		req.Memo = memo
		_, err := ComputeIdentity(context.Background(), req)
		require.NoError(t, err)
	}
	content, err := os.ReadFile(counter)
	require.NoError(t, err)
	require.Equal(t, "x\n", string(content))
}
