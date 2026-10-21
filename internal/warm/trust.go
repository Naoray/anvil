package warm

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"
)

// Policy is the project's declared warm opt-in (anvil.yaml warm.node_modules).
// The zero value keeps the conservative default.
type Policy struct {
	// TrustInstallScripts admits npm lifecycle scripts and native addons
	// when every script identity, the lockfile, toolchain and platform match.
	TrustInstallScripts bool
	// Rebuild runs in the destination after a warm hit. It also admits
	// files that embed the source worktree path, which it must regenerate.
	Rebuild string
}

// ScriptIdentity names one trusted lifecycle script by package path (""
// for the project root, otherwise its lockfile path), script name and the
// SHA-256 of its text. Script text itself is never stored.
type ScriptIdentity struct {
	Package string `yaml:"package"`
	Script  string `yaml:"script"`
	SHA256  string `yaml:"sha256"`
}

// lifecycleScripts are the npm scripts that run during or around install.
var lifecycleScripts = []string{"preinstall", "install", "postinstall", "prepublish", "preprepare", "prepare", "postprepare"}

// installScripts mark a package as hasInstallScript in npm lockfiles.
var installScripts = []string{"preinstall", "install", "postinstall"}

// scriptIdentities lists the lifecycle scripts of the root manifest in
// project and of every lockfile dependency installed under tree, and checks
// them against the lockfile's hasInstallScript set. It returns the sorted
// identities and the installed packages flagged with install scripts.
func scriptIdentities(project, tree string) ([]ScriptIdentity, []string, error) {
	var identities []ScriptIdentity
	root, err := os.ReadFile(filepath.Join(project, "package.json"))
	if err != nil {
		return nil, nil, err
	}
	rootScripts, err := manifestScripts(root, false)
	if err != nil {
		return nil, nil, err
	}
	for name, text := range rootScripts {
		identities = append(identities, identity("", name, text))
	}

	data, err := os.ReadFile(filepath.Join(project, "package-lock.json"))
	if err != nil {
		return nil, nil, err
	}
	var lock struct {
		Packages map[string]struct {
			HasInstallScript bool `json:"hasInstallScript"`
		} `json:"packages"`
	}
	if err := json.Unmarshal(data, &lock); err != nil {
		return nil, nil, err
	}
	paths := make([]string, 0, len(lock.Packages))
	for path := range lock.Packages {
		if path != "" {
			paths = append(paths, path)
		}
	}
	sort.Strings(paths)
	var flagged []string
	for _, path := range paths {
		if !strings.HasPrefix(path, "node_modules/") || !filepath.IsLocal(filepath.FromSlash(path)) {
			return nil, nil, fmt.Errorf("unsupported lockfile package path %q", path)
		}
		dir := filepath.Join(filepath.Dir(tree), filepath.FromSlash(path))
		manifest, err := os.ReadFile(filepath.Join(dir, "package.json"))
		if errors.Is(err, os.ErrNotExist) {
			// Optional dependencies for other platforms are not installed.
			continue
		}
		if err != nil {
			return nil, nil, err
		}
		gyp := false
		if _, err := os.Lstat(filepath.Join(dir, "binding.gyp")); err == nil {
			gyp = true
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, nil, err
		}
		scripts, err := manifestScripts(manifest, gyp)
		if err != nil {
			return nil, nil, fmt.Errorf("%s: %w", path, err)
		}
		hasInstall := false
		for name, text := range scripts {
			identities = append(identities, identity(path, name, text))
			hasInstall = hasInstall || slices.Contains(installScripts, name)
		}
		if hasInstall != lock.Packages[path].HasInstallScript {
			return nil, nil, fmt.Errorf("lockfile install-script metadata disagrees with installed %s", path)
		}
		if hasInstall {
			flagged = append(flagged, path)
		}
	}
	sort.Slice(identities, func(a, b int) bool {
		if identities[a].Package != identities[b].Package {
			return identities[a].Package < identities[b].Package
		}
		return identities[a].Script < identities[b].Script
	})
	return identities, flagged, nil
}

// manifestScripts returns the lifecycle scripts of one package.json. A
// package with binding.gyp and no install script gets npm's implicit
// "node-gyp rebuild" install script.
func manifestScripts(data []byte, gyp bool) (map[string]string, error) {
	var manifest struct {
		Scripts map[string]any `json:"scripts"`
		Gypfile *bool          `json:"gypfile"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, err
	}
	scripts := map[string]string{}
	for _, name := range lifecycleScripts {
		value, ok := manifest.Scripts[name]
		if !ok {
			continue
		}
		text, ok := value.(string)
		if !ok {
			return nil, fmt.Errorf("script %s is not a string", name)
		}
		scripts[name] = text
	}
	if gyp && (manifest.Gypfile == nil || *manifest.Gypfile) && scripts["install"] == "" && scripts["preinstall"] == "" {
		scripts["install"] = "node-gyp rebuild"
	}
	return scripts, nil
}

func identity(pkg, script, text string) ScriptIdentity {
	sum := sha256.Sum256([]byte(text))
	return ScriptIdentity{Package: pkg, Script: script, SHA256: hex.EncodeToString(sum[:])}
}

// trustedScriptsMatch verifies the script identities of tree (resolved
// against project's manifest and lockfile) against the stamped provenance.
func (i *Install) trustedScriptsMatch(expected stamp, project, tree string) bool {
	if !i.policy.TrustInstallScripts {
		return true
	}
	scripts, packages, err := scriptIdentities(project, tree)
	return err == nil && slices.Equal(scripts, expected.TrustedScripts) && slices.Equal(packages, expected.InstallScriptPackages)
}

// Rebuild returns the post-warm command to run in the destination ("" for
// none).
func (i *Install) Rebuild() string { return i.policy.Rebuild }

// CheckDetached fails while any file or symlink of the published warm tree
// still references the source worktree path (e.g. a node-gyp build directory
// the rebuild did not regenerate), or a symlink leaves the destination tree.
func (i *Install) CheckDetached() error {
	if i.warmedFrom == "" {
		return nil
	}
	needles := detachNeedles(i.warmedFrom)
	fold := foldsCase(runtime.GOOS)
	dest := filepath.Join(i.root, i.tree)
	return filepath.WalkDir(dest, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dest, path)
		if err != nil {
			return err
		}
		name := filepath.ToSlash(filepath.Join(i.tree, rel))
		var content func() (io.ReadCloser, error)
		switch {
		case entry.Type()&fs.ModeSymlink != 0:
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			if filepath.IsAbs(link) {
				return fmt.Errorf("%s is an absolute symlink after rebuild", name)
			}
			inside, err := filepath.Rel(dest, filepath.Join(filepath.Dir(path), link))
			if err != nil || !filepath.IsLocal(inside) {
				return fmt.Errorf("%s links outside %s after rebuild", name, i.tree)
			}
			content = func() (io.ReadCloser, error) { return io.NopCloser(strings.NewReader(link)), nil }
		case entry.Type().IsRegular():
			content = func() (io.ReadCloser, error) { return os.Open(path) }
		default:
			return nil
		}
		for _, needle := range needles {
			f, err := content()
			if err != nil {
				return err
			}
			found, readErr := referencesPath(f, needle, fold)
			if err := errors.Join(readErr, f.Close()); err != nil {
				return err
			}
			if found {
				return fmt.Errorf("%s still references the source worktree after rebuild", name)
			}
		}
		return nil
	})
}

// detachNeedles lists the spellings of the source worktree path (see
// sourceNeedles) on this OS.
func detachNeedles(source string) []string {
	return sourceNeedles(sourcePaths(source), runtime.GOOS)
}

// sourcePaths lists the names the OS reports for the worktree paths: as
// given, symlink-resolved and, on Windows, the long and 8.3 short names.
func sourcePaths(paths ...string) []string {
	var out []string
	for _, path := range paths {
		out = append(out, path)
		if resolved, err := filepath.EvalSymlinks(path); err == nil {
			out = append(out, resolved)
		}
	}
	for _, path := range slices.Clone(out) {
		out = append(out, platformPathForms(path)...)
	}
	return out
}

// sourceNeedles lists the spellings under which tools may record the
// worktree paths, using goos's path semantics: POSIX paths with or without
// macOS's /private prefix; Windows paths with native, forward-slash,
// JSON-escaped and MSYS (/c/...) separators, case-folded (see foldsCase).
// Both add Makefile-style escaped spaces.
func sourceNeedles(paths []string, goos string) []string {
	var out []string
	if goos == "windows" {
		for _, path := range paths {
			slash := strings.ReplaceAll(path, `\`, "/")
			out = append(out, path, slash, strings.ReplaceAll(path, `\`, `\\`))
			if len(slash) >= 3 && slash[1] == ':' && slash[2] == '/' {
				out = append(out, "/"+slash[:1]+slash[2:])
			}
		}
		for n := range out {
			out[n] = string(foldASCII([]byte(out[n])))
		}
	} else {
		out = append(out, paths...)
		for _, path := range paths {
			if trimmed, ok := strings.CutPrefix(path, "/private/"); ok {
				out = append(out, "/"+trimmed)
			} else if strings.HasPrefix(path, "/") && runtimeHasPrivateAlias(path) {
				out = append(out, "/private"+path)
			}
		}
	}
	for _, path := range slices.Clone(out) {
		if strings.Contains(path, " ") {
			out = append(out, strings.ReplaceAll(path, " ", `\ `))
		}
	}
	out = slices.DeleteFunc(out, func(path string) bool { return path == "" })
	sort.Strings(out)
	return slices.Compact(out)
}

// foldsCase reports whether goos compares paths case-insensitively for
// needle matching. Only Windows does; other platforms keep exact matches.
func foldsCase(goos string) bool { return goos == "windows" }

// foldASCII lowercases ASCII letters in place, keeping byte offsets.
func foldASCII(b []byte) []byte {
	for n, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[n] = c + 'a' - 'A'
		}
	}
	return b
}

// runtimeHasPrivateAlias reports paths that macOS also exposes below /private.
func runtimeHasPrivateAlias(path string) bool {
	for _, prefix := range []string{"/var/", "/tmp/", "/etc/"} {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

// Discard removes a published warm tree whose post-warm step failed, and any
// provenance for it, so a retry performs a clean cold install.
func (i *Install) Discard() error {
	i.warmedFrom = ""
	dest := filepath.Join(i.root, i.tree)
	if _, err := os.Lstat(dest); err == nil {
		if err := removeStage(dest); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	stored, err := readStamp(i.root, i.tree)
	if err != nil || stored.Inputs == "" {
		return err
	}
	return writeStamp(i.root, i.tree, stamp{})
}

// pathByte reports whether b can continue a path component, so that
// "/wt/feature-2" is not mistaken for a reference to "/wt/feature".
func pathByte(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' || strings.IndexByte("._-+~@", b) >= 0
}

// referencesPath reports whether r mentions the absolute path as a whole
// path (not as a prefix of a sibling or suffix of a longer path). With fold,
// r is ASCII case-folded and path must already be (see sourceNeedles).
func referencesPath(r io.Reader, path string, fold bool) (bool, error) {
	needle := []byte(path)
	if len(needle) == 0 {
		return false, nil
	}
	buf := make([]byte, 64*1024)
	// Windows after the first keep len(needle)+1 bytes of history, so every
	// match is evaluated once with both neighbors known.
	var window []byte
	start := true
	for {
		n, err := r.Read(buf)
		if fold {
			foldASCII(buf[:n])
		}
		window = append(window, buf[:n]...)
		eof := errors.Is(err, io.EOF)
		if err != nil && !eof {
			return false, err
		}
		for offset := 0; ; {
			index := bytes.Index(window[offset:], needle)
			if index < 0 {
				break
			}
			at := offset + index
			offset = at + 1
			if at == 0 && !start {
				continue // already evaluated in the previous window
			}
			end := at + len(needle)
			if end == len(window) && !eof {
				break // decided once the next byte arrives
			}
			if at > 0 && pathByte(window[at-1]) {
				continue
			}
			if end == len(window) || !pathByte(window[end]) {
				return true, nil
			}
		}
		if eof {
			return false, nil
		}
		if keep := len(needle) + 1; len(window) > keep {
			window = append([]byte(nil), window[len(window)-keep:]...)
			start = false
		}
	}
}
