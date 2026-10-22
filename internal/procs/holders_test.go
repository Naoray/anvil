package procs

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func canonicalTempDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

// startHolder runs `sleep` with its working directory elsewhere and stdin
// opened on file, so only its file descriptor table references dir.
func startHolder(t *testing.T, file, cwd string) *exec.Cmd {
	t.Helper()
	handle, err := os.Open(file)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("sleep", "60")
	cmd.Dir = cwd
	cmd.Stdin = handle
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	if err := handle.Close(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := cmd.Process.Kill(); err != nil {
			t.Logf("killing holder: %v", err)
		}
		if err := cmd.Wait(); err != nil {
			t.Logf("holder exited: %v", err)
		}
	})
	return cmd
}

func TestOpenFileSnapshotFindsFileDescriptorHolder(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("procfs scanning is Linux-only; macOS uses lsof")
	}
	dir := canonicalTempDir(t)
	file := filepath.Join(dir, "held.txt")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	other := canonicalTempDir(t)
	holder := startHolder(t, file, other)

	var holders []Holder
	deadline := time.Now().Add(5 * time.Second)
	for {
		snapshot, err := ScanOpenFiles()
		if err != nil {
			t.Fatalf("scan: %v", err)
		}
		if snapshot.Method != MethodProcfs {
			t.Fatalf("method = %q, want %q", snapshot.Method, MethodProcfs)
		}
		holders = snapshot.Holders(dir)
		if len(holders) > 0 || time.Now().After(deadline) {
			if got := snapshot.Holders(other); len(got) != 1 || got[0].PID != holder.Process.Pid {
				// The holder's cwd is `other`; it must be reported via cwd.
				t.Fatalf("holders of cwd directory = %+v", got)
			}
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if len(holders) != 1 || holders[0].PID != holder.Process.Pid {
		t.Fatalf("holders = %+v, want pid %d", holders, holder.Process.Pid)
	}
	if !strings.Contains(strings.Join(holders[0].Via, ","), "fd") {
		t.Fatalf("via = %v, want fd", holders[0].Via)
	}
	if holders[0].Path != file {
		t.Fatalf("path = %q, want %q", holders[0].Path, file)
	}
}

// fixtureRoot returns an absolute directory in the platform's native form,
// so the platform-independent matching and parsing tests hold on Windows too
// (a POSIX literal such as /w/app is not absolute there).
func fixtureRoot(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "w")
}

func TestOpenFileSnapshotIgnoresSiblingPrefix(t *testing.T) {
	root := fixtureRoot(t)
	app, sibling := filepath.Join(root, "app"), filepath.Join(root, "app-2")
	snapshot := Snapshot{Method: MethodProcfs, processes: []processPaths{{pid: 7, command: "x", paths: map[string][]string{}}}}
	snapshot.processes[0].add(filepath.Join(sibling, "f"), "fd")
	if got := snapshot.Holders(app); len(got) != 0 {
		t.Fatalf("a sibling sharing a name prefix is not beneath the directory: %+v", got)
	}
	if got := snapshot.Holders(sibling); len(got) != 1 {
		t.Fatalf("holders = %+v", got)
	}
}

func TestOpenFileSnapshotMatchesMapsPathsWithEscapedSpaces(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("procfs maps paths are Linux-only")
	}
	root := filepath.Join(fixtureRoot(t), "repo with spaces")
	file := filepath.Join(root, "asset bundle.bin")
	escaped := strings.ReplaceAll(file, " ", `\040`)
	snapshot := Snapshot{Method: MethodProcfs, processes: []processPaths{{
		pid: 7, command: "viewer", paths: map[string][]string{},
	}}}
	snapshot.processes[0].add(escaped, "maps")

	got := snapshot.Holders(root)
	if len(got) != 1 || got[0].PID != 7 || got[0].Path != file {
		t.Fatalf("holders = %+v, want pid 7 mapped to %q", got, file)
	}
}

func TestUnescapeProcMapsPath(t *testing.T) {
	got := unescapeProcMapsPath(`/tmp/repo\040with\040spaces\134file`)
	want := `/tmp/repo with spaces\file`
	if got != want {
		t.Fatalf("unescapeProcMapsPath() = %q, want %q", got, want)
	}
}

func TestParseLsofOutput(t *testing.T) {
	root := fixtureRoot(t)
	app, home := filepath.Join(root, "app"), filepath.Join(root, "home", "me")
	output := "p101\ncvim\nfcwd\nn" + app + "\nf3\nn" + filepath.Join(app, "main.go") + "\np202\ncbash\nfcwd\nn" + home + "\n"
	snapshot, err := parseLsof([]byte(output))
	if err != nil {
		t.Fatal(err)
	}
	got := snapshot.Holders(app)
	if len(got) != 1 || got[0].PID != 101 || got[0].Command != "vim" {
		t.Fatalf("holders = %+v", got)
	}
	if len(snapshot.Holders(home)) != 1 {
		t.Fatalf("cwd holder missing")
	}
	if _, err := parseLsof([]byte("pnotanumber\n")); err == nil {
		t.Fatal("malformed lsof output must fail closed")
	}
}

func TestParseLsofOutputIncludesCurrentProcess(t *testing.T) {
	app := filepath.Join(fixtureRoot(t), "app")
	snapshot, err := parseLsof([]byte("p5\ncanvil\nn" + app + "\np6\ncvim\nn" + app + "\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got := snapshot.Holders(app); len(got) != 2 || got[0].PID != 5 || got[1].PID != 6 {
		t.Fatalf("the current process and other holders must be included: %+v", got)
	}
}
