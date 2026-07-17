package anvilagent

import (
	"bytes"
	"testing"
)

func TestSkillContentIsEmbedded(t *testing.T) {
	if !bytes.Contains(Content, []byte("name: anvil-agent")) {
		t.Fatal("expected embedded skill content to include the skill name")
	}
	if !bytes.Contains(Content, []byte("Codex")) {
		t.Fatal("expected embedded skill content to mention Codex")
	}

	requiredGuidance := [][]byte{
		[]byte("anvil exec"),
		[]byte("databases:"),
		[]byte("--keep-db"),
		[]byte("PHPStan/Larastan"),
	}
	for _, fragment := range requiredGuidance {
		if !bytes.Contains(Content, fragment) {
			t.Fatalf("expected embedded skill content to include %q", fragment)
		}
	}
}
