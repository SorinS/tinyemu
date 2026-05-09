---
id: tg-dd67
status: closed
deps: []
links: []
created: 2026-01-19T17:33:31Z
type: bug
priority: 1
assignee: JT Olio
---
# 9P Getattr Integration Bug - Invalid argument on stat

When mounting a 9P filesystem in Linux guest and trying to ls or stat files, the operation fails with 'Invalid argument'. The mount succeeds but subsequent file operations fail.

Symptoms:
- mount -t 9p -o trans=virtio,version=9p2000.L share /mnt/share succeeds
- ls -la /mnt/share/ shows 'total 0' and 'Invalid argument' for each file
- cat /mnt/share/file.txt fails with 'Invalid argument'

Unit tests for 9P protocol handlers all pass, suggesting the issue is in:
1. VirtIO memory transfer for larger responses
2. FID management across multiple operations
3. Response message format edge case

TestLinuxBoot9PMount in machine/boot_test.go demonstrates the issue.

