package checks

import (
	"io/fs"
	"syscall"
)

// statCacheSupported reports that statIdentity provides ctime and inode.
const statCacheSupported = true

// statIdentity returns the change time (ns) and inode of info.
func statIdentity(info fs.FileInfo) (int64, uint64) {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, 0
	}
	return st.Ctim.Nano(), st.Ino
}
