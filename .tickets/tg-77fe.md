---
id: tg-77fe
status: closed
deps: []
links: []
created: 2026-01-23T18:30:30Z
type: task
priority: 2
assignee: JT Olio
parent: tg-45e7
tags: [c-review, port-verification, cpu]
---
# Review glue: setup, interrupt handling, instruction fetch

Review C code lines 200-305 of riscv_cpu_template.h glue function and ensure Go implementation matches exactly.

## C Code Section to Review

- Function entry, variable declarations (lines 200-215)
- Early return for n_cycles1 == 0 (lines 216-217)
- Instruction counter setup (lines 218-219)
- Initial interrupt check and raise_interrupt (lines 221-227)
- pending_exception initialization, code_ptr/code_end setup (lines 229-234)
- Main loop entry and TLB handling (lines 237-288)
- Cycle decrement and opcode extraction (lines 289-305)

## Instructions

1. Read the C code carefully for the section listed above
2. Find the corresponding Go code in cpu/ package
3. Compare line-by-line for exact behavioral match
4. Add/update comments referencing C code: `// Reference: tinyemu-2019-12-21/riscv_cpu_template.h:200-305`
5. Fix ANY deviations including error handling differences
6. Write tests to confirm behavior matches C code

## Files

- `tinyemu-2019-12-21/riscv_cpu_template.h` (lines 200-305)

## Acceptance Criteria

- [ ] Section reviewed against C source
- [ ] Go implementation matches C behavior exactly
- [ ] Comments reference C code with file:line format
- [ ] Tests confirm C-matching behavior
- [ ] go test and go vet pass

