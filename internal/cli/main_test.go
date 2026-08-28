package cli

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// TestMain builds the real anvil binary once and publishes its path through
// the test-only ANVIL_TEST_BIN environment key — no package variable. When
// re-invoked with ANVIL_TEST_HELPER=1 the test binary acts as the child
// process for `anvil exec` integration tests instead of running tests.
func TestMain(m *testing.M) {
	if os.Getenv("ANVIL_TEST_HELPER") == "1" {
		helperMain() // dispatches on ANVIL_HELPER_MODE; calls os.Exit — never returns
	}

	dir, err := os.MkdirTemp("", "anvil-test-bin")
	if err != nil {
		fmt.Fprintln(os.Stderr, "creating test binary dir:", err)
		os.Exit(1)
	}
	bin := filepath.Join(dir, testBinaryName())
	buildCmd := exec.Command("go", "build", "-o", bin, "github.com/naoray/anvil/cmd/anvil")
	if buildOutput, buildErr := buildCmd.CombinedOutput(); buildErr != nil {
		fmt.Fprintf(os.Stderr, "building anvil test binary: %v\n%s", buildErr, buildOutput)
		removeTestBinaryDir(dir)
		os.Exit(1)
	}
	if err := os.Setenv("ANVIL_TEST_BIN", bin); err != nil {
		fmt.Fprintln(os.Stderr, "setting ANVIL_TEST_BIN:", err)
		removeTestBinaryDir(dir)
		os.Exit(1)
	}

	code := m.Run()
	// Cleanup must happen before os.Exit — a defer would never run.
	removeTestBinaryDir(dir)
	os.Exit(code)
}

func testBinaryName() string {
	if runtime.GOOS == "windows" {
		return "anvil.exe"
	}
	return "anvil"
}

func removeTestBinaryDir(dir string) {
	if err := os.RemoveAll(dir); err != nil {
		fmt.Fprintln(os.Stderr, "cleanup of test binary dir failed:", err)
	}
}

// helperMain implements the pure-Go child processes for exec integration
// tests (no sh fixtures, no /tmp assumptions). Every branch exits.
func helperMain() {
	switch mode := os.Getenv("ANVIL_HELPER_MODE"); mode {
	case "env":
		for _, key := range []string{"DB_DATABASE", "ANVIL_DB_DATABASE", "ANVIL_TEST_DB_DATABASE"} {
			fmt.Printf("%s=%s\n", key, os.Getenv(key))
		}
		for i, arg := range os.Args[1:] {
			fmt.Printf("argv[%d]=%s\n", i, arg)
		}
		os.Exit(0)
	case "exit":
		code, err := strconv.Atoi(os.Getenv("ANVIL_HELPER_EXIT"))
		if err != nil {
			fmt.Fprintln(os.Stderr, "invalid ANVIL_HELPER_EXIT:", err)
			os.Exit(1)
		}
		os.Exit(code)
	case "cat":
		if _, err := io.Copy(os.Stdout, os.Stdin); err != nil {
			fmt.Fprintln(os.Stderr, "copying stdin to stdout:", err)
			os.Exit(1)
		}
		os.Exit(0)
	case "stderr":
		fmt.Fprint(os.Stderr, os.Getenv("ANVIL_HELPER_STDERR"))
		os.Exit(0)
	case "database-command":
		command := strings.TrimSuffix(filepath.Base(os.Args[0]), filepath.Ext(os.Args[0]))
		logPath := os.Getenv("ANVIL_TEST_COMMAND_LOG")
		logFile, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			fmt.Fprintln(os.Stderr, "opening database command log:", err)
			os.Exit(1)
		}
		if _, err := fmt.Fprintf(logFile, "%s %s\n", command, strings.Join(os.Args[1:], " ")); err != nil {
			fmt.Fprintln(os.Stderr, "writing database command log:", err)
			_ = logFile.Close()
			os.Exit(1)
		}
		if err := logFile.Close(); err != nil {
			fmt.Fprintln(os.Stderr, "closing database command log:", err)
			os.Exit(1)
		}
		switch command {
		case "herd":
			os.Exit(0)
		case "psql":
			fmt.Println("demo_pg_fixture")
			fmt.Println("demo_pg_fixture_test")
			fmt.Println("demo_pg_fixture_test_1")
			fmt.Println("other")
			os.Exit(0)
		case "dropdb":
			os.Exit(99)
		default:
			fmt.Fprintln(os.Stderr, "unknown database command:", command)
			os.Exit(1)
		}
	default:
		fmt.Fprintf(os.Stderr, "unknown ANVIL_HELPER_MODE %q\n", mode)
		os.Exit(1)
	}
}
