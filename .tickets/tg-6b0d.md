---
id: tg-6b0d
status: closed
deps: [tg-9443]
links: [tg-2846, tg-c359, tg-a94a]
created: 2026-01-23T02:25:09Z
type: task
priority: 2
assignee: JT Olio
---
# Add TCP data exchange boot test with host server

Create a boot test that verifies TCP data can be sent and received between guest and host.

Test approach:
1. Start a real TCP server on the host (listening on localhost)
2. Use slirp.AddHostfwd() to forward a guest port to the host server
3. Boot Linux and use netcat to connect to the forwarded port
4. Verify bidirectional data exchange:
   - Guest sends data to host server, host receives it
   - Host sends response, guest receives it

Can be implemented as two separate tests if easier:
- TestLinuxBootTCPSend: guest sends, host receives
- TestLinuxBootTCPRecv: host sends, guest receives

This tests the real TCP data path through slirp, not just socket creation.

Reference: slirp/slirp.go AddHostfwd for port forwarding setup
Reference: existing TestLinuxBootNetwork for boot test patterns


## Notes

**2026-01-24T20:57:45Z**

Completed by TestLinuxBootTCPHostServer in machine/boot_test.go. The test verifies bidirectional TCP data exchange: guest sends HELLO_FROM_GUEST, host server receives it and sends RESPONSE_FROM_HOST back, guest receives the response. Test passes with the TCP FIN checksum fix in tg-9443.
