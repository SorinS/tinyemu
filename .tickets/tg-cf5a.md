---
id: tg-cf5a
status: closed
deps: []
links: []
created: 2026-01-17T16:45:26Z
type: task
priority: 0
assignee: JT Olio
---
# Add comprehensive unit tests for VirtIO block device I/O operations.

## Background
Linux boot shows 0 block reads/writes despite VirtIO block device reaching DRIVER_OK status. We need to test the block I/O path in isolation.

## Tests Needed
1. Block read request submission and completion
2. Block write request submission and completion  
3. Multi-sector read/write operations
4. Error handling for out-of-bounds access
5. Queue notification -> callback flow for block requests

## Acceptance Criteria
- virtio/ package coverage increases toward 75% target
- All block I/O paths are exercised by unit tests
- Tests verify ReadSectors/WriteSectors callbacks are invoked

## References
- virtio/block.go
- virtio/mmio.go (queue handling)
- C reference: tinyemu-2019-12-21/virtio.c

