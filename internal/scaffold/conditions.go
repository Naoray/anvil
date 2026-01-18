package scaffold

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/go-viper/mapstructure/v2"
	"github.com/michaeldyrynda/arbor/internal/scaffold/types"
)

type ConditionEvaluator struct {
	ctx types.ScaffoldContext
}

func NewConditionEvaluator(ctx types.ScaffoldContext) *ConditionEvaluator {
	return &ConditionEvaluator{ctx: ctx}
}

func (e *ConditionEvaluator) Evaluate(conditions map[string]interface{}) (bool, error) {
	if len(conditions) == 0 {
		return true, nil
	}

	if not, ok := conditions["not"]; ok {
		result, err := e.evaluateCondition(not)
		if err != nil {
			return false, err
		}
		return !result, nil
	}

	return e.evaluateCondition(conditions)
}

func (e *ConditionEvaluator) evaluateCondition(cond interface{}) (bool, error) {
	switch c := cond.(type) {
	case map[string]interface{}:
		return e.evaluateMapCondition(c)
	case []interface{}:
		return e.evaluateArrayCondition(c)
	default:
		return true, nil
	}
}

func (e *ConditionEvaluator) evaluateMapCondition(conditions map[string]interface{}) (bool, error) {
	for key, value := range conditions {
		result, err := e.evaluateSingle(key, value)
		if err != nil {
			return false, err
		}
		if !result {
			return false, nil
		}
	}
	return true, nil
}

func (e *ConditionEvaluator) evaluateArrayCondition(conditions []interface{}) (bool, error) {
	for _, item := range conditions {
		result, err := e.evaluateCondition(item.(map[string]interface{}))
		if err != nil {
			return false, err
		}
		if !result {
			return false, nil
		}
	}
	return true, nil
}

func (e *ConditionEvaluator) evaluateSingle(key string, value interface{}) (bool, error) {
	switch key {
	case "file_exists":
		return e.fileExists(value)
	case "file_contains":
		return e.fileContains(value)
	case "file_has_script":
		return e.fileHasScript(value)
	case "command_exists":
		return e.commandExists(value)
	case "os":
		return e.osMatches(value)
	case "env_exists":
		return e.envExists(value)
	case "not":
		result, err := e.evaluateCondition(value)
		if err != nil {
			return false, err
		}
		return !result, nil
	default:
		return true, nil
	}
}

func (e *ConditionEvaluator) fileExists(value interface{}) (bool, error) {
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

	fullPath := filepath.Join(e.ctx.WorktreePath, path)
	_, err := os.Stat(fullPath)
	return err == nil, nil
}

func (e *ConditionEvaluator) fileContains(value interface{}) (bool, error) {
	var config struct {
		File    string `mapstructure:"file"`
		Pattern string `mapstructure:"pattern"`
	}

	switch v := value.(type) {
	case map[string]interface{}:
		mapstructure.Decode(v, &config)
	case string:
		return false, nil
	}

	if config.File == "" || config.Pattern == "" {
		return false, nil
	}

	fullPath := filepath.Join(e.ctx.WorktreePath, config.File)
	data, err := os.ReadFile(fullPath)
	if err != nil {
		return false, nil
	}

	return strings.Contains(string(data), config.Pattern), nil
}

func (e *ConditionEvaluator) fileHasScript(value interface{}) (bool, error) {
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

	fullPath := filepath.Join(e.ctx.WorktreePath, "package.json")
	data, err := os.ReadFile(fullPath)
	if err != nil {
		return false, nil
	}

	return strings.Contains(string(data), `"`+scriptName+`"`), nil
}

func (e *ConditionEvaluator) commandExists(value interface{}) (bool, error) {
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

func (e *ConditionEvaluator) osMatches(value interface{}) (bool, error) {
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
		if strings.ToLower(os) == strings.ToLower(runtime.GOOS) {
			return true, nil
		}
	}
	return false, nil
}

func (e *ConditionEvaluator) envExists(value interface{}) (bool, error) {
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
