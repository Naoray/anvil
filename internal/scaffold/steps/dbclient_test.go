package steps

import "testing"

func TestEscapeLikePattern(t *testing.T) {
	if got, want := EscapeLikePattern("dashboard_top_provider_test"), `dashboard\_top\_provider\_test`; got != want {
		t.Fatalf("EscapeLikePattern() = %q, want %q", got, want)
	}
	if got, want := EscapeLikePattern(`100%_a\b`), `100\%\_a\\b`; got != want {
		t.Fatalf("EscapeLikePattern() = %q, want %q", got, want)
	}
}
