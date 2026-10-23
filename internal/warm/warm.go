// Package warm reuses only attested, portable dependency trees. It never copies
// workspace state, application files, or Git metadata.
package warm

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

const Cold = "cold"

// ColdUnverified reports an existing node_modules without warm provenance for
// this worktree (e.g. left by an interrupted warm rebuild). It is never reused
// or certified; the normal installer replaces it.
const ColdUnverified = "cold (existing unverified tree)"

// IsCold reports whether a Reuse result requires the normal installer.
func IsCold(result string) bool { return result == Cold || result == ColdUnverified }

type Install struct {
	root, tree, toolchain, inputs string
	args                          []string
	probe                         func(string) (string, error)
	policy                        Policy
	// warmedFrom is the source worktree of a successful Reuse.
	warmedFrom string
}

// stamp is the dependency provenance kept in .anvil.local. Trust fields are
// set only for opt-in node_modules installs and never contain secrets.
type stamp struct {
	Inputs                string           `yaml:"inputs"`
	Tree                  string           `yaml:"tree"`
	TrustInstallScripts   bool             `yaml:"trust_install_scripts,omitempty"`
	TrustedScripts        []ScriptIdentity `yaml:"trusted_scripts,omitempty"`
	InstallScriptPackages []string         `yaml:"install_script_packages,omitempty"`
	Rebuild               string           `yaml:"rebuild,omitempty"`
	Source                string           `yaml:"source,omitempty"`
}

// Prepare recognizes only exact, lockfile-backed installation commands. Other
// commands still execute normally but cannot certify a reusable dependency tree.
func Prepare(root, name, binary string, args []string) *Install {
	return PrepareWithPolicy(root, name, binary, args, Policy{})
}

// PrepareWithPolicy is Prepare with the project's declared warm policy. The
// policy only applies to node_modules; the zero Policy is the default.
func PrepareWithPolicy(root, name, binary string, args []string, policy Policy) *Install {
	if len(args) != 1 {
		return nil
	}
	tree := ""
	probes := []string{}
	switch {
	case name == "node.npm" && binary == "npm" && args[0] == "ci":
		tree = "node_modules"
		probes = []string{"node", "npm"}
	case name == "php.composer" && binary == "composer" && args[0] == "install":
		tree = "vendor"
		probes = []string{"php", "composer"}
	default:
		return nil
	}
	if err := supportedInputs(root, tree); err != nil {
		return nil
	}
	probe := func(path string) (string, error) { return toolchainFingerprint(path, probes) }
	if tree != "node_modules" {
		policy = Policy{}
	}
	toolchain, err := probe(root)
	if err != nil {
		return nil
	}
	i, err := prepareWithPolicy(root, tree, toolchain, args, policy)
	if err != nil || i == nil {
		return nil
	}
	i.probe = probe
	return i
}

func toolchainFingerprint(root string, probes []string) (string, error) {
	h := sha256.New()
	hashText(h, "%s %s anvil-warm-v1\n", runtime.GOOS, runtime.GOARCH)
	for _, tool := range probes {
		path, err := exec.LookPath(tool)
		if err != nil {
			return "", fmt.Errorf("unavailable toolchain/configuration")
		}
		probeArgs := []string{"--version"}
		if tool == "node" {
			probeArgs = []string{"-p", "JSON.stringify({versions:process.versions,platform:process.platform,arch:process.arch})"}
		}
		if tool == "php" {
			probeArgs = []string{"-r", `$e=get_loaded_extensions();sort($e);foreach($e as $x){echo $x,":",phpversion($x),"\n";}echo PHP_VERSION,"\n",PHP_OS_FAMILY,"\n",php_uname("m"),"\n",PHP_INT_SIZE,"\n",json_encode(ini_get_all(null,false));`}
		}
		cmd := exec.Command(path, probeArgs...)
		cmd.Dir = root
		out, err := cmd.Output()
		if err != nil {
			return "", fmt.Errorf("unavailable toolchain/configuration")
		}
		hashText(h, "%s\x00%s\x00%s\x00", tool, path, out)

		if tool == "composer" {
			identityData, err := composerRootIdentity(path, root)
			if err != nil {
				return "", err
			}
			var identity struct {
				Name     string   `json:"name"`
				Versions []string `json:"versions"`
				Source   struct {
					Reference string `json:"reference"`
				} `json:"source"`
			}
			if err = json.Unmarshal(identityData, &identity); err != nil {
				return "", err
			}
			if identity.Name == "" || len(identity.Versions) == 0 {
				return "", fmt.Errorf("unverifiable Composer root identity")
			}
			normalized, err := json.Marshal(identity)
			if err != nil {
				return "", err
			}
			h.Write(normalized)
			canonicalRoot, err := filepath.EvalSymlinks(root)
			if err != nil {
				return "", err
			}
			for _, setting := range []struct{ name, path string }{{"vendor-dir", "vendor"}, {"bin-dir", filepath.Join("vendor", "bin")}} {
				command := exec.Command(path, "--no-plugins", "config", setting.name, "--absolute", "--no-interaction")
				command.Dir = root
				data, err := command.Output()
				if err != nil {
					return "", err
				}
				if filepath.Clean(strings.TrimSpace(string(data))) != filepath.Join(canonicalRoot, setting.path) {
					return "", fmt.Errorf("nonstandard Composer dependency paths require cold install")
				}
			}
			command := exec.Command(path, "--no-plugins", "config", "--global", "home", "--no-interaction")
			command.Dir = root
			data, err := command.Output()
			if err != nil {
				return "", err
			}
			home := strings.TrimSpace(string(data))
			if !filepath.IsAbs(home) {
				return "", fmt.Errorf("unverifiable Composer home")
			}
			if _, err := os.Stat(filepath.Join(home, "vendor", "composer", "installed.json")); !errors.Is(err, os.ErrNotExist) {
				return "", fmt.Errorf("global Composer packages require cold install")
			}
			for _, name := range []string{"config.json", "auth.json"} {
				if !hashOptional(h, filepath.Join(home, name)) {
					return "", fmt.Errorf("unverifiable Composer configuration")
				}
			}
		}
		if tool == "npm" {
			prefixCommand := exec.Command(path, "prefix")
			prefixCommand.Dir = root
			prefixData, err := prefixCommand.Output()
			if err != nil {
				return "", err
			}
			canonicalRoot, err := filepath.EvalSymlinks(root)
			if err != nil {
				return "", err
			}
			if filepath.Clean(strings.TrimSpace(string(prefixData))) != canonicalRoot {
				return "", fmt.Errorf("nonstandard npm project root requires cold install")
			}
			command := exec.Command(path, "config", "list", "--json")
			command.Dir = root
			data, err := command.Output()
			if err != nil {
				return "", err
			}
			var settings map[string]any
			if err = json.Unmarshal(data, &settings); err != nil {
				return "", err
			}
			canonical, err := json.Marshal(settings)
			if err != nil {
				return "", err
			}
			h.Write(canonical)
		}
	}
	// Include configuration environment without storing credentials or values.
	env := os.Environ()
	sort.Strings(env)
	for _, v := range env {
		key, _, _ := strings.Cut(v, "=")
		upper := strings.ToUpper(key)
		if strings.HasPrefix(upper, "NPM_") || strings.HasPrefix(upper, "NODE_") || strings.HasPrefix(upper, "COMPOSER_") || strings.HasPrefix(upper, "PHP_") {
			hashText(h, "%s\n", v)
		}
	}
	// Ancestor configuration can affect the install too; differing or unreadable
	// configuration goes cold. Hash contents, never persist secrets.
	for _, name := range []string{".npmrc", ".yarnrc", "auth.json"} {
		dir := filepath.Dir(root)
		for {
			if !hashOptional(h, filepath.Join(dir, name)) {
				return "", fmt.Errorf("unavailable toolchain/configuration")
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("unavailable toolchain/configuration")
	}
	for _, name := range []string{".npmrc", ".config/composer/config.json", ".composer/config.json", ".config/composer/composer.lock", ".composer/composer.lock"} {
		if !hashOptional(h, filepath.Join(home, name)) {
			return "", fmt.Errorf("unavailable toolchain/configuration")
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func prepare(root, tree, toolchain string, args []string) (*Install, error) {
	return prepareWithPolicy(root, tree, toolchain, args, Policy{})
}

func prepareWithPolicy(root, tree, toolchain string, args []string, policy Policy) (*Install, error) {
	if tree != "vendor" && tree != "node_modules" {
		return nil, fmt.Errorf("unsupported dependency tree %q", tree)
	}
	if tree != "node_modules" && policy != (Policy{}) {
		return nil, fmt.Errorf("warm policy applies only to node_modules")
	}
	i := &Install{root: root, tree: tree, toolchain: toolchain, args: append([]string(nil), args...), policy: policy}
	digest, err := i.fingerprint()
	if err != nil {
		return nil, nil
	}
	i.inputs = digest
	return i, nil
}

func (i *Install) Tree() string { return i.tree }

// supportedInputs deliberately rejects effective lock/manifest modes that this
// package does not attest. Repeat this check before probing and before stamping.
func supportedInputs(root, tree string) error {
	if tree == "node_modules" {
		if _, err := os.Lstat(filepath.Join(root, "npm-shrinkwrap.json")); !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("present or uncertain npm shrinkwrap requires cold install")
		}
	}
	if tree == "vendor" {
		if manifest := os.Getenv("COMPOSER"); manifest != "" && manifest != "composer.json" {
			return fmt.Errorf("alternate Composer manifest requires cold install")
		}
	}
	return nil
}

func (i *Install) fingerprint() (string, error) {
	if err := supportedInputs(i.root, i.tree); err != nil {
		return "", err
	}
	files := []string{"package.json", "package-lock.json", ".npmrc", ".node-version", ".nvmrc", ".tool-versions", "anvil.yaml"}
	if i.tree == "vendor" {
		files = []string{"composer.json", "composer.lock", "auth.json", "php.ini", ".php-version", ".tool-versions", "anvil.yaml"}
	}
	toolchain := i.toolchain
	if i.probe != nil {
		var err error
		toolchain, err = i.probe(i.root)
		if err != nil {
			return "", err
		}
	}
	h := sha256.New()
	hashText(h, "%s %s %v\n", toolchain, i.tree, i.args)
	if i.policy.TrustInstallScripts {
		// Only opt-in installs hash the policy, so default fingerprints (and
		// existing provenance) are unchanged; toggling it invalidates.
		hashText(h, "trust-install-scripts\x00%q\n", i.policy.Rebuild)
	}
	for n, name := range files {
		path := filepath.Join(i.root, name)
		info, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) && n > 1 {
			hashText(h, "%s absent\n", name)
			continue
		}
		if err != nil {
			return "", err
		}
		if !info.Mode().IsRegular() {
			return "", fmt.Errorf("nonregular install input: %s", name)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		if n < 2 {
			if err := portableManifest(i.tree, data, n == 1, i.policy.TrustInstallScripts); err != nil {
				return "", err
			}
		}
		if n == 0 && i.tree == "node_modules" {
			gyp, err := hasBindingGyp(i.root)
			if err != nil {
				return "", err
			}
			scripts, _, _, err := manifestScripts(data, gyp)
			if err != nil {
				return "", err
			}
			if len(scripts) > 0 {
				if !i.policy.TrustInstallScripts {
					return "", fmt.Errorf("root lifecycle scripts require trusted warm reuse")
				}
				// Warm hits never run root lifecycle scripts, whose effects
				// (e.g. codegen outside node_modules) only a rebuild reproduces.
				if i.policy.Rebuild == "" {
					return "", fmt.Errorf("root lifecycle scripts require a warm rebuild command")
				}
			}
		}
		hashText(h, "%s\x00%o\x00%d\x00", name, info.Mode().Perm(), len(data))
		h.Write(data)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// portableManifest rejects inputs whose installs are not attested. trusted
// (node_modules opt-in only) admits install scripts, whose identities are
// then verified separately by scriptIdentities.
func portableManifest(tree string, data []byte, lock, trusted bool) error {
	trusted = trusted && tree == "node_modules"
	if tree == "node_modules" && lock {
		var metadata struct {
			Version  int `json:"lockfileVersion"`
			Packages map[string]struct {
				Integrity string `json:"integrity"`
			} `json:"packages"`
		}
		if err := json.Unmarshal(data, &metadata); err != nil {
			return err
		}
		if metadata.Version < 2 || metadata.Version > 3 || metadata.Packages == nil {
			return fmt.Errorf("npm reuse requires a complete v2/v3 lockfile")
		}
		for path, pkg := range metadata.Packages {
			if path != "" && pkg.Integrity == "" {
				return fmt.Errorf("npm dependency without integrity metadata requires cold install")
			}
		}
	}

	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	// Install scripts/plugins and local links may embed workspace-specific paths
	// or generate artifacts outside the allowlist; these installs stay cold.
	var walk func(any) error
	walk = func(v any) error {
		switch x := v.(type) {
		case map[string]any:
			for k, v := range x {
				if k == "workspaces" || k == "hasInstallScript" && v == true && !trusted || k == "link" && v == true || k == "type" && (v == "path" || tree == "vendor" && v == "composer-plugin") {
					return fmt.Errorf("nonportable dependency input: %s", k)
				}
				if k == "scripts" && !trusted {
					if m, ok := v.(map[string]any); ok {
						for script := range m {
							if tree == "vendor" || script == "preinstall" || script == "install" || script == "postinstall" || script == "prepare" || script == "prepublish" || script == "preprepare" || script == "postprepare" {
								return fmt.Errorf("install scripts require cold setup")
							}
						}
					}
				}
				if err := walk(v); err != nil {
					return err
				}
			}
		case []any:
			for _, v := range x {
				if err := walk(v); err != nil {
					return err
				}
			}
		case string:
			if strings.HasPrefix(x, "file:") || strings.HasPrefix(x, "link:") || strings.HasPrefix(x, "workspace:") {
				return fmt.Errorf("local dependency requires cold install")
			}
		}
		return nil
	}
	return walk(value)
}

func hashOptional(h hash.Hash, path string) bool {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		hashText(h, "absent\n")
		return true
	}
	if err != nil {
		return false
	}
	hashText(h, "%d\x00", len(b))
	_, err = h.Write(b)
	return err == nil
}

// Record is called only after a successful install. Changed inputs and unsafe
// trees invalidate previous provenance instead of certifying uncertain output.
func (i *Install) Record() error {
	_, err := i.record()
	return err
}

// RecordWarm records a warm tree after a post-warm rebuild and fails when
// the rebuilt tree can no longer be certified (its provenance is cleared).
func (i *Install) RecordWarm() error {
	record, err := i.record()
	if err != nil {
		return err
	}
	if record.Inputs == "" {
		return fmt.Errorf("rebuilt %s can no longer be verified as a portable dependency tree", i.tree)
	}
	return nil
}

func (i *Install) record() (stamp, error) {
	current, err := i.fingerprint()
	record := stamp{}
	if err == nil && current == i.inputs {
		tree, e := treeDigest(filepath.Join(i.root, i.tree), i.digestOptions())
		if e == nil {
			after, e := i.fingerprint()
			if e == nil && after == i.inputs {
				stable, e := treeDigest(filepath.Join(i.root, i.tree), i.digestOptions())
				if e == nil && stable == tree {
					record = i.trustStamp(stamp{Inputs: i.inputs, Tree: tree})
				}
			}
		}
	}
	return record, writeStamp(i.root, i.tree, record)
}

// trustStamp adds the trusted-script provenance of opt-in installs. Script
// identities that cannot be verified clear the record instead.
func (i *Install) trustStamp(record stamp) stamp {
	if !i.policy.TrustInstallScripts {
		if i.tree != "node_modules" {
			return record
		}
		scripts, packages, err := scriptIdentities(i.root, filepath.Join(i.root, i.tree))
		if err != nil || len(scripts) > 0 || len(packages) > 0 {
			return stamp{}
		}
		return record
	}
	scripts, packages, err := scriptIdentities(i.root, filepath.Join(i.root, i.tree))
	if err != nil {
		return stamp{}
	}
	record.TrustInstallScripts = true
	record.TrustedScripts = scripts
	record.InstallScriptPackages = packages
	record.Rebuild = i.policy.Rebuild
	record.Source = i.warmedFrom
	return record
}

func (i *Install) digestOptions() digestOptions {
	return digestOptions{
		nativeAddons:   i.policy.TrustInstallScripts,
		workspacePaths: i.policy.TrustInstallScripts && i.policy.Rebuild != "",
	}
}

// Reuse stages a verified candidate before publishing it. A cold result makes
// no destination dependency tree; unexpected copy errors are returned.
func (i *Install) Reuse(source, mode string) (string, error) { return i.reuse(source, mode, copyFile) }

type fileCopier func(string, string, os.FileMode, string) (string, error)

func (i *Install) reuse(source, mode string, copyOne fileCopier) (result string, err error) {
	if mode != "auto" && mode != "copy" {
		return Cold, fmt.Errorf("copy mode must be auto or copy")
	}
	dest := filepath.Join(i.root, i.tree)
	if _, e := os.Lstat(dest); e == nil {
		if i.tree == "node_modules" {
			if own, e := readStamp(i.root, i.tree); e == nil && own.Inputs == "" {
				return ColdUnverified, nil
			}
		}
		return Cold, fmt.Errorf("warm destination already exists: %s", dest)
	} else if !errors.Is(e, os.ErrNotExist) {
		return Cold, e
	}
	expected, e := readStamp(source, i.tree)
	if e != nil || expected.Inputs == "" || expected.Tree == "" {
		return Cold, nil
	}
	inputs, e := i.fingerprint()
	if e != nil || inputs != i.inputs || expected.Inputs != i.inputs {
		return Cold, nil
	}
	srcInstall := *i
	srcInstall.root = source
	inputs, e = srcInstall.fingerprint()
	if e != nil || inputs != i.inputs {
		return Cold, nil
	}
	if expected.TrustInstallScripts != i.policy.TrustInstallScripts || expected.Rebuild != i.policy.Rebuild {
		return Cold, nil
	}
	src := filepath.Join(source, i.tree)
	digest, e := treeDigest(src, i.digestOptions())
	if e != nil || digest != expected.Tree || !i.trustedScriptsMatch(expected, source, src) {
		return Cold, nil
	}
	stage, e := os.MkdirTemp(i.root, ".anvil-warm-")
	if e != nil {
		return Cold, e
	}
	defer func() {
		if e := removeStage(stage); e != nil {
			err = errors.Join(err, fmt.Errorf("removing warm stage: %w", e))
		}
	}()
	candidate := filepath.Join(stage, i.tree)
	result, e = copyTree(src, candidate, mode, copyOne)
	if errors.Is(e, errSourceChanged) || errors.Is(e, os.ErrNotExist) {
		return Cold, nil
	}
	if e != nil {
		return Cold, e
	}
	// Verify both ends again: user-run installers need not honor any Anvil lock.
	for _, root := range []string{src, candidate} {
		digest, e = treeDigest(root, i.digestOptions())
		if e != nil || digest != expected.Tree {
			return Cold, nil
		}
	}
	// The candidate is checked against the target's manifest and lockfile.
	if !i.trustedScriptsMatch(expected, source, src) || !i.trustedScriptsMatch(expected, i.root, candidate) {
		return Cold, nil
	}
	for _, install := range []*Install{i, &srcInstall} {
		inputs, e = install.fingerprint()
		if e != nil || inputs != i.inputs {
			return Cold, nil
		}
	}
	if e = publish(candidate, dest); e != nil {
		return Cold, fmt.Errorf("publishing warm dependencies: %w", e)
	}
	i.warmedFrom = source

	return result, nil
}

// digestOptions relaxes treeDigest for trusted node_modules installs only.
type digestOptions struct {
	// nativeAddons admits compiled .node files.
	nativeAddons bool
	// workspacePaths admits files embedding the worktree path; set only
	// when a rebuild regenerates them in the destination (see CheckDetached).
	workspacePaths bool
}

func treeDigest(root string, opts digestOptions) (string, error) {
	info, err := os.Lstat(root)
	if err != nil {
		return "", err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("dependency root must be a directory")
	}
	given := root
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	// Tools may record the worktree under the given or the resolved path.
	needles := sourceNeedles(sourcePaths(filepath.Dir(given), filepath.Dir(root)), runtime.GOOS)
	fold := foldsCase(runtime.GOOS)
	h := sha256.New()
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		base := strings.ToLower(entry.Name())
		if base == ".git" || base == ".env" || base == ".anvil.local" || strings.HasSuffix(base, ".sqlite") || strings.HasSuffix(base, ".sqlite3") || strings.HasSuffix(base, ".db") {
			return fmt.Errorf("private state/database in dependency tree: %s", rel)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		hashText(h, "%s\x00%o\x00", filepath.ToSlash(rel), info.Mode())
		switch {
		case info.IsDir():
			return nil
		case info.Mode()&os.ModeSymlink != 0:
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			if filepath.IsAbs(link) {
				return fmt.Errorf("absolute dependency symlink: %s", rel)
			}
			resolved, err := filepath.EvalSymlinks(path)
			if err != nil {
				return err
			}
			relative, err := filepath.Rel(root, resolved)
			if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
				return fmt.Errorf("escaping dependency symlink: %s", rel)
			}
			hashText(h, "%s\n", link)
			return nil
		case info.Mode().IsRegular():
			if strings.HasSuffix(path, ".node") && !opts.nativeAddons {
				return fmt.Errorf("native addon needs cold install")
			}
			f, err := os.Open(path)
			if err != nil {
				return err
			}
			hashText(h, "%d\x00", info.Size())
			var readErr error
			if opts.workspacePaths {
				_, readErr = io.Copy(h, f)
			} else {
				_, readErr = hashPortableFile(h, f, needles, fold)
			}
			return errors.Join(readErr, f.Close())
		default:
			return fmt.Errorf("special dependency file: %s", rel)
		}
	})
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// hashPortableFile hashes f and fails when it contains any needle (see
// sourceNeedles); fold matches ASCII case-insensitively.
func hashPortableFile(h io.Writer, f io.Reader, needles []string, fold bool) (int64, error) {
	keep := 0
	for _, needle := range needles {
		keep = max(keep, len(needle))
	}
	tail := []byte{}
	buf := make([]byte, 64*1024)
	var total int64
	for {
		n, err := f.Read(buf)
		if n > 0 {
			current := append(tail, buf[:n]...)
			if fold {
				foldASCII(current[len(tail):])
			}
			for _, needle := range needles {
				if bytes.Contains(current, []byte(needle)) {
					return total, fmt.Errorf("dependency contains workspace-specific path")
				}
			}
			if _, e := h.Write(buf[:n]); e != nil {
				return total, e
			}
			total += int64(n)
			tail = append([]byte(nil), current[len(current)-min(keep, len(current)):]...)
		}
		if err == io.EOF {
			return total, nil
		}
		if err != nil {
			return total, err
		}
	}
}

func hashText(h hash.Hash, format string, args ...any) {
	if _, err := io.WriteString(h, fmt.Sprintf(format, args...)); err != nil {
		panic("hash write failed: " + err.Error())
	}
}

// Composer's self query can create its vendor directory. Keep all probe output
// directories private so eligibility checks do not mutate either workspace.
func composerRootIdentity(path, root string) (data []byte, err error) {
	probe, err := os.MkdirTemp("", "anvil-composer-probe-")
	if err != nil {
		return nil, err
	}
	defer func() {
		if e := os.RemoveAll(probe); e != nil {
			err = errors.Join(err, fmt.Errorf("cleaning Composer probe: %w", e))
		}
	}()
	command := exec.Command(path, "--no-plugins", "--no-scripts", "show", "--self", "--format=json", "--no-interaction")
	command.Dir = root
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if !strings.EqualFold(key, "COMPOSER_VENDOR_DIR") && !strings.EqualFold(key, "COMPOSER_BIN_DIR") && !strings.EqualFold(key, "COMPOSER_CACHE_DIR") {
			command.Env = append(command.Env, entry)
		}
	}
	command.Env = append(command.Env, "COMPOSER_VENDOR_DIR="+filepath.Join(probe, "vendor"), "COMPOSER_BIN_DIR="+filepath.Join(probe, "bin"), "COMPOSER_CACHE_DIR="+filepath.Join(probe, "cache"))
	return command.Output()
}
