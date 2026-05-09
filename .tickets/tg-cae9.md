---
id: tg-cae9
status: closed
deps: []
links: []
created: 2026-01-23T18:32:39Z
type: task
priority: 2
assignee: JT Olio
parent: tg-dc2c
tags: [c-review, port-verification, virtio, 9p]
---
# Review virtio_9p_recv_request: read, write, clunk, error handling

Review C code lines 2543-2622 of virtio.c virtio_9p_recv_request function and ensure Go implementation matches exactly.

## C Code Section to Review

- case 116 read (lines 2543-2568):
  - unmarshall fid, offset, count
  - fid_find
  - allocate buf (count + 4)
  - fs_read into buf+4
  - put_le32 for count prefix
  - virtio_9p_send_reply
  - free buf
- case 118 write (lines 2569-2598):
  - unmarshall fid, offset, count
  - fid_find
  - allocate buf1
  - memcpy_from_queue for data
  - fs_write
  - free buf1
  - marshall write count
- case 120 clunk (lines 2599-2609):
  - unmarshall fid
  - fid_delete
  - empty reply
- default case (lines 2610-2613):
  - printf unsupported operation
  - goto protocol_error
- return 0 (line 2614)
- error label (lines 2615-2617):
  - virtio_9p_send_error
  - return 0
- protocol_error and fid_not_found labels (lines 2618-2621):
  - err = -P9_EPROTO
  - goto error
- Function end (line 2622)

## Instructions

1. Read the C code carefully for read, write, clunk, and error handling
2. Find the corresponding Go code in virtio/ package
3. Compare line-by-line for exact behavioral match
4. Add/update comments referencing C code: `// Reference: tinyemu-2019-12-21/virtio.c:2543-2622`
5. Fix ANY deviations including error handling differences
6. Write tests to confirm behavior matches C code

## Files

- `tinyemu-2019-12-21/virtio.c` (lines 2543-2622)

## Acceptance Criteria

- [ ] read, write, clunk, error handling reviewed against C source
- [ ] Go implementation matches C behavior exactly
- [ ] Comments reference C code with file:line format
- [ ] Tests confirm C-matching behavior
- [ ] go test and go vet pass


## Notes

**2026-01-24T18:56:18Z**

VERIFIED - Go implementation matches C behavior exactly

handleRead (Go:432-458) vs C case 116 read (C:2543-2568):
- Unmarshal fid, offset, count ✓
- fid_find/getFid check ✓
- fs_read/Read call ✓
- Error handling returns fs error ✓
- Response with 4-byte count prefix + data ✓

handleWrite (Go:460-486) vs C case 118 write (C:2569-2598):
- Unmarshal fid, offset, count ✓ (Go includes data in parse)
- fid_find/getFid check ✓
- fs_write/Write call ✓
- Error handling ✓
- Response with written count ✓

handleClunk (Go:488-506) vs C case 120 clunk (C:2599-2609):
- Unmarshal fid ✓
- fid_delete/deleteFid + Clunk ✓
- Empty response ✓

Error handling:
- sendError with p9.EPROTO for protocol errors matches C's goto protocol_error
- p9.ErrorCode(err) for FS errors matches C's err = n; goto error

All virtio tests pass.
