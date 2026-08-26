package config

import "testing"

func TestValidateStepConfig(t *testing.T) {
	tests := []struct {
		name     string
		stepName string
		cfg      StepConfig
		wantErr  string
	}{
		{
			name:     "file.copy accepts required fields",
			stepName: StepFileCopy,
			cfg: StepConfig{
				From: "source.txt",
				To:   "dest.txt",
			},
		},
		{
			name:     "file.copy reports missing from",
			stepName: StepFileCopy,
			cfg:      StepConfig{To: "dest.txt"},
			wantErr:  "file.copy: 'from' is required",
		},
		{
			name:     "file.copy reports missing to",
			stepName: StepFileCopy,
			cfg:      StepConfig{From: "source.txt"},
			wantErr:  "file.copy: 'to' is required",
		},
		{
			name:     "file.copy reports from before to when both are missing",
			stepName: StepFileCopy,
			wantErr:  "file.copy: 'from' is required",
		},
		{
			name:     "bash.run accepts a command",
			stepName: StepBashRun,
			cfg:      StepConfig{Command: "echo hello"},
		},
		{
			name:     "bash.run reports missing command",
			stepName: StepBashRun,
			wantErr:  "bash.run: 'command' is required",
		},
		{
			name:     "command.run accepts a command",
			stepName: StepCommandRun,
			cfg:      StepConfig{Command: "ls -la"},
		},
		{
			name:     "command.run reports missing command",
			stepName: StepCommandRun,
			wantErr:  "command.run: 'command' is required",
		},
		{
			name:     "env.read accepts a key",
			stepName: StepEnvRead,
			cfg:      StepConfig{Key: "DB_DATABASE", StoreAs: "Database", File: ".env.local"},
		},
		{
			name:     "env.read reports missing key",
			stepName: StepEnvRead,
			cfg:      StepConfig{StoreAs: "Database"},
			wantErr:  "env.read: 'key' is required",
		},
		{
			name:     "env.write accepts a key and value",
			stepName: StepEnvWrite,
			cfg:      StepConfig{Key: "DB_DATABASE", Value: "test_db", File: ".env.local"},
		},
		{
			name:     "env.write accepts an empty value",
			stepName: StepEnvWrite,
			cfg:      StepConfig{Key: "DB_DATABASE"},
		},
		{
			name:     "env.write reports missing key",
			stepName: StepEnvWrite,
			cfg:      StepConfig{Value: "test_db"},
			wantErr:  "env.write: 'key' is required",
		},
		{
			name:     "env.copy accepts one key",
			stepName: StepEnvCopy,
			cfg:      StepConfig{Source: "../main", Key: "API_KEY"},
		},
		{
			name:     "env.copy accepts multiple keys",
			stepName: StepEnvCopy,
			cfg:      StepConfig{Source: "../main", Keys: []string{"API_KEY", "API_SECRET"}},
		},
		{
			name:     "env.copy reports missing source before keys",
			stepName: StepEnvCopy,
			cfg:      StepConfig{Key: "API_KEY"},
			wantErr:  "env.copy: 'source' is required",
		},
		{
			name:     "env.copy reports missing key and keys",
			stepName: StepEnvCopy,
			cfg:      StepConfig{Source: "../main"},
			wantErr:  "env.copy: either 'key' or 'keys' must be specified",
		},
		{
			name:     "env.copy accepts source file with one key",
			stepName: StepEnvCopy,
			cfg: StepConfig{
				Source:     "../main",
				SourceFile: ".env.testing",
				Key:        "API_KEY",
				File:       ".env.local",
			},
		},
		{
			name:     "db.create accepts no fields",
			stepName: StepDbCreate,
		},
		{
			name:     "db.create accepts optional type and args",
			stepName: StepDbCreate,
			cfg:      StepConfig{Type: "mysql", Args: []string{"--charset=utf8mb4"}},
		},
		{
			name:     "db.create accepts an empty role",
			stepName: StepDbCreate,
			cfg:      StepConfig{Role: ""},
		},
		{
			name:     "db.create accepts the application role",
			stepName: StepDbCreate,
			cfg:      StepConfig{Role: DbRoleApplication},
		},
		{
			name:     "db.create accepts the testing role",
			stepName: StepDbCreate,
			cfg:      StepConfig{Role: DbRoleTesting},
		},
		{
			name:     "db.create reports an unsupported role",
			stepName: StepDbCreate,
			cfg:      StepConfig{Role: "staging"},
			wantErr:  `db.create: invalid role "staging" (supported roles: application, testing)`,
		},
		{
			name:     "db.destroy accepts optional type args and role",
			stepName: StepDbDestroy,
			cfg:      StepConfig{Type: "sqlite", Args: []string{"--force"}, Role: "staging"},
		},
		{
			name:     "db.destroy accepts no fields",
			stepName: StepDbDestroy,
		},
		{
			name:     "php is a binary fallback",
			stepName: "php",
			cfg:      StepConfig{Args: []string{"-v"}},
		},
		{
			name:     "php.composer is a binary fallback",
			stepName: "php.composer",
			cfg:      StepConfig{Args: []string{"install"}},
		},
		{
			name:     "php.laravel is a binary fallback",
			stepName: "php.laravel",
			cfg:      StepConfig{Args: []string{"migrate"}},
		},
		{
			name:     "node.npm is a binary fallback",
			stepName: "node.npm",
			cfg:      StepConfig{Args: []string{"install"}},
		},
		{
			name:     "node.yarn is a binary fallback",
			stepName: "node.yarn",
			cfg:      StepConfig{Args: []string{"install"}},
		},
		{
			name:     "node.pnpm is a binary fallback",
			stepName: "node.pnpm",
			cfg:      StepConfig{Args: []string{"install"}},
		},
		{
			name:     "node.bun is a binary fallback",
			stepName: "node.bun",
			cfg:      StepConfig{Args: []string{"install"}},
		},
		{
			name:     "herd is a binary fallback",
			stepName: "herd",
			cfg:      StepConfig{Args: []string{"link"}},
		},
		{
			name:     "yerd is a binary fallback",
			stepName: "yerd",
			cfg:      StepConfig{Args: []string{"link"}},
		},
		{
			name:     "custom names use the binary fallback",
			stepName: "custom.step",
			cfg:      StepConfig{Name: "custom.step", StoreAs: "result"},
		},
		{
			name:    "an empty name reports the binary error",
			cfg:     StepConfig{Args: []string{"install"}},
			wantErr: "binary step: 'name' is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateStepConfig(tt.stepName, tt.cfg)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("ValidateStepConfig() unexpected error = %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("ValidateStepConfig() expected error %q but got nil", tt.wantErr)
			}
			if err.Error() != tt.wantErr {
				t.Errorf("ValidateStepConfig() error = %q, want %q", err.Error(), tt.wantErr)
			}
		})
	}
}
