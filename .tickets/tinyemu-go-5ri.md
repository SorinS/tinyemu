---
id: tinyemu-go-5ri
status: closed
deps: []
links: []
created: 2026-01-16T19:35:17.035413323-05:00
type: task
priority: 1
---
# Improve CPU package test coverage to 80%

Current coverage: 60.2%. This is insufficient for the most critical component.

## Assessment
The boot-blocking bug (store access fault) should ideally have been caught by tests. Low coverage in the CPU package risks missing other behavioral differences.

## Target
Improve cpu/ package test coverage from 60.2% to 80%+

## Priority Areas
- Exception handling paths
- Privilege mode transitions (MRET/SRET)
- Interrupt delivery
- Memory access fault conditions

Reference: PROJECT_STATUS_REPORT.md Recommendations


