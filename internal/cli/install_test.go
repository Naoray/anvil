package cli

import "testing"

func TestExtractVersionYerd(t *testing.T) {
	if got := extractVersion("yerd 2.0.4\n", "yerd"); got != "2.0.4" {
		t.Fatalf("extractVersion() = %q, want %q", got, "2.0.4")
	}
}
