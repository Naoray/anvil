//go:build !windows

package checks

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/naoray/anvil/internal/lockfile"
)

const supervisorArgument = "__anvil-check-supervisor"

func init() {
	if len(os.Args) == 4 && os.Args[1] == supervisorArgument {
		os.Exit(supervisorMain())
	}
}

// supervisedCommand gives a separate supervisor copies of the acquired locks.
// EOF on its heartbeat means the parent exited (even SIGKILL). The supervisor
// retains every lock until it has terminated the command's process group.
func supervisedCommand(ctx context.Context, run string, guards []*lockfile.Lock) (*exec.Cmd, func() error, error) {
	executable, err := os.Executable()
	if err != nil {
		return nil, nil, err
	}
	reader, writer, err := os.Pipe()
	if err != nil {
		return nil, nil, err
	}
	var closeOnce sync.Once
	var closeErr error
	closeHeartbeat := func() error { closeOnce.Do(func() { closeErr = writer.Close() }); return closeErr }
	finish := func() error { return errors.Join(reader.Close(), closeHeartbeat()) }
	cmd := exec.CommandContext(ctx, executable, supervisorArgument, run, strconv.Itoa(len(guards)))
	cmd.ExtraFiles = append(cmd.ExtraFiles, reader)
	for _, guard := range guards {
		cmd.ExtraFiles = append(cmd.ExtraFiles, guard.File())
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	// Do not kill the supervisor on cancellation: it must keep the locks while
	// terminating the complete group. Closing the heartbeat requests that cleanup.
	cmd.Cancel = closeHeartbeat
	return cmd, finish, nil
}

func supervisorMain() (code int) {
	count, err := strconv.Atoi(os.Args[3])
	if err != nil || count < 0 {
		return 1
	}
	heartbeat := os.NewFile(3, "check-heartbeat")
	if heartbeat == nil {
		return 1
	}
	defer func() {
		if err := heartbeat.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
			code = 1
		}
	}()
	syscall.CloseOnExec(3)
	guards := make([]*os.File, 0, count)
	for i := 0; i < count; i++ {
		fd := 4 + i
		file := os.NewFile(uintptr(fd), "inherited-check-lock")
		if file == nil {
			return 1
		}
		guards = append(guards, file)
		syscall.CloseOnExec(fd)
	}
	defer func() {
		for _, guard := range guards {
			if err := guard.Close(); err != nil {
				code = 1
			}
		}
	}()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		if _, err := io.Copy(io.Discard, heartbeat); err != nil && !errors.Is(err, os.ErrClosed) {
			cancel()
			return
		}
		cancel()
	}()
	cmd := exec.CommandContext(ctx, "sh", "-c", os.Args[2])
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	configureGroup(cmd)
	runErr := cmd.Run()
	killGroup(cmd)
	if cmd.Process != nil {
		// Fail closed if the OS cannot establish that every member is gone. A
		// surviving or unreaped group keeps both cleanup and resource use excluded.
		for {
			err := syscall.Kill(-cmd.Process.Pid, 0)
			if errors.Is(err, syscall.ESRCH) {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	if runErr == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(runErr, &exitErr) {
		if code := exitErr.ExitCode(); code > 0 {
			return code
		}
		return 1
	}
	if _, err := fmt.Fprintln(os.Stderr, "starting supervised check:", runErr); err != nil {
		return 1
	}
	return 1
}
