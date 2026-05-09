---
id: tg-a94a
status: open
deps: [tg-c9bc]
links: [tg-2846, tg-6b0d, tg-c359]
created: 2026-01-22T16:35:17Z
type: task
priority: 3
assignee: JT Olio
---
# Add TCP boot test using external connectivity (NAT)

Add a TCP test to boot_test.go that verifies guest-initiated TCP through Slirp's NAT to external addresses.

When the guest connects to an address outside the virtual network, Slirp creates a real outgoing TCP connection through the host's network stack.

Test approach:
1. Have the guest connect to a well-known external service (e.g., 8.8.8.8:53 or a public TCP echo server)
2. Verify the TCP handshake completes (SYN/SYN-ACK/ACK)
3. Optionally verify data transfer

Note: This test requires actual network connectivity from the test machine.
May need to be skipped in CI environments without network access.

Reference: slirp/tcp_input.go for outgoing connection handling

