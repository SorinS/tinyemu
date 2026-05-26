//go:build linux

package slirp

import "syscall"

var dnsAddrStat syscall.Stat_t

func statMtimSec(stat *syscall.Stat_t) int64 {
	return stat.Mtim.Sec
}

func dnsStatSnapshot() error {
	return syscall.Stat("/etc/resolv.conf", &dnsAddrStat)
}

func dnsStatUnchanged() bool {
	var newStat syscall.Stat_t
	if err := syscall.Stat("/etc/resolv.conf", &newStat); err != nil {
		return false
	}
	return newStat.Dev == dnsAddrStat.Dev &&
		newStat.Ino == dnsAddrStat.Ino &&
		newStat.Size == dnsAddrStat.Size &&
		statMtimSec(&newStat) == statMtimSec(&dnsAddrStat)
}
