---
id: tg-35bb
status: closed
deps: [tg-e2a9]
links: []
created: 2026-01-25T03:31:04Z
type: task
priority: 1
assignee: JT Olio
parent: tg-6d6d
---
# Write buildroot build script

Create images/buildroot/build.sh that:
1. Downloads buildroot tarball if not present
2. Runs make with BR2_EXTERNAL pointing to our configs
3. Copies output images to images/output/
4. Reports final sizes

Should be idempotent - can be re-run to rebuild.

Reference: docs/image-build-plan-1769311563.md

## Acceptance Criteria

- Script downloads buildroot automatically
- Build completes successfully
- Outputs copied to images/output/

