---
id: tg-6cb4
status: closed
deps: [tg-7831, tg-82b7]
links: []
created: 2026-01-20T20:01:18Z
type: task
priority: 1
assignee: JT Olio
---
# Implement LISTEN state with ICMP error support

Please see docs/1768939013-tcp-input-full-implementation-plan.md for context.

Fix LISTEN state handling to match C exactly, including ICMP error messages on connection failure.

C Reference: tcp_input.c:538-642

Key code paths:
1. RST handling (line 540-541)
2. ACK handling - dropwithreset (line 542-543)
3. SYN check (line 544-545)
4. SS_CTL check and cont_input jump (lines 556-571)
5. EMU_NOCONNECT check and cont_input jump (lines 576-578)
6. tcp_fconnect() error handling (lines 581-613):
   - ECONNREFUSED: tcp_respond() with RST|ACK
   - EHOSTUNREACH: icmp_error() with ICMP_UNREACH_HOST
   - Other errors: icmp_error() with ICMP_UNREACH_NET
   - Before ICMP: restore TCP header to network byte order, restore save_ip
7. cont_conn label (lines 616-623) - connection continuation
8. cont_input label (lines 624-641) - template setup and state transition
9. trimthenstep6 handling

Depends on: Header preservation (tg-7831), tcpDropWithReset (tg-82b7)

## Acceptance Criteria

- ICMP errors sent for host/network unreachable
- RST sent for connection refused
- cont_conn and cont_input paths work correctly
- Header restored before ICMP/RST
- Matches C behavior at tcp_input.c:538-642

**Every commit must:**

- [ ] Pass `go test ./...`
- [ ] Pass `go vet ./...`
- [ ] Have `gofmt -s` and `goimports -local github.com/sorins/tinyemu-go` run.
- [ ] Maintain/improve test coverage (`go test -cover ./...`)
- [ ] Reference corresponding C code in comments (for each logic step within each function)
- [ ] Include regression tests for any bug fixes

