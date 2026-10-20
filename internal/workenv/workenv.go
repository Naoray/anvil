// Package workenv computes the environment Anvil exports to commands run in
// a worktree (`anvil run`, preparation, checks and managed processes). It is
// read-only: it derives values from declared configuration and observed
// state, and never allocates or writes anything.
package workenv

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/naoray/anvil/internal/config"
	"github.com/naoray/anvil/internal/utils"
)

// Variable sources.
const (
	SourceAnvil     = "anvil"
	SourceProject   = "project"
	SourceComponent = "component"
	SourceDatabase  = "database"
	SourceResource  = "resource"
)

// Variable is one exported environment value.
type Variable struct {
	Name   string `json:"name"`
	Value  string `json:"value,omitempty"`
	Source string `json:"source"`
	Secret bool   `json:"secret"`
}

// Environment is an ordered set of exported variables. Later variables with
// the same name replace earlier ones.
type Environment struct {
	vars  []Variable
	index map[string]int
}

// Set adds or replaces a variable.
func (e *Environment) Set(v Variable) {
	if e.index == nil {
		e.index = map[string]int{}
	}
	if i, ok := e.index[v.Name]; ok {
		e.vars[i] = v
		return
	}
	e.index[v.Name] = len(e.vars)
	e.vars = append(e.vars, v)
}

// Get returns a variable's value.
func (e *Environment) Get(name string) (string, bool) {
	if i, ok := e.index[name]; ok {
		return e.vars[i].Value, true
	}
	return "", false
}

// Variables returns the variables in definition order.
func (e *Environment) Variables() []Variable {
	return append([]Variable(nil), e.vars...)
}

// Map returns name → value.
func (e *Environment) Map() map[string]string {
	out := make(map[string]string, len(e.vars))
	for _, v := range e.vars {
		out[v.Name] = v.Value
	}
	return out
}

// Redacted returns the variables with secret values removed, for JSON
// output and logs.
func (e *Environment) Redacted() []Variable {
	out := e.Variables()
	for i := range out {
		if out[i].Secret {
			out[i].Value = ""
		}
	}
	return out
}

// Environ overlays the variables on base (os.Environ format). Names in unset
// are removed from the result first.
func (e *Environment) Environ(base []string, unset []string, caseInsensitive bool) []string {
	drop := map[string]bool{}
	norm := func(key string) string {
		if caseInsensitive {
			return strings.ToUpper(key)
		}
		return key
	}
	for _, v := range e.vars {
		drop[norm(v.Name)] = true
	}
	for _, name := range unset {
		drop[norm(name)] = true
	}
	out := make([]string, 0, len(base)+len(e.vars))
	for _, entry := range base {
		key, _, found := strings.Cut(entry, "=")
		if found && drop[norm(key)] {
			continue
		}
		out = append(out, entry)
	}
	for _, v := range e.vars {
		out = append(out, v.Name+"="+v.Value)
	}
	return out
}

var secretNamePattern = regexp.MustCompile(`(?i)(SECRET|TOKEN|PASSWORD|PASSWD|PRIVATE|CREDENTIAL|API_?KEY|ACCESS_?KEY|AUTH)`)

// IsSecretName applies Anvil's conservative secret-name heuristic.
func IsSecretName(name string, declared []string) bool {
	for _, candidate := range declared {
		if candidate == name {
			return true
		}
	}
	return secretNamePattern.MatchString(name)
}

// Identity describes the worktree an environment is built for.
type Identity struct {
	ID      string
	Path    string
	Branch  string
	Project string
}

// Options configure Build.
type Options struct {
	Identity  Identity
	Config    *config.Config
	State     *config.LocalState
	Component *ComponentEnv
	// Resources are added by resource management (directories, ports,
	// shared resources) before project values, so project values can
	// reference them.
	Resources []Variable
}

// ComponentEnv is the environment contributed by one selected component.
type ComponentEnv struct {
	Name string
	Env  map[string]string
}

// Build derives the worktree environment.
func Build(opts Options) *Environment {
	env := &Environment{}
	id := opts.Identity
	name := filepath.Base(id.Path)
	anvil := func(key, value string) {
		env.Set(Variable{Name: key, Value: value, Source: SourceAnvil})
	}
	anvil("ANVIL", "1")
	anvil("ANVIL_WORKTREE_ID", id.ID)
	anvil("ANVIL_WORKTREE_PATH", id.Path)
	anvil("ANVIL_WORKTREE_NAME", name)
	anvil("ANVIL_BRANCH", id.Branch)
	anvil("ANVIL_PROJECT", id.Project)
	anvil("ANVIL_INSTANCE", InstanceSlug(id.Project, name, id.ID))

	if opts.State != nil {
		for _, database := range opts.State.Databases {
			switch database.Role {
			case config.DbRoleApplication:
				env.Set(Variable{Name: "ANVIL_DB_DATABASE", Value: database.Name, Source: SourceDatabase})
			case config.DbRoleTesting:
				env.Set(Variable{Name: "ANVIL_TEST_DB_DATABASE", Value: database.Name, Source: SourceDatabase})
			}
		}
		if opts.State.BorrowedApplication != nil {
			env.Set(Variable{Name: "ANVIL_DB_DATABASE", Value: opts.State.BorrowedApplication.Name, Source: SourceDatabase})
		}
	}
	for _, v := range opts.Resources {
		env.Set(v)
	}

	var declared []string
	if opts.Config != nil {
		declared = opts.Config.SecretEnv
		setExpanded(env, opts.Config.Env, SourceProject, declared)
	}
	if opts.Component != nil {
		setExpanded(env, opts.Component.Env, SourceComponent, declared)
	}
	return env
}

// setExpanded adds values in name order, expanding ${VAR} against the
// environment built so far and then the process environment.
func setExpanded(env *Environment, values map[string]string, source string, declaredSecrets []string) {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	base := env.Map()
	for _, key := range keys {
		env.Set(Variable{
			Name:   key,
			Value:  utils.ExpandEnv(values[key], base),
			Source: source,
			Secret: IsSecretName(key, declaredSecrets),
		})
	}
}

var slugUnsafe = regexp.MustCompile(`[^a-z0-9]+`)

const maxSlug = 40

// InstanceSlug is a short, stable, identifier-safe name for this worktree
// ([a-z0-9-]). Applications use it to namespace bundle identifiers,
// LaunchAgent labels, keychain services or socket names; Anvil cannot apply
// such isolation itself.
func InstanceSlug(project, name, id string) string {
	slug := strings.Trim(slugUnsafe.ReplaceAllString(strings.ToLower(project+"-"+name), "-"), "-")
	if slug == "" {
		slug = "worktree"
	}
	suffix := id
	if suffix == "" {
		sum := sha256.Sum256([]byte(project + "\x00" + name))
		suffix = hex.EncodeToString(sum[:])
	}
	suffix = strings.TrimPrefix(suffix, "wt_")
	if len(suffix) > 6 {
		suffix = suffix[:6]
	}
	if len(slug) > maxSlug-7 {
		slug = strings.Trim(slug[:maxSlug-7], "-")
	}
	return slug + "-" + suffix
}

// ProcessEnviron is os.Environ, isolated for tests.
var ProcessEnviron = os.Environ
