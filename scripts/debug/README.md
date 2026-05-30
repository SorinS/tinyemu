# debug scripts

Thin wrappers that set one `TINYEMU_*` debug env var, boot a target via
`run64_iso.sh`, capture stdout+stderr to a known log path, and print a
compact summary (wall time, last kernel timestamp, tail).

Why: avoids retyping `timeout N ./run64_iso.sh <target> > /tmp/X.log 2>&1`
plus the env-var prefix, and gives the harness a stable filename to read
back from.

## Usage

```sh
./scripts/debug/<name>.sh [target] [budget_seconds]
```

Defaults: `target=alpine`, `budget=480`.

Each script writes to `/tmp/debug_<name>.log` so its output is
predictable across invocations. The summary prints what was set, where
the log lives, the wall time used, and the last 25 lines of output.

## Available

| script             | env var                       | when to use                                      |
| ------------------ | ----------------------------- | ------------------------------------------------ |
| `plain.sh`         | (none)                        | baseline — no debug env, just capture + summary  |
| `virtio_pci.sh`    | `TINYEMU_VIRTIO_PCI_DEBUG=1`  | virtio R/W + queue notifies                      |
| `io.sh`            | `TINYEMU_X86_IO_DEBUG=1`      | every IN/OUT port access                         |
| `intr.sh`          | `TINYEMU_X64_INTR=1`          | LIDT + every delivered interrupt                 |
| `cr3.sh`           | `TINYEMU_X64_CR3=1`           | every CR3 write + PML4 sentinel entries          |
| `ripsample.sh`     | `TINYEMU_X64_RIPSAMPLE=10000` | sampled RIPs (pair with `scripts/sym.sh`)        |
| `bios.sh`          | `TINYEMU_BIOS_DEBUG=1`        | SeaBIOS debug-port writes (BIOS-boot only)       |
| `userpf.sh`        | `TINYEMU_X86_USERPF=1`        | dump regs on user-mode PF to addr < 0x1000       |

## Shared runner

`scripts/debug/_runner.sh` does the bookkeeping (timeout, capture, tail,
panic grep). Each wrapper is a 3-line shim that sets its env var(s) and
then sources the runner. To add a new debug knob, copy any existing
wrapper, change `DEBUG_NAME` and the `export TINYEMU_…`, and update this
table.
