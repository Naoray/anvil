package types

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"

	"github.com/go-viper/mapstructure/v2"

	"github.com/naoray/anvil/internal/config"
	"github.com/naoray/anvil/internal/scaffold/words"
	"github.com/naoray/anvil/internal/utils"
)

type ScaffoldContext struct {
	DatabaseLockHeld bool
	BorrowAppDB      string
	DatabaseActions  map[string]string
	ResetDatabases   []string
	SiteDriver       string
	WorktreePath     string
	Branch           string
	RepoName         string
	SiteName         string
	Preset           string
	Env              map[string]string
	Path             string
	RepoPath         string
	DbSuffix         string
	Vars             map[string]string
	// CleanupReport, when set, collects database cleanup targets and warnings.
	CleanupReport           *CleanupReport
	dbSuffixFromState       bool
	dbSuffixFromLegacyState bool
	mu                      sync.RWMutex
}

// CleanupReport collects what cleanup steps did or, in a dry run, would do,
// for machine-readable output. Methods are safe on a nil report.
type CleanupReport struct {
	mu        sync.Mutex
	databases []string
	warnings  []string
}

// AddDatabases records databases dropped, or listed by a dry run.
func (r *CleanupReport) AddDatabases(names ...string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.databases = append(r.databases, names...)
}

// Warn records a nonfatal cleanup diagnostic.
func (r *CleanupReport) Warn(message string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.warnings = append(r.warnings, message)
}

func (r *CleanupReport) Databases() []string {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.databases...)
}

func (r *CleanupReport) Warnings() []string {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.warnings...)
}

type StepOptions struct {
	WarmSource      string
	CopyMode        string
	WarmNodeModules config.WarmNodeModulesConfig
	Args            []string
	DryRun          bool
	Verbose         bool
	Quiet           bool
}

type ScaffoldStep interface {
	Name() string
	Run(ctx *ScaffoldContext, opts StepOptions) error
	Condition(ctx *ScaffoldContext) (bool, error)
}

func (ctx *ScaffoldContext) EvaluateCondition(conditions map[string]any) (bool, error) {
	if err := ValidateConditionPaths(ctx.WorktreePath, ctx.WorktreePath, conditions); err != nil {
		return false, err
	}
	return ctx.evaluateCondition(ctx.WorktreePath, conditions)
}

// EvaluateConditionAt evaluates conditions with file paths relative to root
// instead of the worktree root. Steps with a configured working directory use
// it so component conditions read their own files.
func (ctx *ScaffoldContext) EvaluateConditionAt(root string, conditions map[string]any) (bool, error) {
	if err := ValidateConditionPaths(ctx.WorktreePath, root, conditions); err != nil {
		return false, err
	}
	return ctx.evaluateCondition(root, conditions)
}

func (ctx *ScaffoldContext) evaluateCondition(root string, cond any) (bool, error) {
	switch c := cond.(type) {
	case map[string]any:
		for _, key := range conditionKeys(c) {
			result, err := ctx.evaluateSingle(root, key, c[key])
			if err != nil || !result {
				return false, err
			}
		}
	case []any:
		for _, item := range c {
			result, err := ctx.evaluateCondition(root, item)
			if err != nil || !result {
				return false, err
			}
		}
	case []map[string]any:
		for _, item := range c {
			result, err := ctx.evaluateCondition(root, item)
			if err != nil || !result {
				return false, err
			}
		}
	default:
		return false, fmt.Errorf("invalid condition shape %T", cond)
	}
	return true, nil
}

func conditionKeys(conditions map[string]any) []string {
	keys := make([]string, 0, len(conditions))
	for key := range conditions {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func (ctx *ScaffoldContext) evaluateSingle(root, key string, value any) (bool, error) {
	if list, ok := value.([]string); ok {
		items := make([]any, len(list))
		for i, item := range list {
			items[i] = item
		}
		value = items
	}

	switch key {
	case "file_exists":
		return ctx.fileExists(root, value)
	case "file_contains":
		return ctx.fileContains(root, value)
	case "file_has_script":
		return ctx.fileHasScript(root, value)
	case "command_exists":
		return ctx.commandExists(value)
	case "os":
		return ctx.osMatches(value)
	case "env_exists":
		return ctx.envExists(value)
	case "env_not_exists":
		return ctx.envNotExists(value)
	case "env_file_contains":
		return ctx.envFileContains(root, value)
	case "env_file_missing":
		return ctx.envFileMissing(root, value)
	case "context_var":
		config := value.(map[string]any)
		ctx.mu.RLock()
		defer ctx.mu.RUnlock()
		actual, exists := ctx.Vars[config["name"].(string)]
		return exists && actual == config["value"].(string), nil
	case "not":
		result, err := ctx.evaluateCondition(root, value)
		if err != nil {
			return false, err
		}
		return !result, nil
	default:
		return false, fmt.Errorf("unknown condition %q", key)
	}
}

func (ctx *ScaffoldContext) fileExists(root string, value any) (bool, error) {
	switch v := value.(type) {
	case string:
		// Single file
		fullPath := filepath.Join(root, v)
		_, err := os.Stat(fullPath)
		return err == nil, nil
	case []any:
		// Array of files - all must exist
		for _, item := range v {
			if path, ok := item.(string); ok {
				fullPath := filepath.Join(root, path)
				_, err := os.Stat(fullPath)
				if err != nil {
					return false, nil
				}
			}
		}
		return true, nil
	case map[string]any:
		// Map format with "file" key
		if p, ok := v["file"].(string); ok {
			fullPath := filepath.Join(root, p)
			_, err := os.Stat(fullPath)
			return err == nil, nil
		}
	}

	return false, nil
}

func (ctx *ScaffoldContext) fileContains(root string, value any) (bool, error) {
	var config struct {
		File    string `mapstructure:"file"`
		Pattern string `mapstructure:"pattern"`
	}

	switch v := value.(type) {
	case map[string]any:
		if err := mapstructure.Decode(v, &config); err != nil {
			return false, nil
		}
	case string:
		return false, nil
	}

	if config.File == "" || config.Pattern == "" {
		return false, nil
	}

	fullPath := filepath.Join(root, config.File)
	data, err := os.ReadFile(fullPath)
	if err != nil {
		return false, nil
	}

	return strings.Contains(string(data), config.Pattern), nil
}

func (ctx *ScaffoldContext) fileHasScript(root string, value any) (bool, error) {
	var scriptName string
	switch v := value.(type) {
	case string:
		scriptName = v
	case map[string]any:
		if s, ok := v["name"].(string); ok {
			scriptName = s
		}
	}

	if scriptName == "" {
		return false, nil
	}

	fullPath := filepath.Join(root, "package.json")
	data, err := os.ReadFile(fullPath)
	if err != nil {
		return false, nil
	}

	return strings.Contains(string(data), `"`+scriptName+`"`), nil
}

func (ctx *ScaffoldContext) commandExists(value any) (bool, error) {
	switch v := value.(type) {
	case string:
		// Single command
		_, err := exec.LookPath(v)
		return err == nil, nil
	case []any:
		// Array of commands - all must exist
		for _, item := range v {
			if cmdName, ok := item.(string); ok {
				_, err := exec.LookPath(cmdName)
				if err != nil {
					return false, nil
				}
			}
		}
		return true, nil
	case map[string]any:
		// Map format with "command" key
		if c, ok := v["command"].(string); ok {
			_, err := exec.LookPath(c)
			return err == nil, nil
		}
	}

	return false, nil
}

func (ctx *ScaffoldContext) osMatches(value any) (bool, error) {
	var osList []string
	switch v := value.(type) {
	case string:
		osList = []string{v}
	case []any:
		for _, item := range v {
			if s, ok := item.(string); ok {
				osList = append(osList, s)
			}
		}
	}

	for _, os := range osList {
		if strings.EqualFold(os, runtime.GOOS) {
			return true, nil
		}
	}
	return false, nil
}

func (ctx *ScaffoldContext) envExists(value any) (bool, error) {
	switch v := value.(type) {
	case string:
		// Single environment variable
		_, exists := os.LookupEnv(v)
		return exists, nil
	case []any:
		// Array of environment variables - all must exist
		for _, item := range v {
			if envName, ok := item.(string); ok {
				_, exists := os.LookupEnv(envName)
				if !exists {
					return false, nil
				}
			}
		}
		return true, nil
	case map[string]any:
		// Map format with "env" key
		if e, ok := v["env"].(string); ok {
			_, exists := os.LookupEnv(e)
			return exists, nil
		}
	}

	return false, nil
}

func (ctx *ScaffoldContext) envNotExists(value any) (bool, error) {
	exists, err := ctx.envExists(value)
	if err != nil {
		return false, err
	}
	return !exists, nil
}

func (ctx *ScaffoldContext) envFileContains(root string, value any) (bool, error) {
	var config struct {
		File string `mapstructure:"file"`
		Key  string `mapstructure:"key"`
	}

	switch v := value.(type) {
	case map[string]any:
		if err := mapstructure.Decode(v, &config); err != nil {
			return false, nil
		}
	case string:
		config.Key = v
		config.File = ".env"
	}

	if config.File == "" || config.Key == "" {
		return false, nil
	}

	env := utils.ReadEnvFile(root, config.File)
	val, exists := env[config.Key]
	return exists && val != "", nil
}

func (ctx *ScaffoldContext) envFileMissing(root string, value any) (bool, error) {
	contains, err := ctx.envFileContains(root, value)
	if err != nil {
		return false, err
	}
	return !contains, nil
}

func (ctx *ScaffoldContext) SetVar(key, value string) {
	ctx.mu.Lock()
	defer ctx.mu.Unlock()
	if ctx.Vars == nil {
		ctx.Vars = make(map[string]string)
	}
	ctx.Vars[key] = value
}

func (ctx *ScaffoldContext) GetVar(key string) string {
	ctx.mu.RLock()
	defer ctx.mu.RUnlock()
	return ctx.Vars[key]
}

func (ctx *ScaffoldContext) SetDbSuffix(suffix string) {
	ctx.mu.Lock()
	defer ctx.mu.Unlock()
	ctx.DbSuffix = suffix
}

func (ctx *ScaffoldContext) GetDbSuffix() string {
	ctx.mu.RLock()
	defer ctx.mu.RUnlock()
	return ctx.DbSuffix
}

func (ctx *ScaffoldContext) SetDbSuffixLoadedFromState() {
	ctx.mu.Lock()
	defer ctx.mu.Unlock()
	ctx.dbSuffixFromState = true
}

func (ctx *ScaffoldContext) DbSuffixFromState() bool {
	ctx.mu.RLock()
	defer ctx.mu.RUnlock()
	return ctx.dbSuffixFromState
}

func (ctx *ScaffoldContext) SetDbSuffixLoadedFromLegacyState() {
	ctx.mu.Lock()
	defer ctx.mu.Unlock()
	ctx.dbSuffixFromLegacyState = true
}

func (ctx *ScaffoldContext) DbSuffixFromLegacyState() bool {
	ctx.mu.RLock()
	defer ctx.mu.RUnlock()
	return ctx.dbSuffixFromLegacyState
}

func (ctx *ScaffoldContext) SnapshotForTemplate() map[string]string {
	ctx.mu.RLock()
	defer ctx.mu.RUnlock()

	sanitized := sanitizeSiteName(ctx.SiteName)

	// Build a truncated database name that respects identifier limits.
	var dbName string
	var testDbName string
	if ctx.DbSuffix != "" {
		dbName = buildDatabaseName(sanitized, ctx.DbSuffix, maxDbNameLength)
		testDbName = words.BuildTestDatabaseName(sanitized, ctx.DbSuffix)
	}

	snapshot := map[string]string{
		"Path":              ctx.Path,
		"RepoPath":          ctx.RepoPath,
		"RepoName":          ctx.RepoName,
		"SiteName":          ctx.SiteName,
		"SanitizedSiteName": sanitized,
		"Branch":            ctx.Branch,
		"DbSuffix":          ctx.DbSuffix,
		"DatabaseName":      dbName,
		"TestDatabaseName":  testDbName,
	}
	for k, v := range ctx.Vars {
		snapshot[k] = v
	}
	return snapshot
}

const maxDbNameLength = 63

func buildDatabaseName(sanitized string, suffix string, maxLength int) string {
	maxSiteLen := maxLength - len(suffix) - 1
	if maxSiteLen < 1 {
		maxSiteLen = 1
	}
	if len(sanitized) > maxSiteLen {
		sanitized = sanitized[:maxSiteLen]
		sanitized = strings.TrimRight(sanitized, "_")
	}
	return sanitized + "_" + suffix
}

func sanitizeSiteName(name string) string {
	name = strings.ToLower(name)
	re := regexp.MustCompile(`[^a-z0-9_]`)
	name = re.ReplaceAllString(name, "_")
	re = regexp.MustCompile(`_+`)
	name = re.ReplaceAllString(name, "_")
	name = strings.Trim(name, "_")
	return name
}
