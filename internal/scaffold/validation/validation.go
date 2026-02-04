// Package validation provides a framework for validating scaffold step configurations.
// It allows for reusable validation rules that can be composed together.
package validation

import (
	"errors"
	"fmt"

	"github.com/artisanexperiences/arbor/internal/config"
)

// Rule defines a single validation rule that can be applied to a StepConfig.
type Rule interface {
	// Validate checks the rule against the provided config.
	// Returns an error if validation fails, nil otherwise.
	Validate(cfg config.StepConfig) error
}

// Validator aggregates multiple rules and validates them together.
// It can collect all validation errors at once using errors.Join.
type Validator struct {
	// StepName is the name of the step being validated (for error context).
	StepName string

	// Rules is the list of validation rules to apply.
	Rules []Rule
}

// NewValidator creates a new Validator for the given step name.
func NewValidator(stepName string) *Validator {
	return &Validator{
		StepName: stepName,
		Rules:    make([]Rule, 0),
	}
}

// AddRule adds a validation rule to the validator.
func (v *Validator) AddRule(rule Rule) *Validator {
	v.Rules = append(v.Rules, rule)
	return v
}

// Validate runs all validation rules against the provided config.
// It collects all errors and returns them joined together using errors.Join.
// Returns nil if all rules pass.
func (v *Validator) Validate(cfg config.StepConfig) error {
	if len(v.Rules) == 0 {
		return nil
	}

	var errs []error
	for _, rule := range v.Rules {
		if err := rule.Validate(cfg); err != nil {
			errs = append(errs, err)
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("validating step %q: %w", v.StepName, errors.Join(errs...))
	}

	return nil
}

// ValidateFirst fails fast on the first validation error.
// Use this when you only need to know if validation passes, not all errors.
func (v *Validator) ValidateFirst(cfg config.StepConfig) error {
	for _, rule := range v.Rules {
		if err := rule.Validate(cfg); err != nil {
			return fmt.Errorf("validating step %q: %w", v.StepName, err)
		}
	}
	return nil
}

// HasRules returns true if the validator has any rules registered.
func (v *Validator) HasRules() bool {
	return len(v.Rules) > 0
}

// RuleCount returns the number of rules in the validator.
func (v *Validator) RuleCount() int {
	return len(v.Rules)
}
