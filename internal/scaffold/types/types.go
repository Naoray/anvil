package types

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"

	"github.com/go-viper/mapstructure/v2"

	"github.com/naoray/anvil/internal/utils"
)

type ScaffoldContext struct {
	WorktreePath string
	Branch       string
	RepoName     string
	SiteName     string
	Preset       string
	Env          map[string]string
	Path         string
	RepoPath     string
	DbSuffix     string
	Vars         map[string]string
	mu           sync.RWMutex
}

type StepOptions struct {
	Args    []string
	DryRun  bool
	Verbose bool
	Quiet   bool
}

type ScaffoldStep interface {
	Name() string
	Run(ctx *ScaffoldContext, opts StepOptions) error
	Condition(ctx *ScaffoldContext) bool
}

func (ctx *ScaffoldContext) EvaluateCondition(conditions map[string]any) (bool, error) {
	if len(conditions) == 0 {
		return true, nil
	}

	if not, ok := conditions["not"]; ok {
		result, err := ctx.evaluateCondition(not)
		if err != nil {
			return false, err
		}
		return !result, nil
	}

	return ctx.evaluateCondition(conditions)
}

func (ctx *ScaffoldContext) evaluateCondition(cond any) (bool, error) {
	switch c := cond.(type) {
	case map[string]any:
		return ctx.evaluateMapCondition(c)
	case []any:
		return ctx.evaluateArrayCondition(c)
	default:
		return true, nil
	}
}

func (ctx *ScaffoldContext) evaluateMapCondition(conditions map[string]any) (bool, error) {
	for key, value := range conditions {
		result, err := ctx.evaluateSingle(key, value)
		if err != nil {
			return false, err
		}
		if !result {
			return false, nil
		}
	}
	return true, nil
}

func (ctx *ScaffoldContext) evaluateArrayCondition(conditions []any) (bool, error) {
	for _, item := range conditions {
		result, err := ctx.evaluateCondition(item.(map[string]any))
		if err != nil {
			return false, err
		}
		if !result {
			return false, nil
		}
	}
	return true, nil
}

func (ctx *ScaffoldContext) evaluateSingle(key string, value any) (bool, error) {
	switch key {
	case "file_exists":
		return ctx.fileExists(value)
	case "file_contains":
		return ctx.fileContains(value)
	case "file_has_script":
		return ctx.fileHasScript(value)
	case "command_exists":
		return ctx.commandExists(value)
	case "os":
		return ctx.osMatches(value)
	case "env_exists":
		return ctx.envExists(value)
	case "env_not_exists":
		return ctx.envNotExists(value)
	case "env_file_contains":
		return ctx.envFileContains(value)
	case "env_file_missing":
		return ctx.envFileMissing(value)
	case "not":
		result, err := ctx.evaluateCondition(value)
		if err != nil {
			return false, err
		}
		return !result, nil
	default:
		return true, nil
	}
}

func (ctx *ScaffoldContext) fileExists(value any) (bool, error) {
	switch v := value.(type) {
	case string:
		// Single file
		fullPath := filepath.Join(ctx.WorktreePath, v)
		_, err := os.Stat(fullPath)
		return err == nil, nil
	case []any:
		// Array of files - all must exist
		for _, item := range v {
			if path, ok := item.(string); ok {
				fullPath := filepath.Join(ctx.WorktreePath, path)
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
			fullPath := filepath.Join(ctx.WorktreePath, p)
			_, err := os.Stat(fullPath)
			return err == nil, nil
		}
	}

	return false, nil
}

func (ctx *ScaffoldContext) fileContains(value any) (bool, error) {
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

	fullPath := filepath.Join(ctx.WorktreePath, config.File)
	data, err := os.ReadFile(fullPath)
	if err != nil {
		return false, nil
	}

	return strings.Contains(string(data), config.Pattern), nil
}

func (ctx *ScaffoldContext) fileHasScript(value any) (bool, error) {
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

	fullPath := filepath.Join(ctx.WorktreePath, "package.json")
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

func (ctx *ScaffoldContext) envFileContains(value any) (bool, error) {
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

	env := utils.ReadEnvFile(ctx.WorktreePath, config.File)
	val, exists := env[config.Key]
	return exists && val != "", nil
}

func (ctx *ScaffoldContext) envFileMissing(value any) (bool, error) {
	contains, err := ctx.envFileContains(value)
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

func (ctx *ScaffoldContext) SnapshotForTemplate() map[string]string {
	ctx.mu.RLock()
	defer ctx.mu.RUnlock()

	sanitized := sanitizeSiteName(ctx.SiteName)

	// Build a truncated database name that respects identifier limits.
	var dbName string
	if ctx.DbSuffix != "" {
		dbName = buildDatabaseName(sanitized, ctx.DbSuffix, maxDbNameLength)
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
