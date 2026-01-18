package steps

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/go-viper/mapstructure/v2"
	"github.com/michaeldyrynda/arbor/internal/config"
	"github.com/michaeldyrynda/arbor/internal/scaffold/types"
)

type BinaryStep struct {
	name      string
	binary    string
	args      []string
	priority  int
	condition map[string]interface{}
}

func NewBinaryStep(name, binary string, args []string, priority int) *BinaryStep {
	return &BinaryStep{
		name:      name,
		binary:    binary,
		args:      args,
		priority:  priority,
		condition: nil,
	}
}

func NewBinaryStepWithCondition(name string, cfg config.StepConfig, binary string, priority int) *BinaryStep {
	return &BinaryStep{
		name:      name,
		binary:    binary,
		args:      cfg.Args,
		priority:  priority,
		condition: cfg.Condition,
	}
}

func (s *BinaryStep) Name() string {
	return s.name
}

func (s *BinaryStep) Priority() int {
	return s.priority
}

func (s *BinaryStep) Condition(ctx types.ScaffoldContext) bool {
	if s.condition != nil && len(s.condition) > 0 {
		evaluator := NewStepConditionEvaluator(ctx)
		result, err := evaluator.Evaluate(s.condition)
		if err != nil {
			return false
		}
		return result
	}

	binaries := strings.Fields(s.binary)
	if len(binaries) == 0 {
		return false
	}
	_, err := exec.LookPath(binaries[0])
	return err == nil
}

func (s *BinaryStep) Run(ctx types.ScaffoldContext, opts types.StepOptions) error {
	allArgs := append(s.args, opts.Args...)
	allArgs = s.replaceTemplate(allArgs, ctx)
	if opts.Verbose {
		binaryParts := strings.Fields(s.binary)
		fullCmd := append(binaryParts, allArgs...)
		fmt.Printf("  Running: %s\n", strings.Join(fullCmd, " "))
	}
	cmd := exec.Command(strings.Fields(s.binary)[0], append(strings.Fields(s.binary)[1:], allArgs...)...)
	cmd.Dir = ctx.WorktreePath
	return cmd.Run()
}

func (s *BinaryStep) replaceTemplate(args []string, ctx types.ScaffoldContext) []string {
	for i, arg := range args {
		arg = strings.ReplaceAll(arg, "{{ .RepoName }}", ctx.RepoName)
		arg = strings.ReplaceAll(arg, "{{ .Branch }}", ctx.Branch)
		args[i] = arg
	}
	return args
}

type StepConditionEvaluator struct {
	ctx types.ScaffoldContext
}

func NewStepConditionEvaluator(ctx types.ScaffoldContext) *StepConditionEvaluator {
	return &StepConditionEvaluator{ctx: ctx}
}

func (e *StepConditionEvaluator) Evaluate(conditions map[string]interface{}) (bool, error) {
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

func (e *StepConditionEvaluator) evaluateCondition(cond interface{}) (bool, error) {
	switch c := cond.(type) {
	case map[string]interface{}:
		return e.evaluateMapCondition(c)
	case []interface{}:
		return e.evaluateArrayCondition(c)
	default:
		return true, nil
	}
}

func (e *StepConditionEvaluator) evaluateMapCondition(conditions map[string]interface{}) (bool, error) {
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

func (e *StepConditionEvaluator) evaluateArrayCondition(conditions []interface{}) (bool, error) {
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

func (e *StepConditionEvaluator) evaluateSingle(key string, value interface{}) (bool, error) {
	switch key {
	case "file_exists":
		return e.fileExists(value)
	case "file_contains":
		return e.fileContains(value)
	case "command_exists":
		return e.commandExists(value)
	case "os":
		return e.osMatches(value)
	case "env_exists":
		return e.envExists(value)
	case "env_not_exists":
		return e.envNotExists(value)
	case "env_file_contains":
		return e.envFileContains(value)
	case "env_file_missing":
		return e.envFileMissing(value)
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

func (e *StepConditionEvaluator) fileExists(value interface{}) (bool, error) {
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

func (e *StepConditionEvaluator) fileContains(value interface{}) (bool, error) {
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

func (e *StepConditionEvaluator) commandExists(value interface{}) (bool, error) {
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

func (e *StepConditionEvaluator) osMatches(value interface{}) (bool, error) {
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

func (e *StepConditionEvaluator) envExists(value interface{}) (bool, error) {
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

func (e *StepConditionEvaluator) envNotExists(value interface{}) (bool, error) {
	exists, err := e.envExists(value)
	if err != nil {
		return false, err
	}
	return !exists, nil
}

func (e *StepConditionEvaluator) envFileContains(value interface{}) (bool, error) {
	var config struct {
		File string `mapstructure:"file"`
		Key  string `mapstructure:"key"`
	}

	switch v := value.(type) {
	case map[string]interface{}:
		mapstructure.Decode(v, &config)
	case string:
		config.Key = v
		config.File = ".env"
	}

	if config.File == "" || config.Key == "" {
		return false, nil
	}

	env := e.readEnvFile(config.File)
	value, exists := env[config.Key]
	return exists && value != "", nil
}

func (e *StepConditionEvaluator) envFileMissing(value interface{}) (bool, error) {
	contains, err := e.envFileContains(value)
	if err != nil {
		return false, err
	}
	return !contains, nil
}

func (e *StepConditionEvaluator) readEnvFile(filename string) map[string]string {
	result := make(map[string]string)

	envPath := filepath.Join(e.ctx.WorktreePath, filename)
	data, err := os.ReadFile(envPath)
	if err != nil {
		return result
	}

	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			key := strings.TrimSpace(parts[0])
			value := strings.TrimSpace(parts[1])
			result[key] = value
		}
	}

	return result
}
