---
id: tg-69bd
status: open
deps: [tg-35bb, tg-fe9c, tg-348a, tg-595f, tg-bba1, tg-754a]
links: []
created: 2026-01-25T03:31:34Z
type: task
priority: 1
assignee: JT Olio
parent: tg-6d6d
---
# Test buildroot image boot in TinyEMU

Verify the buildroot image boots correctly:
1. Create testdata/boot/minimal.cfg pointing to new images
2. Boot with: go run ./cmd/temu testdata/boot/minimal.cfg
3. Verify kernel boots to shell prompt
4. Verify no kernel panics or errors

Reference: docs/image-build-plan-1769311563.md

## Acceptance Criteria

- Image boots to shell prompt
- No kernel panics
- Console is interactive

