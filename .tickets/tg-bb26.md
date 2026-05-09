---
id: tg-bb26
status: closed
deps: []
links: []
created: 2026-01-23T18:30:31Z
type: task
priority: 2
assignee: JT Olio
parent: tg-45e7
tags: [c-review, port-verification, cpu, fpu]
---
# Review glue: FPU instructions and exception handling

Review C code lines 1477-1734 of riscv_cpu_template.h glue function (FPU and exception handling) and ensure Go implementation matches exactly.

## C Code Section to Review

- FP load (opcode 0x07, lines 1479-1518):
  - flw (funct3=2)
  - fld (funct3=3, FLEN>=64)
  - flq (funct3=4, FLEN>=128)
- FP store (opcode 0x27, lines 1519-1546):
  - fsw, fsd, fsq
- FP fused multiply-add (lines 1547-1684):
  - fmadd (opcode 0x43)
  - fmsub (opcode 0x47)
  - fnmsub (opcode 0x4b)
  - fnmadd (opcode 0x4f)
- FP operations (opcode 0x53, lines 1685-1706):
  - Includes riscv_cpu_fp_template.h for F_SIZE 32/64/128
- Default illegal instruction (lines 1708-1710)
- Jump instruction label (line 1712-1713)
- Exception handling (lines 1714-1724):
  - illegal_insn label
  - mmu_exception label
  - exception label
  - raise_exception2 call
- Interpreter exit (lines 1725-1734):
  - done_interp, the_end labels
  - insn_counter update

## Instructions

1. Read the C code carefully for FPU and exception handling
2. Find the corresponding Go code in cpu/ package
3. Compare line-by-line for exact behavioral match
4. Add/update comments referencing C code: `// Reference: tinyemu-2019-12-21/riscv_cpu_template.h:1477-1734`
5. Fix ANY deviations including error handling differences
6. Write tests to confirm behavior matches C code

## Files

- `tinyemu-2019-12-21/riscv_cpu_template.h` (lines 1477-1734)
- `tinyemu-2019-12-21/riscv_cpu_fp_template.h` (included for FP ops)

## Acceptance Criteria

- [ ] All FPU instructions reviewed against C source
- [ ] Exception handling reviewed against C source
- [ ] Go implementation matches C behavior exactly
- [ ] Comments reference C code with file:line format
- [ ] Tests confirm C-matching behavior
- [ ] go test and go vet pass

