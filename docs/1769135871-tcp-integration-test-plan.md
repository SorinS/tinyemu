# TCP Bidirectional Integration Testing Plan

**Date**: 2026-01-22
**Status**: Planning

## Executive Summary

This document outlines a systematic plan to implement full integration tests that verify bidirectional TCP communication between the host and guest in TinyEMU-Go. Previous attempts have established that unit tests pass but full boot integration tests fail at the TCP handshake level.

## Current State Analysis

### What Works

1. **Unit-level TCP**: `TestIntegrationTCPExecForwarding` passes
   - SYN packets are processed correctly
   - TCP sockets are created with correct addresses
   - SYN-ACK packets are generated with correct source/destination IPs
   - TCP state machine reaches SYN_RECEIVED state

2. **Lower-level networking**: `TestLinuxBootNetwork` passes
   - DHCP works (guest gets IP via udhcpc)
   - ICMP ping to gateway (10.0.2.2) works
   - VirtIO network device recognized (eth0)
   - ARP resolution works

3. **Boot infrastructure**: The boot test framework is robust
   - Console I/O works
   - State machine pattern for test progression
   - Timeout handling and error detection

### What Doesn't Work

1. **Boot-level TCP via exec forwarding**: `TestLinuxBootNetworkTCP` fails
   - Socket is found via `FindCtlSocket` (addresses correct)
   - `so.Extra` is nil (should contain the exec string "tcp-test-handler")
   - Guest's nc times out: "can't connect to remote host (10.0.2.100): Connection timed out"
   - **Implication**: The TCP three-way handshake doesn't complete

### Root Cause Hypotheses

Based on the evidence, the likely issues are:

1. **SYN-ACK delivery failure**: The SYN-ACK packet is generated (per unit test) but may not reach the guest's VirtIO network queue

2. **Poll loop timing**: The emulator's `slirp.Poll(es)` may not be called at the right times relative to CPU execution, causing packets to be delayed or lost

3. **Exec forwarding setup incomplete**: The `so.Extra` being nil suggests that `TCPCtl` isn't being called, or it's being called before the exec list check happens

4. **VirtIO queue draining**: Packets may be sitting in slirp's output queue but not being delivered to the guest via VirtIO

## Test Architecture Goals

We want two types of tests:

### A. Guest-to-Host TCP (Guest Initiates)

```
Guest (nc client) ──→ Slirp NAT ──→ Host TCP Server
```

- Guest runs: `echo "HELLO" | nc -w 5 <host-accessible-addr> <port>`
- Host receives data, sends response
- Guest receives response (visible in nc output)

### B. Host-to-Guest TCP (Host Initiates)

```
Host (Go test client) ──→ Slirp Port Forward ──→ Guest (nc -l server)
```

- Guest runs: `nc -l -p 8080` (listen mode)
- Host connects via forwarded port
- Bidirectional data exchange

## Systematic Debugging Plan

Before implementing full tests, we need to understand why the current approach fails.

### Phase 1: Instrument the Data Path

**Objective**: Trace exactly where packets go (or don't go) during TCP handshake

#### Step 1.1: Add packet tracing to Slirp output

Add logging to track every packet that goes through:
- `slirp.OutputFunc` (packet leaving slirp toward guest)
- `slirp.Input` (packet from guest into slirp)
- VirtIO net device receive/transmit

#### Step 1.2: Trace TCP state transitions

Add detailed logging around:
- `TCPInput()` calls (with packet details)
- `TCPOutput()` calls (especially SYN-ACK generation)
- `TCPCtl()` invocations (exec forwarding path)

#### Step 1.3: Verify VirtIO packet delivery

Confirm that when `OutputFunc` is called with a SYN-ACK:
- The packet reaches the VirtIO network device
- It's queued for the guest
- The guest's virtio-net driver receives it

### Phase 2: Minimal Reproduction

**Objective**: Create the smallest possible test that reproduces the handshake failure

#### Step 2.1: Direct Slirp test with full handshake

Extend `TestIntegrationTCPExecForwarding` to:
1. Send SYN (already done)
2. Capture SYN-ACK
3. Send ACK (complete handshake)
4. Send data
5. Verify data in `so.SoRcv` buffer

This eliminates VirtIO and emulator timing from the equation.

#### Step 2.2: VirtIO loopback test

Create a test that:
1. Sends a raw packet to VirtIO net device
2. Verifies it comes out through slirp
3. Injects a response packet
4. Verifies it reaches the VirtIO receive queue

### Phase 3: Fix Identified Issues

Based on Phase 1-2 findings, implement fixes. Likely candidates:

#### Candidate Fix A: Poll loop synchronization

Ensure `slirp.Poll()` is called:
- After every VirtIO transmit that sends packets to slirp
- Before checking VirtIO receive queue
- With proper timing for TCP timers

#### Candidate Fix B: Exec forwarding socket setup

Ensure `TCPCtl` is called at the right time:
- After SYN is received
- Before SYN-ACK is sent
- With proper exec list traversal

#### Candidate Fix C: VirtIO packet queueing

Verify the guest virtio-net driver receives packets:
- Check IRQ delivery for received packets
- Verify virtio queue notifications

## Implementation Plan

### Approach A: Host Server with AddHostfwd (Recommended First)

This is the simplest path because it uses real Go networking on the host side.

```go
func TestLinuxBootTCPHostServer(t *testing.T) {
    // 1. Start TCP server on host
    listener, _ := net.Listen("tcp", "127.0.0.1:0")
    hostPort := listener.Addr().(*net.TCPAddr).Port

    // 2. Use AddHostfwd to forward guest port to host
    //    Guest connects to 10.0.2.2:9000 → forwarded to host listener
    sl.AddHostfwd(false, net.IPv4zero, hostPort, guestIP, 9000)

    // 3. Boot guest, configure network

    // 4. Guest: echo "HELLO" | nc 10.0.2.2 9000

    // 5. Host: accept connection, read "HELLO", send response

    // 6. Guest: verify response in nc output
}
```

**Why this is better than exec forwarding**:
- Uses real TCP sockets (Go's net package)
- No ExPty=3 complexity
- Tests the same code path as real port forwarding

### Approach B: Host Server with External NAT

Test guest connecting to a real external address:

```go
func TestLinuxBootTCPExternalNAT(t *testing.T) {
    // 1. Start TCP server on host's external interface
    listener, _ := net.Listen("tcp", ":0")

    // 2. Get host's IP address (from perspective of virtual network)
    hostIP := getHostExternalIP()

    // 3. Boot guest, configure network

    // 4. Guest: echo "HELLO" | nc <hostIP> <port>

    // 5. This goes through slirp's NAT path (not exec forwarding)
}
```

**Trade-off**: Requires understanding slirp's NAT for external connections.

### Approach C: Guest Server with TCPListen

Test host initiating connection to guest:

```go
func TestLinuxBootTCPGuestServer(t *testing.T) {
    // 1. Configure port forward: host:8080 → guest:8080
    sl.TCPListen(net.IPv4zero, 8080, guestIP, 8080, SSHostFwd)

    // 2. Boot guest, start listener
    sendCommand("nc -l -p 8080 &")

    // 3. Host: connect to localhost:8080 (forwarded to guest)
    conn, _ := net.Dial("tcp", "localhost:8080")

    // 4. Send/receive data
}
```

**Challenge**: Guest's nc may not have `-l` option, or may exit after first connection.

## Recommended Execution Order

### Week 1: Debugging and Instrumentation

1. **Day 1-2**: Implement packet tracing (Phase 1)
   - Add debug logging to slirp output path
   - Add debug logging to VirtIO net device
   - Run `TestLinuxBootNetworkTCP` with tracing

2. **Day 3**: Analyze traces
   - Identify where SYN-ACK is generated
   - Identify if/when it reaches VirtIO
   - Identify if guest receives it

3. **Day 4-5**: Create minimal reproduction (Phase 2)
   - Extend unit test to complete handshake
   - If unit test works, issue is in VirtIO/emulator integration
   - If unit test fails, issue is in TCP state machine

### Week 2: Implementation

4. **Day 1-2**: Implement Approach A (Host Server with AddHostfwd)
   - This is most likely to work given real sockets
   - Provides clearest test of the data path

5. **Day 3-4**: Implement Approach C (Guest Server)
   - Tests the reverse direction
   - May need guest rootfs modification for nc -l

6. **Day 5**: Polish and documentation
   - Clean up debug logging
   - Document any fixes made
   - Update tickets

## Specific Implementation Tasks

### Task 1: Debug Tracing Infrastructure

Create a debug mode for slirp that logs:
- Every packet in/out with hex dump
- TCP state transitions
- Socket creation/destruction

```go
// In slirp/slirp.go
var DebugTracing = false

func (s *Slirp) tracePacket(direction string, pkt []byte) {
    if !DebugTracing {
        return
    }
    log.Printf("[SLIRP %s] %d bytes: %x", direction, len(pkt), pkt[:min(64, len(pkt))])
}
```

### Task 2: Complete Handshake Unit Test

```go
func TestIntegrationTCPFullHandshake(t *testing.T) {
    h := newTestHelper()

    // Setup exec forwarding
    h.slirp.AddExec(3, "test", execAddr, execPort)

    // Send SYN
    synFrame := h.buildTCPSYN(synSrcPort, execPort)
    h.slirp.Input(synFrame)

    // Capture and verify SYN-ACK
    synAck := h.captureOutputTCP()
    require.NotNil(t, synAck)
    require.Equal(t, THSyn|THAck, synAck.Flags)

    // Send ACK
    ackFrame := h.buildTCPACK(synSrcPort, execPort, synAck.Seq+1, synAck.AckSeq)
    h.slirp.Input(ackFrame)

    // Verify socket is established
    so := h.slirp.FindCtlSocket(execAddr, execPort)
    require.NotNil(t, so)
    tp := SoToTCPCB(so)
    require.Equal(t, TCPSEstablished, tp.TState)

    // Send data
    dataFrame := h.buildTCPData(synSrcPort, execPort, []byte("HELLO"))
    h.slirp.Input(dataFrame)

    // Verify data received
    require.Equal(t, "HELLO", string(so.SoRcv.SbBytes()))
}
```

### Task 3: Host Server Integration Test

```go
func TestLinuxBootTCPHostServer(t *testing.T) {
    // Full implementation per Approach A above
    // Key difference from current test: use AddHostfwd instead of AddExec
}
```

### Task 4: VirtIO Packet Tracing

Add tracing to virtio/net.go to log when packets are:
- Received from guest (transmit queue)
- Delivered to guest (receive queue)
- IRQ delivered

## Success Criteria

Tests are considered passing when:

1. **Guest-to-Host test**:
   - Guest sends "HELLO_FROM_GUEST\n"
   - Host receives and logs it
   - Host sends "RESPONSE_FROM_HOST\n"
   - Guest's nc output contains "RESPONSE_FROM_HOST"

2. **Host-to-Guest test**:
   - Guest's nc -l is listening
   - Host connects and sends "HELLO_FROM_HOST\n"
   - Guest echoes it back
   - Host receives the echo

## Risk Mitigation

1. **If exec forwarding is fundamentally broken**: Fall back to AddHostfwd approach which uses real sockets

2. **If VirtIO packet delivery has timing issues**: Add synchronization points or increase polling frequency

3. **If guest nc doesn't support needed features**: Consider using a custom minimal TCP test program loaded via 9P

## Appendix: Key Code Paths

### Guest-to-Host (Exec Forwarding)

```
Guest nc send → VirtIO TX → slirp.Input → ip_input → tcp_input
  → exec list check → TCPCtl → socket setup
  → tcp_output (SYN-ACK) → OutputFunc → VirtIO RX → Guest nc recv
```

### Host-to-Guest (Port Forward)

```
Host net.Dial → OS TCP → slirp.Poll → listener.Accept
  → new socket with guest addr → tcp_output (SYN) → VirtIO RX → Guest nc
  → Guest nc reply → VirtIO TX → slirp.Input → tcp_input → real socket write
```

## References

- `slirp/tcp_input.go`: TCP input processing
- `slirp/tcp_output.go`: TCP output generation
- `slirp/tcp_subr.go`: TCPCtl and exec forwarding
- `slirp/slirp.go`: AddExec, AddHostfwd, TCPListen
- `machine/boot_test.go`: Existing boot test infrastructure
- `virtio/net.go`: VirtIO network device
