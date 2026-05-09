---
id: tinyemu-go-777
status: closed
deps: []
links: []
created: 2026-01-16T21:54:19.391963436-05:00
type: bug
priority: 1
---
# RVC compliance test fails at test case 30

The rv64uc-p-rvc compliance test fails at test case 30.

## Symptoms
```
go test -v -run "TestRISCVCompliance/rv64uc/rvc" ./cpu/
    compliance_test.go:160: test case 30 failed (tohost=0x3d)
```

tohost = 0x3d = 61 = (30 << 1) | 1, confirming test case 30 failed.

## Investigation needed
1. Disassemble the test binary to find test case 30
2. Identify which compressed instruction is being tested
3. Compare expansion logic against RISC-V spec

## Previously fixed
Commit cda1715 fixed a compressed JAL/JALR link address bug (PC+2 not PC+4).
This may be a related issue with another compressed instruction.

## Files to check
- cpu/compressed.go (C extension instruction expansion)
- cpu/exec.go (PC increment for compressed instructions)
- tinyemu-2019-12-21/riscv_cpu_template.h (reference implementation)


