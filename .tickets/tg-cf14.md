---
id: tg-cf14
status: closed
deps: []
links: []
created: 2026-01-23T18:31:46Z
type: task
priority: 2
assignee: JT Olio
parent: tg-5945
tags: [c-review, port-verification, slirp, tcp]
---
# Review tcp_input: initialization, header parsing, checksum

Review C code lines 216-312 of tcp_input.c tcp_input function and ensure Go implementation matches exactly.

## C Code Section to Review

- Function signature and variable declarations (lines 216-231)
- DEBUG_CALL and DEBUG_ARGS (lines 233-235)
- Null mbuf case (m == NULL) for connection continuation (lines 240-253)
- Get IP and TCP header together, strip options (lines 254-264)
- Save copy of IP header for ICMP errors (lines 269-274)
- Checksum calculation and validation (lines 278-287)
- TCP offset validation, pull out options (lines 289-302)
- Convert TCP fields to host format (lines 304-311)
- Drop TCP/IP headers from mbuf (lines 315-317)

## Instructions

1. Read the C code carefully for initialization and header processing
2. Find the corresponding Go code in slirp/ package
3. Compare line-by-line for exact behavioral match
4. Add/update comments referencing C code: `// Reference: tinyemu-2019-12-21/slirp/tcp_input.c:216-312`
5. Fix ANY deviations including error handling differences
6. Write tests to confirm behavior matches C code

## Files

- `tinyemu-2019-12-21/slirp/tcp_input.c` (lines 216-312)

## Acceptance Criteria

- [ ] Header parsing reviewed against C source
- [ ] Go implementation matches C behavior exactly
- [ ] Comments reference C code with file:line format
- [ ] Tests confirm C-matching behavior
- [ ] go test and go vet pass


## Notes

**2026-01-24T03:35:22Z**

## Review Complete

Verified Go implementation matches C code for lines 216-312:

**Variable declarations (lines 216-231):**
- Go declares all variables upfront due to goto usage ✓

**Null mbuf case (lines 240-253):**
- Continue connection when m == nil ✓
- Restore tp, m, tiwin, tiflags from socket ✓
- goto cont_conn equivalent ✓

**Get IP/TCP header (lines 254-264):**
- Length check (iphlen + 20) ✓
- IP options stripping ✓

**Save IP header (lines 268-274):**
- SavedTCPIPHeader captures header for RST/ICMP ✓
- Includes IP total length restoration ✓

**Checksum calculation (lines 276-287):**
- Pseudo-header construction with zeroed fields ✓
- ti_x1 = 0, ti_len = htons(tlen) ✓
- cksum call with drop on failure ✓

**TCP offset validation (lines 289-302):**
- off = ti_off << 2 ✓
- Drop if off < 20 or off > tlen ✓
- tiLen adjusted for data only ✓
- Options extraction when off > 20 ✓

**Convert to host format (lines 304-311):**
- NTOHL for seq, ack ✓
- NTOHS for win, urp ✓

**Drop headers (lines 313-317):**
- m.Adj(headerLen) equivalent to m_data adjustment ✓

Added C code references to TCP field conversion and header dropping.
