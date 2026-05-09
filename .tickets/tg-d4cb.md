---
id: tg-d4cb
status: closed
deps: []
links: []
created: 2026-01-21T17:52:16Z
type: bug
priority: 1
assignee: JT Olio
---
# TCP state machine tests fail through regular path

Tests that verify TCP state transitions through TCPInput fail when packets go through the regular TCP state machine path (not header prediction fast path).

## Design

## Problem

Tests that send packets through TCPInput to verify state transitions fail unexpectedly:
- RcvNxt doesn't advance for data packets
- State transitions (ESTABLISHED→CLOSE_WAIT, FIN_WAIT_1→CLOSING, etc.) don't occur
- FIN processing doesn't execute

## Working vs Failing Tests

**Working tests** use one of:
1. Header prediction fast path (packets with only THAck, matching seq/ack/window)
   - Example: TestSmallPacketEscapeCharOptimization
2. Specific switch cases (SYN_SENT has its own case)
   - Example: TestTcpInputSynSentState
3. Direct calls to tcpReass (bypass TCPInput)
   - Example: TestTcpReassInOrder, TestTcpReassFIN

**Failing tests** use the regular path:
- Packets with THFin skip header prediction (line 389: tiflags must == THAck)
- Go through ACK processing switch → step6 → data processing → FIN processing
- State transitions and RcvNxt advancement don't happen

## Observations

1. Socket lookup appears correct (TCPLastSo matches packet addresses)
2. tiLen calculation appears correct (IP total - iphlen - TCP header)
3. Code trace suggests we should reach FIN processing at line 877
4. But RcvNxt stays unchanged and TState doesn't transition

## Suspected Areas

1. Something in ACK processing (lines 642-799) causing early return
2. Data processing block (lines 837-875) not being entered
3. tiflags being cleared somewhere before FIN check
4. Socket/TCPCB pointer mismatch between test and TCPInput

## Test Case

```go
// This test fails - state stays ESTABLISHED, RcvNxt stays 1000
tp.TState = TCPSEstablished
tp.RcvNxt = 1000
// Send FIN|ACK packet at seq=1000
// Expected: TState=CLOSE_WAIT, RcvNxt=1001
// Actual: TState=ESTABLISHED, RcvNxt=1000
```

## Reference

See ticket tg-390f notes for full investigation details.

## Acceptance Criteria

- [ ] Identify root cause of test failures
- [ ] Fix the infrastructure issue OR document why tests can't work this way
- [ ] Tests for FIN processing state transitions pass
- [ ] Tests for RST processing pass
- [ ] Tests for ACK processing in SYN_RECEIVED pass

