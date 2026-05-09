package slirp

import (
	"bufio"
	"net"
	"os"
	"strings"
	"syscall"
)

// DNS address caching state
// Reference: tinyemu-2019-12-21/slirp/slirp.c:43-44, 100
var (
	dnsAddr     net.IP
	dnsAddrTime uint32
	dnsAddrStat syscall.Stat_t
)

// getDnsAddr returns the system's configured DNS server address.
// It parses /etc/resolv.conf and caches the result.
// Returns the DNS address and true on success, or nil and false on error.
// Reference: tinyemu-2019-12-21/slirp/slirp.c:102-165
func getDnsAddr() (net.IP, bool) {
	curtime := GetTimeMs()

	// Check if we have a cached address
	// Reference: tinyemu-2019-12-21/slirp/slirp.c:110-125
	if dnsAddr != nil {
		// Return cached if less than 1 second old
		if curtime-dnsAddrTime < 1000 {
			return dnsAddr, true
		}

		// Check if /etc/resolv.conf has changed
		oldStat := dnsAddrStat
		var newStat syscall.Stat_t
		if err := syscall.Stat("/etc/resolv.conf", &newStat); err != nil {
			return nil, false
		}

		// If file unchanged, return cached
		if newStat.Dev == oldStat.Dev &&
			newStat.Ino == oldStat.Ino &&
			newStat.Size == oldStat.Size &&
			statMtimSec(&newStat) == statMtimSec(&oldStat) {
			return dnsAddr, true
		}

		dnsAddrStat = newStat
	}

	// Parse /etc/resolv.conf
	// Reference: tinyemu-2019-12-21/slirp/slirp.c:128-164
	f, err := os.Open("/etc/resolv.conf")
	if err != nil {
		return nil, false
	}
	defer f.Close()

	// Stat the file for future cache validation
	// Reference: tinyemu-2019-12-21/slirp/slirp.c:117
	if err := syscall.Stat("/etc/resolv.conf", &dnsAddrStat); err != nil {
		return nil, false
	}

	scanner := bufio.NewScanner(f)
	found := false

	for scanner.Scan() {
		line := scanner.Text()

		// Parse "nameserver <address>" lines
		// Reference: tinyemu-2019-12-21/slirp/slirp.c:136
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "nameserver") {
			continue
		}

		// Skip "nameserver" prefix and any whitespace
		rest := strings.TrimPrefix(line, "nameserver")
		rest = strings.TrimLeft(rest, " \t")

		// Get the address (first whitespace-separated token)
		fields := strings.Fields(rest)
		if len(fields) == 0 {
			continue
		}
		addrStr := fields[0]

		// Parse the IP address
		ip := net.ParseIP(addrStr)
		if ip == nil {
			continue
		}
		ip = ip.To4()
		if ip == nil {
			continue // Skip IPv6 addresses, C version only handles IPv4
		}

		// Take the first valid nameserver
		// Reference: tinyemu-2019-12-21/slirp/slirp.c:140-144
		if !found {
			dnsAddr = ip
			dnsAddrTime = curtime
			found = true
			// C continues to count up to 3 for debug output, but we only need the first
			break
		}
	}

	if !found {
		return nil, false
	}

	return dnsAddr, true
}
