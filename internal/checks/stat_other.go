//go:build !linux && !darwin

package checks

import "io/fs"

// statCacheSupported is false: without ctime and inode, the directory
// identity cache is disabled and every file is hashed.
const statCacheSupported = false

func statIdentity(fs.FileInfo) (int64, uint64) { return 0, 0 }
