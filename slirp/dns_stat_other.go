//go:build !linux && !darwin

package slirp

// On platforms without /etc/resolv.conf we don't try to maintain a stat
// cache. dnsStatSnapshot is a no-op and dnsStatUnchanged always reports
// "changed" so the caller falls back to opening the file each time —
// which itself will fail on those platforms, leaving getDnsAddr to
// return (nil, false). That matches the original C TinyEMU behaviour
// when the resolv.conf path is missing.

func dnsStatSnapshot() error { return nil }
func dnsStatUnchanged() bool { return false }
