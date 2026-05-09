---
id: tg-fbf5
status: closed
deps: [tg-838f]
links: []
created: 2026-01-22T02:27:14Z
type: bug
priority: 1
assignee: JT Olio
tags: [slirp, mbuf, c-divergence]
---
# Mbuf m_adj copies data instead of adjusting pointer

The Go m_adj implementation copies data when trimming from head, while C just adjusts the m_data pointer. This causes behavioral differences:

1. In C, trimmed data remains in the buffer and m_data can be moved backwards to re-expose it (used in ip_reass to restore IP headers after reassembly)
2. In Go, trimmed data is overwritten, so moving backwards is impossible

The fix requires refactoring Mbuf to have separate Dat (backing buffer) and Data (current view) slices, plus an Offset field to track position.

Reference: tinyemu-2019-12-21/slirp/mbuf.c:158-172

