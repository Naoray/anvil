package scaffold

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/naoray/anvil/internal/config"
	"github.com/naoray/anvil/internal/scaffold/steps"
	"github.com/naoray/anvil/internal/scaffold/types"
)

func TestIntegration_TemplateReplacementChain(t *testing.T) {
	t.Run("env.read sets variable used by env.write", func(t *testing.T) {
		tmpDir := t.TempDir()

		envContent := `DB_CONNECTION=mysql
DB_HOST=127.0.0.1
DB_USERNAME=root
DB_PASSWORD=root
APP_NAME=original_app
`
		envFile := filepath.Join(tmpDir, ".env")
		require.NoError(t, os.WriteFile(envFile, []byte(envContent), 0644))

		ctx := &types.ScaffoldContext{
			WorktreePath: tmpDir,
			SiteName:     "newapp",
			Branch:       "test",
		}

		readStep, err := steps.Create("env.read", config.StepConfig{Key: "APP_NAME", StoreAs: "OriginalApp"})
		require.NoError(t, err)
		err = readStep.Run(ctx, types.StepOptions{Verbose: false})
		require.NoError(t, err)
		assert.Equal(t, "original_app", ctx.GetVar("OriginalApp"))

		writeStep, err := steps.Create("env.write", config.StepConfig{Key: "NEW_APP", Value: "{{ .SiteName }}"})
		require.NoError(t, err)
		err = writeStep.Run(ctx, types.StepOptions{Verbose: false})
		require.NoError(t, err)

		content, err := os.ReadFile(envFile)
		require.NoError(t, err)
		assert.Contains(t, string(content), "NEW_APP=newapp")
	})
}

func TestIntegration_DatabaseCreationWithEnv(t *testing.T) {
	t.Run("db.create generates suffix and persists to local state", func(t *testing.T) {
		tmpDir := t.TempDir()

		envContent := `DB_CONNECTION=mysql
DB_HOST=127.0.0.1
DB_USERNAME=root
DB_PASSWORD=root
APP_NAME=myapp
`
		envFile := filepath.Join(tmpDir, ".env")
		require.NoError(t, os.WriteFile(envFile, []byte(envContent), 0644))

		ctx := &types.ScaffoldContext{
			WorktreePath: tmpDir,
			SiteName:     "myapp",
			Branch:       "test",
		}

		mockClient := steps.NewMockDatabaseClient()
		dbStep := steps.NewDbCreateStepWithFactory(config.StepConfig{}, steps.MockClientFactory(mockClient))
		require.NotNil(t, dbStep)
		err := dbStep.Run(ctx, types.StepOptions{Verbose: false})
		require.NoError(t, err)

		suffix := ctx.GetDbSuffix()
		assert.NotEmpty(t, suffix, "DbSuffix should be set after db.create")

		localState, err := config.ReadLocalState(tmpDir)
		require.NoError(t, err)
		assert.Equal(t, suffix, localState.DbSuffix, "DbSuffix should be persisted to .anvil.local")

		parts := strings.Split(suffix, "_")
		assert.Len(t, parts, 2, "Suffix should be in format {adjective}_{noun}")
	})
}

func TestIntegration_EnvReadWriteFlow(t *testing.T) {
	t.Run("env.read → env.write → binary step with template variables", func(t *testing.T) {
		tmpDir := t.TempDir()

		envContent := `APP_NAME=original
`
		envFile := filepath.Join(tmpDir, ".env")
		require.NoError(t, os.WriteFile(envFile, []byte(envContent), 0644))

		ctx := &types.ScaffoldContext{
			WorktreePath: tmpDir,
			SiteName:     "newapp",
			Path:         "feature-auth",
		}

		readStep, err := steps.Create("env.read", config.StepConfig{Key: "APP_NAME", StoreAs: "OriginalName"})
		require.NoError(t, err)
		err = readStep.Run(ctx, types.StepOptions{Verbose: false})
		require.NoError(t, err)

		writeStep, err := steps.Create("env.write", config.StepConfig{Key: "NEW_NAME", Value: "{{ .SiteName }}_{{ .Path }}"})
		require.NoError(t, err)
		err = writeStep.Run(ctx, types.StepOptions{Verbose: false})
		require.NoError(t, err)

		content, err := os.ReadFile(envFile)
		require.NoError(t, err)
		assert.Contains(t, string(content), "NEW_NAME=newapp_feature-auth")
		assert.Contains(t, string(content), "APP_NAME=original")
	})
}

func TestIntegration_DatabaseCreateEnvWriteMigrate(t *testing.T) {
	t.Run("db.create → env.write → template in write step", func(t *testing.T) {
		tmpDir := t.TempDir()

		envContent := `DB_CONNECTION=mysql
DB_HOST=127.0.0.1
DB_USERNAME=root
DB_PASSWORD=root
APP_NAME=myapp
`
		envFile := filepath.Join(tmpDir, ".env")
		require.NoError(t, os.WriteFile(envFile, []byte(envContent), 0644))

		ctx := &types.ScaffoldContext{
			WorktreePath: tmpDir,
			SiteName:     "myapp",
			Branch:       "test",
		}

		mockClient := steps.NewMockDatabaseClient()
		dbStep := steps.NewDbCreateStepWithFactory(config.StepConfig{}, steps.MockClientFactory(mockClient))
		require.NotNil(t, dbStep)
		err := dbStep.Run(ctx, types.StepOptions{Verbose: false})
		require.NoError(t, err)

		suffix := ctx.GetDbSuffix()
		assert.NotEmpty(t, suffix)

		writeStep, err := steps.Create("env.write", config.StepConfig{Key: "DB_DATABASE", Value: "{{ .SiteName }}_{{ .DbSuffix }}"})
		require.NoError(t, err)
		err = writeStep.Run(ctx, types.StepOptions{Verbose: false})
		require.NoError(t, err)

		content, err := os.ReadFile(envFile)
		require.NoError(t, err)
		expectedDbName := "myapp_" + suffix
		assert.Contains(t, string(content), "DB_DATABASE="+expectedDbName)
	})
}

func TestIntegration_DatabaseDestroyCleanup(t *testing.T) {
	t.Run("db.destroy reads suffix from local state and cleans up", func(t *testing.T) {
		tmpDir := t.TempDir()

		envContent := `DB_CONNECTION=mysql
DB_HOST=127.0.0.1
DB_USERNAME=root
DB_PASSWORD=root
APP_NAME=myapp
`
		envFile := filepath.Join(tmpDir, ".env")
		require.NoError(t, os.WriteFile(envFile, []byte(envContent), 0644))

		err := config.WriteLocalState(tmpDir, config.LocalState{DbSuffix: "swift_runner"})
		require.NoError(t, err)

		ctx := &types.ScaffoldContext{
			WorktreePath: tmpDir,
		}

		destroyStep, err := steps.Create("db.destroy", config.StepConfig{})
		require.NoError(t, err)
		err = destroyStep.Run(ctx, types.StepOptions{Verbose: false})
		require.NoError(t, err)

		suffix := ctx.GetDbSuffix()
		assert.Equal(t, "swift_runner", suffix, "DbSuffix should be read from local state")
	})
}

func TestIntegration_BunIntegration(t *testing.T) {
	t.Run("node.bun step is registered and functional", func(t *testing.T) {
		step, err := steps.Create("node.bun", config.StepConfig{
			Args: []string{"--version"},
		})

		require.NoError(t, err)
		assert.Equal(t, "node.bun", step.Name())
	})
}

func TestIntegration_FullLifecycle(t *testing.T) {
	t.Run("simulate full workflow: create db, write env, cleanup", func(t *testing.T) {
		tmpDir := t.TempDir()

		envContent := `DB_CONNECTION=mysql
DB_HOST=127.0.0.1
DB_USERNAME=root
DB_PASSWORD=root
APP_NAME=myapp
`
		envFile := filepath.Join(tmpDir, ".env")
		require.NoError(t, os.WriteFile(envFile, []byte(envContent), 0644))

		ctx := &types.ScaffoldContext{
			WorktreePath: tmpDir,
			SiteName:     "myapp",
			Branch:       "test",
			Path:         "feature-auth",
		}

		mockClient := steps.NewMockDatabaseClient()
		dbStep := steps.NewDbCreateStepWithFactory(config.StepConfig{}, steps.MockClientFactory(mockClient))
		require.NotNil(t, dbStep)
		err := dbStep.Run(ctx, types.StepOptions{Verbose: false})
		require.NoError(t, err)

		suffix := ctx.GetDbSuffix()
		assert.NotEmpty(t, suffix)

		writeDbStep, err := steps.Create("env.write", config.StepConfig{Key: "DB_DATABASE", Value: "{{ .SiteName }}_{{ .DbSuffix }}"})
		require.NoError(t, err)
		err = writeDbStep.Run(ctx, types.StepOptions{Verbose: false})
		require.NoError(t, err)

		content, err := os.ReadFile(envFile)
		require.NoError(t, err)
		expectedDbName := "myapp_" + suffix
		assert.Contains(t, string(content), "DB_DATABASE="+expectedDbName)

		writeDomainStep, err := steps.Create("env.write", config.StepConfig{Key: "APP_DOMAIN", Value: "app.{{ .Path }}.test"})
		require.NoError(t, err)
		err = writeDomainStep.Run(ctx, types.StepOptions{Verbose: false})
		require.NoError(t, err)

		content, err = os.ReadFile(envFile)
		require.NoError(t, err)
		assert.Contains(t, string(content), "APP_DOMAIN=app.feature-auth.test")

		destroyStep := steps.NewDbDestroyStepWithFactory(config.StepConfig{}, steps.MockClientFactory(mockClient))
		require.NotNil(t, destroyStep)
		err = destroyStep.Run(ctx, types.StepOptions{Verbose: false})
		require.NoError(t, err)

		destroyedSuffix := ctx.GetDbSuffix()
		assert.NotEmpty(t, destroyedSuffix, "DbSuffix should still be set after destroy")
	})
}

func TestIntegration_RunScaffoldSuffixLoading(t *testing.T) {
	t.Run("RunScaffold loads existing suffix from local state", func(t *testing.T) {
		tmpDir := t.TempDir()

		envContent := `DB_CONNECTION=mysql
DB_HOST=127.0.0.1
DB_USERNAME=root
DB_PASSWORD=root
APP_NAME=myapp
`
		envFile := filepath.Join(tmpDir, ".env")
		require.NoError(t, os.WriteFile(envFile, []byte(envContent), 0644))

		existingSuffix := "existing_suffix"
		err := config.WriteLocalState(tmpDir, config.LocalState{DbSuffix: existingSuffix})
		require.NoError(t, err)

		cfg := &config.Config{Preset: ""}
		manager := NewScaffoldManager()

		err = manager.RunScaffold(tmpDir, "test", "myrepo", "myapp", "", cfg, false, false, false)
		require.NoError(t, err)

		localStateAfter, err := config.ReadLocalState(tmpDir)
		require.NoError(t, err)
		assert.Equal(t, existingSuffix, localStateAfter.DbSuffix, "RunScaffold should preserve existing suffix from local state")
	})

	t.Run("RunScaffold generates new suffix when none exists", func(t *testing.T) {
		tmpDir := t.TempDir()

		envContent := `DB_CONNECTION=mysql
DB_HOST=127.0.0.1
DB_USERNAME=root
DB_PASSWORD=root
APP_NAME=myapp
`
		envFile := filepath.Join(tmpDir, ".env")
		require.NoError(t, os.WriteFile(envFile, []byte(envContent), 0644))

		cfg := &config.Config{Preset: ""}
		manager := NewScaffoldManager()

		err := manager.RunScaffold(tmpDir, "test", "myrepo", "myapp", "", cfg, false, false, false)
		require.NoError(t, err)

		localStateAfter, err := config.ReadLocalState(tmpDir)
		require.NoError(t, err)
		assert.NotEmpty(t, localStateAfter.DbSuffix, "RunScaffold should generate new suffix when none exists in local state")

		parts := strings.Split(localStateAfter.DbSuffix, "_")
		assert.Len(t, parts, 2, "Suffix should be in format {adjective}_{noun}")
	})
}

func TestIntegration_MultipleDatabasesSharedSuffix(t *testing.T) {
	t.Run("multiple db.create steps share same suffix", func(t *testing.T) {
		tmpDir := t.TempDir()

		envContent := `DB_CONNECTION=mysql
DB_HOST=127.0.0.1
DB_USERNAME=root
DB_PASSWORD=root
APP_NAME=myapp
`
		envFile := filepath.Join(tmpDir, ".env")
		require.NoError(t, os.WriteFile(envFile, []byte(envContent), 0644))

		ctx := &types.ScaffoldContext{
			WorktreePath: tmpDir,
			SiteName:     "myapp",
			Branch:       "test",
			Path:         "feature-test",
			Env:          make(map[string]string),
			Vars:         make(map[string]string),
		}

		mockClient := steps.NewMockDatabaseClient()
		factory := steps.MockClientFactory(mockClient)

		appStep := steps.NewDbCreateStepWithFactory(config.StepConfig{Args: []string{"--prefix", "app"}}, factory)
		require.NotNil(t, appStep)
		err := appStep.Run(ctx, types.StepOptions{Verbose: false})
		require.NoError(t, err)

		firstSuffix := ctx.GetDbSuffix()
		assert.NotEmpty(t, firstSuffix, "First db.create should set suffix")

		quotesStep := steps.NewDbCreateStepWithFactory(config.StepConfig{Args: []string{"--prefix", "quotes"}}, factory)
		require.NotNil(t, quotesStep)
		err = quotesStep.Run(ctx, types.StepOptions{Verbose: false})
		require.NoError(t, err)

		secondSuffix := ctx.GetDbSuffix()
		assert.NotEmpty(t, secondSuffix, "Second db.create should set suffix")

		knowledgeStep := steps.NewDbCreateStepWithFactory(config.StepConfig{Args: []string{"--prefix", "knowledge"}}, factory)
		require.NotNil(t, knowledgeStep)
		err = knowledgeStep.Run(ctx, types.StepOptions{Verbose: false})
		require.NoError(t, err)

		thirdSuffix := ctx.GetDbSuffix()
		assert.NotEmpty(t, thirdSuffix, "Third db.create should set suffix")

		assert.Equal(t, firstSuffix, secondSuffix, "All three databases should use the same suffix")
		assert.Equal(t, secondSuffix, thirdSuffix, "All three databases should use the same suffix")

		localState, err := config.ReadLocalState(tmpDir)
		require.NoError(t, err)
		assert.Equal(t, firstSuffix, localState.DbSuffix, "Suffix should be persisted to local state")

		createCalls := mockClient.GetCreateCalls()
		assert.Len(t, createCalls, 3, "Should have created 3 databases")
		assert.True(t, strings.HasPrefix(createCalls[0], "app_"), "First db should use 'app' prefix")
		assert.True(t, strings.HasPrefix(createCalls[1], "quotes_"), "Second db should use 'quotes' prefix")
		assert.True(t, strings.HasPrefix(createCalls[2], "knowledge_"), "Third db should use 'knowledge' prefix")
	})
}

func TestIntegration_SanitizedSiteNameForDatabase(t *testing.T) {
	t.Run("db.create with hyphenated sitename → env.write with SanitizedSiteName matches", func(t *testing.T) {
		tmpDir := t.TempDir()

		envContent := `DB_CONNECTION=mysql
DB_HOST=127.0.0.1
DB_USERNAME=root
DB_PASSWORD=root
APP_NAME=some-feature
`
		envFile := filepath.Join(tmpDir, ".env")
		require.NoError(t, os.WriteFile(envFile, []byte(envContent), 0644))

		ctx := &types.ScaffoldContext{
			WorktreePath: tmpDir,
			SiteName:     "some-feature",
			Branch:       "test",
		}

		mockClient := steps.NewMockDatabaseClient()
		dbStep := steps.NewDbCreateStepWithFactory(config.StepConfig{}, steps.MockClientFactory(mockClient))
		require.NotNil(t, dbStep)
		err := dbStep.Run(ctx, types.StepOptions{Verbose: false})
		require.NoError(t, err)

		suffix := ctx.GetDbSuffix()
		assert.NotEmpty(t, suffix)

		createCalls := mockClient.GetCreateCalls()
		require.Len(t, createCalls, 1)
		actualDbName := createCalls[0]
		assert.True(t, strings.HasPrefix(actualDbName, "some_feature_"), "Database should be created with sanitized name (underscores)")

		writeStep, err := steps.Create("env.write", config.StepConfig{Key: "DB_DATABASE", Value: "{{ .SanitizedSiteName }}_{{ .DbSuffix }}"})
		require.NoError(t, err)
		err = writeStep.Run(ctx, types.StepOptions{Verbose: false})
		require.NoError(t, err)

		content, err := os.ReadFile(envFile)
		require.NoError(t, err)
		expectedDbName := "some_feature_" + suffix
		assert.Contains(t, string(content), "DB_DATABASE="+expectedDbName, "env.write should use SanitizedSiteName to match actual database name")
		assert.Equal(t, actualDbName, expectedDbName, "Database name from db.create should match env.write value")
	})
}

func TestIntegration_PreFlightChecks(t *testing.T) {
	t.Run("pre-flight success - all dependencies exist", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Create test file
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "test.txt"), []byte("test"), 0644))

		// Set test env var
		t.Setenv("TEST_VAR_1", "value1")

		cfg := &config.Config{
			Scaffold: config.ScaffoldConfig{
				PreFlight: &config.PreFlight{
					Condition: map[string]interface{}{
						"env_exists":     []interface{}{"TEST_VAR_1"},
						"command_exists": []interface{}{"go"},
						"file_exists":    []interface{}{"test.txt"},
					},
				},
				Steps: []config.StepConfig{},
			},
		}

		manager := NewScaffoldManager()
		err := manager.RunScaffold(tmpDir, "test", "testrepo", "testsite", "", cfg, false, false, true)
		assert.NoError(t, err, "Pre-flight should pass when all dependencies exist")
	})

	t.Run("pre-flight failure - map form conditions", func(t *testing.T) {
		tmpDir := t.TempDir()

		cfg := &config.Config{
			Scaffold: config.ScaffoldConfig{
				PreFlight: &config.PreFlight{
					Condition: map[string]interface{}{
						"env_exists":     map[string]interface{}{"env": "NONEXISTENT_MAP_ENV"},
						"command_exists": map[string]interface{}{"command": "nonexistent-map-command"},
						"file_exists":    map[string]interface{}{"file": "missing-map-file.txt"},
					},
				},
				Steps: []config.StepConfig{},
			},
		}

		manager := NewScaffoldManager()
		err := manager.RunScaffold(tmpDir, "test", "testrepo", "testsite", "", cfg, false, false, true)
		require.Error(t, err, "Pre-flight should fail when map form dependencies are missing")
		assert.Contains(t, err.Error(), "Missing environment variables")
		assert.Contains(t, err.Error(), "NONEXISTENT_MAP_ENV")
		assert.Contains(t, err.Error(), "Missing commands")
		assert.Contains(t, err.Error(), "nonexistent-map-command")
		assert.Contains(t, err.Error(), "Missing files")
		assert.Contains(t, err.Error(), "missing-map-file.txt")
	})

	t.Run("pre-flight failure - nested not condition", func(t *testing.T) {
		tmpDir := t.TempDir()
		t.Setenv("NESTED_MISSING_ENV", "present")

		cfg := &config.Config{
			Scaffold: config.ScaffoldConfig{
				PreFlight: &config.PreFlight{
					Condition: map[string]interface{}{
						"not": map[string]interface{}{
							"env_exists": []interface{}{"NESTED_MISSING_ENV"},
						},
					},
				},
				Steps: []config.StepConfig{},
			},
		}

		manager := NewScaffoldManager()
		err := manager.RunScaffold(tmpDir, "test", "testrepo", "testsite", "", cfg, false, false, true)
		require.Error(t, err, "Pre-flight should fail when nested condition fails")
		assert.EqualError(t, err, "pre-flight checks failed")
	})

	t.Run("pre-flight failure - missing env var", func(t *testing.T) {
		tmpDir := t.TempDir()

		cfg := &config.Config{
			Scaffold: config.ScaffoldConfig{
				PreFlight: &config.PreFlight{
					Condition: map[string]interface{}{
						"env_exists": []interface{}{"NONEXISTENT_VAR_12345"},
					},
				},
				Steps: []config.StepConfig{},
			},
		}

		manager := NewScaffoldManager()
		err := manager.RunScaffold(tmpDir, "test", "testrepo", "testsite", "", cfg, false, false, true)
		assert.Error(t, err, "Pre-flight should fail when env var is missing")
		assert.Contains(t, err.Error(), "pre-flight checks failed")
		assert.Contains(t, err.Error(), "Missing environment variables")
		assert.Contains(t, err.Error(), "NONEXISTENT_VAR_12345")
	})

	t.Run("pre-flight failure - missing command", func(t *testing.T) {
		tmpDir := t.TempDir()

		cfg := &config.Config{
			Scaffold: config.ScaffoldConfig{
				PreFlight: &config.PreFlight{
					Condition: map[string]interface{}{
						"command_exists": []interface{}{"nonexistentcommand12345"},
					},
				},
				Steps: []config.StepConfig{},
			},
		}

		manager := NewScaffoldManager()
		err := manager.RunScaffold(tmpDir, "test", "testrepo", "testsite", "", cfg, false, false, true)
		assert.Error(t, err, "Pre-flight should fail when command is missing")
		assert.Contains(t, err.Error(), "pre-flight checks failed")
		assert.Contains(t, err.Error(), "Missing commands")
		assert.Contains(t, err.Error(), "nonexistentcommand12345")
	})

	t.Run("pre-flight failure - missing file", func(t *testing.T) {
		tmpDir := t.TempDir()

		cfg := &config.Config{
			Scaffold: config.ScaffoldConfig{
				PreFlight: &config.PreFlight{
					Condition: map[string]interface{}{
						"file_exists": []interface{}{"nonexistent.txt"},
					},
				},
				Steps: []config.StepConfig{},
			},
		}

		manager := NewScaffoldManager()
		err := manager.RunScaffold(tmpDir, "test", "testrepo", "testsite", "", cfg, false, false, true)
		assert.Error(t, err, "Pre-flight should fail when file is missing")
		assert.Contains(t, err.Error(), "pre-flight checks failed")
		assert.Contains(t, err.Error(), "Missing files")
		assert.Contains(t, err.Error(), "nonexistent.txt")
	})

	t.Run("pre-flight failure - multiple missing dependencies", func(t *testing.T) {
		tmpDir := t.TempDir()

		cfg := &config.Config{
			Scaffold: config.ScaffoldConfig{
				PreFlight: &config.PreFlight{
					Condition: map[string]interface{}{
						"env_exists":     []interface{}{"MISSING_VAR_1", "MISSING_VAR_2"},
						"command_exists": []interface{}{"missingcmd1", "missingcmd2"},
						"file_exists":    []interface{}{"missing1.txt", "missing2.txt"},
					},
				},
				Steps: []config.StepConfig{},
			},
		}

		manager := NewScaffoldManager()
		err := manager.RunScaffold(tmpDir, "test", "testrepo", "testsite", "", cfg, false, false, true)
		assert.Error(t, err, "Pre-flight should fail when multiple dependencies are missing")
		assert.Contains(t, err.Error(), "pre-flight checks failed")
		assert.Contains(t, err.Error(), "Missing environment variables")
		assert.Contains(t, err.Error(), "MISSING_VAR_1")
		assert.Contains(t, err.Error(), "MISSING_VAR_2")
		assert.Contains(t, err.Error(), "Missing commands")
		assert.Contains(t, err.Error(), "missingcmd1")
		assert.Contains(t, err.Error(), "missingcmd2")
		assert.Contains(t, err.Error(), "Missing files")
		assert.Contains(t, err.Error(), "missing1.txt")
		assert.Contains(t, err.Error(), "missing2.txt")
	})

	t.Run("no pre-flight configured - scaffold runs normally", func(t *testing.T) {
		tmpDir := t.TempDir()

		cfg := &config.Config{
			Scaffold: config.ScaffoldConfig{
				Steps: []config.StepConfig{},
			},
		}

		manager := NewScaffoldManager()
		err := manager.RunScaffold(tmpDir, "test", "testrepo", "testsite", "", cfg, false, false, true)
		assert.NoError(t, err, "Scaffold should run normally when no pre-flight is configured")
	})

	t.Run("pre-flight with mixed results - some exist, some don't", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Create one file but not another
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "exists.txt"), []byte("test"), 0644))

		cfg := &config.Config{
			Scaffold: config.ScaffoldConfig{
				PreFlight: &config.PreFlight{
					Condition: map[string]interface{}{
						"file_exists": []interface{}{"exists.txt", "missing.txt"},
					},
				},
				Steps: []config.StepConfig{},
			},
		}

		manager := NewScaffoldManager()
		err := manager.RunScaffold(tmpDir, "test", "testrepo", "testsite", "", cfg, false, false, true)
		assert.Error(t, err, "Pre-flight should fail when ANY file is missing")
		assert.Contains(t, err.Error(), "Missing files")
		assert.Contains(t, err.Error(), "missing.txt")
		assert.NotContains(t, err.Error(), "exists.txt", "Should not list files that exist")
	})
}
