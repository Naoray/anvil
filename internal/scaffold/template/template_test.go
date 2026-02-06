package template

import (
	"testing"

	"github.com/naoray/anvil/internal/scaffold/types"
)

func TestReplaceTemplateVars(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		ctx         *types.ScaffoldContext
		expected    string
		expectError bool
	}{
		{
			name:     "no template variables",
			input:    "plain text",
			ctx:      &types.ScaffoldContext{Path: "feature-auth", RepoPath: "myapp"},
			expected: "plain text",
		},
		{
			name:     "single Path variable no spaces",
			input:    "{{.Path}}",
			ctx:      &types.ScaffoldContext{Path: "feature-auth"},
			expected: "feature-auth",
		},
		{
			name:     "single Path variable with spaces",
			input:    "{{ .Path }}",
			ctx:      &types.ScaffoldContext{Path: "feature-auth"},
			expected: "feature-auth",
		},
		{
			name:     "single Path variable with multiple spaces",
			input:    "{{  .Path  }}",
			ctx:      &types.ScaffoldContext{Path: "feature-auth"},
			expected: "feature-auth",
		},
		{
			name:     "multiple variables",
			input:    "{{.Path}}-{{.RepoPath}}",
			ctx:      &types.ScaffoldContext{Path: "feature-auth", RepoPath: "myapp"},
			expected: "feature-auth-myapp",
		},
		{
			name:     "RepoName variable",
			input:    "{{ .RepoName }}",
			ctx:      &types.ScaffoldContext{RepoName: "test-repo"},
			expected: "test-repo",
		},
		{
			name:     "SiteName variable",
			input:    "{{ .SiteName }}",
			ctx:      &types.ScaffoldContext{SiteName: "mysite"},
			expected: "mysite",
		},
		{
			name:     "Branch variable",
			input:    "{{ .Branch }}",
			ctx:      &types.ScaffoldContext{Branch: "feature/test"},
			expected: "feature/test",
		},
		{
			name:     "DbSuffix variable",
			input:    "{{ .DbSuffix }}",
			ctx:      &types.ScaffoldContext{DbSuffix: "swift_runner"},
			expected: "swift_runner",
		},
		{
			name:     "dynamic variable from Vars",
			input:    "{{ .CustomVar }}",
			ctx:      &types.ScaffoldContext{Vars: map[string]string{"CustomVar": "custom-value"}},
			expected: "custom-value",
		},
		{
			name:        "unknown variable should error",
			input:       "{{ .UnknownVar }}",
			ctx:         &types.ScaffoldContext{},
			expectError: true,
		},
		{
			name:     "complex template with text and variables",
			input:    "app.{{ .Path }}.test",
			ctx:      &types.ScaffoldContext{Path: "feature-auth"},
			expected: "app.feature-auth.test",
		},
		{
			name:     "database name pattern",
			input:    "{{ .SiteName }}_{{ .DbSuffix }}",
			ctx:      &types.ScaffoldContext{SiteName: "myapp", DbSuffix: "swift_runner"},
			expected: "myapp_swift_runner",
		},
		{
			name:     "sanitized database name pattern",
			input:    "{{ .SanitizedSiteName }}_{{ .DbSuffix }}",
			ctx:      &types.ScaffoldContext{SiteName: "my-app", DbSuffix: "swift_runner"},
			expected: "my_app_swift_runner",
		},
		{
			name:     "SanitizedSiteName removes hyphens",
			input:    "{{ .SanitizedSiteName }}",
			ctx:      &types.ScaffoldContext{SiteName: "feature-auth-system"},
			expected: "feature_auth_system",
		},
		{
			name:     "SanitizedSiteName removes special characters",
			input:    "{{ .SanitizedSiteName }}",
			ctx:      &types.ScaffoldContext{SiteName: "My Test-App!"},
			expected: "my_test_app",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ReplaceTemplateVars(tt.input, tt.ctx)

			if tt.expectError {
				if err == nil {
					t.Errorf("expected error, got none")
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestReplaceTemplateVars_SnapshotForTemplate(t *testing.T) {
	ctx := &types.ScaffoldContext{
		Path:     "feature-auth",
		RepoPath: "myapp",
		RepoName: "test-repo",
		SiteName: "mysite",
		Branch:   "feature/test",
		DbSuffix: "swift_runner",
		Vars:     map[string]string{"CustomVar": "custom-value"},
	}

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "all built-in variables",
			input:    "{{ .Path }}-{{ .RepoPath }}-{{ .RepoName }}-{{ .SiteName }}-{{ .Branch }}-{{ .DbSuffix }}",
			expected: "feature-auth-myapp-test-repo-mysite-feature/test-swift_runner",
		},
		{
			name:     "custom variable",
			input:    "{{ .CustomVar }}",
			expected: "custom-value",
		},
		{
			name:     "built-in and custom variables together",
			input:    "{{ .SiteName }}_{{ .DbSuffix }}_{{ .CustomVar }}",
			expected: "mysite_swift_runner_custom-value",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ReplaceTemplateVars(tt.input, ctx)
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}
