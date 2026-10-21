package checks

import (
	"fmt"
	"sort"
	"strings"

	"github.com/naoray/anvil/internal/workenv"
)

// ContentDigest identifies what a clean checkout validated independently of
// the worktree it ran in: HEAD's tree (not the commit, so identical content
// on another commit matches), the clean state, the check definition
// (commands and declared inputs, with worktree-relative input paths), the
// content environment, toolchain probes, platform and declared shared
// resources. It is a pure function of the recorded identity, so a stored
// result's claim can be recomputed. Empty for a dirty checkout or an identity
// recorded before content identities existed.
func ContentDigest(identity Identity) string {
	if identity.Git.Dirty || identity.Git.DirtyPaths != 0 || identity.Git.Tree == "" || identity.ContentEnvDigest == "" || identity.ContentToolchainDigest == "" {
		return ""
	}
	return digestFields("anvil-check-content-v1", identity.Git.Tree, "clean", identity.DefinitionDigest, identity.InputsDigest,
		identity.ContentEnvDigest, identity.ContentToolchainDigest, identity.ResourcesDigest, identity.Platform.OS, identity.Platform.Arch)
}

// Placeholders that replace worktree-specific values in the content
// environment.
const (
	placeholderWorktree   = "${ANVIL_WORKTREE}"
	placeholderRuntimeDir = "${ANVIL_RUNTIME_DIR}"
	placeholderID         = "${ANVIL_WORKTREE_ID}"
	placeholderInstance   = "${ANVIL_INSTANCE}"
)

// replacement substitutes a worktree-specific value wherever it occurs as a
// whole token: the characters around a match must not belong to class.
type replacement struct {
	value, placeholder string
	class              func(byte) bool
}

func isWordByte(b byte) bool {
	return b == '_' || b >= '0' && b <= '9' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z'
}

func isPathByte(b byte) bool { return isWordByte(b) || b == '.' || b == '-' }

func isDigitByte(b byte) bool { return b >= '0' && b <= '9' }

func (r replacement) apply(text string) string {
	var out strings.Builder
	copied := 0
	for from := 0; from < len(text); {
		index := strings.Index(text[from:], r.value)
		if index < 0 {
			break
		}
		index += from
		end := index + len(r.value)
		if (index == 0 || !r.class(text[index-1])) && (end == len(text) || !r.class(text[end])) {
			out.WriteString(text[copied:index])
			out.WriteString(r.placeholder)
			copied, from = end, end
			continue
		}
		from = index + 1 // not a whole token; look for a later match
	}
	out.WriteString(text[copied:])
	return out.String()
}

// contentNormalizer rewrites this worktree's own worktree-specific values to
// placeholders, so two worktrees at the same content produce the same
// content environment:
//
//   - the worktree path → ${ANVIL_WORKTREE} and its runtime directory →
//     ${ANVIL_RUNTIME_DIR} (at path boundaries): inputs, artifacts and
//     command directories are already identified relative to them;
//   - the worktree ID and instance slug → placeholders: they only name the
//     worktree and never describe validated content;
//   - ANVIL_WORKTREE_NAME and ANVIL_BRANCH → placeholders for those variables
//     only: names are not unique, so they are not searched for in other values;
//   - each reserved port (ANVIL_PORT_<NAME>) → ${ANVIL_PORT_<NAME>} wherever it
//     occurs as a whole number: ports isolate concurrently running worktrees;
//   - Anvil database names (ANVIL_DB_DATABASE, ANVIL_TEST_DB_DATABASE) →
//     placeholders by role, as whole identifiers, which also covers DB_DATABASE
//     or DSNs derived from them: each worktree's database is provisioned from
//     the same declared steps, and a check whose result depends on
//     undeclared database contents must not be marked reusable.
//
// Only values belonging to this worktree are rewritten; a value that names
// another worktree's path stays literal and does not match.
type contentNormalizer struct {
	replacements []replacement
	exact        map[string]string
}

func newContentNormalizer(worktreePath, runtimeDir string, vars []workenv.Variable) contentNormalizer {
	n := contentNormalizer{exact: map[string]string{}}
	add := func(value, placeholder string, class func(byte) bool) {
		if value != "" {
			n.replacements = append(n.replacements, replacement{value: value, placeholder: placeholder, class: class})
		}
	}
	add(worktreePath, placeholderWorktree, isPathByte)
	add(runtimeDir, placeholderRuntimeDir, isPathByte)
	for _, variable := range vars {
		if variable.Source == workenv.SourceInherited {
			continue
		}
		switch name := variable.Name; {
		case name == "ANVIL_WORKTREE_PATH":
			add(variable.Value, placeholderWorktree, isPathByte)
		case name == "ANVIL_RUNTIME_DIR":
			add(variable.Value, placeholderRuntimeDir, isPathByte)
		case name == "ANVIL_WORKTREE_ID":
			add(variable.Value, placeholderID, isWordByte)
		case name == "ANVIL_INSTANCE":
			add(variable.Value, placeholderInstance, isWordByte)
		case name == "ANVIL_DB_DATABASE", name == "ANVIL_TEST_DB_DATABASE":
			add(variable.Value, "${"+name+"}", isWordByte)
		case name == "ANVIL_WORKTREE_NAME", name == "ANVIL_BRANCH":
			n.exact[name] = "${" + name + "}"
		case strings.HasPrefix(name, "ANVIL_PORT_") && name != "ANVIL_PORT_RANGE" && isNumber(variable.Value):
			add(variable.Value, "${"+name+"}", isDigitByte)
		}
	}
	// Longer values first: a runtime directory contains the worktree ID.
	sort.SliceStable(n.replacements, func(i, j int) bool {
		if len(n.replacements[i].value) != len(n.replacements[j].value) {
			return len(n.replacements[i].value) > len(n.replacements[j].value)
		}
		return n.replacements[i].value < n.replacements[j].value
	})
	return n
}

func isNumber(value string) bool {
	if value == "" {
		return false
	}
	for i := 0; i < len(value); i++ {
		if !isDigitByte(value[i]) {
			return false
		}
	}
	return true
}

func (n contentNormalizer) value(name, value string) string {
	if placeholder, ok := n.exact[name]; ok {
		return placeholder
	}
	for _, r := range n.replacements {
		value = r.apply(value)
	}
	return value
}

// toolchainEnv lists inherited variables that commonly change what a
// toolchain builds or tests, so they count in the content identity whenever
// they are set: the executable search path, Go build mode and target, Node
// runtime options and mode, Rust flags and target, PHP's ini scan directory
// and CI (which many test runners switch behavior on). The list is kept short
// on purpose; every other inherited variable (agent session IDs, terminal and
// shell state) is ignored unless a check names it in identity_env.
var toolchainEnv = []string{
	"PATH", "GOFLAGS", "GOOS", "GOARCH", "CGO_ENABLED", "NODE_OPTIONS", "NODE_ENV",
	"RUSTFLAGS", "CARGO_BUILD_TARGET", "PHP_INI_SCAN_DIR", "CI",
}

// contentEnvironmentDigest hashes one command's directory and declared
// environment for the content identity. Inherited variables count only when
// listed in toolchainEnv or the check's identity_env; their values are
// normalized too (PATH entries inside the worktree). Secret values stay
// placeholders.
func contentEnvironmentDigest(req IdentityRequest, target CommandTarget) string {
	include := map[string]bool{}
	for _, name := range append(append([]string{}, toolchainEnv...), req.Check.IdentityEnv...) {
		include[name] = true
	}
	vars := target.Env.Variables()
	normalizer := newContentNormalizer(req.WorktreePath, req.RuntimeDir, vars)
	fields := []string{"dir=" + normalizer.value("", target.Dir)}
	for _, variable := range vars {
		if variable.Source == workenv.SourceInherited && !include[variable.Name] {
			continue
		}
		value := normalizer.value(variable.Name, variable.Value)
		if variable.Secret {
			value = "<secret>"
		}
		source := "declared"
		if variable.Source == workenv.SourceInherited {
			source = "inherited"
		}
		fields = append(fields, source+":"+variable.Name+"="+value)
	}
	sort.Strings(fields[1:])
	return digestFields(fields...)
}

// contentToolchainDigest hashes the toolchain probes with worktree-specific
// values normalized, so a probe printing a tool's path inside the worktree
// (node_modules/.bin, a vendored binary) matches across worktrees.
func contentToolchainDigest(req IdentityRequest, probes []ToolProbe) string {
	var vars []workenv.Variable
	if req.Env != nil {
		vars = req.Env.Variables()
	}
	normalizer := newContentNormalizer(req.WorktreePath, req.RuntimeDir, vars)
	fields := []string{"anvil-check-content-toolchain-v1"}
	for _, probe := range probes {
		fields = append(fields, probe.Command, normalizer.value("", probe.Output), fmt.Sprint(probe.ExitCode))
	}
	return digestFields(fields...)
}
