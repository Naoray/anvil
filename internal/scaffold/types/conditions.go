package types

import "fmt"

// ValidateConditions checks syntax only; runtime values may be produced by earlier steps.
func ValidateConditions(conditions map[string]any) error {
	return validateCondition(conditions)
}

func validateCondition(condition any) error {
	switch c := condition.(type) {
	case map[string]any:
		for _, key := range conditionKeys(c) {
			if err := validateOperand(key, c[key]); err != nil {
				return fmt.Errorf("condition %q: %w", key, err)
			}
		}
	case []any:
		for i, item := range c {
			if err := validateCondition(item); err != nil {
				return fmt.Errorf("condition item %d: %w", i, err)
			}
		}
	case []map[string]any:
		for i, item := range c {
			if err := validateCondition(item); err != nil {
				return fmt.Errorf("condition item %d: %w", i, err)
			}
		}
	default:
		return fmt.Errorf("expected condition map or conjunction, got %T", condition)
	}
	return nil
}

func validateOperand(key string, value any) error {
	switch key {
	case "not":
		return validateCondition(value)
	case "context_var":
		return validateFields(value, "name", "value")
	case "file_contains":
		return validateFields(value, "file", "pattern")
	case "file_has_script":
		if _, ok := value.(map[string]any); ok {
			return validateFields(value, "name")
		}
		return validateString(value)
	case "env_file_contains", "env_file_missing":
		if _, ok := value.(map[string]any); ok {
			return validateFields(value, "file", "key")
		}
		return validateString(value)
	case "file_exists", "command_exists", "env_exists", "env_not_exists", "os":
		if _, ok := value.(map[string]any); ok {
			field := map[string]string{"file_exists": "file", "command_exists": "command", "env_exists": "env", "env_not_exists": "env"}[key]
			if field == "" {
				return fmt.Errorf("expected string or string list")
			}
			return validateFields(value, field)
		}
		switch v := value.(type) {
		case []any:
			for i, item := range v {
				if err := validateString(item); err != nil {
					return fmt.Errorf("item %d: %w", i, err)
				}
			}
			return nil
		case []string:
			for i, item := range v {
				if err := validateString(item); err != nil {
					return fmt.Errorf("item %d: %w", i, err)
				}
			}
			return nil
		default:
			return validateString(value)
		}
	default:
		return fmt.Errorf("unsupported condition name")
	}
}

func validateString(value any) error {
	if s, ok := value.(string); !ok || s == "" {
		return fmt.Errorf("expected nonempty string, got %T", value)
	}
	return nil
}

func validateFields(value any, fields ...string) error {
	m, ok := value.(map[string]any)
	if !ok {
		return fmt.Errorf("expected map with fields %v", fields)
	}
	for _, key := range conditionKeys(m) {
		known := false
		for _, field := range fields {
			if key == field {
				known = true
				break
			}
		}
		if !known {
			return fmt.Errorf("unsupported field %q", key)
		}
	}
	for _, field := range fields {
		v, exists := m[field]
		if !exists {
			return fmt.Errorf("missing field %q", field)
		}
		if field == "value" {
			if _, ok := v.(string); ok {
				continue
			}
		}
		if err := validateString(v); err != nil {
			return fmt.Errorf("field %q: %w", field, err)
		}
	}
	return nil
}
