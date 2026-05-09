---
id: tg-d48f
status: closed
deps: []
links: []
created: 2026-01-22T04:42:38Z
type: task
priority: 3
assignee: JT Olio
---
# Implement ICMP error fragmentation handling

In ip_icmp.go:461, when generating ICMP errors that would exceed MTU, the code currently just drops the packet:

  // Fragmentation needed but not implemented in this simplified version
  m.Free()
  return -1

The C implementation properly handles this case. Need to either implement fragmentation or properly handle the ICMP error generation within MTU limits.

Reference: tinyemu-2019-12-21/slirp/ip_icmp.c

