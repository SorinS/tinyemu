//go:build darwin

package slirp

import "syscall"

func statMtimSec(stat *syscall.Stat_t) int64 {
	return stat.Mtimespec.Sec
}
