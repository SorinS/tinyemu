---
id: tg-f8ca
status: closed
deps: []
links: []
created: 2026-01-19T18:49:36Z
type: task
priority: 1
assignee: JT Olio
---
# Verify cross-compilation for Windows and macOS

Confirm that the temu binary cross-compiles successfully for Windows and macOS targets.

Requirements:
- Test GOOS=windows GOARCH=amd64 go build ./cmd/temu
- Test GOOS=darwin GOARCH=amd64 go build ./cmd/temu
- Test GOOS=darwin GOARCH=arm64 go build ./cmd/temu

Note: Disabling HostFS support in these compilation contexts is acceptable since it relies on Unix-specific syscalls. Use build tags if needed to stub out or disable HostFS on non-Linux platforms. If it's possible to support HostFS though it is preferred.

Acceptance criteria:
- All three cross-compilation targets build without errors
- Document any build tags or conditional compilation added

