# TCP Input Full Implementation Plan

This document describes the work required to implement a complete Go version of
`tcp_input()` from `tinyemu-2019-12-21/slirp/tcp_input.c` (lines 216-1287) with
exact behavioral matching to the C version.

## Current State

The current Go implementation in `slirp/tcp_input.go` is a **simplified version**
that handles the basic TCP state machine but has several gaps:

1. **Simplified TCP reassembly** - Only accepts in-order segments, drops out-of-order
2. **No RST packet sending** - `tcpDropWithReset` silently drops without sending RST
3. **Stub tcp_ctl** - Returns 1 without actual exec/pty handling
4. **No ICMP error messages** - Connection failures don't send ICMP unreachable
5. **Missing header preservation** - Can't send proper RST because headers are stripped
6. **Missing small packet optimization** - No escape character check for immediate ACK

## C Code Structure Analysis

The C `tcp_input()` function (1072 lines) has this structure:

```
tcp_input(m, iphlen, inso)
├── Connection continuation (m == NULL case)
├── Header parsing and validation
├── Checksum verification
├── Socket lookup (findso label)
├── New connection handling (SYN only)
├── State machine:
│   ├── LISTEN state (with cont_conn, cont_input labels)
│   ├── SYN_SENT state
│   └── All other states
├── Segment trimming (leading/trailing)
├── RST processing
├── SYN-in-window check
├── ACK processing (with synrx_to_est label)
├── Window update (step6 label)
├── URG processing
├── Data processing (dodata label)
├── FIN processing
├── Small packet optimization
├── Output decision
└── Error handling (dropafterack, dropwithreset, drop labels)
```

## Required Work Items

### 1. TCP Reassembly Queue (tcp_reass)

**C Reference:** `tcp_input.c:104-209`

The C implementation maintains a doubly-linked list of out-of-order segments per
connection. Each segment in the queue is a `tcpiphdr` with:
- Sequence number (`ti_seq`)
- Length (`ti_len`)
- Flags (`ti_flags`)
- Pointer to mbuf (`ti_mbuf`)
- Next/prev links via `qlink` structure

**Required Changes:**

1. Add `Next`/`Prev` fields to `TCPIPHdr` for queue linkage
2. Add `TiLen` field to `TCPIPHdr` to track segment length
3. Initialize reassembly queue in `tcpNewTCPCB()` (circular list pointing to self)
4. Implement full `tcp_reass()`:
   - Find insertion point by sequence number
   - Handle overlap with preceding segment (trim or drop)
   - Handle overlap with succeeding segments (trim or dequeue)
   - Insert new segment
   - Present contiguous data to user (`present` label)
5. Free reassembly queue in `TCPClose()`

**Files:** `slirp/tcp.go`, `slirp/tcp_input.go`, `slirp/tcp_timer.go`

### 2. Header Preservation for RST Sending

**C Reference:** `tcp_input.c:268-274`, `tcp_input.c:1268-1278`

The C code saves a copy of the IP header (`save_ip`) before processing so it can
reconstruct the packet for sending ICMP errors or RST responses. The current Go
implementation strips headers and cannot send proper RST packets.

**Required Changes:**

1. Save full IP+TCP header before stripping (not just IP header)
2. Store original `ti` pointer for `dropwithreset` handling
3. Implement proper `tcpDropWithReset()` that calls `tcp_respond()`
4. Handle the three RST cases from C:
   - ACK set: `tcp_respond(tp, ti, m, 0, ti->ti_ack, TH_RST)`
   - SYN set: `tcp_respond(tp, ti, m, ti->ti_seq+ti->ti_len, 0, TH_RST|TH_ACK)`
   - Other: `tcp_respond(tp, ti, m, ti->ti_seq+ti->ti_len, 0, TH_RST|TH_ACK)`

**Files:** `slirp/tcp_input.go`

### 3. Full tcp_ctl Implementation

**C Reference:** `tcp_subr.c:884-915`

The `tcp_ctl()` function handles special control sockets for exec/pty functionality.
It's called when a connection to the virtual network reaches ESTABLISHED state with
the `SS_CTL` flag set.

**Required Changes:**

1. Implement `TCPCtl()` function matching C behavior:
   - Check exec_list for matching port/address
   - Handle `ex_pty == 3` case (return 1, set `so->extra`)
   - Handle fork_exec case (not needed for emulator - can stub)
   - Default: write error message to send buffer, return 0
2. The current stub returns 1 always - needs proper logic

**Files:** `slirp/tcp_input.go` or new `slirp/tcp_subr.go`

### 4. ICMP Error Integration

**C Reference:** `tcp_input.c:581-601`

When `tcp_fconnect()` fails with errors other than EINPROGRESS/EWOULDBLOCK, the C
code sends appropriate ICMP error messages:

- `ECONNREFUSED` → Send RST|ACK via `tcp_respond()`
- `EHOSTUNREACH` → Send ICMP_UNREACH_HOST via `icmp_error()`
- Other errors → Send ICMP_UNREACH_NET via `icmp_error()`

**Required Changes:**

1. Modify `tcpFConnect()` to return actual error codes
2. Update LISTEN state handling to check error types
3. Call `ICMPError()` for host/network unreachable cases
4. Restore TCP header to network byte order before ICMP (C does this)
5. Restore original IP header (`*ip = save_ip`)

**Files:** `slirp/tcp_input.go`

### 5. LISTEN State Full Implementation

**C Reference:** `tcp_input.c:538-642`

The LISTEN state handling has several code paths that need exact matching:

**Required Changes:**

1. Fix `cont_conn` label handling (connection continuation when m==NULL)
2. Fix `cont_input` label handling (for SS_CTL and EMU_NOCONNECT)
3. Implement proper error handling in `tcp_fconnect()` failure path
4. Save `ti` pointer (not just extracted fields) for later use
5. Handle the `trimthenstep6` label properly

**Files:** `slirp/tcp_input.go`

### 6. Small Packet ACK Optimization

**C Reference:** `tcp_input.c:1243-1246`

```c
if (ti->ti_len && (unsigned)ti->ti_len <= 5 &&
    ((struct tcpiphdr_2 *)ti)->first_char == (char)27) {
    tp->t_flags |= TF_ACKNOW;
}
```

This optimization immediately ACKs small packets that start with escape character
(0x1B), commonly used for terminal control sequences.

**Required Changes:**

1. After data processing, check if segment length is 1-5 bytes
2. Check if first byte of data is 0x1B (escape)
3. If so, set `TF_ACKNOW` flag

**Files:** `slirp/tcp_input.go`

### 7. TCP_REASS Macro Inline Version

**C Reference:** `tcp_input.c:62-99`

The C code has a `TCP_REASS` macro that handles the common case inline (in-order
segment on established connection with empty reassembly queue) and calls
`tcp_reass()` for complex cases.

**Required Changes:**

1. Implement the inline fast path in data processing section
2. Set `TF_DELACK` for in-order data (not `TF_ACKNOW` as current code does)
3. Set `TF_ACKNOW` for out-of-order or when `TH_PUSH` is set

**Files:** `slirp/tcp_input.go`

### 8. Sequence Number Comparison Functions

**C Reference:** `tcp_input.c:48-50` (via tcp_seq.h)

The C code uses `SEQ_LT`, `SEQ_LEQ`, `SEQ_GT`, `SEQ_GEQ` macros for proper
sequence number comparison with wraparound handling.

**Current State:** Go has `seqLT`, `seqLEQ`, `seqGT` - verify they exist and work.

**Required Changes:**

1. Verify all sequence comparison functions exist
2. Add `seqGEQ` if missing
3. Ensure they handle 32-bit wraparound correctly

**Files:** `slirp/tcp_input.go` or `slirp/tcp.go`

### 9. Test Coverage

The implementation needs comprehensive tests covering:

1. **Reassembly queue tests:**
   - In-order segments
   - Out-of-order segments
   - Overlapping segments
   - Duplicate segments
   - Queue overflow scenarios

2. **State machine tests:**
   - All state transitions
   - RST handling in each state
   - SYN-in-window attacks
   - FIN processing

3. **Error handling tests:**
   - Connection refused → RST
   - Host unreachable → ICMP
   - Network unreachable → ICMP

4. **Edge cases:**
   - Zero window probes
   - Persist timer interaction
   - Duplicate ACK handling
   - Fast retransmit triggering

**Files:** `slirp/tcp_input_test.go`

## Implementation Order

Recommended order based on dependencies:

1. **Sequence comparison functions** (foundation)
2. **Header preservation** (needed for RST)
3. **tcpDropWithReset** (needed by many paths)
4. **TCP reassembly queue** (core functionality)
5. **LISTEN state fixes** (includes ICMP)
6. **tcp_ctl** (needed for SS_CTL handling)
7. **Small packet optimization** (minor)
8. **Tests** (throughout, but comprehensive at end)

## Acceptance Criteria

For each work item:

- [ ] Go implementation matches C behavior exactly
- [ ] Comments reference C code with `file:line` format
- [ ] Unit tests demonstrate C-matching behavior
- [ ] `go test ./slirp/...` passes
- [ ] `go vet ./slirp/...` passes
- [ ] `gofmt -s` and `goimports` applied

## Risk Areas

1. **Reassembly queue memory management** - C uses manual free, Go has GC.
   Need to ensure segments are properly unlinked so GC can collect.

2. **Header preservation** - Current design strips headers early. May need
   architectural change to preserve original packet data.

3. **Sequence wraparound** - All sequence comparisons must use signed arithmetic
   to handle 32-bit wraparound correctly.

4. **Timing** - TCP timers interact with input processing. Ensure timer
   callbacks don't race with input processing.

## References

- `tinyemu-2019-12-21/slirp/tcp_input.c` - Main implementation
- `tinyemu-2019-12-21/slirp/tcp_subr.c` - Helper functions (tcp_respond, tcp_ctl)
- `tinyemu-2019-12-21/slirp/tcpip.h` - Header structures and macros
- `tinyemu-2019-12-21/slirp/tcp_var.h` - TCP control block structure
- `tinyemu-2019-12-21/slirp/ip_icmp.c` - ICMP error sending
