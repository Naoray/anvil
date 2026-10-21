//go:build darwin

package procs

import "golang.org/x/sys/unix"

// groupMembers returns the live PIDs whose process group is pgid.
func groupMembers(pgid int) ([]int, error) {
	infos, err := unix.SysctlKinfoProcSlice("kern.proc.pgrp", pgid)
	if err != nil {
		return nil, err
	}
	pids := make([]int, 0, len(infos))
	for _, info := range infos {
		if info.Eproc.Pgid == int32(pgid) && info.Proc.P_stat != sZOMB {
			pids = append(pids, int(info.Proc.P_pid))
		}
	}
	return pids, nil
}
