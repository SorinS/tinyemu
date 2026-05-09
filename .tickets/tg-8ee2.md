---
id: tg-8ee2
status: closed
deps: []
links: []
created: 2026-01-19T18:49:37Z
type: feature
priority: 1
assignee: JT Olio
---
# Add CLI argument parsing to temu binary

Improve the temu CLI to accept command-line arguments in addition to (or instead of) a JSON config file.

Current state: temu requires a JSON configuration file to run.

Proposed improvements:
- Add optional command-line flags that can override JSON config values
- Consider making the JSON file optional if enough arguments are provided
- Common flags to support:
  - -m, --memory: RAM size (e.g., -m 128M)
  - -kernel: Path to kernel image
  - -bios: Path to BIOS/bootloader
  - -drive: Block device image (can be repeated)
  - -9p: 9P share directory with mount tag (e.g., -9p /path:tagname)
  - -append: Kernel command line arguments
  - -smp: Number of CPUs (if/when SMP is supported)

Example usage:
  temu -m 128M -bios bbl64.bin -kernel vmlinux -drive root.img -append 'console=hvc0 root=/dev/vda'

Acceptance criteria:
- temu can be invoked with command-line arguments
- JSON config file becomes optional when sufficient arguments are provided
- --help shows available options
- Backwards compatible with existing JSON config workflow

