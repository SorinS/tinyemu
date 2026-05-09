---
id: tg-efe3
status: closed
deps: []
links: []
created: 2026-01-22T04:42:31Z
type: task
priority: 3
assignee: JT Olio
---
# Check IP fragment queue in updateTimerFlags

The C slirp_select_fill checks the IP fragment queue to determine if slow timeouts are needed:

  do_slowtimo |= (&slirp->ipq.ip_link \!= slirp->ipq.ip_link.next);

The Go implementation in device.go:updateTimerFlags has a comment noting this is not implemented:
  // Note: We don't check IP fragment queue since fragmentation isn't implemented

This should be implemented to match C semantics.

Reference: tinyemu-2019-12-21/slirp/slirp.c:261-262

