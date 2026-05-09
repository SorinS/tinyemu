---
id: tg-fe9c
status: closed
deps: [tg-e2a9]
links: []
created: 2026-01-25T03:28:43Z
type: task
priority: 1
assignee: JT Olio
parent: tg-6d6d
---
# Write buildroot defconfig for TinyEMU RISC-V

Create tinyemu_riscv64_defconfig with:
- RISC-V 64-bit target (LP64D ABI, RVC extension)
- musl toolchain for smaller binaries
- Busybox init system
- ext4 rootfs (8MB max)
- Packages: curl, openssl, ca-certificates
- Size optimizations enabled

Reference: docs/image-build-plan-1769311563.md

## Acceptance Criteria

- Defconfig builds successfully with buildroot
- Resulting image contains curl with TLS support

