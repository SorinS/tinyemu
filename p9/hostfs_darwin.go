//go:build darwin

package p9

import (
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// Sentinel values for UTIME_NOW and UTIME_OMIT.
// Darwin's syscall.UtimesNano does not support these magic values natively,
// so utimesNanoAt resolves them to actual timestamps before calling the syscall.
const (
	utimeNow  = -1
	utimeOmit = -2
)

// utimesNanoAt sets file times using syscall.UtimesNano.
// It resolves utimeNow and utimeOmit sentinel values because Darwin does not
// support the UTIME_NOW / UTIME_OMIT magic values in UtimesNano.
func utimesNanoAt(path string, ts []unix.Timespec) error {
	var resolved [2]syscall.Timespec
	needStat := false

	for i, t := range ts {
		if t.Nsec == utimeNow {
			now := time.Now()
			resolved[i] = syscall.Timespec{Sec: now.Unix(), Nsec: int64(now.Nanosecond())}
		} else if t.Nsec == utimeOmit {
			needStat = true
		} else {
			resolved[i] = syscall.Timespec{Sec: t.Sec, Nsec: t.Nsec}
		}
	}

	if needStat {
		var stat syscall.Stat_t
		if err := syscall.Lstat(path, &stat); err != nil {
			return err
		}
		for i, t := range ts {
			if t.Nsec == utimeOmit {
				if i == 0 {
					resolved[i] = syscall.Timespec{Sec: stat.Atimespec.Sec, Nsec: stat.Atimespec.Nsec}
				} else {
					resolved[i] = syscall.Timespec{Sec: stat.Mtimespec.Sec, Nsec: stat.Mtimespec.Nsec}
				}
			}
		}
	}

	return syscall.UtimesNano(path, resolved[:])
}

func statATime(sys *syscall.Stat_t) (uint64, uint32) {
	return uint64(sys.Atimespec.Sec), uint32(sys.Atimespec.Nsec)
}

func statMTime(sys *syscall.Stat_t) (uint64, uint32) {
	return uint64(sys.Mtimespec.Sec), uint32(sys.Mtimespec.Nsec)
}

func statCTime(sys *syscall.Stat_t) (uint64, uint32) {
	return uint64(sys.Ctimespec.Sec), uint32(sys.Ctimespec.Nsec)
}

func statfsNameLen(stat *syscall.Statfs_t) uint32 {
	return syscall.NAME_MAX
}
