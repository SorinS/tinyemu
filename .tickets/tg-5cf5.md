---
id: tg-5cf5
status: closed
deps: [tg-838f]
links: []
created: 2026-01-22T02:28:14Z
type: bug
priority: 1
assignee: JT Olio
tags: [slirp, mbuf, c-divergence]
---
# m_inc does not preserve data offset within buffer

C m_inc preserves the offset of m_data within the buffer when reallocating:
  datasize = m->m_data - m->m_dat;  // save offset
  dat = (char *)malloc(size);
  memcpy(dat, m->m_dat, m->m_size);  // copy ENTIRE buffer
  m->m_data = m->m_ext + datasize;   // restore offset

Go m_inc copies only valid data to the START of new buffer:
  newData := make([]byte, size)
  copy(newData, m.Data[:m.Len])  // copies only Len bytes to position 0

This loses any gap/offset at the beginning. If code depends on the gap being preserved (e.g., for prepending headers later), this will fail.

This is related to the Mbuf struct refactoring needed for m_adj - once Dat/Data/Offset fields exist, m_inc must preserve Offset.

Reference: tinyemu-2019-12-21/slirp/mbuf.c:129-154

