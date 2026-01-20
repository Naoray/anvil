package types

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/go-viper/mapstructure/v2"

	"github.com/michaeldyrynda/arbor/internal/utils"
)

type ScaffoldContext struct {
	WorktreePath string
	Branch       string
	RepoName     string
	Preset       string
	Env          map[string]string
}

type StepOptions struct {
	Args    []string
	DryRun  bool
	Verbose bool
}

type ScaffoldStep interface {
	Name() string
	Run(ctx ScaffoldContext, opts StepOptions) error
	Priority() int
	Condition(ctx ScaffoldContext) bool
}

func (ctx *ScaffoldContext) EvaluateCondition(conditions map[string]interface{}) (bool, error) {
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

func (ctx *ScaffoldContext) evaluateCondition(cond interface{}) (bool, error) {
	switch c := cond.(type) {
	case map[string]interface{}:
		return ctx.evaluateMapCondition(c)
	case []interface{}:
		return ctx.evaluateArrayCondition(c)
	default:
		return true, nil
	}
}

func (ctx *ScaffoldContext) evaluateMapCondition(conditions map[string]interface{}) (bool, error) {
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

func (ctx *ScaffoldContext) evaluateArrayCondition(conditions []interface{}) (bool, error) {
	for _, item := range conditions {
		result, err := ctx.evaluateCondition(item.(map[string]interface{}))
		if err != nil {
			return false, err
		}
		if !result {
			return false, nil
		}
	}
	return true, nil
}

func (ctx *ScaffoldContext) evaluateSingle(key string, value interface{}) (bool, error) {
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

func (ctx *ScaffoldContext) fileExists(value interface{}) (bool, error) {
	var path string
	switch v := value.(type) {
	case string:
		path = v
	case map[string]interface{}:
		if p, ok := v["file"].(string); ok {
			path = p
		}
	}

	if path == "" {
		return false, nil
	}

	fullPath := filepath.Join(ctx.WorktreePath, path)
	_, err := os.Stat(fullPath)
	return err == nil, nil
}

func (ctx *ScaffoldContext) fileContains(value interface{}) (bool, error) {
	var config struct {
		File    string `mapstructure:"file"`
		Pattern string `mapstructure:"pattern"`
	}

	switch v := value.(type) {
	case map[string]interface{}:
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

func (ctx *ScaffoldContext) fileHasScript(value interface{}) (bool, error) {
	var scriptName string
	switch v := value.(type) {
	case string:
		scriptName = v
	case map[string]interface{}:
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

func (ctx *ScaffoldContext) commandExists(value interface{}) (bool, error) {
	var cmdName string
	switch v := value.(type) {
	case string:
		cmdName = v
	case map[string]interface{}:
		if c, ok := v["command"].(string); ok {
			cmdName = c
		}
	}

	if cmdName == "" {
		return false, nil
	}

	_, err := exec.LookPath(cmdName)
	return err == nil, nil
}

func (ctx *ScaffoldContext) osMatches(value interface{}) (bool, error) {
	var osList []string
	switch v := value.(type) {
	case string:
		osList = []string{v}
	case []interface{}:
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

func (ctx *ScaffoldContext) envExists(value interface{}) (bool, error) {
	var envName string
	switch v := value.(type) {
	case string:
		envName = v
	case map[string]interface{}:
		if e, ok := v["env"].(string); ok {
			envName = e
		}
	}

	if envName == "" {
		return false, nil
	}

	_, exists := os.LookupEnv(envName)
	return exists, nil
}

func (ctx *ScaffoldContext) envNotExists(value interface{}) (bool, error) {
	exists, err := ctx.envExists(value)
	if err != nil {
		return false, err
	}
	return !exists, nil
}

func (ctx *ScaffoldContext) envFileContains(value interface{}) (bool, error) {
	var config struct {
		File string `mapstructure:"file"`
		Key  string `mapstructure:"key"`
	}

	switch v := value.(type) {
	case map[string]interface{}:
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

func (ctx *ScaffoldContext) envFileMissing(value interface{}) (bool, error) {
	contains, err := ctx.envFileContains(value)
	if err != nil {
		return false, err
	}
	return !contains, nil
}
