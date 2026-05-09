---
id: tinyemu-go-b1w
status: closed
deps: [tinyemu-go-9tv]
links: []
created: 2026-01-15T16:28:41.250177788-05:00
type: task
priority: 0
---
# Build and Embed riscv-tests Binaries

**PRIORITY: This is the first step for systematic testing.**

Build riscv-tests with RISC-V GNU toolchain. Generate golden reference outputs using Spike or QEMU. Commit prebuilt ELF binaries and reference files for:
- rv64ui-p-* (base integer - MUST pass before Linux boot)  
- rv64um-p-* (multiply/divide - MUST pass before Linux boot)
- rv64ua-p-* (atomics - MUST pass before Linux boot)
- rv64uf-p-*, rv64ud-p-* (floating point)
- rv64uc-p-* (compressed)

Place in testdata/riscv-tests/isa/


