# tinyemu-go — usage guide

Multi-ISA system emulator (RISC-V 32/64, x86 32-bit, x86_64) written in Go.
The emulator runs as `bin/temu.<os>-<arch>.bin`; the shell scripts in the
project root drive it for common workflows.

---

## Booting a guest OS

### RISC-V 64

```sh
./run_riscv64.sh
```

Boots a RISC-V Linux from `bin/riscv64/root-riscv64.cfg`. The config file
declares kernel, BIOS, ramdisk, and any block devices — see
[bin/riscv64/](../bin/riscv64) for the layout. No variants.

### x86 (32-bit)

```sh
./run86_iso.sh tinycore
./run86_iso.sh alpine
./run86_iso.sh alpine nohw         # skip hwdrivers coldplug (~80s saved)
./run86_iso.sh alpine nomodloop    # skip modloop RSA verify (~110s saved)
./run86_iso.sh alpine fast         # nohw + nomodloop
./run86_iso.sh alpine superfast    # fast + skip syslog/bootmisc/firstboot
./run86_iso.sh alpine bare         # init=/bin/sh — raw busybox, no OpenRC
```

Boots Alpine Linux 3.19.0 (32-bit standard) or TinyCore on the cpu/x86
backend. The script invokes `scripts/extract_alpine.sh` to pull
`vmlinuz` + `initrd` (and patched variants) out of
`bin/iso/alpine-standard-3.19.0-x86.iso`, then runs the emulator with the
right kernel command line and ISO attached.

### x86_64 (long mode)

```sh
./run64_iso.sh tinycore
./run64_iso.sh alpine              # Alpine-standard 3.23.4 x86_64
./run64_iso.sh alpine fast         # patched-initrd, skip slow OpenRC
./run64_iso.sh alpine superfast
./run64_iso.sh alpine bare         # rdinit=/bin/sh shortcut
./run64_iso.sh alpine-debug        # Alpine-virt 3.19.1 with System.map
./run64_iso.sh alpine-debug bare   # bare shell on the alpine-virt kernel
```

`alpine` mirrors the 32-bit `run86_iso.sh alpine` path on x86_64.
`alpine-debug` uses Alpine's "virt" kernel (smaller) shipped with a full
`System.map-virt` symbol table — paired with `scripts/sym.sh` to resolve
RIP/CR2 addresses from fault traces during boot debugging.

---

## Networking in the guest

`-net-user` is on by default in the run scripts; slirp serves a
self-contained 10.0.2.0/24 with the host bridged via NAT. Inside the
guest:

```sh
ip link set eth0 up
ip addr add 10.0.2.15/24 dev eth0
ip route add default via 10.0.2.2
echo nameserver 10.0.2.3 > /etc/resolv.conf
```

That's enough for `ping 10.0.2.2`, DNS, and TCP to the outside.

### Prefer IPv4 (recommended)

Slirp's address translation is IPv4-only — the gateway has no IPv6
route. Modern `getaddrinfo` (used by `apk`, `wget`, `curl`) prefers
AAAA answers when they exist, so resolution succeeds but the connect
fails with "Network unreachable" or "Socket not connected".

Tell `getaddrinfo` to prefer IPv4 instead of papering over it in
slirp:

```sh
cat >> /etc/gai.conf <<EOF
precedence ::ffff:0:0/96  100
EOF
```

Once that's in place `apk update`, `apk add curl`, and similar all
work end-to-end against `http://dl-cdn.alpinelinux.org`. Single-shot
override per command: `wget -4 …`, `curl -4 …`.

### What works / doesn't

| operation                  | status                                                |
|----------------------------|-------------------------------------------------------|
| DNS (UDP/53 via 10.0.2.3)  | works (slirp forwards to host's resolver)             |
| TCP outbound               | works (HTTP, HTTPS, APKINDEX, package downloads)      |
| ICMP echo to 10.0.2.2      | works (slirp answers locally)                         |
| ICMP echo to outside hosts | **doesn't** — needs raw sockets, temu isn't root      |
| inbound from host          | not exposed (slirp is NAT-only, no port forwards yet) |

### Service-grade defaults

Some apk fetches stall briefly on bulk transfers (a few hundred ms)
under software MMU emulation — the guest's RX ring fills, kernel
emits window=0 ACK, slirp pauses until the window reopens. Visible
but not a bug; downloads still complete.

---

## Running individual assembly programs

```sh
./run_asm.sh x86_64 examples/hello.asm
./run_asm.sh x86    examples/loop.asm 50000   # explicit step budget
```

The script assembles a NASM source (flat binary), loads it at physical
`0x10000`, and runs the requested CPU backend until `HLT` or budget
exhaustion. Prints final register state on stderr.

Requires `nasm` — `brew install nasm` on macOS (Homebrew path is
auto-detected at `/opt/homebrew/bin/nasm`).

Example source (`examples/hello.asm`):

```nasm
bits 64
    mov rax, 42
    mov rbx, 100
    add rax, rbx
    hlt
```

Final state will show `RAX = 0x8e` (= 142).

---

## Running the test suites

```sh
go test ./cpu/x86/        # 57 test files, ~10K lines — i386 backend
go test ./cpu/x86_64/     # 24 test files, ~5K lines — long-mode backend
go test ./cpu/riscv...    # riscv backend
go test ./test/x86/       # integration: NASM-compiled programs through full Step
go test ./test/x86_64/    # ditto, for cpu/x86_64
go test ./...             # everything (slow; some 32-bit tests use big bignum suites)
```

Spec-sweep inventory of opcode coverage (verbose mode prints which
opcodes are wired and which are missing):

```sh
go test ./cpu/x86_64/ -run TestOpcodeSweep -v
```

---

## Direct `bin/temu` invocation

The shell scripts wrap `bin/temu.<os>-<arch>.bin` which has its own
flag interface. Use the binary directly when you need control beyond
what the scripts expose:

```sh
bin/temu.darwin-arm64.bin -h                # show help
bin/temu.darwin-arm64.bin                   # via riscv64.cfg by default
bin/temu.darwin-arm64.bin <config-file>     # JSON config (riscv style)
bin/temu.darwin-arm64.bin -machine x86_64 \
    -m 512 -kernel vmlinuz -initrd initrd \
    -drive disk.iso -ro -net-user \
    -append "console=ttyS0,115200 root=/dev/vda"
```

Common flags:

| flag           | meaning                                                 |
|----------------|---------------------------------------------------------|
| `-machine`     | `riscv64`, `riscv32`, `x86`, `x86_64`                   |
| `-m`           | RAM size in MB                                          |
| `-kernel`      | kernel image (vmlinuz, vmlinux, etc.)                   |
| `-initrd`      | initial ramdisk                                         |
| `-bios`        | BIOS/SBI/firmware image                                 |
| `-drive`       | block device file (ISO, qcow, raw). May repeat.         |
| `-ro` / `-rw`  | read-only / read-write for attached drives              |
| `-net-user`    | enable user-mode (slirp) networking                     |
| `-append`      | append text to the kernel command line                  |
| `-stdin-prefix`| bytes to inject into the guest console before stdin    |
| `-ctrlc`       | allow Ctrl-C to stop the emulator (otherwise passed through to guest) |
| `-debug`       | verbose debug output                                    |
| `-version`     | print version and exit                                  |

---

## Diagnostic environment variables (x86_64)

Set these before `./run64_iso.sh`. They print to stderr, are off by
default, and have zero cost when not set.

| var                          | purpose                                           |
|------------------------------|---------------------------------------------------|
| `TINYEMU_X64_INTR=1`         | log every IDT delivery, gate walk, IRQ ack        |
| `TINYEMU_X64_CR3=1`          | log every CR3 write with PML4 entries snapshot    |
| `TINYEMU_X64_MSR=1`          | log every RDMSR/WRMSR with RIP                    |
| `TINYEMU_X64_IO=1`           | log every IN/OUT port access                      |
| `TINYEMU_X64_PIC=1`          | log every 8259 deliverDebug                       |
| `TINYEMU_X64_PHYSWATCH=<lo>-<hi>` | log writes to a physical-address range       |
| `TINYEMU_X64_VAWATCH=<lo>-<hi>`   | log writes/reads to a virtual-address range  |
| `TINYEMU_X64_RIPTRACE=<lo>-<hi>`  | print every instruction's RIP+regs in range  |
| `TINYEMU_X64_RIPSAMPLE=<N>`  | sample one in N cycles                            |
| `TINYEMU_X64_RIPTRAP=1`      | dump last 32 RIPs on a user NX/page fault         |
| `TINYEMU_X64_STRWATCH=<hex>` | when RIP matches, dump the string at RDI          |

Combine for layered debugging — e.g. capture full RIP context for a
specific function with `TINYEMU_X64_RIPTRACE=7f7dad400000-7f7dad500000`.

Resolve any kernel RIP/CR2 to a symbol:

```sh
./scripts/sym.sh 0xffffffff81a012f0
```

Uses the System.map-virt in `bin/alpine64-debug/`.

---

## Profiling

```sh
./profile.sh                  # collects CPU profile via go tool pprof
go tool pprof -http=:8080 cpu.prof
```

Configure target/duration inside `profile.sh`.

---

## Where things live

```
bin/temu.<os>-<arch>.bin       built emulator binary
cpu/x86/                       i386 32-bit backend
cpu/x86_64/                    long-mode 64-bit backend
cpu/riscv32, cpu/riscv64       RISC-V backends
machine/pc/                    PC (x86/x86_64) chassis — PIC, PIT, RTC,
                               ata_piix, virtio-blk-pci, virtio-net
machine/                       generic machine plumbing
devices/                       device base abstractions
mem/                           physical/virtual memory maps
cmd/temu/                      main binary
test/x86/                      integration tests via assembled programs
test/x86_64/                   ditto, 64-bit
scripts/                       extract_* helpers and sym.sh
examples/                      sample .asm sources for run_asm.sh
docs/                          reference docs and PDFs (Intel SDM, AMD APM)
```

---

## Building

```sh
go build -o bin/temu.darwin-arm64.bin ./cmd/temu
# or on Linux:
go build -o bin/temu.linux-amd64.bin ./cmd/temu
```

No external dependencies beyond the Go module graph (see `go.mod`).
NASM is only needed for `run_asm.sh` and the assembly-based tests.
