---
id: tinyemu-go-x4y
status: closed
deps: [tinyemu-go-pzw]
links: []
created: 2026-01-16T20:15:30.547199875-05:00
type: bug
priority: 2
---
# No console output during Linux boot

Linux boot produces no console output.

**Note:** This is a Linux boot debugging issue. Per docs/TESTING_STRATEGY.md, we should not debug Linux boot until compliance tests pass.

Defer this until:
1. rv64ui, rv64um, rv64ua compliance tests pass
2. rv64uc compliance tests pass (compressed instruction issues fixed)
3. Basic timer and interrupt tests pass

Root cause may be instruction-level bugs that compliance tests will reveal.


