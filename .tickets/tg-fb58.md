---
id: tg-fb58
status: closed
deps: []
links: []
created: 2026-01-22T02:27:29Z
type: bug
priority: 2
assignee: JT Olio
tags: [slirp, ip, c-divergence]
---
# ip_output byte order assumptions may differ from C

In C ip_output, ip_len and ip_off are expected to be in HOST byte order on entry, then converted to network order with htons() before sending.

The Go implementation reads these fields with binary.BigEndian (network order) and writes them back the same way. If callers construct packets with host byte order (as C callers do), the Go code will misinterpret the values.

Need to audit all ip_output callers to verify byte order consistency, and ensure Go matches C expectations.

C code:
  ip->ip_len = htons((uint16_t)ip->ip_len);
  ip->ip_off = htons((uint16_t)ip->ip_off);

Reference: tinyemu-2019-12-21/slirp/ip_output.c:79-80


## Notes

**2026-01-22T03:07:48Z**

After audit: Not a bug. Go code consistently uses network byte order (BigEndian) throughout, unlike C which uses host byte order + htons(). All callers construct packets with BigEndian. Removed misleading comments and no-op code in IPOutput.
