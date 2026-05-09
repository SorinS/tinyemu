---
id: tg-4ef8
status: closed
deps: []
links: []
created: 2026-01-23T18:30:31Z
type: task
priority: 2
assignee: JT Olio
parent: tg-45e7
tags: [c-review, port-verification, cpu, base-isa]
---
# Review glue: integer register/immediate operations (OP-IMM/OP)

Review C code lines 943-1224 of riscv_cpu_template.h glue function (integer operations) and ensure Go implementation matches exactly.

## C Code Section to Review

- OP-IMM (opcode 0x13, lines 943-982):
  - addi, slli, slti, sltiu, xori, srli/srai, ori, andi
- OP-IMM-32 (opcode 0x1b, XLEN>=64, lines 983-1011):
  - addiw, slliw, srliw/sraiw
- OP-IMM-64 (opcode 0x5b, XLEN>=128, lines 1012-1040):
  - addid, sllid, srlid/sraid
- OP (opcode 0x33, lines 1041-1116):
  - M extension (imm=1): mul, mulh, mulhsu, mulhu, div, divu, rem, remu
  - Base (imm=0/0x20): add, sub, sll, slt, sltu, xor, srl, sra, or, and
- OP-32 (opcode 0x3b, XLEN>=64, lines 1117-1170):
  - M extension: mulw, divw, divuw, remw, remuw
  - Base: addw, subw, sllw, srlw, sraw
- OP-64 (opcode 0x7b, XLEN>=128, lines 1171-1224):
  - M extension: muld, divd, divud, remd, remud
  - Base: addd, subd, slld, srld, srad

## Instructions

1. Read the C code carefully for all integer operations
2. Find the corresponding Go code in cpu/ package
3. Compare line-by-line for exact behavioral match (especially sign extension!)
4. Add/update comments referencing C code: `// Reference: tinyemu-2019-12-21/riscv_cpu_template.h:943-1224`
5. Fix ANY deviations including error handling differences
6. Write tests to confirm behavior matches C code

## Files

- `tinyemu-2019-12-21/riscv_cpu_template.h` (lines 943-1224)

## Acceptance Criteria

- [ ] All integer operations reviewed against C source
- [ ] Go implementation matches C behavior exactly
- [ ] Comments reference C code with file:line format
- [ ] Tests confirm C-matching behavior
- [ ] go test and go vet pass

