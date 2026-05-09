---
id: tinyemu-go-0s3
status: closed
deps: [tinyemu-go-sfl]
links: []
created: 2026-01-15T16:28:51.558731059-05:00
type: task
priority: 2
---
# Create Test Harness for ISA Compliance

Implement test runner that loads ELF, runs to ECALL/halt, extracts signature from begin_signature/end_signature symbols, compares against golden. Integrate with go test.


