---
id: tg-ea33
status: closed
deps: []
links: []
created: 2026-01-22T02:27:47Z
type: task
priority: 1
assignee: JT Olio
tags: [slirp, mbuf, c-divergence]
---
# Verify dtom functionality is fully covered by explicit M fields

C has a dtom() function that finds the mbuf containing a given pointer by walking the used list. Go doesn't implement dtom(), instead using explicit M *Mbuf fields in structures like IPQ and IPASFrag.

Need to verify:
1. All C code paths that use dtom() have equivalent Go handling via explicit M fields
2. No code path exists where we need to find an mbuf from a pointer but don't have an explicit reference

If any gaps are found, either add the missing M field or implement dtom().

Reference: tinyemu-2019-12-21/slirp/mbuf.c:196-218

