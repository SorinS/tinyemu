---
id: tg-d9c0
status: closed
deps: []
links: []
created: 2026-01-20T19:59:06Z
type: task
priority: 1
assignee: JT Olio
---
# Implement TCP sequence comparison functions

Please see docs/1768939013-tcp-input-full-implementation-plan.md for context.

Verify and implement all TCP sequence number comparison functions (seqLT, seqLEQ, seqGT, seqGEQ) with proper 32-bit wraparound handling. These are foundational for tcp_input.

## Acceptance Criteria

- seqLT, seqLEQ, seqGT, seqGEQ functions exist and work correctly
- Functions handle 32-bit sequence wraparound
- Unit tests verify wraparound edge cases
- Reference: tcp_input.c uses SEQ_LT, SEQ_LEQ, SEQ_GT, SEQ_GEQ macros

**Every commit must:**

- [ ] Pass `go test ./...`
- [ ] Pass `go vet ./...`
- [ ] Have `gofmt -s` and `goimports -local github.com/sorins/tinyemu-go` run.
- [ ] Maintain/improve test coverage (`go test -cover ./...`)
- [ ] Reference corresponding C code in comments (for each logic step within each function)
- [ ] Include regression tests for any bug fixes

