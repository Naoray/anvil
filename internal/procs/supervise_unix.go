//go:build linux || darwin

package procs

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"time"

	"golang.org/x/sys/unix"
)

// supervisePoll bounds how quickly the supervisor notices an empty group.
const supervisePoll = 250 * time.Millisecond

// Supervise runs the group-leader role when args request it, returning the
// exit code and true; otherwise it returns false and does nothing. The
// binary's main calls it first.
func Supervise(args []string) (int, bool) {
	if len(args) != 3 || args[1] != SuperviseArg {
		return 0, false
	}
	return supervise(args[2]), true
}

// supervise runs `sh -c command` inside its own process group and stays
// alive until every other member of the group has exited. A live leader
// with the recorded start identity therefore proves the group is still the
// one Anvil launched, even when the command backgrounds its work and
// returns.
func supervise(command string) int {
	// Catch rather than ignore: caught signals reset to their default in
	// exec'd children, so the command still receives SIGTERM normally. The
	// channel is never read; Notify drops signals once it is full.
	signal.Notify(make(chan os.Signal, 1), unix.SIGTERM, unix.SIGINT, unix.SIGHUP)

	code := 0
	cmd := exec.Command("sh", "-c", command)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			fmt.Fprintln(os.Stderr, "anvil supervisor:", err)
			return 127
		}
		code = exitErr.ExitCode()
	}
	self := os.Getpid()
	for {
		members, err := groupMembers(self)
		if err != nil {
			fmt.Fprintln(os.Stderr, "anvil supervisor:", err)
			return code
		}
		if !hasOtherMember(members, self) {
			return code
		}
		time.Sleep(supervisePoll)
	}
}

func hasOtherMember(members []int, self int) bool {
	for _, pid := range members {
		if pid != self {
			return true
		}
	}
	return false
}
