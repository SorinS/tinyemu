---
id: tinyemu-go-2px
status: closed
deps: [tinyemu-go-tsg, tinyemu-go-5dj, tinyemu-go-cqd, tinyemu-go-je8, tg-bcad, tg-3fd7, tg-0b7b, tg-8c81, tg-2e21]
links: []
created: 2026-01-15T15:52:21.222088098-05:00
type: task
priority: 2
---
# Linux Boot Integration Tests

**Do not attempt until compliance tests pass.**

Prerequisites:
- rv64ui, rv64um, rv64ua compliance tests pass (required)
- rv64uf, rv64ud, rv64uc compliance tests pass (strongly recommended)

Test process:
1. Boot Linux kernel with BusyBox rootfs
2. Verify shell prompt appears within 30 seconds
3. Run basic commands (echo, uname -m, cat /proc/cpuinfo)
4. Verify correct output

Uses go test with -short skip for normal CI runs.


