package validation

import (
	"errors"
	"fmt"
	"os"

	"github.com/michaeldyrynda/arbor/internal/config"
)

// RequiredField validates that a specific field is not empty.
type RequiredField struct {
	Field     string
	GetValue  func(config.StepConfig) string
	FieldName string // Human-readable field name for error messages
}

// Validate checks that the required field has a non-empty value.
func (r RequiredField) Validate(cfg config.StepConfig) error {
	value := r.GetValue(cfg)
	if value == "" {
		fieldName := r.FieldName
		if fieldName == "" {
			fieldName = r.Field
		}
		return fmt.Errorf("required field %q is missing", fieldName)
	}
	return nil
}

// RequiredFields validates that multiple fields are not empty.
type RequiredFields struct {
	Fields []RequiredField
}

// Validate checks that all required fields have non-empty values.
func (r RequiredFields) Validate(cfg config.StepConfig) error {
	var errs []error
	for _, field := range r.Fields {
		if err := field.Validate(cfg); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

// StatFunc is a function type for checking if a path exists.
// This allows injection of the file system for testing.
type StatFunc func(path string) (os.FileInfo, error)

// DefaultStatFunc uses os.Stat for real file system checks.
var DefaultStatFunc StatFunc = os.Stat

// FileExists validates that a file path exists.
type FileExists struct {
	GetPath   func(config.StepConfig) string
	FieldName string
	StatFn    StatFunc // Optional: defaults to os.Stat if nil
}

// Validate checks that the specified file exists.
func (f FileExists) Validate(cfg config.StepConfig) error {
	path := f.GetPath(cfg)
	if path == "" {
		return nil // Empty path is considered valid (optional file)
	}

	statFn := f.StatFn
	if statFn == nil {
		statFn = DefaultStatFunc
	}

	if _, err := statFn(path); os.IsNotExist(err) {
		fieldName := f.FieldName
		if fieldName == "" {
			fieldName = "file"
		}
		return fmt.Errorf("%s %q does not exist", fieldName, path)
	}
	return nil
}

// OneOf validates that a field value is one of the allowed values.
type OneOf struct {
	GetValue  func(config.StepConfig) string
	FieldName string
	Allowed   []string
}

// Validate checks that the field value is in the allowed list.
func (o OneOf) Validate(cfg config.StepConfig) error {
	value := o.GetValue(cfg)
	if value == "" {
		return nil // Empty is considered valid
	}

	for _, allowed := range o.Allowed {
		if value == allowed {
			return nil
		}
	}

	return fmt.Errorf("field %q must be one of %v, got %q", o.FieldName, o.Allowed, value)
}

// NotEmpty validates that a slice field is not empty.
type NotEmpty struct {
	GetValue  func(config.StepConfig) []string
	FieldName string
}

// Validate checks that the slice field has at least one element.
func (n NotEmpty) Validate(cfg config.StepConfig) error {
	value := n.GetValue(cfg)
	if len(value) == 0 {
		return fmt.Errorf("field %q must not be empty", n.FieldName)
	}
	return nil
}

// CustomRule allows defining a validation rule using a function.
type CustomRule struct {
	Name       string
	ValidateFn func(config.StepConfig) error
}

// Validate executes the custom validation function.
func (c CustomRule) Validate(cfg config.StepConfig) error {
	return c.ValidateFn(cfg)
}

// StepValidator is a convenience function that creates a pre-configured validator
// for common step types.
type StepValidator struct {
	Name  string
	Rules []Rule
}

// NewFileCopyValidator creates a validator for file.copy step.
func NewFileCopyValidator() *Validator {
	return NewValidator("file.copy").
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
}

// NewBashRunValidator creates a validator for bash.run step.
func NewBashRunValidator() *Validator {
	return NewValidator("bash.run").
		AddRule(RequiredField{
			Field:     "command",
			GetValue:  func(cfg config.StepConfig) string { return cfg.Command },
			FieldName: "command",
		})
}

// NewCommandRunValidator creates a validator for command.run step.
func NewCommandRunValidator() *Validator {
	return NewValidator("command.run").
		AddRule(RequiredField{
			Field:     "command",
			GetValue:  func(cfg config.StepConfig) string { return cfg.Command },
			FieldName: "command",
		})
}

// NewEnvReadValidator creates a validator for env.read step.
func NewEnvReadValidator() *Validator {
	return NewValidator("env.read").
		AddRule(RequiredField{
			Field:     "key",
			GetValue:  func(cfg config.StepConfig) string { return cfg.Key },
			FieldName: "key",
		})
}

// NewEnvWriteValidator creates a validator for env.write step.
func NewEnvWriteValidator() *Validator {
	return NewValidator("env.write").
		AddRule(RequiredField{
			Field:     "key",
			GetValue:  func(cfg config.StepConfig) string { return cfg.Key },
			FieldName: "key",
		})
}
