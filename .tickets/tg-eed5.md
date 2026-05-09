---
id: tg-eed5
status: closed
deps: [tg-99bb, tg-b1e4]
links: [tg-4666, tg-da69, tg-99bb, tg-b1e4, tg-c359]
created: 2026-01-23T03:04:45Z
type: task
priority: 2
assignee: JT Olio
---
# TCP testing: Host server with AddHostfwd (guest-to-host)

Implement boot test where guest connects to host TCP server via AddHostfwd.

## Context
See docs/1769135871-tcp-integration-test-plan.md for full analysis.
See docs/COMMIT_EXPECTATIONS.md for commit standards.

This is the recommended first approach because it uses real Go networking
on the host side, avoiding the ExPty=3 exec forwarding complexity.

## Implementation
1. Start real TCP server on host (net.Listen)
2. Use AddHostfwd to forward: guest connects to 10.0.2.2:PORT -> host server
3. Boot guest, configure network
4. Guest: echo 'HELLO_FROM_GUEST' | nc 10.0.2.2 PORT
5. Host: accept connection, read data, send response
6. Verify guest's nc output contains host response

## Test Flow
Guest (nc client) --> Slirp AddHostfwd --> Host TCP Server (Go net.Listener)

## Acceptance Criteria
- Guest sends data, host receives it
- Host sends response, guest receives it (visible in console output)
- Test completes without timeout

Reference: slirp/slirp.go AddHostfwd, machine/boot_test.go

