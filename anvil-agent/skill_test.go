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
		[]byte("site_driver"),
		[]byte("yerd link"),
		[]byte("yerd secure"),
		[]byte("yerd unlink"),
		[]byte("yerd service start"),
		[]byte("yerd db create"),
		[]byte("yerd db drop"),
		[]byte("site_driver: herd"),
		[]byte("herd services:start"),
		[]byte("Herd-provided database binaries"),
	}
	for _, fragment := range requiredGuidance {
		if !bytes.Contains(Content, fragment) {
			t.Fatalf("expected embedded skill content to include %q", fragment)
		}
	}
	if bytes.Contains(Content, []byte("direct SQL")) {
		t.Fatal("embedded skill must not recommend direct SQL clients for Herd")
	}
}
