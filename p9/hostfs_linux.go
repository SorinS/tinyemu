//go:build linux

package p9

import (
	"syscall"

	"golang.org/x/sys/unix"
)

// UTIME constants for utimensat
// Reference: tinyemu-2019-12-21/fs_disk.c uses UTIME_NOW and UTIME_OMIT
const (
	utimeNow  = unix.UTIME_NOW
	utimeOmit = unix.UTIME_OMIT
)

// utimesNanoAt wraps unix.UtimesNanoAt for setting file times.
// Uses AT_SYMLINK_NOFOLLOW to match C behavior with lstat paths.
// Reference: tinyemu-2019-12-21/fs_disk.c:462
func utimesNanoAt(path string, ts []unix.Timespec) error {
	return unix.UtimesNanoAt(unix.AT_FDCWD, path, ts, unix.AT_SYMLINK_NOFOLLOW)
}

func statATime(sys *syscall.Stat_t) (uint64, uint32) {
	return uint64(sys.Atim.Sec), uint32(sys.Atim.Nsec)
}

func statMTime(sys *syscall.Stat_t) (uint64, uint32) {
	return uint64(sys.Mtim.Sec), uint32(sys.Mtim.Nsec)
}

func statCTime(sys *syscall.Stat_t) (uint64, uint32) {
	return uint64(sys.Ctim.Sec), uint32(sys.Ctim.Nsec)
}

func statfsNameLen(stat *syscall.Statfs_t) uint32 {
	return uint32(stat.Namelen)
}
