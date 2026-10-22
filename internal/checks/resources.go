package checks

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/naoray/anvil/internal/config"
	"github.com/naoray/anvil/internal/workenv"
)

// SharedResource is one declared shared resource with its resolved path.
type SharedResource struct {
	Name     string
	Path     string
	Required bool
	Identity config.SharedIdentity
}

// Shared resource states recorded in an identity.
const (
	ResourcePresent = "present"
	ResourceAbsent  = "absent"
)

// ResourceIdentity records the identity of one shared resource a check
// declared. Absent optional resources have an empty digest.
type ResourceIdentity struct {
	Name   string `json:"name"`
	Path   string `json:"path"`
	State  string `json:"state"`
	Method string `json:"method"`
	// Source is the identity file or command (empty for directory).
	Source string `json:"source,omitempty"`
	Digest string `json:"digest"`
}

// ResourceIdentityError reports that a declared shared resource could not be
// identified; no result may be reused or recorded without its identity.
type ResourceIdentityError struct {
	Resource string
	Err      error
}

func (e *ResourceIdentityError) Error() string {
	return fmt.Sprintf("shared resource %q: %v", e.Resource, e.Err)
}

func (e *ResourceIdentityError) Unwrap() error { return e.Err }

// ResourceMemo shares resource identities between the identity computations
// of one read-only invocation (for example `check --list` with several checks
// declaring the same resource). Execution never uses it, so every run
// observes the resource as it is when that run starts.
type ResourceMemo struct {
	mu      sync.Mutex
	entries map[string]memoEntry
}

type memoEntry struct {
	entry ResourceIdentity
	err   error
}

// NewResourceMemo returns an empty memo.
func NewResourceMemo() *ResourceMemo {
	return &ResourceMemo{entries: map[string]memoEntry{}}
}

// resourceIdentities identifies exactly the resources the check declares.
func resourceIdentities(ctx context.Context, req IdentityRequest) ([]ResourceIdentity, string, error) {
	if len(req.Check.Resources) == 0 {
		return []ResourceIdentity{}, "", nil
	}
	byName := map[string]SharedResource{}
	for _, shared := range req.Resources {
		byName[shared.Name] = shared
	}
	names := append([]string(nil), req.Check.Resources...)
	sort.Strings(names)
	out := make([]ResourceIdentity, 0, len(names))
	fields := []string{}
	for _, name := range names {
		shared, ok := byName[name]
		if !ok {
			return nil, "", &ResourceIdentityError{Resource: name, Err: errors.New("not declared under resources.shared")}
		}
		entry, err := req.Memo.identify(ctx, req, shared)
		if err != nil {
			if ctx.Err() != nil {
				return nil, "", ctx.Err()
			}
			return nil, "", &ResourceIdentityError{Resource: name, Err: err}
		}
		out = append(out, entry)
		fields = append(fields, entry.Name, entry.Path, entry.State, entry.Method, entry.Source, entry.Digest)
	}
	return out, digestFields(fields...), nil
}

// identify consults the memo (when there is one) keyed by everything that
// determines the identity except the resource's name.
func (m *ResourceMemo) identify(ctx context.Context, req IdentityRequest, shared SharedResource) (ResourceIdentity, error) {
	if m == nil {
		return identifyResource(ctx, req, shared)
	}
	key := digestFields(shared.Path, fmt.Sprint(shared.Required), shared.Identity.Method(), shared.Identity.File, shared.Identity.Command)
	m.mu.Lock()
	defer m.mu.Unlock()
	if cached, ok := m.entries[key]; ok {
		cached.entry.Name = shared.Name
		return cached.entry, cached.err
	}
	entry, err := identifyResource(ctx, req, shared)
	if ctx.Err() == nil {
		m.entries[key] = memoEntry{entry: entry, err: err}
	}
	return entry, err
}

func identifyResource(ctx context.Context, req IdentityRequest, shared SharedResource) (ResourceIdentity, error) {
	entry := ResourceIdentity{Name: shared.Name, Path: shared.Path, Method: shared.Identity.Method()}
	switch entry.Method {
	case config.IdentityFile:
		entry.Source = shared.Identity.File
	case config.IdentityCommand:
		entry.Source = shared.Identity.Command
	case config.IdentityDirectory:
	default:
		return entry, errors.New("declares no identity (set identity file, command or directory)")
	}
	if shared.Path == "" || !filepath.IsAbs(shared.Path) {
		return entry, fmt.Errorf("path %q could not be resolved to an absolute path", shared.Path)
	}
	info, err := os.Stat(shared.Path)
	switch {
	case errors.Is(err, os.ErrNotExist) && shared.Required:
		return entry, fmt.Errorf("required but missing (%s)", shared.Path)
	case errors.Is(err, os.ErrNotExist):
		entry.State = ResourceAbsent
		return entry, nil
	case err != nil:
		return entry, err
	}
	entry.State = ResourcePresent
	if entry.Method != config.IdentityDirectory && !info.IsDir() {
		return entry, fmt.Errorf("%s is not a directory", shared.Path)
	}
	switch entry.Method {
	case config.IdentityFile:
		entry.Digest, err = resourceFileDigest(shared.Path, shared.Identity.File)
	case config.IdentityCommand:
		entry.Digest, err = resourceCommandDigest(ctx, req, shared.Path, shared.Identity.Command)
	case config.IdentityDirectory:
		cacheDir := ""
		if req.RuntimeRoot != "" {
			cacheDir = filepath.Join(req.RuntimeRoot, "cache", "resource-identity")
		}
		entry.Digest, err = newTreeDigester(cacheDir, req.CacheWrite, req.Warn).digest(ctx, shared.Path)
	}
	return entry, err
}

// resourceFileDigest hashes one file that must resolve inside the resource.
// The opened file must be the file the resolved path names (no swap between
// resolution and reading).
func resourceFileDigest(root, name string) (sum string, err error) {
	if err := config.ValidateRelativePath(name); err != nil {
		return "", fmt.Errorf("identity file: %w", err)
	}
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	realPath, err := filepath.EvalSymlinks(filepath.Join(root, name))
	if err != nil {
		return "", fmt.Errorf("identity file %s: %w", name, err)
	}
	if !within(realRoot, realPath) {
		return "", fmt.Errorf("identity file %s resolves outside the resource", name)
	}
	file, err := os.Open(realPath)
	if err != nil {
		return "", fmt.Errorf("identity file %s: %w", name, err)
	}
	defer func() { err = errors.Join(err, file.Close()) }()
	opened, err := file.Stat()
	if err != nil {
		return "", fmt.Errorf("identity file %s: %w", name, err)
	}
	if !opened.Mode().IsRegular() {
		return "", fmt.Errorf("identity file %s is not a regular file", name)
	}
	current, err := os.Lstat(realPath)
	if err != nil {
		return "", fmt.Errorf("identity file %s: %w", name, err)
	}
	if !os.SameFile(opened, current) {
		return "", fmt.Errorf("identity file %s changed while it was being identified", name)
	}
	h := sha256.New()
	if _, err := io.Copy(h, file); err != nil {
		return "", fmt.Errorf("identity file %s: %w", name, err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// machineEnviron drops ANVIL_* variables inherited from a worktree context
// (for example when Anvil runs inside `anvil run`), keeping the machine-wide
// runtime root override.
func machineEnviron(environ []string) []string {
	out := make([]string, 0, len(environ))
	for _, entry := range environ {
		name, _, _ := strings.Cut(entry, "=")
		upper := strings.ToUpper(name)
		if strings.HasPrefix(upper, "ANVIL_") && upper != "ANVIL_RUNTIME_ROOT" {
			continue
		}
		out = append(out, entry)
	}
	return out
}

// resourceCommandDigest runs the identity command in the resource directory
// under the same supervision and timeout as toolchain probes. Shared
// resources are machine-wide, so the command receives only the inherited
// process environment (minus the role's env_unset and any inherited ANVIL_*
// worktree values), never worktree values.
// Only stdout is hashed; a non-zero exit or timeout is an error.
func resourceCommandDigest(ctx context.Context, req IdentityRequest, dir, command string) (string, error) {
	probeCtx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	cmd, finish, err := supervisedCommand(probeCtx, command, req.Guards)
	if err != nil {
		return "", err
	}
	cmd.Dir = dir
	cmd.Env = (&workenv.Environment{}).Environ(machineEnviron(os.Environ()), req.Unset, runtime.GOOS == "windows")
	stdout := sha256.New() // streamed, never buffered
	stderr := &boundedBuffer{limit: 4 << 10}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	runErr := cmd.Run()
	if err := finish(); err != nil {
		return "", err
	}
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if probeCtx.Err() != nil {
		return "", fmt.Errorf("identity command timed out after %s: %w", probeTimeout, probeCtx.Err())
	}
	if runErr != nil {
		var exitErr *exec.ExitError
		message := firstLine(stderr.String())
		if errors.As(runErr, &exitErr) && message != "" {
			return "", fmt.Errorf("identity command failed: %w: %s", runErr, message)
		}
		return "", fmt.Errorf("identity command failed: %w", runErr)
	}
	return hex.EncodeToString(stdout.Sum(nil)), nil
}

// racyMargin covers coarse filesystem timestamp granularity (1s, or 2s on
// FAT): a cache entry is only stored when the file's mtime and ctime are at
// least this much older than the start of the scan that read it. A file
// modified after that scan necessarily gets a newer timestamp, so a stored
// entry can never hide a same-tick edit. (A variable only so tests can
// exercise cache writes without waiting.)
var racyMargin = 2 * time.Second

// staleTempAge is how old a cache temporary file must be before a writer
// treats it as abandoned (a concurrent writer renames its own within moments).
const staleTempAge = time.Hour

const treeCacheVersion = "anvil.resource-identity-cache/v1"

// treeCacheEntry keys one file's digest by its metadata.
type treeCacheEntry struct {
	Size    int64  `json:"size"`
	MtimeNS int64  `json:"mtime_ns"`
	CtimeNS int64  `json:"ctime_ns"`
	Inode   uint64 `json:"inode"`
	Mode    uint32 `json:"mode"`
	SHA256  string `json:"sha256"`
}

type treeCache struct {
	Version string                    `json:"version"`
	Root    string                    `json:"root"`
	Files   map[string]treeCacheEntry `json:"files"`
}

// treeDigester content-addresses a resource tree, reusing file digests whose
// metadata is unchanged since a non-racy earlier scan. The cache is
// best-effort: failing to read it means rehashing, failing to write it is a
// warning, and neither ever affects the digest.
type treeDigester struct {
	// cacheDir holds one cache file per canonical root ("" disables caching).
	cacheDir string
	// write allows creating and replacing cache files; read-only commands
	// only read an existing cache.
	write bool
	warn  func(string)
	hash  func(path string) (string, error)
	now   func() time.Time
}

func newTreeDigester(cacheDir string, write bool, warn func(string)) treeDigester {
	if !statCacheSupported {
		// Without ctime and inode, metadata cannot prove a file unchanged.
		cacheDir = ""
	}
	return treeDigester{cacheDir: cacheDir, write: write, warn: warn, now: time.Now, hash: func(path string) (string, error) {
		sum, _, err := fileDigest(path)
		return sum, err
	}}
}

func (d treeDigester) warnf(format string, args ...any) {
	if d.warn != nil {
		d.warn(fmt.Sprintf(format, args...))
	}
}

func (d treeDigester) cachePath(root string) string {
	sum := sha256.Sum256([]byte(root))
	return filepath.Join(d.cacheDir, hex.EncodeToString(sum[:])+".json")
}

// loadCache never trusts an unreadable, corrupt or foreign cache.
func (d treeDigester) loadCache(root string) map[string]treeCacheEntry {
	if d.cacheDir == "" {
		return nil
	}
	content, err := os.ReadFile(d.cachePath(root))
	if err != nil {
		return nil
	}
	var cache treeCache
	if err := json.Unmarshal(content, &cache); err != nil || cache.Version != treeCacheVersion || cache.Root != root {
		return nil
	}
	return cache.Files
}

// saveCache replaces the cache atomically; concurrent writers each rename a
// complete private temporary file, so readers see one whole version.
func (d treeDigester) saveCache(root string, files map[string]treeCacheEntry) error {
	if err := os.MkdirAll(d.cacheDir, 0o700); err != nil {
		return err
	}
	d.removeStaleTemps()
	content, err := json.Marshal(treeCache{Version: treeCacheVersion, Root: root, Files: files})
	if err != nil {
		return err
	}
	path := d.cachePath(root)
	temp, err := os.CreateTemp(d.cacheDir, filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	_, writeErr := temp.Write(content)
	if err := errors.Join(writeErr, temp.Sync(), temp.Close()); err != nil {
		return errors.Join(err, os.Remove(temp.Name()))
	}
	if err := os.Rename(temp.Name(), path); err != nil {
		return errors.Join(err, os.Remove(temp.Name()))
	}
	return nil
}

// removeStaleTemps deletes temporary files abandoned by killed writers.
func (d treeDigester) removeStaleTemps() {
	temps, err := filepath.Glob(filepath.Join(d.cacheDir, "*.tmp-*"))
	if err != nil {
		d.warnf("resource identity cache: %v", err)
		return
	}
	for _, temp := range temps {
		info, err := os.Lstat(temp)
		if err != nil || time.Since(info.ModTime()) < staleTempAge {
			continue
		}
		if err := os.Remove(temp); err != nil && !errors.Is(err, os.ErrNotExist) {
			d.warnf("resource identity cache: %v", err)
		}
	}
}

// digest hashes relative paths, types, permission bits, file contents and
// symlink targets in lexical order. Symlinks are not followed: their target
// text is hashed, and a link whose resolved target lies outside the resource
// (or does not exist) is refused because its contents would escape the
// identity. Other special files (sockets, FIFOs, devices) are refused.
func (d treeDigester) digest(ctx context.Context, path string) (string, error) {
	root, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	if root, err = filepath.Abs(root); err != nil {
		return "", err
	}
	scanStart := d.now()
	cached := d.loadCache(root)
	next := map[string]treeCacheEntry{}
	h := sha256.New()
	err = filepath.WalkDir(root, func(current string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		rel, err := filepath.Rel(root, current)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		info, err := entry.Info()
		if err != nil {
			return err
		}
		mode := fmt.Sprintf("%o", uint32(info.Mode().Perm()))
		switch {
		case info.IsDir():
			writeField(h, "dir")
			writeField(h, rel)
			writeField(h, mode)
		case info.Mode()&os.ModeSymlink != 0:
			target, err := os.Readlink(current)
			if err != nil {
				return err
			}
			resolved, err := filepath.EvalSymlinks(current)
			if errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("symlink %s is dangling (%s)", rel, target)
			}
			if err != nil {
				return fmt.Errorf("symlink %s: %w", rel, err)
			}
			if !within(root, resolved) {
				return fmt.Errorf("symlink %s points outside the resource (%s)", rel, target)
			}
			writeField(h, "link")
			writeField(h, rel)
			writeField(h, filepath.ToSlash(target))
		case info.Mode().IsRegular():
			sum, err := d.fileSum(current, rel, info, scanStart, cached, next)
			if err != nil {
				return err
			}
			writeField(h, "file")
			writeField(h, rel)
			writeField(h, mode)
			writeField(h, sum)
		default:
			return fmt.Errorf("unsupported file type %s at %s", info.Mode().Type(), rel)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if d.cacheDir != "" && d.write && !maps.Equal(cached, next) {
		if err := d.saveCache(root, next); err != nil {
			d.warnf("resource identity cache not saved: %v", err)
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func (d treeDigester) fileSum(path, rel string, info fs.FileInfo, scanStart time.Time, cached, next map[string]treeCacheEntry) (string, error) {
	ctime, inode := statIdentity(info)
	key := treeCacheEntry{Size: info.Size(), MtimeNS: info.ModTime().UnixNano(), CtimeNS: ctime, Inode: inode, Mode: uint32(info.Mode())}
	if previous, ok := cached[rel]; ok && previous.SHA256 != "" {
		candidate := previous
		candidate.SHA256 = ""
		if candidate == key {
			next[rel] = previous
			return previous.SHA256, nil
		}
	}
	sum, err := d.hash(path)
	if err != nil {
		return "", err
	}
	safeBefore := scanStart.Add(-racyMargin).UnixNano()
	if key.MtimeNS < safeBefore && key.CtimeNS < safeBefore {
		key.SHA256 = sum
		next[rel] = key
	}
	return sum, nil
}
