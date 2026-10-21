package scaffold

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/naoray/anvil/internal/config"
	"github.com/naoray/anvil/internal/dblifecycle"
	"github.com/naoray/anvil/internal/scaffold/steps"
	"github.com/naoray/anvil/internal/scaffold/types"
	"github.com/naoray/anvil/internal/scaffold/words"
	"github.com/naoray/anvil/internal/ui"
	"github.com/naoray/anvil/internal/wtstate"
)

type ScaffoldManager struct {
	registry StepRegistry
}

type CleanupOptions struct {
	DryRun              bool
	Verbose             bool
	Quiet               bool
	DatabaseLockHeld    bool
	SkipDatabaseCleanup bool
}

// StepRegistry defines the interface for step creation.
// This abstraction allows for dependency injection and testing.
type StepRegistry interface {
	Create(name string, cfg config.StepConfig) (types.ScaffoldStep, error)
}

// NewScaffoldManager creates a new scaffold manager with a fresh default registry.
func NewScaffoldManager() *ScaffoldManager {
	registry := steps.NewRegistry()
	registry.RegisterDefaults()
	return NewScaffoldManagerWithRegistry(registry)
}

// NewScaffoldManagerWithRegistry creates a new scaffold manager with the given step registry.
func NewScaffoldManagerWithRegistry(registry StepRegistry) *ScaffoldManager {
	if registry == nil {
		panic("scaffold manager requires an explicit step registry")
	}
	return &ScaffoldManager{
		registry: registry,
	}
}

func (m *ScaffoldManager) GetStepsForWorktree(
	cfg *config.Config,
	worktreePath, branch, presetName string,
	defaultSteps []config.StepConfig,
) ([]types.ScaffoldStep, error) {
	return m.stepsFromConfig(effectiveStepConfigs(cfg, defaultSteps))
}

// effectiveStepConfigs merges preset defaults with configured steps.
func effectiveStepConfigs(cfg *config.Config, defaultSteps []config.StepConfig) []config.StepConfig {
	stepConfigs := cfg.Scaffold.Steps
	if !cfg.Scaffold.Override {
		stepConfigs = append(append([]config.StepConfig(nil), defaultSteps...), stepConfigs...)
	}
	return append([]config.StepConfig(nil), stepConfigs...)
}

func (m *ScaffoldManager) GetCleanupSteps(
	cfg *config.Config,
	worktreePath, branch, presetName string,
	cleanupSteps []config.CleanupStep,
) ([]types.ScaffoldStep, error) {
	var stepsList []types.ScaffoldStep

	for _, cleanupConfig := range cleanupSteps {
		stepConfig := m.cleanupConfigToStepConfig(cleanupConfig)
		step, err := m.registry.Create(cleanupConfig.Name, stepConfig)
		if err != nil {
			return nil, fmt.Errorf("creating cleanup step %q: %w", cleanupConfig.Name, err)
		}
		stepsList = append(stepsList, step)
	}

	for _, cleanupConfig := range cfg.Cleanup.Steps {
		stepConfig := m.cleanupConfigToStepConfig(cleanupConfig)
		step, err := m.registry.Create(cleanupConfig.Name, stepConfig)
		if err != nil {
			return nil, fmt.Errorf("creating cleanup step %q: %w", cleanupConfig.Name, err)
		}
		stepsList = append(stepsList, step)
	}

	return stepsList, nil
}

func (m *ScaffoldManager) cleanupConfigToStepConfig(cleanupConfig config.CleanupStep) config.StepConfig {
	stepConfig := config.StepConfig{
		Name:            cleanupConfig.Name,
		ConditionHolder: config.ConditionHolder{Condition: make(map[string]any)},
		Args:            append([]string(nil), cleanupConfig.Args...),
	}
	if cleanupConfig.Name == "herd" && len(stepConfig.Args) == 0 {
		stepConfig.Args = []string{"unlink"}
	}
	for k, v := range cleanupConfig.Condition {
		if k == "command" {
			if cmd, ok := v.(string); ok && cmd != "" {
				stepConfig.Command = cmd
			} else {
				stepConfig.Condition[k] = v
			}
		} else {
			stepConfig.Condition[k] = v
		}
	}
	return stepConfig
}

func (m *ScaffoldManager) stepsFromConfig(stepConfigs []config.StepConfig) ([]types.ScaffoldStep, error) {
	stepsList := make([]types.ScaffoldStep, 0, len(stepConfigs))

	for _, cfg := range stepConfigs {
		step, err := m.registry.Create(cfg.Name, cfg)
		if err != nil {
			return nil, fmt.Errorf("creating step %q: %w", cfg.Name, err)
		}
		stepsList = append(stepsList, step)
	}

	return stepsList, nil
}

type WarmOptions struct {
	Source   string
	CopyMode string
}

// ScaffoldRequest describes one scaffold or preparation run.
type ScaffoldRequest struct {
	WorktreePath string
	Branch       string
	RepoName     string
	SiteName     string
	Preset       string
	DefaultSteps []config.StepConfig
	// ExtraSteps run after preset and configured scaffold steps (for example
	// compiled component preparation).
	ExtraSteps []config.StepConfig
	Config     *config.Config
	DryRun     bool
	Verbose    bool
	Quiet      bool
	Warm       WarmOptions
	// Env is the worktree environment exported to command steps.
	Env map[string]string
	// SkipCompleted skips steps whose completion record still matches their
	// definition, inputs and outputs. Completion is recorded on every real run.
	SkipCompleted bool
}

func (m *ScaffoldManager) RunScaffold(
	worktreePath, branch, repoName, siteName, preset string,
	defaultSteps []config.StepConfig,
	cfg *config.Config,
	dryRun, verbose, quiet bool,
	warmOptions ...WarmOptions,
) error {
	request := ScaffoldRequest{
		WorktreePath: worktreePath,
		Branch:       branch,
		RepoName:     repoName,
		SiteName:     siteName,
		Preset:       preset,
		DefaultSteps: defaultSteps,
		Config:       cfg,
		DryRun:       dryRun,
		Verbose:      verbose,
		Quiet:        quiet,
	}
	if len(warmOptions) > 0 {
		request.Warm = warmOptions[0]
	}
	_, err := m.RunScaffoldWithOptions(request)
	return err
}

// RunScaffoldWithOptions validates every step before any effect, then runs
// them under the worktree's preparation lock, persisting each completion as
// it happens so interrupted or failed runs resume safely.
func (m *ScaffoldManager) RunScaffoldWithOptions(request ScaffoldRequest) (report *PrepareReport, runErr error) {
	cfg := request.Config
	worktreePath := request.WorktreePath
	ctx := m.newScaffoldContext(worktreePath, request.Branch, request.RepoName, request.SiteName, request.Preset)
	for key, value := range request.Env {
		ctx.Env[key] = value
	}

	ctx.BorrowAppDB = cfg.Scaffold.BorrowAppDB
	ctx.DatabaseActions = cfg.Scaffold.DatabaseActions
	ctx.ResetDatabases = cfg.Scaffold.ResetDatabases
	ctx.SiteDriver = cfg.Scaffold.DatabaseDriver
	stepConfigs := append(effectiveStepConfigs(cfg, request.DefaultSteps), request.ExtraSteps...)
	if ctx.BorrowAppDB != "" {
		hasApplication := false
		for _, step := range stepConfigs {
			if step.Name == config.StepDbCreate && (step.Role == "" || step.Role == config.DbRoleApplication) {
				hasApplication = true
			}
		}
		if !hasApplication {
			return nil, fmt.Errorf("--borrow-app-db requires an application db.create step; use the laravel preset or configure that step explicitly")
		}
	}

	stepsList, err := m.stepsFromConfig(stepConfigs)
	if err != nil {
		return nil, fmt.Errorf("getting scaffold steps: %w", err)
	}
	if err := checkContainment(worktreePath, stepConfigs); err != nil {
		return nil, err
	}

	if len(ctx.DatabaseActions) > 0 || len(ctx.ResetDatabases) > 0 {
		found := false
		for _, step := range stepsList {
			if step.Name() == "db.migrate" {
				found = true
			}
		}
		if !found {
			policyConfig := config.StepConfig{Name: "db.migrate"}
			policy, err := m.registry.Create("db.migrate", policyConfig)
			if err != nil {
				return nil, err
			}
			stepsList = append(stepsList, policy)
			stepConfigs = append(stepConfigs, policyConfig)
		}
	}

	// Run pre-flight checks with spinner
	if !request.Quiet {
		if err := m.runPreFlightWithSpinner(&ctx, &cfg.Scaffold); err != nil {
			return nil, err
		}
	} else {
		// Quiet mode: run without spinner
		if err := m.runPreFlightChecks(&ctx, &cfg.Scaffold); err != nil {
			return nil, err
		}
	}

	if !request.DryRun {
		lock, err := wtstate.AcquirePrepareLock(worktreePath)
		if err != nil {
			return nil, fmt.Errorf("locking worktree preparation: %w", err)
		}
		defer func() { runErr = errors.Join(runErr, lock.Release()) }()
	}

	if err := prepareDbSuffix(&ctx, worktreePath, request.DryRun); err != nil {
		return nil, err
	}

	opts := m.stepOptionsFromFlags(request.DryRun, request.Verbose, request.Quiet)
	opts.WarmSource = request.Warm.Source
	opts.CopyMode = request.Warm.CopyMode

	plan, planDigest := buildPlan(stepConfigs)
	recorder := &prepareRecorder{
		worktreePath:  worktreePath,
		plan:          plan,
		skipCompleted: request.SkipCompleted,
		now:           time.Now,
		reasons:       map[int]string{},
	}
	executorOptions := ExecutorOptions{}
	if !request.DryRun {
		if _, _, err := wtstate.EnsureIdentity(worktreePath); err != nil {
			return nil, fmt.Errorf("recording worktree identity: %w", err)
		}
		state, err := wtstate.ReadPrepare(worktreePath)
		if err != nil {
			return nil, err
		}
		if state != nil {
			recorder.records = state.Steps
		}
		if err := wtstate.BeginPrepare(worktreePath, planDigest, time.Now()); err != nil {
			return nil, fmt.Errorf("recording preparation start: %w", err)
		}
		executorOptions.Hooks = recorder
	}

	executor := NewStepExecutorWithOptions(stepsList, &ctx, opts, executorOptions)
	execErr := executor.Execute()
	if execErr == nil && ctx.BorrowAppDB != "" && ctx.GetVar("BorrowApplicationApplied") != "true" {
		execErr = fmt.Errorf("--borrow-app-db was not applied because the application db.create step was skipped")
	}
	report = buildReport(plan, planDigest, executor.Results(), recorder.reasons)
	if !request.DryRun {
		failedStep := ""
		for _, outcome := range report.Steps {
			if outcome.Result == OutcomeFailed {
				failedStep = outcome.Key
			}
		}
		if err := wtstate.FinishPrepare(worktreePath, time.Now(), failedStep, execErr, recorder.planKeys()); err != nil {
			return report, errors.Join(execErr, fmt.Errorf("recording preparation outcome: %w", err))
		}
	}
	return report, execErr
}

// checkContainment rejects steps whose directories or declared files escape
// the worktree through symlinks, before any effect.
func checkContainment(worktreePath string, configs []config.StepConfig) error {
	if worktreePath == "" {
		return nil
	}
	for _, cfg := range configs {
		if err := config.CheckStepContainment(worktreePath, cfg); err != nil {
			return fmt.Errorf("getting scaffold steps: %w", err)
		}
	}
	return nil
}

func prepareDbSuffix(ctx *types.ScaffoldContext, worktreePath string, dryRun bool) error {
	migrated := false
	if !dryRun {
		var err error
		migrated, err = config.MigrateDbSuffixToLocal(worktreePath)
		if err != nil {
			return fmt.Errorf("migrating db_suffix: %w", err)
		}
	}

	localState, err := config.ReadLocalState(worktreePath)
	if err != nil {
		return fmt.Errorf("reading local state: %w", err)
	}
	if localState.DbSuffix != "" {
		ctx.SetDbSuffix(localState.DbSuffix)
		ctx.SetDbSuffixLoadedFromState()
		if migrated && len(localState.Databases) == 0 {
			ctx.SetDbSuffixLoadedFromLegacyState()
		}
		return nil
	}

	newSuffix := words.GenerateSuffix()
	ctx.SetDbSuffix(newSuffix)
	if dryRun {
		return nil
	}
	if err := config.WriteLocalState(worktreePath, config.LocalState{DbSuffix: newSuffix}); err != nil {
		return fmt.Errorf("writing db_suffix to local state: %w", err)
	}
	return nil
}

func (m *ScaffoldManager) RunCleanup(
	worktreePath, branch, repoName, siteName, preset string,
	cleanupSteps []config.CleanupStep,
	cfg *config.Config,
	dryRun, verbose, quiet bool,
) error {
	return m.RunCleanupWithOptions(
		worktreePath,
		branch,
		repoName,
		siteName,
		preset,
		cleanupSteps,
		cfg,
		CleanupOptions{DryRun: dryRun, Verbose: verbose, Quiet: quiet},
	)
}

func (m *ScaffoldManager) RunCleanupWithOptions(
	worktreePath, branch, repoName, siteName, preset string,
	cleanupSteps []config.CleanupStep,
	cfg *config.Config,
	cleanupOpts CleanupOptions,
) (runErr error) {
	ctx := m.newScaffoldContext(worktreePath, branch, repoName, siteName, preset)

	if !cleanupOpts.SkipDatabaseCleanup {
		if !cleanupOpts.DatabaseLockHeld {
			release, err := dblifecycle.Lock(worktreePath)
			if err != nil {
				return err
			}
			defer func() { runErr = errors.Join(runErr, release()) }()
		}
		ctx.DatabaseLockHeld = true
		if err := dblifecycle.CheckCleanup(worktreePath); err != nil {
			return err
		}
	}

	stepsList, err := m.GetCleanupSteps(cfg, worktreePath, branch, preset, cleanupSteps)
	if err != nil {
		return fmt.Errorf("getting cleanup steps: %w", err)
	}
	if cleanupOpts.SkipDatabaseCleanup {
		filtered := make([]types.ScaffoldStep, 0, len(stepsList))
		for _, step := range stepsList {
			if step.Name() != config.StepDbDestroy {
				filtered = append(filtered, step)
			}
		}
		stepsList = filtered
	}

	opts := m.stepOptionsFromFlags(cleanupOpts.DryRun, cleanupOpts.Verbose, cleanupOpts.Quiet)

	executor := NewStepExecutorWithOptions(
		stepsList,
		&ctx,
		opts,
		ExecutorOptions{DelegateDryRunToSteps: true},
	)
	if err := executor.Execute(); err != nil {
		return err
	}

	return nil
}

func (m *ScaffoldManager) newScaffoldContext(worktreePath, branch, repoName, siteName, preset string) types.ScaffoldContext {
	path := filepath.Base(worktreePath)
	repoPath := filepath.Base(filepath.Dir(worktreePath))
	return types.ScaffoldContext{
		WorktreePath: worktreePath,
		Branch:       branch,
		RepoName:     repoName,
		SiteName:     siteName,
		Preset:       preset,
		Env:          make(map[string]string),
		Path:         path,
		RepoPath:     repoPath,
		Vars:         make(map[string]string),
	}
}

func (m *ScaffoldManager) stepOptionsFromFlags(dryRun, verbose, quiet bool) types.StepOptions {
	return types.StepOptions{
		DryRun:  dryRun,
		Verbose: verbose,
		Quiet:   quiet,
	}
}

// runPreFlightChecks validates dependencies before scaffold execution.
// Returns an error with detailed information if any checks fail.
func (m *ScaffoldManager) runPreFlightChecks(ctx *types.ScaffoldContext, cfg *config.ScaffoldConfig) error {
	// Skip if no pre-flight configured
	if cfg.PreFlight == nil || len(cfg.PreFlight.Condition) == 0 {
		return nil
	}

	// Evaluate the condition
	result, err := ctx.EvaluateCondition(cfg.PreFlight.Condition)
	if err != nil {
		return fmt.Errorf("pre-flight check error: %w", err)
	}

	if !result {
		// Generate detailed error message showing what failed
		return m.generatePreFlightError(ctx, cfg.PreFlight.Condition)
	}

	return nil
}

// runPreFlightWithSpinner runs pre-flight checks with a spinner.
func (m *ScaffoldManager) runPreFlightWithSpinner(ctx *types.ScaffoldContext, cfg *config.ScaffoldConfig) error {
	// Skip if no pre-flight configured
	if cfg.PreFlight == nil || len(cfg.PreFlight.Condition) == 0 {
		return nil
	}

	var checkErr error
	err := ui.RunWithSpinner("Running pre-flight checks", func() error {
		checkErr = m.runPreFlightChecks(ctx, cfg)
		return checkErr
	})

	if err != nil {
		return err
	}

	return checkErr
}

// generatePreFlightError creates a detailed error message showing which checks failed.
func (m *ScaffoldManager) generatePreFlightError(ctx *types.ScaffoldContext, conditions map[string]any) error {
	var errorParts []string

	collected := m.collectPreFlightValues(conditions)

	missingEnv := uniqueStringsPreserveOrder(m.checkMissingEnvVars(collected.envs))
	if len(missingEnv) > 0 {
		errorParts = append(errorParts,
			fmt.Sprintf("Missing environment variables:\n  - %s",
				strings.Join(missingEnv, "\n  - ")))
	}

	missingCommands := uniqueStringsPreserveOrder(m.checkMissingCommands(collected.commands))
	if len(missingCommands) > 0 {
		errorParts = append(errorParts,
			fmt.Sprintf("Missing commands:\n  - %s",
				strings.Join(missingCommands, "\n  - ")))
	}

	missingFiles, fileErrors := m.checkMissingFiles(ctx, collected.files)
	missingFiles = uniqueStringsPreserveOrder(missingFiles)
	if len(missingFiles) > 0 {
		errorParts = append(errorParts,
			fmt.Sprintf("Missing files:\n  - %s",
				strings.Join(missingFiles, "\n  - ")))
	}

	fileErrors = uniqueStringsPreserveOrder(fileErrors)
	if len(fileErrors) > 0 {
		errorParts = append(errorParts,
			fmt.Sprintf("File check errors:\n  - %s",
				strings.Join(fileErrors, "\n  - ")))
	}

	if len(errorParts) > 0 {
		return fmt.Errorf("pre-flight checks failed:\n\n%s\n\nPlease resolve these issues and try again",
			strings.Join(errorParts, "\n\n"))
	}

	return fmt.Errorf("pre-flight checks failed")
}

type preFlightValues struct {
	envs     []string
	commands []string
	files    []string
}

func (m *ScaffoldManager) collectPreFlightValues(conditions map[string]any) preFlightValues {
	var values preFlightValues
	collectPreFlightValuesFromCondition(conditions, &values)
	return values
}

func collectPreFlightValuesFromCondition(condition any, values *preFlightValues) {
	switch v := condition.(type) {
	case map[string]any:
		if notValue, ok := v["not"]; ok {
			collectPreFlightValuesFromCondition(notValue, values)
			return
		}

		keys := make([]string, 0, len(v))
		for key := range v {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			value := v[key]
			switch key {
			case "env_exists":
				values.envs = append(values.envs, extractStringValues(value, "env")...)
			case "command_exists":
				values.commands = append(values.commands, extractStringValues(value, "command")...)
			case "file_exists":
				values.files = append(values.files, extractStringValues(value, "file")...)
			}
		}
	case []any:
		for _, item := range v {
			collectPreFlightValuesFromCondition(item, values)
		}
	case []map[string]any:
		for _, item := range v {
			collectPreFlightValuesFromCondition(item, values)
		}
	}
}

func extractStringValues(value any, mapKey string) []string {
	var values []string

	switch v := value.(type) {
	case string:
		values = append(values, v)
	case []string:
		values = append(values, v...)
	case []any:
		for _, item := range v {
			if s, ok := item.(string); ok {
				values = append(values, s)
			}
		}
	case map[string]any:
		if s, ok := v[mapKey].(string); ok {
			values = append(values, s)
		}
	}

	return values
}

func uniqueStringsPreserveOrder(values []string) []string {
	if len(values) == 0 {
		return values
	}

	seen := make(map[string]struct{}, len(values))
	unique := make([]string, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		unique = append(unique, value)
	}

	return unique
}

// checkMissingEnvVars returns list of environment variables that don't exist.
func (m *ScaffoldManager) checkMissingEnvVars(value any) []string {
	var missing []string

	for _, envName := range extractStringValues(value, "env") {
		if _, exists := os.LookupEnv(envName); !exists {
			missing = append(missing, envName)
		}
	}

	return missing
}

// checkMissingCommands returns list of commands that don't exist in PATH.
func (m *ScaffoldManager) checkMissingCommands(value any) []string {
	var missing []string

	for _, cmdName := range extractStringValues(value, "command") {
		if _, err := exec.LookPath(cmdName); err != nil {
			missing = append(missing, cmdName)
		}
	}

	return missing
}

// checkMissingFiles returns list of files that don't exist in worktree.
func (m *ScaffoldManager) checkMissingFiles(ctx *types.ScaffoldContext, value any) ([]string, []string) {
	var missing []string
	var errors []string

	for _, path := range extractStringValues(value, "file") {
		fullPath := filepath.Join(ctx.WorktreePath, path)
		if _, err := os.Stat(fullPath); err != nil {
			if os.IsNotExist(err) {
				missing = append(missing, path)
				continue
			}
			errors = append(errors, fmt.Sprintf("%s: %v", path, err))
		}
	}

	return missing, errors
}
