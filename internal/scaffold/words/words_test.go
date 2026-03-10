package words

import (
	"strings"
	"testing"
)

func TestSanitizeSiteName(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "lowercase conversion",
			input:    "MyApp",
			expected: "myapp",
		},
		{
			name:     "replace hyphens with underscores",
			input:    "my-app",
			expected: "my_app",
		},
		{
			name:     "replace spaces with underscores",
			input:    "my app",
			expected: "my_app",
		},
		{
			name:     "replace special characters with underscores",
			input:    "my@app!",
			expected: "my_app",
		},
		{
			name:     "collapse multiple underscores",
			input:    "my__app",
			expected: "my_app",
		},
		{
			name:     "collapse multiple special characters",
			input:    "my---app",
			expected: "my_app",
		},
		{
			name:     "trim leading underscores",
			input:    "__myapp",
			expected: "myapp",
		},
		{
			name:     "trim trailing underscores",
			input:    "myapp__",
			expected: "myapp",
		},
		{
			name:     "trim both leading and trailing underscores",
			input:    "__myapp__",
			expected: "myapp",
		},
		{
			name:     "already sanitized name",
			input:    "my_app",
			expected: "my_app",
		},
		{
			name:     "empty string returns empty",
			input:    "",
			expected: "",
		},
		{
			name:     "complex case",
			input:    "My-Test_App 123!",
			expected: "my_test_app_123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SanitizeSiteName(tt.input)
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestGenerateSuffix(t *testing.T) {
	t.Run("generates valid suffix format", func(t *testing.T) {
		suffix := GenerateSuffix()
		if len(suffix) == 0 {
			t.Fatal("suffix should not be empty")
		}

		parts := 0
		for _, c := range suffix {
			if c == '_' {
				parts++
			}
		}
		if parts != 1 {
			t.Errorf("suffix should contain exactly one underscore, got %d", parts)
		}
	})

	t.Run("generates different suffixes", func(t *testing.T) {
		suffixes := make(map[string]bool)
		for i := 0; i < 100; i++ {
			suffix := GenerateSuffix()
			suffixes[suffix] = true
		}

		// With ~72 adjectives and ~65 nouns, we should get good distribution
		if len(suffixes) < 50 {
			t.Errorf("expected at least 50 unique suffixes, got %d", len(suffixes))
		}
	})

	t.Run("suffix uses words from lists", func(t *testing.T) {
		suffix := GenerateSuffix()
		parts := splitSuffix(suffix)
		if len(parts) != 2 {
			t.Fatalf("expected 2 parts, got %d: %s", len(parts), suffix)
		}

		adj := parts[0]
		noun := parts[1]

		if !isAdjective(adj) {
			t.Errorf("first part should be an adjective, got %q", adj)
		}
		if !isNoun(noun) {
			t.Errorf("second part should be a noun, got %q", noun)
		}
	})
}

func TestGenerateDatabaseName(t *testing.T) {
	t.Run("generates name with suffix", func(t *testing.T) {
		name := GenerateDatabaseName("myapp", 0)
		if len(name) == 0 {
			t.Fatal("name should not be empty")
		}
		if name == "myapp" {
			t.Error("name should include suffix")
		}
	})

	t.Run("uses default max length when 0", func(t *testing.T) {
		name := GenerateDatabaseName("myapp", 0)
		if len(name) > 63 {
			t.Errorf("name should not exceed 63 characters, got %d", len(name))
		}
	})

	t.Run("respects custom max length", func(t *testing.T) {
		name := GenerateDatabaseName("myapp", 30)
		if len(name) > 30 {
			t.Errorf("name should not exceed %d characters, got %d", 30, len(name))
		}
	})

	t.Run("truncates long site names", func(t *testing.T) {
		name := GenerateDatabaseName("verylongsitenamethatneedstobetruncated", 30)
		if len(name) > 30 {
			t.Errorf("name should not exceed %d characters, got %d", 30, len(name))
		}
		if name[0:4] != "very" {
			t.Errorf("name should start with truncated site name, got %s", name[0:4])
		}
	})

	t.Run("sanitizes site name", func(t *testing.T) {
		name := GenerateDatabaseName("My-App", 0)
		if name == "My-App_" {
			t.Error("site name should be sanitized")
		}
		if name[0:6] != "my_app" {
			t.Errorf("sanitized site name should be lowercase with underscores, got %s", name[0:6])
		}
	})

	t.Run("includes suffix separator", func(t *testing.T) {
		name := GenerateDatabaseName("myapp", 0)
		underscoreCount := 0
		for _, c := range name {
			if c == '_' {
				underscoreCount++
			}
		}
		if underscoreCount < 2 {
			t.Errorf("name should contain at least 2 underscores (site_suffix format), got %d", underscoreCount)
		}
	})
}

func TestBuildDatabaseName(t *testing.T) {
	t.Run("short name fits within limit", func(t *testing.T) {
		name := BuildDatabaseName("myapp", "stable_resolver", 0)
		if name != "myapp_stable_resolver" {
			t.Errorf("expected myapp_stable_resolver, got %s", name)
		}
	})

	t.Run("truncates long site name to fit 63 char default", func(t *testing.T) {
		name := BuildDatabaseName("hotfix/track-collection-reflection-exception-handling", "stable_resolver", 0)
		if len(name) > 63 {
			t.Errorf("name should not exceed 63 characters, got %d: %s", len(name), name)
		}
		if !strings.HasSuffix(name, "_stable_resolver") {
			t.Errorf("name should end with suffix, got %s", name)
		}
	})

	t.Run("respects custom max length", func(t *testing.T) {
		name := BuildDatabaseName("a_very_long_site_name_here", "stable_resolver", 30)
		if len(name) > 30 {
			t.Errorf("name should not exceed 30 characters, got %d: %s", len(name), name)
		}
	})

	t.Run("does not end with trailing underscore after truncation", func(t *testing.T) {
		// Construct a name where truncation would land on an underscore
		name := BuildDatabaseName("my_app_feature_x", "stable_resolver", 30)
		parts := strings.SplitN(name, "_stable_resolver", 2)
		sitePart := parts[0]
		if strings.HasSuffix(sitePart, "_") {
			t.Errorf("site name part should not end with underscore, got %s", sitePart)
		}
	})
}

func TestMaxLengthEnforcement(t *testing.T) {
	t.Run("respects PostgreSQL limit of 63", func(t *testing.T) {
		name := GenerateDatabaseName("verylongsitenamethatdefinitelyexceedslimitsbyalot", 0)
		if len(name) > 63 {
			t.Errorf("PostgreSQL limit is 63, got %d", len(name))
		}
	})

	t.Run("respects MySQL limit of 64 when specified", func(t *testing.T) {
		name := GenerateDatabaseName("verylongsitenamethatdefinitelyexceedslimitsbyalot", 64)
		if len(name) > 64 {
			t.Errorf("MySQL limit is 64, got %d", len(name))
		}
	})
}

func TestWordListsSafety(t *testing.T) {
	t.Run("adjectives are lowercase", func(t *testing.T) {
		for _, adj := range Adjectives {
			if adj != strings.ToLower(adj) {
				t.Errorf("adjective %q should be lowercase", adj)
			}
		}
	})

	t.Run("nouns are lowercase", func(t *testing.T) {
		for _, noun := range Nouns {
			if noun != strings.ToLower(noun) {
				t.Errorf("noun %q should be lowercase", noun)
			}
		}
	})

	t.Run("word lists are not empty", func(t *testing.T) {
		if len(Adjectives) < 50 {
			t.Errorf("adjectives list too small: %d", len(Adjectives))
		}
		if len(Nouns) < 50 {
			t.Errorf("nouns list too small: %d", len(Nouns))
		}
	})
}

func TestGenerateSuffixDistribution(t *testing.T) {
	t.Run("good distribution across words", func(t *testing.T) {
		adjCounts := make(map[string]int)
		nounCounts := make(map[string]int)

		for i := 0; i < 1000; i++ {
			suffix := GenerateSuffix()
			parts := splitSuffix(suffix)
			if len(parts) != 2 {
				t.Fatalf("expected 2 parts, got %d", len(parts))
			}
			adjCounts[parts[0]]++
			nounCounts[parts[1]]++
		}

		// Check that we got decent variety
		uniqueAdjs := 0
		for _, count := range adjCounts {
			if count > 0 {
				uniqueAdjs++
			}
		}

		uniqueNouns := 0
		for _, count := range nounCounts {
			if count > 0 {
				uniqueNouns++
			}
		}

		if uniqueAdjs < 50 {
			t.Errorf("expected at least 50 unique adjectives, got %d", uniqueAdjs)
		}
		if uniqueNouns < 50 {
			t.Errorf("expected at least 50 unique nouns, got %d", uniqueNouns)
		}
	})
}

func splitSuffix(suffix string) []string {
	parts := []string{}
	current := ""
	for _, c := range suffix {
		if c == '_' {
			parts = append(parts, current)
			current = ""
		} else {
			current += string(c)
		}
	}
	if current != "" {
		parts = append(parts, current)
	}
	return parts
}

func isAdjective(word string) bool {
	for _, adj := range Adjectives {
		if adj == word {
			return true
		}
	}
	return false
}

func isNoun(word string) bool {
	for _, noun := range Nouns {
		if noun == word {
			return true
		}
	}
	return false
}
