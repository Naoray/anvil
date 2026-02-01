package validation

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/michaeldyrynda/arbor/internal/config"
)

func TestValidator(t *testing.T) {
	t.Run("validates with no rules", func(t *testing.T) {
		v := NewValidator("test.step")
		err := v.Validate(config.StepConfig{})
		if err != nil {
			t.Errorf("expected no error with no rules, got: %v", err)
		}
	})

	t.Run("validates single passing rule", func(t *testing.T) {
		v := NewValidator("test.step").
			AddRule(RequiredField{
				Field:     "name",
				GetValue:  func(c config.StepConfig) string { return c.Name },
				FieldName: "name",
			})

		err := v.Validate(config.StepConfig{Name: "test"})
		if err != nil {
			t.Errorf("expected no error, got: %v", err)
		}
	})

	t.Run("validates single failing rule", func(t *testing.T) {
		v := NewValidator("test.step").
			AddRule(RequiredField{
				Field:     "name",
				GetValue:  func(c config.StepConfig) string { return c.Name },
				FieldName: "name",
			})

		err := v.Validate(config.StepConfig{})
		if err == nil {
			t.Error("expected error for missing required field")
		}
		// Error should contain step name context
		errStr := err.Error()
		if !contains(errStr, "test.step") {
			t.Errorf("expected error to contain step name, got: %s", errStr)
		}
		if !contains(errStr, "name") {
			t.Errorf("expected error to mention field name, got: %s", errStr)
		}
	})

	t.Run("collects multiple errors", func(t *testing.T) {
		v := NewValidator("test.step").
			AddRule(RequiredField{
				Field:     "from",
				GetValue:  func(c config.StepConfig) string { return c.From },
				FieldName: "from",
			}).
			AddRule(RequiredField{
				Field:     "to",
				GetValue:  func(c config.StepConfig) string { return c.To },
				FieldName: "to",
			})

		err := v.Validate(config.StepConfig{})
		if err == nil {
			t.Error("expected error for missing required fields")
		}
		// Should mention both missing fields
		errStr := err.Error()
		if !contains(errStr, "from") {
			t.Errorf("expected error to mention 'from', got: %s", errStr)
		}
		if !contains(errStr, "to") {
			t.Errorf("expected error to mention 'to', got: %s", errStr)
		}
	})

	t.Run("ValidateFirst stops at first error", func(t *testing.T) {
		v := NewValidator("test.step").
			AddRule(RequiredField{
				Field:     "from",
				GetValue:  func(c config.StepConfig) string { return c.From },
				FieldName: "from",
			}).
			AddRule(RequiredField{
				Field:     "to",
				GetValue:  func(c config.StepConfig) string { return c.To },
				FieldName: "to",
			})

		err := v.ValidateFirst(config.StepConfig{})
		if err == nil {
			t.Error("expected error for missing required field")
		}
		// Should only mention the first missing field
		errStr := err.Error()
		if !contains(errStr, "from") {
			t.Errorf("expected error to mention 'from', got: %s", errStr)
		}
		// Should not mention second field since we stop at first error
	})

	t.Run("HasRules returns correct value", func(t *testing.T) {
		empty := NewValidator("test.step")
		if empty.HasRules() {
			t.Error("expected HasRules to be false for empty validator")
		}

		withRules := NewValidator("test.step").
			AddRule(RequiredField{
				Field:     "name",
				GetValue:  func(c config.StepConfig) string { return c.Name },
				FieldName: "name",
			})
		if !withRules.HasRules() {
			t.Error("expected HasRules to be true for validator with rules")
		}
	})

	t.Run("RuleCount returns correct count", func(t *testing.T) {
		v := NewValidator("test.step").
			AddRule(RequiredField{
				Field:     "field1",
				GetValue:  func(c config.StepConfig) string { return "" },
				FieldName: "field1",
			}).
			AddRule(RequiredField{
				Field:     "field2",
				GetValue:  func(c config.StepConfig) string { return "" },
				FieldName: "field2",
			})

		if v.RuleCount() != 2 {
			t.Errorf("expected RuleCount 2, got %d", v.RuleCount())
		}
	})
}

func TestRequiredField(t *testing.T) {
	t.Run("passes when field has value", func(t *testing.T) {
		rule := RequiredField{
			Field:     "command",
			GetValue:  func(c config.StepConfig) string { return c.Command },
			FieldName: "command",
		}

		err := rule.Validate(config.StepConfig{Command: "echo test"})
		if err != nil {
			t.Errorf("expected no error, got: %v", err)
		}
	})

	t.Run("fails when field is empty", func(t *testing.T) {
		rule := RequiredField{
			Field:     "command",
			GetValue:  func(c config.StepConfig) string { return c.Command },
			FieldName: "command",
		}

		err := rule.Validate(config.StepConfig{})
		if err == nil {
			t.Error("expected error for empty field")
		}
		if !contains(err.Error(), "command") {
			t.Errorf("expected error to mention field name, got: %v", err)
		}
	})

	t.Run("uses FieldName in error if provided", func(t *testing.T) {
		rule := RequiredField{
			Field:     "cmd",
			GetValue:  func(c config.StepConfig) string { return c.Command },
			FieldName: "command",
		}

		err := rule.Validate(config.StepConfig{})
		if !contains(err.Error(), "command") {
			t.Errorf("expected error to use FieldName, got: %v", err)
		}
	})
}

func TestRequiredFields(t *testing.T) {
	t.Run("passes when all fields have values", func(t *testing.T) {
		rule := RequiredFields{
			Fields: []RequiredField{
				{Field: "from", GetValue: func(c config.StepConfig) string { return c.From }, FieldName: "from"},
				{Field: "to", GetValue: func(c config.StepConfig) string { return c.To }, FieldName: "to"},
			},
		}

		err := rule.Validate(config.StepConfig{From: "a", To: "b"})
		if err != nil {
			t.Errorf("expected no error, got: %v", err)
		}
	})

	t.Run("fails when any field is empty", func(t *testing.T) {
		rule := RequiredFields{
			Fields: []RequiredField{
				{Field: "from", GetValue: func(c config.StepConfig) string { return c.From }, FieldName: "from"},
				{Field: "to", GetValue: func(c config.StepConfig) string { return c.To }, FieldName: "to"},
			},
		}

		err := rule.Validate(config.StepConfig{From: "a"})
		if err == nil {
			t.Error("expected error for missing field")
		}
	})
}

func TestOneOf(t *testing.T) {
	t.Run("passes when value is in allowed list", func(t *testing.T) {
		rule := OneOf{
			GetValue:  func(c config.StepConfig) string { return c.Type },
			FieldName: "type",
			Allowed:   []string{"mysql", "sqlite", "postgres"},
		}

		err := rule.Validate(config.StepConfig{Type: "mysql"})
		if err != nil {
			t.Errorf("expected no error, got: %v", err)
		}
	})

	t.Run("passes when value is empty", func(t *testing.T) {
		rule := OneOf{
			GetValue:  func(c config.StepConfig) string { return c.Type },
			FieldName: "type",
			Allowed:   []string{"mysql", "sqlite"},
		}

		err := rule.Validate(config.StepConfig{})
		if err != nil {
			t.Errorf("expected no error for empty value, got: %v", err)
		}
	})

	t.Run("fails when value is not in allowed list", func(t *testing.T) {
		rule := OneOf{
			GetValue:  func(c config.StepConfig) string { return c.Type },
			FieldName: "type",
			Allowed:   []string{"mysql", "sqlite"},
		}

		err := rule.Validate(config.StepConfig{Type: "oracle"})
		if err == nil {
			t.Error("expected error for invalid value")
		}
	})
}

func TestNotEmpty(t *testing.T) {
	t.Run("passes when slice is not empty", func(t *testing.T) {
		rule := NotEmpty{
			GetValue:  func(c config.StepConfig) []string { return c.Args },
			FieldName: "args",
		}

		err := rule.Validate(config.StepConfig{Args: []string{"-v"}})
		if err != nil {
			t.Errorf("expected no error, got: %v", err)
		}
	})

	t.Run("fails when slice is empty", func(t *testing.T) {
		rule := NotEmpty{
			GetValue:  func(c config.StepConfig) []string { return c.Args },
			FieldName: "args",
		}

		err := rule.Validate(config.StepConfig{})
		if err == nil {
			t.Error("expected error for empty slice")
		}
	})
}

func TestCustomRule(t *testing.T) {
	t.Run("executes custom validation", func(t *testing.T) {
		validated := false
		rule := CustomRule{
			Name: "custom",
			ValidateFn: func(c config.StepConfig) error {
				validated = true
				return nil
			},
		}

		rule.Validate(config.StepConfig{})
		if !validated {
			t.Error("expected custom validation to be executed")
		}
	})

	t.Run("returns custom error", func(t *testing.T) {
		rule := CustomRule{
			Name: "custom",
			ValidateFn: func(c config.StepConfig) error {
				return errors.New("custom error")
			},
		}

		err := rule.Validate(config.StepConfig{})
		if err == nil || err.Error() != "custom error" {
			t.Errorf("expected custom error, got: %v", err)
		}
	})
}

func TestFileExists(t *testing.T) {
	t.Run("passes when path is empty (optional)", func(t *testing.T) {
		rule := FileExists{
			GetPath:   func(c config.StepConfig) string { return c.From },
			FieldName: "source",
		}

		err := rule.Validate(config.StepConfig{})
		if err != nil {
			t.Errorf("expected no error for empty path, got: %v", err)
		}
	})

	t.Run("uses injected StatFn", func(t *testing.T) {
		mockStat := func(path string) (os.FileInfo, error) {
			if path == "/exists/file.txt" {
				return nil, nil // File exists
			}
			return nil, os.ErrNotExist
		}

		rule := FileExists{
			GetPath:   func(c config.StepConfig) string { return c.From },
			FieldName: "source",
			StatFn:    mockStat,
		}

		// File exists
		err := rule.Validate(config.StepConfig{From: "/exists/file.txt"})
		if err != nil {
			t.Errorf("expected no error for existing file, got: %v", err)
		}

		// File does not exist
		err = rule.Validate(config.StepConfig{From: "/missing/file.txt"})
		if err == nil {
			t.Error("expected error for missing file")
		}
		if !contains(err.Error(), "source") {
			t.Errorf("expected error to mention field name, got: %v", err)
		}
	})

	t.Run("defaults to os.Stat when StatFn is nil", func(t *testing.T) {
		rule := FileExists{
			GetPath:   func(c config.StepConfig) string { return c.From },
			FieldName: "source",
			// StatFn is nil, should use DefaultStatFunc
		}

		// Non-existent path should error
		err := rule.Validate(config.StepConfig{From: "/this/path/should/not/exist/ever"})
		if err == nil {
			t.Error("expected error for non-existent path")
		}
	})
}

func TestStepValidators(t *testing.T) {
	tests := []struct {
		name      string
		validator *Validator
		cfg       config.StepConfig
		wantErr   bool
	}{
		{
			name:      "FileCopyValidator passes with all fields",
			validator: NewFileCopyValidator(),
			cfg:       config.StepConfig{From: "a", To: "b"},
			wantErr:   false,
		},
		{
			name:      "FileCopyValidator fails without from",
			validator: NewFileCopyValidator(),
			cfg:       config.StepConfig{To: "b"},
			wantErr:   true,
		},
		{
			name:      "BashRunValidator passes with command",
			validator: NewBashRunValidator(),
			cfg:       config.StepConfig{Command: "echo test"},
			wantErr:   false,
		},
		{
			name:      "BashRunValidator fails without command",
			validator: NewBashRunValidator(),
			cfg:       config.StepConfig{},
			wantErr:   true,
		},
		{
			name:      "EnvReadValidator passes with key",
			validator: NewEnvReadValidator(),
			cfg:       config.StepConfig{Key: "TEST"},
			wantErr:   false,
		},
		{
			name:      "EnvReadValidator fails without key",
			validator: NewEnvReadValidator(),
			cfg:       config.StepConfig{},
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.validator.Validate(tt.cfg)
			if tt.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

// contains wraps strings.Contains for readability in tests.
func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}
