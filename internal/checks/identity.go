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
}

const probeTimeout = 15 * time.Second
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
	identity.Toolchain = probeToolchain(ctx, req.WorktreePath, req.Check.Toolchain, req.Env)
	var toolFields []string
	for _, probe := range identity.Toolchain {
		toolFields = append(toolFields, probe.Command, probe.Output, fmt.Sprint(probe.ExitCode))
	}
	identity.ToolchainDigest = digestFields(toolFields...)

	// Values are hashed, never stored. Secret values are excluded from the
	// digest entirely so a low-entropy secret cannot be recovered from it.
	var envFields []string
	for _, variable := range req.Env.Variables() {
		identity.EnvNames = append(identity.EnvNames, variable.Name)
		value := variable.Value
		if variable.Secret {
			value = "<secret>"
		}
		envFields = append(envFields, variable.Name+"="+value)
	}
	sort.Strings(identity.EnvNames)
	sort.Strings(envFields)
	identity.EnvDigest = digestFields(envFields...)

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

func probeToolchain(ctx context.Context, dir string, commands []string, env *workenv.Environment) []ToolProbe {
	probes := make([]ToolProbe, 0, len(commands))
	for _, command := range commands {
		probeCtx, cancel := context.WithTimeout(ctx, probeTimeout)
		cmd := exec.CommandContext(probeCtx, "sh", "-c", command)
		cmd.Dir = dir
		cmd.Env = env.Environ(os.Environ(), nil, runtime.GOOS == "windows")
		out, err := cmd.CombinedOutput()
		cancel()
		probe := ToolProbe{Command: command, Output: firstLine(string(out))}
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
	return probes
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
			err := filepath.WalkDir(match, func(path string, entry fs.DirEntry, err error) error {
				if err != nil {
					return err
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
