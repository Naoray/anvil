//go:build linux

package procs

import (
	"os"
	"strconv"
	"testing"
)

func TestScanProcfsMountNamespaces(t *testing.T) {
	const own, other = 1000, 2000
	uids := map[int]int{10: own, 11: own, 12: other, 13: own, 14: own}
	namespaces := map[int]string{10: "mnt:[1]", 11: "mnt:[2]", 12: "mnt:[2]"}
	source := procfsSource{
		euid: own, selfNamespace: "mnt:[1]",
		pids:         func() ([]int, error) { return []int{10, 11, 12, 13, 14}, nil },
		kernelThread: func(pid int) bool { return pid == 14 }, // e.g. kdevtmpfs: init's namespace, no user files
		inspect: func(pid int) (processPaths, int, error) {
			return processPaths{pid: pid, command: "p" + strconv.Itoa(pid), paths: map[string][]string{"/w/app/f": {"fd"}}}, uids[pid], nil
		},
		namespace: func(pid int) (string, error) {
			if pid == 13 {
				return "", os.ErrPermission
			}
			return namespaces[pid], nil
		},
		command: func(pid int) string { return "p" + strconv.Itoa(pid) },
	}
	snapshot, err := source.scan()
	if err != nil {
		t.Fatal(err)
	}
	holders := snapshot.Holders("/w/app")
	if len(holders) != 1 || holders[0].PID != 10 {
		t.Fatalf("only the same-namespace process maps its paths: %+v", holders)
	}
	if len(snapshot.ForeignNamespaceOwn) != 1 || snapshot.ForeignNamespaceOwn[0].PID != 11 || snapshot.ForeignNamespaceOwn[0].Command != "p11" {
		t.Fatalf("own foreign-namespace processes = %+v", snapshot.ForeignNamespaceOwn)
	}
	if snapshot.SkippedOtherUsers != 1 {
		t.Fatalf("another user's foreign-namespace process follows the other-user rule: %d", snapshot.SkippedOtherUsers)
	}
	if len(snapshot.UninspectableOwn) != 1 || snapshot.UninspectableOwn[0].PID != 13 {
		t.Fatalf("an unreadable namespace link of an own process is missing evidence: %+v", snapshot.UninspectableOwn)
	}
}
