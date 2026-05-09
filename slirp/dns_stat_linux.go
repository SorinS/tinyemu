//go:build linux

package slirp

import "syscall"

func statMtimSec(stat *syscall.Stat_t) int64 {
	return stat.Mtim.Sec
}
