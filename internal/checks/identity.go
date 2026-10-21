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
	Platform         Platform         `json:"platform"`
	Digest           string           `json:"identity_digest"`
}

// IdentityRequest describes what to identify.
type IdentityRequest struct {
	WorktreePath string
	RuntimeDir   string
	Check        config.CheckConfig
	Env          *workenv.Environment
	// Resolve returns each command's directory and environment; nil uses
	// Env in the worktree for every command.
	Resolve func(config.CheckCommand) (CommandTarget, error)
	// Unset names variables removed from the inherited environment.
	Unset []string
	// SecretEnv declares additional secret variable names.
	SecretEnv []string
}

// probeTimeout bounds each toolchain probe (variable for tests).
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
// environment values and declared inputs. It runs only the declared
// toolchain probe commands and writes nothing.
func ComputeIdentity(ctx context.Context, req IdentityRequest) (Identity, error) {
	state, err := git.ReadWorkingState(req.WorktreePath)
	if err != nil {
		return Identity{}, err
	}
	identity := Identity{
		Git:              state,
		DefinitionDigest: DefinitionDigest(req.Check),
		Platform:         Platform{OS: runtime.GOOS, Arch: runtime.GOARCH},
	}
	envMap := req.Env.Map()
	identity.Toolchain, err = probeToolchain(ctx, req.WorktreePath, req.Check.Toolchain, req.Env)
	if err != nil {
		return Identity{}, err
	}
	var toolFields []string
	for _, probe := range identity.Toolchain {
		toolFields = append(toolFields, probe.Command, probe.Output, fmt.Sprint(probe.ExitCode))
	}
	identity.ToolchainDigest = digestFields(toolFields...)

	if identity.EnvNames, identity.EnvDigest, err = commandEnvDigest(req); err != nil {
		return Identity{}, err
	}

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
	identity.Digest = digestFields(
		"anvil-check-identity-v1", state.Digest, identity.DefinitionDigest, identity.ToolchainDigest,
		identity.EnvDigest, identity.InputsDigest, identity.Platform.OS, identity.Platform.Arch,
	)
	return identity, nil
}

// volatileEnv names vary between shells and sessions without changing what
// a command does; they never make a result stale.
var volatileEnv = map[string]bool{
	"_": true, "PWD": true, "OLDPWD": true, "SHLVL": true, "COLUMNS": true, "LINES": true,
	"TERM_SESSION_ID": true, "ITERM_SESSION_ID": true, "SECURITYSESSIONID": true, "WINDOWID": true,
	"KITTY_WINDOW_ID": true, "WEZTERM_PANE": true, "TMUX": true, "TMUX_PANE": true, "STY": true,
	"SSH_AUTH_SOCK": true, "SSH_AGENT_PID": true, "SSH_CLIENT": true, "SSH_CONNECTION": true, "SSH_TTY": true,
	"GPG_TTY": true, "XDG_SESSION_ID": true, "XDG_VTNR": true, "VSCODE_IPC_HOOK_CLI": true,
}

// commandEnvDigest fingerprints the environment each command receives:
// Anvil's variables, component and command values, and the inherited
// environment, minus volatile session variables. Values are hashed, never
// stored; secret values are excluded so a low-entropy secret cannot be
// recovered from the digest, and only their presence counts.
func commandEnvDigest(req IdentityRequest) ([]string, string, error) {
	names := map[string]bool{}
	h := sha256.New()
	writeField(h, "anvil-check-env-v2")
	for i, command := range req.Check.Commands {
		target := CommandTarget{Env: req.Env}
		if req.Resolve != nil {
			resolved, err := req.Resolve(command)
			if err != nil {
				return nil, "", fmt.Errorf("command %d environment: %w", i+1, err)
			}
			target = resolved
		}
		secret := map[string]bool{}
		for _, variable := range target.Env.Variables() {
			secret[variable.Name] = variable.Secret
		}
		var fields []string
		for _, entry := range target.Env.Environ(os.Environ(), req.Unset, false) {
			name, value, ok := strings.Cut(entry, "=")
			if !ok || volatileEnv[name] {
				continue
			}
			if secret[name] || workenv.IsSecretName(name, req.SecretEnv) {
				value = "<secret>"
			}
			names[name] = true
			fields = append(fields, name+"="+value)
		}
		sort.Strings(fields)
		writeField(h, fmt.Sprintf("command:%d", i))
		for _, field := range fields {
			writeField(h, field)
		}
	}
	sorted := make([]string, 0, len(names))
	for name := range names {
		sorted = append(sorted, name)
	}
	sort.Strings(sorted)
	return sorted, hex.EncodeToString(h.Sum(nil)), nil
}

// probeToolchain runs each probe supervised like a check command: in its
// own process group, killed with it on timeout or cancellation, with output
// bounded. A cancelled parent context aborts identification.
func probeToolchain(ctx context.Context, dir string, commands []string, env *workenv.Environment) ([]ToolProbe, error) {
	probes := make([]ToolProbe, 0, len(commands))
	for _, command := range commands {
		probe, err := runProbe(ctx, dir, command, env)
		if err != nil {
			return nil, err
		}
		probes = append(probes, probe)
	}
	return probes, nil
}

func runProbe(ctx context.Context, dir, command string, env *workenv.Environment) (ToolProbe, error) {
	probeCtx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	cmd := exec.CommandContext(probeCtx, "sh", "-c", command)
	cmd.Dir = dir
	cmd.Env = env.Environ(os.Environ(), nil, runtime.GOOS == "windows")
	output := &boundedBuffer{limit: 64 << 10}
	cmd.Stdout, cmd.Stderr = output, output
	configureGroup(cmd)
	cmd.WaitDelay = time.Second
	err := cmd.Run()
	killGroup(cmd)
	if ctx.Err() != nil {
		return ToolProbe{}, fmt.Errorf("toolchain probe %q: %w", command, ctx.Err())
	}
	probe := ToolProbe{Command: command, Output: firstLine(output.String())}
	var exitErr *exec.ExitError
	switch {
	case probeCtx.Err() != nil:
		probe.ExitCode, probe.Output = -1, "timed out after "+probeTimeout.String()
	case errors.As(err, &exitErr):
		probe.ExitCode = exitErr.ExitCode()
	case err != nil:
		probe.ExitCode = -1
		probe.Output = firstLine(err.Error())
	}
	return probe, nil
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
		matches, err := filepath.Glob(expanded)
		if err != nil {
			return nil, fmt.Errorf("pattern %q: %w", pattern, err)
		}
		sort.Strings(matches)
		for _, match := range matches {
			if !resolvesWithin(worktreePath, runtimeDir, match) {
				return nil, fmt.Errorf("path %q must stay inside the worktree or its runtime directory (%s resolves outside)", pattern, match)
			}
		}
		out[pattern] = matches
	}
	return out, nil
}

// resolvesWithin reports whether path, after following symlinks, is inside
// the canonical worktree or runtime directory.
func resolvesWithin(worktreePath, runtimeDir, path string) bool {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return false
	}
	if within(canonicalRoot(worktreePath), resolved) {
		return true
	}
	return runtimeDir != "" && within(canonicalRoot(runtimeDir), resolved)
}

func canonicalRoot(root string) string {
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		return resolved
	}
	return root
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
			err := walkContained(worktreePath, runtimeDir, match, func(label, path string, info fs.FileInfo) error {
				writeField(h, label)
				if info.Mode().IsRegular() {
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

// walkContained visits start and everything below it in lexical order,
// following symlinks. Every link must resolve inside the worktree or runtime
// directory; visit receives the path as reached (label, display form), the
// real path and the target's FileInfo, so link targets contribute their own
// content. Directory cycles are visited once.
func walkContained(worktreePath, runtimeDir, start string, visit func(label, path string, info fs.FileInfo) error) error {
	seen := map[string]bool{}
	var walk func(path, label string) error
	walk = func(path, label string) error {
		real, err := filepath.EvalSymlinks(path)
		if err != nil {
			return err
		}
		if !resolvesWithin(worktreePath, runtimeDir, real) {
			return fmt.Errorf("path %s must stay inside the worktree or its runtime directory (resolves to %s)", label, real)
		}
		info, err := os.Stat(real)
		if err != nil {
			return err
		}
		if lstat, err := os.Lstat(path); err == nil && lstat.Mode()&fs.ModeSymlink != 0 {
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			label += " -> " + target
		}
		if err := visit(label, real, info); err != nil {
			return err
		}
		if !info.IsDir() || seen[real] {
			return nil
		}
		seen[real] = true
		entries, err := os.ReadDir(real)
		if err != nil {
			return err
		}
		base := displayPath(worktreePath, runtimeDir, path)
		for _, entry := range entries {
			if err := walk(filepath.Join(path, entry.Name()), base+"/"+entry.Name()); err != nil {
				return err
			}
		}
		return nil
	}
	return walk(start, displayPath(worktreePath, runtimeDir, start))
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
