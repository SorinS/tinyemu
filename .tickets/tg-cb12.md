---
id: tg-cb12
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
# Review glue: base integer instructions (lui/auipc/jal/jalr/branch/load/store)

Review C code lines 778-942 of riscv_cpu_template.h glue function (base integer instructions) and ensure Go implementation matches exactly.

## C Code Section to Review

- lui (opcode 0x37, lines 778-781)
- auipc (opcode 0x17, lines 782-785)
- jal (opcode 0x6f, lines 786-795)
- jalr (opcode 0x67, lines 796-802)
- Branch instructions (opcode 0x63, lines 803-828):
  - beq/bne (funct3>>1=0)
  - blt/bge (funct3>>1=2)
  - bltu/bgeu (funct3>>1=3)
- Load instructions (opcode 0x03, lines 829-907):
  - lb, lh, lw, lbu, lhu (all XLEN)
  - ld, lwu (XLEN>=64)
  - ldu (XLEN>=128)
- Store instructions (opcode 0x23, lines 908-942):
  - sb, sh, sw (all XLEN)
  - sd (XLEN>=64)
  - sq (XLEN>=128)

## Instructions

1. Read the C code carefully for base integer instructions
2. Find the corresponding Go code in cpu/ package
3. Compare line-by-line for exact behavioral match
4. Add/update comments referencing C code: `// Reference: tinyemu-2019-12-21/riscv_cpu_template.h:778-942`
5. Fix ANY deviations including error handling differences
6. Write tests to confirm behavior matches C code

## Files

- `tinyemu-2019-12-21/riscv_cpu_template.h` (lines 778-942)

## Acceptance Criteria

- [ ] All base integer instructions reviewed against C source
- [ ] Go implementation matches C behavior exactly
- [ ] Comments reference C code with file:line format
- [ ] Tests confirm C-matching behavior
- [ ] go test and go vet pass

