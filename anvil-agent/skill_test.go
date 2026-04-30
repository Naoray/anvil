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
}
