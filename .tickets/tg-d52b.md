---
id: tg-d52b
status: open
deps: [tg-c9bc]
links: []
created: 2026-01-22T04:42:22Z
type: task
priority: 3
assignee: JT Olio
---
# Implement UDP port forwarding (udpexec_list)

The C SLIRP implementation has udpexec_list for UDP port forwarding that can spawn processes. This is currently not implemented in Go - see XXXXX comment in udp.go:179.

Reference: tinyemu-2019-12-21/slirp/udp.c lines 162-166 (check udpexec_list)
Reference: tinyemu-2019-12-21/slirp/misc.c fork_exec()

This is related to the exec_list feature that allows SLIRP to spawn processes when certain UDP ports receive traffic.

