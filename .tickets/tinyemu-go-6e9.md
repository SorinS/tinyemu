---
id: tinyemu-go-6e9
status: closed
deps: []
links: []
created: 2026-01-16T21:12:46.486367704-05:00
type: bug
priority: 2
---
# SRET/MRET returns to PC=0 after exception

SRET/MRET exception return sets PC to 0.

**Note:** This may be caught by rv64si/rv64mi compliance tests. Defer investigation until after compliance test infrastructure is in place.

If rv64ui/rv64um/rv64ua tests pass but this still occurs, investigate trap handling in cpu/exec.go.


