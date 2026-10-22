// Package checks runs named validation workflows with existing project
// commands and records trustworthy provenance: what was checked (commit and
// exact dirty state), with which definition, toolchain, environment and
// inputs, what happened, and which artifacts were produced. It never decides
// benchmark compatibility on a runner's behalf; it exposes identities that
// runners can key their own caches by.
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
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/naoray/anvil/internal/config"
	"github.com/naoray/anvil/internal/git"
	"github.com/naoray/anvil/internal/lifecycle"
	"github.com/naoray/anvil/internal/lockfile"
	"github.com/naoray/anvil/internal/utils"
	"github.com/naoray/anvil/internal/workenv"
)

// ToolProbe is the recorded output of one toolchain command.
type ToolProbe struct {
	Command  string `json:"command"`
	Output   string `json:"output"`
	ExitCode int    `json:"exit_code"`
}

// Platform identifies the machine class a result was produced on.
type Platform struct {
	OS   string `json:"os"`
	Arch string `json:"arch"`
}

// Identity is everything a result's validity depends on. Digest combines
// all parts; any change makes an earlier result stale.
type Identity struct {
	Git              git.WorkingState `json:"git"`
	DefinitionDigest string           `json:"definition_digest"`
	Toolchain        []ToolProbe      `json:"toolchain"`
	ToolchainDigest  string           `json:"toolchain_digest"`
	EnvNames         []string         `json:"environment_names"`
	EnvDigest        string           `json:"environment_digest"`
	Inputs           []string         `json:"inputs"`
	InputsDigest     string           `json:"inputs_digest"`
	// Resources are the shared resources the check declares, sorted by
	// name; ResourcesDigest is empty when it declares none.
	Resources       []ResourceIdentity `json:"resources"`
	ResourcesDigest string             `json:"resources_digest"`
	Platform        Platform           `json:"platform"`
	Digest          string             `json:"identity_digest"`
	// ContentEnvDigest covers only declared and Anvil-set variables (plus
	// the check's identity_env), with worktree-specific values normalized
	// (see contentEnvironmentDigest). It is independent of the worktree.
	ContentEnvDigest string `json:"content_environment_digest,omitempty"`
	// ContentToolchainDigest hashes the toolchain probes with this worktree's
	// path, runtime directory and other worktree-specific values normalized
	// as in the content environment. ToolchainDigest keeps the raw output.
	ContentToolchainDigest string `json:"content_toolchain_digest,omitempty"`
	// ContentDigest identifies the checked content independently of the
	// worktree (see ContentDigest); empty for a dirty checkout, which is
	// never reused across worktrees.
	ContentDigest string `json:"content_digest,omitempty"`
}

// IdentityRequest describes what to identify.
type IdentityRequest struct {
	WorktreePath string
	RuntimeDir   string
	Check        config.CheckConfig
	Env          *workenv.Environment
	Resolve      func(config.CheckCommand) (CommandTarget, error)
	Targets      []CommandTarget
	Unset        []string
	SecretEnv    []string
	Guards       []*lockfile.Lock
	// Resources are all declared shared resources with resolved paths; only
	// those named by Check.Resources are identified.
	Resources []SharedResource
	// RuntimeRoot hosts the machine-wide directory identity cache ("" disables it).
	RuntimeRoot string
	// CacheWrite lets directory identities create and update that cache.
	// Only execution sets it; read-only commands only read an existing cache.
	CacheWrite bool
	// Memo shares resource identities within one read-only invocation.
	Memo *ResourceMemo
	// Warn receives nonfatal problems (an unwritable identity cache).
	Warn func(string)
}

var probeTimeout = 15 * time.Second

const maxProbeOutput = 200

func digestFields(fields ...string) string {
	h := sha256.New()
	for _, field := range fields {
		writeField(h, field)
	}
	return hex.EncodeToString(h.Sum(nil))
}

func writeField(w io.Writer, value string) {
	if _, err := fmt.Fprintf(w, "%d:%s\n", len(value), value); err != nil {
		panic(err) // hash writers never fail
	}
}

// DefinitionDigest identifies the check definition.
func DefinitionDigest(check config.CheckConfig) string {
	encoded, err := json.Marshal(check)
	if err != nil {
		panic(fmt.Sprintf("encoding check definition: %v", err))
	}
	return digestFields(string(encoded))
}

// ComputeIdentity reads the working state, probes toolchains and hashes
// environment values, declared inputs and declared shared resources. It runs
// only the declared toolchain probe and resource identity commands, and
// writes nothing except the best-effort directory identity cache when
// req.CacheWrite is set.
func ComputeIdentity(ctx context.Context, req IdentityRequest) (identity Identity, err error) {
	gate, err := lifecycle.AcquireShared(req.WorktreePath)
	if err != nil {
		return Identity{}, err
	}
	defer func() { err = errors.Join(err, gate.Release()) }()
	req.Guards = append(append([]*lockfile.Lock{}, req.Guards...), gate)

	state, err := git.ReadWorkingState(req.WorktreePath)
	if err != nil {
		return Identity{}, err
	}
	identity = Identity{
		Git:              state,
		DefinitionDigest: DefinitionDigest(req.Check),
		Platform:         Platform{OS: runtime.GOOS, Arch: runtime.GOARCH},
	}
	envMap := req.Env.Map()
	identity.Toolchain, err = probeToolchain(ctx, req.WorktreePath, req.Check.Toolchain, req.Env, req.Unset, req.Guards)
	if err != nil {
		return Identity{}, err
	}
	var toolFields []string
	for _, probe := range identity.Toolchain {
		toolFields = append(toolFields, probe.Command, probe.Output, fmt.Sprint(probe.ExitCode))
	}
	identity.ToolchainDigest = digestFields(toolFields...)
	identity.ContentToolchainDigest = contentToolchainDigest(req, identity.Toolchain)

	// Resolve exactly the environments execution uses, including inheritance.
	targets := req.Targets
	if targets == nil {
		targets, err = resolveTargets(req)
		if err != nil {
			return Identity{}, err
		}
	}
	var envFields, contentFields []string
	names := map[string]bool{}
	for i, target := range targets {
		contentFields = append(contentFields, fmt.Sprint(i), contentEnvironmentDigest(req, target))
		fields := []string{target.Dir}
		for _, variable := range target.Env.Variables() {
			names[variable.Name] = true
			value := variable.Value
			if variable.Secret {
				value = "<secret>"
			}
			fields = append(fields, variable.Name+"="+value)
		}
		sort.Strings(fields[1:])
		envFields = append(envFields, fmt.Sprint(i), digestFields(fields...))
	}
	for name := range names {
		identity.EnvNames = append(identity.EnvNames, name)
	}
	sort.Strings(identity.EnvNames)
	identity.EnvDigest = digestFields(envFields...)
	identity.ContentEnvDigest = digestFields(append([]string{"anvil-check-content-env-v1"}, contentFields...)...)

	paths, err := ResolvePaths(req.WorktreePath, req.RuntimeDir, req.Check.Inputs, envMap)
	if err != nil {
		return Identity{}, fmt.Errorf("inputs: %w", err)
	}
	identity.Inputs = req.Check.Inputs
	if identity.Inputs == nil {
		identity.Inputs = []string{}
	}
	if identity.InputsDigest, err = pathsDigest(req.WorktreePath, req.RuntimeDir, paths); err != nil {
		return Identity{}, fmt.Errorf("inputs: %w", err)
	}
	if identity.Resources, identity.ResourcesDigest, err = resourceIdentities(ctx, req); err != nil {
		return Identity{}, err
	}
	fields := []string{
		"anvil-check-identity-v1", state.Digest, identity.DefinitionDigest, identity.ToolchainDigest,
		identity.EnvDigest, identity.InputsDigest, identity.Platform.OS, identity.Platform.Arch,
	}
	// Appended only when resources are declared, so checks without them keep
	// the digest of earlier versions and their stored results stay current.
	if len(identity.Resources) > 0 {
		fields = append(fields, "resources", identity.ResourcesDigest)
	}
	identity.Digest = digestFields(fields...)
	identity.ContentDigest = ContentDigest(identity)
	return identity, nil
}

func probeToolchain(ctx context.Context, dir string, commands []string, env *workenv.Environment, unset []string, guards []*lockfile.Lock) ([]ToolProbe, error) {
	probes := make([]ToolProbe, 0, len(commands))
	for _, command := range commands {
		probeCtx, cancel := context.WithTimeout(ctx, probeTimeout)
		cmd, finish, err := supervisedCommand(probeCtx, command, guards)
		if err != nil {
			cancel()
			return nil, err
		}
		cmd.Dir = dir
		cmd.Env = env.Environ(os.Environ(), unset, runtime.GOOS == "windows")
		output := &boundedBuffer{limit: 64 << 10}
		cmd.Stdout, cmd.Stderr = output, output
		err = cmd.Run()
		if closeErr := finish(); closeErr != nil {
			cancel()
			return nil, closeErr
		}
		probeErr := probeCtx.Err()
		cancel()
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if probeErr != nil {
			return nil, fmt.Errorf("toolchain probe timed out: %w", probeErr)
		}
		probe := ToolProbe{Command: command, Output: firstLine(output.String())}
		var exitErr *exec.ExitError
		switch {
		case errors.As(err, &exitErr):
			probe.ExitCode = exitErr.ExitCode()
		case err != nil:
			probe.ExitCode = -1
			probe.Output = firstLine(err.Error())
		}
		probes = append(probes, probe)
	}
	return probes, nil
}

func firstLine(text string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(text), "\n")
	line = strings.TrimSpace(line)
	if len(line) > maxProbeOutput {
		line = line[:maxProbeOutput]
	}
	return line
}

// ResolvePaths expands ${VAR} in patterns and globs them. Relative patterns
// are worktree-relative; absolute ones must stay inside the worktree or its
// runtime directory.
func ResolvePaths(worktreePath, runtimeDir string, patterns []string, env map[string]string) (map[string][]string, error) {
	out := map[string][]string{}
	for _, pattern := range patterns {
		expanded := utils.ExpandEnv(pattern, env)
		if !filepath.IsAbs(expanded) {
			if err := config.ValidateRelativePath(expanded); err != nil {
				return nil, err
			}
			expanded = filepath.Join(worktreePath, expanded)
		}
		if !within(worktreePath, expanded) && (runtimeDir == "" || !within(runtimeDir, expanded)) {
			return nil, fmt.Errorf("path %q must stay inside the worktree or its runtime directory", pattern)
		}
		if err := validateCheckPath(worktreePath, runtimeDir, expanded); err != nil {
			return nil, err
		}
		matches, err := filepath.Glob(expanded)
		if err != nil {
			return nil, fmt.Errorf("pattern %q: %w", pattern, err)
		}
		for _, match := range matches {
			if err := validateCheckPath(worktreePath, runtimeDir, match); err != nil {
				return nil, err
			}
		}
		sort.Strings(matches)
		out[pattern] = matches
	}
	return out, nil
}

func within(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

func displayPath(worktreePath, runtimeDir, path string) string {
	if within(worktreePath, path) {
		if rel, err := filepath.Rel(worktreePath, path); err == nil {
			return filepath.ToSlash(rel)
		}
	}
	if runtimeDir != "" && within(runtimeDir, path) {
		if rel, err := filepath.Rel(runtimeDir, path); err == nil {
			return "${ANVIL_RUNTIME_DIR}/" + filepath.ToSlash(rel)
		}
	}
	return path
}

// pathsDigest content-addresses resolved paths, recording missing patterns.
func pathsDigest(worktreePath, runtimeDir string, resolved map[string][]string) (string, error) {
	patterns := make([]string, 0, len(resolved))
	for pattern := range resolved {
		patterns = append(patterns, pattern)
	}
	sort.Strings(patterns)
	h := sha256.New()
	for _, pattern := range patterns {
		writeField(h, "pattern:"+pattern)
		if len(resolved[pattern]) == 0 {
			writeField(h, "missing")
		}
		for _, match := range resolved[pattern] {
			err := filepath.WalkDir(match, func(path string, entry fs.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if err := validateCheckPath(worktreePath, runtimeDir, path); err != nil {
					return err
				}
				if !entry.Type().IsRegular() && !entry.IsDir() {
					return fmt.Errorf("unsupported input type: %s", path)
				}
				writeField(h, displayPath(worktreePath, runtimeDir, path))
				if entry.Type().IsRegular() {
					sum, _, err := fileDigest(path)
					if err != nil {
						return err
					}
					writeField(h, sum)
				}
				return nil
			})
			if err != nil {
				return "", err
			}
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func fileDigest(path string) (string, int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	h := sha256.New()
	size, copyErr := io.Copy(h, file)
	if err := errors.Join(copyErr, file.Close()); err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), size, nil
}

// effectiveEnvironment snapshots inherited values once. Only hashes, names and
// secret placeholders are persisted; execution uses this same in-memory set.
// Inherited values are marked workenv.SourceInherited; declared and Anvil-set
// variables keep their source.
func effectiveEnvironment(env *workenv.Environment, unset, secrets []string) *workenv.Environment {
	out := &workenv.Environment{}
	for _, entry := range env.Environ(os.Environ(), unset, runtime.GOOS == "windows") {
		name, value, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		out.Set(workenv.Variable{Name: name, Value: value, Source: workenv.SourceInherited, Secret: workenv.IsSecretName(name, secrets)})
	}
	for _, variable := range env.Variables() {
		variable.Secret = variable.Secret || workenv.IsSecretName(variable.Name, secrets)
		out.Set(variable)
	}
	return out
}

func resolveTargets(req IdentityRequest) ([]CommandTarget, error) {
	targets := make([]CommandTarget, 0, len(req.Check.Commands))
	for _, command := range req.Check.Commands {
		target := CommandTarget{Dir: req.WorktreePath, Env: req.Env}
		if req.Resolve != nil {
			var err error
			target, err = req.Resolve(command)
			if err != nil {
				return nil, err
			}
		}
		target.Env = effectiveEnvironment(target.Env, req.Unset, req.SecretEnv)
		targets = append(targets, target)
	}
	return targets, nil
}

// Reject links anywhere below an allowed root, including ancestors of glob
// matches and links discovered while walking a directory. The roots themselves
// may use the platform's canonical aliases (for example /var on macOS).
func validateCheckPath(worktreePath, runtimeDir, path string) error {
	root := worktreePath
	if !within(root, path) {
		if runtimeDir == "" || !within(runtimeDir, path) {
			return fmt.Errorf("path %q must stay inside the worktree or its runtime directory", path)
		}
		root = runtimeDir
	}
	for current := filepath.Clean(path); current != filepath.Clean(root); current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink inputs and artifacts are unsupported: %s", current)
		}
	}
	return nil
}

// boundedBuffer keeps the first limit bytes written and discards the rest.
type boundedBuffer struct {
	limit int
	data  []byte
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	if room := b.limit - len(b.data); room > 0 {
		if len(p) < room {
			room = len(p)
		}
		b.data = append(b.data, p[:room]...)
	}
	return len(p), nil
}

func (b *boundedBuffer) String() string { return string(b.data) }
