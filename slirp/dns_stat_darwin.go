//go:build darwin

package slirp

import "syscall"

// dnsAddrStat holds the last-seen stat of /etc/resolv.conf. We use it to
// notice when the file has changed and the cached nameserver should be
// re-parsed. On non-unix platforms the equivalent file does not exist;
// see dns_stat_other.go.
var dnsAddrStat syscall.Stat_t

func statMtimSec(stat *syscall.Stat_t) int64 {
	return stat.Mtimespec.Sec
}

// dnsStatSnapshot stat()s /etc/resolv.conf and stores the result in
// dnsAddrStat for later comparison.
func dnsStatSnapshot() error {
	return syscall.Stat("/etc/resolv.conf", &dnsAddrStat)
}

// dnsStatUnchanged reports whether /etc/resolv.conf still matches the
// cached stat. Returns false if the stat fails (treat as changed) or any
// of dev/ino/size/mtime differ.
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
