---
id: tg-bcad
status: closed
deps: []
links: []
created: 2026-01-17T22:20:14Z
type: task
priority: 1
assignee: JT Olio
---
# Diagnostic Boot Test - Capture Failing Instruction

Create a test that boots Linux far enough to capture the exact instruction causing 'illegal instruction in init'.

Requirements:
- Log PC, instruction encoding, and privilege mode at each illegal instruction trap
- Run C TinyEMU with equivalent debug output for comparison
- Identify the specific instruction that fails in Go but works in C

This is diagnostic work to match C behavior, not to add new functionality.

Reference: The boot currently reaches 'Freeing unused kernel memory' then hits 'Oops - illegal instruction' when running init.

