package config

import (
	"testing"
)

func TestValidateStepConfig(t *testing.T) {
	tests := []struct {
		name     string
		stepName string
		cfg      StepConfig
		wantErr  bool
		errMsg   string
	}{
		{
			name:     "file.copy with all required fields",
			stepName: "file.copy",
			cfg: StepConfig{
				From: "source.txt",
				To:   "dest.txt",
			},
			wantErr: false,
		},
		{
			name:     "file.copy missing from",
			stepName: "file.copy",
			cfg: StepConfig{
				To: "dest.txt",
			},
			wantErr: true,
			errMsg:  "file.copy: 'from' is required",
		},
		{
			name:     "file.copy missing to",
			stepName: "file.copy",
			cfg: StepConfig{
				From: "source.txt",
			},
			wantErr: true,
			errMsg:  "file.copy: 'to' is required",
		},
		{
			name:     "bash.run with command",
			stepName: "bash.run",
			cfg: StepConfig{
				Command: "echo hello",
			},
			wantErr: false,
		},
		{
			name:     "bash.run missing command",
			stepName: "bash.run",
			cfg:      StepConfig{},
			wantErr:  true,
			errMsg:   "bash.run: 'command' is required",
		},
		{
			name:     "command.run with command",
			stepName: "command.run",
			cfg: StepConfig{
				Command: "ls -la",
			},
			wantErr: false,
		},
		{
			name:     "command.run missing command",
			stepName: "command.run",
			cfg:      StepConfig{},
			wantErr:  true,
			errMsg:   "command.run: 'command' is required",
		},
		{
			name:     "env.read with key",
			stepName: "env.read",
			cfg: StepConfig{
				Key:     "DB_DATABASE",
				StoreAs: "Database",
			},
			wantErr: false,
		},
		{
			name:     "env.read missing key",
			stepName: "env.read",
			cfg: StepConfig{
				StoreAs: "Database",
			},
			wantErr: true,
			errMsg:  "env.read: 'key' is required",
		},
		{
			name:     "env.write with key",
			stepName: "env.write",
			cfg: StepConfig{
				Key:   "DB_DATABASE",
				Value: "test_db",
			},
			wantErr: false,
		},
		{
			name:     "env.write missing key",
			stepName: "env.write",
			cfg: StepConfig{
				Value: "test_db",
			},
			wantErr: true,
			errMsg:  "env.write: 'key' is required",
		},
		{
			name:     "env.copy with source and key",
			stepName: "env.copy",
			cfg: StepConfig{
				Source: "../main",
				Key:    "API_KEY",
			},
			wantErr: false,
		},
		{
			name:     "env.copy with source and keys",
			stepName: "env.copy",
			cfg: StepConfig{
				Source: "../main",
				Keys:   []string{"API_KEY", "API_SECRET"},
			},
			wantErr: false,
		},
		{
			name:     "env.copy missing source",
			stepName: "env.copy",
			cfg: StepConfig{
				Key: "API_KEY",
			},
			wantErr: true,
			errMsg:  "env.copy: 'source' is required",
		},
		{
			name:     "env.copy missing key and keys",
			stepName: "env.copy",
			cfg: StepConfig{
				Source: "../main",
			},
			wantErr: true,
			errMsg:  "env.copy: either 'key' or 'keys' must be specified",
		},
		{
			name:     "db.create with optional fields",
			stepName: "db.create",
			cfg: StepConfig{
				Type: "mysql",
				Args: []string{"--charset=utf8mb4"},
			},
			wantErr: false,
		},
		{
			name:     "db.create with no fields",
			stepName: "db.create",
			cfg:      StepConfig{},
			wantErr:  false,
		},
		{
			name:     "db.destroy with optional fields",
			stepName: "db.destroy",
			cfg: StepConfig{
				Type: "mysql",
			},
			wantErr: false,
		},
		{
			name:     "php binary step with name only",
			stepName: "php",
			cfg: StepConfig{
				Args: []string{"-v"},
			},
			wantErr: false,
		},
		{
			name:     "npm binary step with args",
			stepName: "node.npm",
			cfg: StepConfig{
				Args: []string{"install"},
			},
			wantErr: false,
		},
		{
			name:     "binary step missing name",
			stepName: "",
			cfg: StepConfig{
				Args: []string{"install"},
			},
			wantErr: true,
			errMsg:  "binary step: 'name' is required",
		},
		{
			name:     "unknown step treated as binary",
			stepName: "custom.step",
			cfg:      StepConfig{},
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateStepConfig(tt.stepName, tt.cfg)
			if tt.wantErr {
				if err == nil {
					t.Errorf("ValidateStepConfig() expected error but got nil")
					return
				}
				if tt.errMsg != "" && err.Error() != tt.errMsg {
					t.Errorf("ValidateStepConfig() error = %v, want %v", err.Error(), tt.errMsg)
				}
			} else {
				if err != nil {
					t.Errorf("ValidateStepConfig() unexpected error = %v", err)
				}
			}
		})
	}
}

func TestFileCopyConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  FileCopyConfig
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid config",
			config: FileCopyConfig{
				BaseStepConfig: BaseStepConfig{Name: "file.copy"},
				From:           "source.txt",
				To:             "dest.txt",
			},
			wantErr: false,
		},
		{
			name: "missing from",
			config: FileCopyConfig{
				BaseStepConfig: BaseStepConfig{Name: "file.copy"},
				To:             "dest.txt",
			},
			wantErr: true,
			errMsg:  "file.copy: 'from' is required",
		},
		{
			name: "missing to",
			config: FileCopyConfig{
				BaseStepConfig: BaseStepConfig{Name: "file.copy"},
				From:           "source.txt",
			},
			wantErr: true,
			errMsg:  "file.copy: 'to' is required",
		},
		{
			name: "missing both",
			config: FileCopyConfig{
				BaseStepConfig: BaseStepConfig{Name: "file.copy"},
			},
			wantErr: true,
			errMsg:  "file.copy: 'from' is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr {
				if err == nil {
					t.Errorf("Validate() expected error but got nil")
					return
				}
				if tt.errMsg != "" && err.Error() != tt.errMsg {
					t.Errorf("Validate() error = %v, want %v", err.Error(), tt.errMsg)
				}
			} else {
				if err != nil {
					t.Errorf("Validate() unexpected error = %v", err)
				}
			}
		})
	}
}

func TestBashRunConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  BashRunConfig
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid config with command",
			config: BashRunConfig{
				BaseStepConfig: BaseStepConfig{Name: "bash.run"},
				Command:        "echo hello",
			},
			wantErr: false,
		},
		{
			name: "missing command",
			config: BashRunConfig{
				BaseStepConfig: BaseStepConfig{Name: "bash.run"},
			},
			wantErr: true,
			errMsg:  "bash.run: 'command' is required",
		},
		{
			name: "empty command",
			config: BashRunConfig{
				BaseStepConfig: BaseStepConfig{Name: "bash.run"},
				Command:        "",
			},
			wantErr: true,
			errMsg:  "bash.run: 'command' is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr {
				if err == nil {
					t.Errorf("Validate() expected error but got nil")
					return
				}
				if tt.errMsg != "" && err.Error() != tt.errMsg {
					t.Errorf("Validate() error = %v, want %v", err.Error(), tt.errMsg)
				}
			} else {
				if err != nil {
					t.Errorf("Validate() unexpected error = %v", err)
				}
			}
		})
	}
}

func TestEnvReadConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  EnvReadConfig
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid config with key",
			config: EnvReadConfig{
				BaseStepConfig: BaseStepConfig{Name: "env.read"},
				Key:            "DB_DATABASE",
				StoreAs:        "Database",
			},
			wantErr: false,
		},
		{
			name: "valid config with key only",
			config: EnvReadConfig{
				BaseStepConfig: BaseStepConfig{Name: "env.read"},
				Key:            "DB_DATABASE",
			},
			wantErr: false,
		},
		{
			name: "missing key",
			config: EnvReadConfig{
				BaseStepConfig: BaseStepConfig{Name: "env.read"},
				StoreAs:        "Database",
			},
			wantErr: true,
			errMsg:  "env.read: 'key' is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr {
				if err == nil {
					t.Errorf("Validate() expected error but got nil")
					return
				}
				if tt.errMsg != "" && err.Error() != tt.errMsg {
					t.Errorf("Validate() error = %v, want %v", err.Error(), tt.errMsg)
				}
			} else {
				if err != nil {
					t.Errorf("Validate() unexpected error = %v", err)
				}
			}
		})
	}
}

func TestEnvWriteConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  EnvWriteConfig
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid config with key and value",
			config: EnvWriteConfig{
				BaseStepConfig: BaseStepConfig{Name: "env.write"},
				Key:            "DB_DATABASE",
				Value:          "test_db",
			},
			wantErr: false,
		},
		{
			name: "valid config with key only (value can be empty)",
			config: EnvWriteConfig{
				BaseStepConfig: BaseStepConfig{Name: "env.write"},
				Key:            "DB_DATABASE",
			},
			wantErr: false,
		},
		{
			name: "missing key",
			config: EnvWriteConfig{
				BaseStepConfig: BaseStepConfig{Name: "env.write"},
				Value:          "test_db",
			},
			wantErr: true,
			errMsg:  "env.write: 'key' is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr {
				if err == nil {
					t.Errorf("Validate() expected error but got nil")
					return
				}
				if tt.errMsg != "" && err.Error() != tt.errMsg {
					t.Errorf("Validate() error = %v, want %v", err.Error(), tt.errMsg)
				}
			} else {
				if err != nil {
					t.Errorf("Validate() unexpected error = %v", err)
				}
			}
		})
	}
}

func TestEnvCopyConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  EnvCopyConfig
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid config with source and key",
			config: EnvCopyConfig{
				BaseStepConfig: BaseStepConfig{Name: "env.copy"},
				Source:         "../main",
				Key:            "API_KEY",
			},
			wantErr: false,
		},
		{
			name: "valid config with source and keys",
			config: EnvCopyConfig{
				BaseStepConfig: BaseStepConfig{Name: "env.copy"},
				Source:         "../main",
				Keys:           []string{"API_KEY", "API_SECRET"},
			},
			wantErr: false,
		},
		{
			name: "missing source",
			config: EnvCopyConfig{
				BaseStepConfig: BaseStepConfig{Name: "env.copy"},
				Key:            "API_KEY",
			},
			wantErr: true,
			errMsg:  "env.copy: 'source' is required",
		},
		{
			name: "missing key and keys",
			config: EnvCopyConfig{
				BaseStepConfig: BaseStepConfig{Name: "env.copy"},
				Source:         "../main",
			},
			wantErr: true,
			errMsg:  "env.copy: either 'key' or 'keys' must be specified",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr {
				if err == nil {
					t.Errorf("Validate() expected error but got nil")
					return
				}
				if tt.errMsg != "" && err.Error() != tt.errMsg {
					t.Errorf("Validate() error = %v, want %v", err.Error(), tt.errMsg)
				}
			} else {
				if err != nil {
					t.Errorf("Validate() unexpected error = %v", err)
				}
			}
		})
	}
}

func TestDbConfigs_Validate(t *testing.T) {
	t.Run("db.create accepts all optional fields", func(t *testing.T) {
		config := DbCreateConfig{
			BaseStepConfig: BaseStepConfig{Name: "db.create"},
			Type:           "mysql",
			Args:           []string{"--charset=utf8mb4"},
		}
		if err := config.Validate(); err != nil {
			t.Errorf("Validate() unexpected error = %v", err)
		}
	})

	t.Run("db.create accepts no fields", func(t *testing.T) {
		config := DbCreateConfig{
			BaseStepConfig: BaseStepConfig{Name: "db.create"},
		}
		if err := config.Validate(); err != nil {
			t.Errorf("Validate() unexpected error = %v", err)
		}
	})

	t.Run("db.destroy accepts all optional fields", func(t *testing.T) {
		config := DbDestroyConfig{
			BaseStepConfig: BaseStepConfig{Name: "db.destroy"},
			Type:           "mysql",
			Args:           []string{"--force"},
		}
		if err := config.Validate(); err != nil {
			t.Errorf("Validate() unexpected error = %v", err)
		}
	})
}

func TestBinaryStepConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  BinaryStepConfig
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid config with name",
			config: BinaryStepConfig{
				BaseStepConfig: BaseStepConfig{Name: "php"},
				Args:           []string{"-v"},
			},
			wantErr: false,
		},
		{
			name: "valid config with name only",
			config: BinaryStepConfig{
				BaseStepConfig: BaseStepConfig{Name: "npm"},
			},
			wantErr: false,
		},
		{
			name: "missing name",
			config: BinaryStepConfig{
				Args: []string{"install"},
			},
			wantErr: true,
			errMsg:  "binary step: 'name' is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr {
				if err == nil {
					t.Errorf("Validate() expected error but got nil")
					return
				}
				if tt.errMsg != "" && err.Error() != tt.errMsg {
					t.Errorf("Validate() error = %v, want %v", err.Error(), tt.errMsg)
				}
			} else {
				if err != nil {
					t.Errorf("Validate() unexpected error = %v", err)
				}
			}
		})
	}
}
