# Debugging tinyemu-go

How to diagnose problems in the emulator and its guests. Every technique
here was used to drive a real boot — names of commits and bug sites are
preserved so you can re-derive the trail from `git log`.

The pattern most of these techniques follow:

1. **Establish ground truth.** Where in the guest is the failure? Which
   mode, which CS:RIP, which register state? Get a one-line statement
   you can defend.
2. **Bisect into one instruction.** Coarse RIP sampling → fine
   sampling → cycle-window step trace.
3. **Read the bytes.** Disassemble the guest at the failing site. The
   bytes are the executable spec.
4. **Watch the source.** If a value at memory is wrong, watch every
   write to that address and see who set it.
5. **Pin it.** Add a unit test so the bug can't come back silently.

---

## 1. RIP sampling — "where is the CPU spending time?"

The first question for any hang or runaway: where is RIP? `TINYEMU_X64_RIPSAMPLE=N` prints one line every N executed instructions:

```sh
TINYEMU_X64_RIPSAMPLE=100000 timeout 8 ./bin/temu.darwin-arm64.bin \
    -machine x86_64 -bios bin/seabios/bios.bin -m 128 2>/tmp/sample.log
```

Each line looks like:

```
[sample] cycle=2284328 mode=real16 CS=0x200:0x2000 RIP=0x4960 lin=0x6960
```

What you do with it:

- **Hang in a loop?** Tail the samples — if RIP is the same for many
  consecutive lines, the guest is stuck.
- **Runaway?** Look for RIP advancing linearly across unmapped memory
  (linear address > installed RAM, or inside an "all-zero" data region).
- **Mode transitions?** Group by `mode=` and find where the CPU switches
  modes — that's where most boot bugs live.

Tighten the stride (`RIPSAMPLE=1` for single-instruction resolution) once
you've narrowed the failure window to a few thousand cycles.

### The 8-byte preview enhancement

The sampler can also dump the next 8 instruction bytes at each sample point
— see how it's wired in `cpu/x86_64/exec.go`. If you uncomment that
block you can `grep` the trace for opcodes (`0xcf` IRET, `0xea` JMP FAR,
…) and trace control flow without disassembling.

## 2. Cycle-window step trace — "show me everything in this window"

When you know the failure is between cycle X and Y, get every instruction
with full register state:

```sh
TINYEMU_X64_TRACE_CYCLES=20272304-20276600 ./bin/temu.darwin-arm64.bin \
    -machine x86_64 -bios bin/seabios/bios.bin -m 128 \
    -drive bin/menuet64/M6416000.IMG 2>/tmp/step.log
```

Each line:

```
[step] cyc=20272620 mode=pm32 RIP=0xef173 lin=0xef173 bytes=8a 45 00 3c 71 ...
       RAX=0xea1c0 RBX=0xea1c0 RCX=0x10 RDX=0xea1c0 RBP=0xf990 RSP=0xea08c ...
```

The bytes column is the executable spec for the next instruction. Decode it
by hand from Intel SDM Vol 2 — or paste it into `ndisasm -b32 -` (or
`-b16`). Register state shows the data the instruction operates on.

This is how the SeaBIOS disk read was diagnosed: a window trace around
INT 13h showed `mov ebp,[eax+4]` reading `op->drive_fl` as 0, which
became the symptom that pointed to the GDT-walk bug in `opMOVtoSreg`.

## 3. RIP-range trace — "every time we enter this function"

When you don't know *when* but you do know *where*:

```sh
TINYEMU_X64_RIPTRACE=d2e9-d340 ./bin/temu.darwin-arm64.bin ...
```

Logs every instruction whose RIP falls in `[lo, hi)`. Useful for
"every time we land in `irqentry_extrastack`, dump the registers".

## 4. Watchpoint — "who wrote this value?"

When the symptom is "memory says X but I never wrote X":

```sh
TINYEMU_X64_PHYSWATCH=0x6dfe-0x6e02 ./bin/temu.darwin-arm64.bin ...
```

Every byte-write into the range logs the writer's RIP and the
post-write qword. You can rebuild the value's history byte by byte.

This pinned the SeaBIOS push-width bug: the watchpoint showed
`bregs.code` being written with `0xF000_D05F` (the right handler), but
the stack at the IRET frame had `0x0200_D05F` — half the value lost.
The two-byte misalignment turned out to be `pushl` doing a `pushw`
because `Group 5 /6` was using `stackSlotSize` instead of `operandSize`.

### Sanity check before believing the watch

Watchpoints only see *guest* writes via `writeMem8`. Memory populated at
boot via `mm.PhysMem` (BIOS ROM, initrd staging) is invisible. If a
value seems to appear out of nowhere, check whether it was staged at
load time vs written by the CPU.

## 5. One-shot register/memory dump

Stop time at a specific cycle and dump everything you care about:

```sh
TINYEMU_X64_DUMPCYCLE=20272618 ./bin/temu.darwin-arm64.bin ...
```

Outputs at cycle N:

```
[dump] cycle=20272618 EAX=0xea1c0 ESP=0xea08c DS=0x10:0xda800 SS=0x10:0xda800
       bregs@0x1c49c0={00 00 ... 40 bytes ...}
[dump] stack@0x1c488c={ ... 16 bytes ... }
```

It also dereferences `[DS:EAX]` (40 bytes) and `[SS:ESP]` (16 bytes).
The DS *base* in this output is what made `DS=0x10:0xda800` jump out —
even though the selector said 0x10 (the flat data segment), the cached
base was the stale `0xda800` from the prior real-mode load. That's
the entire story of the GDT-walk fix.

## 6. Interrupt trace

`TINYEMU_X64_INTR=1` logs every interrupt delivery:

```
[intr-rm] cycle=20272304 vec=19 IDT.base=0x0 gate@0x4c -> 0xf000:0xe3fe
[intr] deliver vec=14 hasErr=true ec=0x4 RIP=...
```

Use this when you suspect the failure is at an IRQ boundary (timer
spurious, missing EOI, wrong IVT gate, …). It records cycles so you
can correlate with RIP samples.

## 7. I/O port trace

`TINYEMU_X86_IO_DEBUG=1` logs every `in`/`out`:

```
[io] outw 0xcfc <= 0x0000c001
[io] in  0xc014 => 0x40
```

How we used it: when SeaBIOS's virtio-blk read returned 0xFFFF for the
queue-size port, the I/O log showed *zero* reads landing in our virtio
BAR range — the device was alive but at the *old* port address. That
led to the BAR-relocation fix (`machine/pc/pci.go SetBARChangeHandler`).

A tighter view of just one device's I/O: `TINYEMU_VIRTIO_PCI_DEBUG=1`
logs each virtio-pci register access with the offset and the device
tag.

## 8. BIOS debug log

SeaBIOS prints to port 0x402 when `CONFIG_DEBUG_IO` is on (it is in the
prebuilt). Route that output:

```sh
TINYEMU_BIOS_DEBUG=stderr ./bin/temu... -bios bin/seabios/bios.bin ...
# or
TINYEMU_BIOS_DEBUG=/tmp/sea.log ./bin/temu... ...
```

This is the cheapest way to know what SeaBIOS thinks it's doing. The
log style is exactly the same as you'd see under QEMU:

```
SeaBIOS (version rel-1.17.0-...)
Found 3 PCI devices (max PCI bus is 00)
PCI: init bdf=00:03.0 id=1af4:1001
...
Booting from Hard Disk...
```

Useful for: confirming the BIOS got past a checkpoint; reading SeaBIOS's
own diagnostics ("ERROR: queue size 65535 > 256"); confirming the boot
device list matches what you attached.

## 9. Static disassembly

For guest code you have on disk, `ndisasm` is the right tool.

```sh
# 16-bit (real / pm16 / BIOS), at a known offset
ndisasm -b16 -o0x7c00 bin/menuet64/M6416000.IMG | head -40

# 32-bit (pm32 / compat), addressed by image offset
ndisasm -b32 -o0xece00 -e0x2ce00 bin/seabios/bios.bin

# 16-bit slice of an image (skip the first 0x30000 bytes)
ndisasm -b16 -k 0,0x30000 bin/seabios/bios.bin
```

The `-o<addr>` switch sets the displayed offsets; `-e<n>` sets the file
offset to read from. Use these to translate "we crashed at lin
0xFD33A" → "image offset 0x3D33A" → real bytes.

For runtime-built code (e.g. SeaBIOS's `entry_08` stub *plus* the
target it pushed onto the stack), static `ndisasm` won't help. Dump
the live bytes from the running emulator — see Section 5 — then `xxd`
or paste them into `ndisasm -u -b16 -` (stdin mode).

## 10. Frequency-based opcode coverage

When booting a real binary, don't chase `ErrNotImplemented` one at a
time. Disassemble the binary, extract the unique opcode bytes, diff
against the dispatcher:

```sh
ndisasm -b16 -k 0,0x30000 bin/seabios/bios.bin > /tmp/s16.asm
ndisasm -b32 -k 0,0x30000 bin/seabios/bios.bin > /tmp/s32.asm

python3 <<'EOF'
import re, collections
PREFIXES = {0x26,0x2E,0x36,0x3E,0x64,0x65,0x66,0x67,0xF0,0xF2,0xF3}
op1 = collections.Counter()
for fn in ('/tmp/s16.asm', '/tmp/s32.asm'):
    for line in open(fn):
        m = re.match(r'^[0-9A-Fa-f]+\s+([0-9A-Fa-f ]+)\s+\S', line)
        if not m: continue
        bs = m.group(1).replace(' ', '')
        if len(bs) % 2: continue
        b = [int(bs[i:i+2],16) for i in range(0,len(bs),2)]
        i = 0
        while i < len(b) and b[i] in PREFIXES: i += 1
        if i < len(b): op1[b[i]] += 1

for op,n in sorted(op1.items()):
    if n >= 20: print(f"{op:#04x} {n}")
EOF
```

Cross-reference against `grep "case op ==" cpu/x86_64/opcodes.go`.
Implement everything missing in one batch. This is how a 12-opcode
batch (`AAM`, `AAD`, `LES`, `LDS`, `LSS`, `ENTER`, `INTO`, `INT1`,
`SALC`, `XLAT`, `CMC`, plus `Group 5 /3,/5`) closed in one commit
instead of twelve.

**Filter noise:** for a 256 KB image like SeaBIOS, ~75% is zero-padding
that decodes as `add [bx+si], al`. Skip the padding with `-k start,len`
and ignore opcodes that appear < ~20 times in your output.

## 11. Bisecting a regression

The pattern that found the `0xEA` bug (one week dormant):

```sh
# 1. Confirm broken at HEAD.
git checkout main
go build -o bin/temu.darwin-arm64.bin ./cmd/temu
./run64_iso.sh alpine-debug fast 2>&1 | grep -E 'ERROR|Linux version'
# → ERROR

# 2. Find a known-good commit (memory note, prior CI run, dated comment).
git checkout d4e03e5  # the "boot achieved" commit from a week earlier
go build && ./run64_iso.sh ...   # → Linux version → good

# 3. Manual bisect — pick the half-way commit and test.
git checkout e285efd
go build && ./run64_iso.sh ...   # → ERROR
# So the bug is between d4e03e5 and e285efd.

# 4. Narrow.
for c in bfad461 f3d9399; do
    git checkout $c && go build && ./run64_iso.sh ... | grep ERROR
done
```

The two-test loop pinned `f3d9399` as the culprit. Total cost: ~5 builds
× ~5 s each + 5 boot attempts × ~70 s timeout each ≈ 6 minutes — to
find a regression that had been dormant a week.

`git bisect run` automates this when you have a non-interactive predicate.
Manual bisect is fine for boots because the failure or success is in
the first line of output.

## 12. Pin every fix with a regression test

The pattern that worked for the `0xEA` fix — see `cpu/x86_64/jmpfar_test.go`:

1. Build a CPU with a GDT in RAM.
2. Put one descriptor at a known selector (the 64-bit code segment).
3. Set the architectural state that triggered the bug (EFER.LMA=1, CS.L=0).
4. Lay down the failing instruction bytes at CS:RIP.
5. `c.Step()` and assert.

Two non-obvious gotchas:

- **Don't enable CR0.PG** unless you're also providing page tables.
  Mode detection only needs EFER.LMA + CS.L; leaving PG off lets the
  fetch use identity translation.
- **Validate the test catches the regression.** Temporarily revert the
  fix, run the test, confirm it fails. Then restore the fix.

```sh
sed -i.bak 's/NEW_CHECK/OLD_CHECK/' cpu/x86_64/opcodes.go
go test -run TestThing ./cpu/x86_64/   # expect FAIL
mv cpu/x86_64/opcodes.go.bak cpu/x86_64/opcodes.go
go test -run TestThing ./cpu/x86_64/   # expect PASS
```

## 13. Choosing between techniques

```
symptom                          start with
─────────────────────────────────────────────────────────
ErrNotImplemented at opcode X    just implement X (or batch §10)
guest hangs                      RIPSAMPLE coarse → fine
guest corrupts memory            PHYSWATCH on the byte range
boot stops at a known checkpoint BIOS_DEBUG log → static ndisasm
INT/IRQ misbehaving              INTR=1 trace
device I/O wrong                 IO_DEBUG + VIRTIO_PCI_DEBUG
mode wrong (compat vs long)      DUMPCYCLE to read CS.access bits
something used to work           git bisect (§11)
```

## 14. Common bug shapes seen in this codebase

These keep coming back; learn to recognise them.

### "Stack-slot vs operand-size"

A `push`/`pop`/`call`/`ret` uses `c.stackSlotSize()` when it should use
`c.pushPopOperandSize(operandSize)`. Symptom: stack misalignment by 2
or 4 bytes after a `66`-prefixed instruction in real mode. Look for
the symptom "POP returned a value whose two 16-bit halves are
swapped" — that's the canonical 2-byte misalignment fingerprint. See
the `CALL/RET` fix (commit `f0f2694`), `RETF/IRET` (`17f178d`),
`PUSH/POP` (`fae96d9`), `0x8F POP r/m` (`060f4ba`).

### "Mode predicate uses LMA but should use CS.L"

Long mode is *active* (`EFER.LMA=1`) during compatibility mode too —
in fact the `ljmp` that *enters* 64-bit mode runs with LMA already on
but CS.L still 0. Always gate "is this 64-bit-only opcode invalid?"
on `c.mode == ModeLong64`, never on `EFER.LMA`. See commit `6e1f148`
and `cpu/x86_64/jmpfar_test.go`.

### "Cached segment base outlived a mode switch"

`opMOVtoSreg` historically only rebuilt the cached base from `sel<<4`
in real mode, leaving the stale base from the previous mode in
protected mode. This was fine *as long as* the prior mode's base
happened to be 0 (flat) — but when SeaBIOS's 16-bit INT handler ran on
the zonelow stack (DS base 0xda800) and then `call32`'d into 32-bit
flat code, the cached DS base stayed 0xda800 and every "flat" pointer
landed 0xda800 bytes high. Fix: walk the GDT on every protected-mode
segment load (commit `106e8b1`).

### "Device sees nothing because firmware moved the BAR"

Linux uses the firmware-assigned BAR; SeaBIOS reassigns BARs from
scratch. If your I/O ports are registered statically at the original
base, SeaBIOS's accesses go to an unmapped port and read 0xFFFF. Add
a BAR-relocation callback (commit `0d9b567`).

### "Timer fires once then never again"

8254 PIT semantics: a reload value of 0 means *65536*, not "disabled".
SeaBIOS programs channel 0 with divisor 0 (the standard ~18.2 Hz tick)
and our code skipped it. Fix: track `active` per-channel and use a
32-bit effective reload with 0→65536 mapping (commit `1e7633a`).

## 15. The env-var reference

Every knob is an environment variable read once at start-up, so set it on
the command line in front of the binary:

```sh
TINYEMU_BIOS_DEBUG=stderr TINYEMU_LAPIC_DEBUG=1 \
    ./bin/temu.darwin-arm64.bin -machine x86_64 -m 512 -apic -bios bin/ovmf/OVMF_DEBUG.fd
```

Naming convention: `X64_*` flags belong to the `x86_64` long-mode
backend, `X86_*` flags to the i386 backend (a handful are honoured by
both). Flags with no arch prefix are machine/device or process-wide.
Ranges are written `lo-hi` as **hex** (e.g. `0x1000-0x2000`); counters
are decimal.

### CPU — x86_64 backend (`cpu/x86_64`)

| Variable | Effect |
|---|---|
| `TINYEMU_X64_TRACE=1` | full per-instruction step trace (everything; very verbose) |
| `TINYEMU_X64_TRACE_CYCLES=lo-hi` | step trace only within a cycle window |
| `TINYEMU_X64_RIPTRACE=lo-hi` | step trace only while RIP is in range |
| `TINYEMU_X64_RIPSAMPLE=N` | log one RIP/mode line every N executed instructions |
| `TINYEMU_X64_RIPTRAP=1` | dump + trap at a built-in RIP breakpoint |
| `TINYEMU_X64_DUMPCYCLE=N` | one-shot register + memory dump at cycle N |
| `TINYEMU_X64_INTR=1` | log every interrupt / exception delivery |
| `TINYEMU_X64_PF=1` | log page-fault handling (walk + injected #PF) |
| `TINYEMU_X64_CR3=1` | log CR3 (page-table base) writes |
| `TINYEMU_X64_MSR=1` | log RDMSR / WRMSR |
| `TINYEMU_X64_IO=1` | log IN / OUT at the CPU layer |
| `TINYEMU_X64_USYS=1` | log user-mode SYSCALLs |
| `TINYEMU_X64_PHYSWATCH=lo-hi` | log every write to a physical-address range, with read-back qword |
| `TINYEMU_X64_VAWATCH=lo-hi` | log every read/write to a virtual-address range |
| `TINYEMU_X64_STRWATCH=lo-hi` | log REP string-instruction (MOVS/STOS/…) activity in range |
| `TINYEMU_X64_ENTRY=addr` | override the long-mode kernel entry point (vmlinux boot) |
| `TINYEMU_X64_PIC=1` | log 8259 PIC activity on the x86_64 path |

Setting `PHYSWATCH`/`VAWATCH` forces multi-byte accesses down the
byte-at-a-time path so each byte is logged (the same-page fast path is
skipped while a watch is armed).

### CPU — i386 backend (`cpu/x86`)

| Variable | Effect |
|---|---|
| `TINYEMU_X86_CHKPT=cycle` / `TINYEMU_X86_CHKPT_INTERVAL=N` | dump a state checkpoint at `cycle`, then every N cycles |
| `TINYEMU_X86_EIPBP=addr` / `TINYEMU_X86_EIPBPS=a,b,…` | break/dump at one or several EIP values |
| `TINYEMU_X86_CPUID_TRACE=1` | log every CPUID |
| `TINYEMU_X86_CR_DEBUG=1` | log control-register writes |
| `TINYEMU_X86_INVLPG_DEBUG=1` | log INVLPG |
| `TINYEMU_X86_INT_DEBUG=1` | log interrupt delivery |
| `TINYEMU_X86_SYS=1` | log syscalls |
| `TINYEMU_X86_USERPF=1` | dump regs + memory near EAX/ESI/EDI for user-mode #PF to addresses < 0x1000 |
| `TINYEMU_X86_PF_DEBUG=1` | log page-fault handling |
| `TINYEMU_X86_ESP_DEBUG=1` | log ESP / stack anomalies |
| `TINYEMU_X86_FPCMP=1` | x87 floating-point compare diagnostics |
| `TINYEMU_X86_X87_TRACE=1` | trace x87 FPU operations |
| `TINYEMU_X86_LOG_UD2=1` | log every #UD (invalid opcode) |
| `TINYEMU_X86_SKIP_UD2=1` | log **and skip** a #UD so the guest survives an unknown opcode |
| `TINYEMU_X86_LOOPHANG=1` | detect and report tight infinite loops |
| `TINYEMU_X86_PHYSWATCH=lo-hi` | log writes to a physical-address range |
| `TINYEMU_X86_PHYSSENTINEL=addr:val` | trip when a sentinel value appears at a phys address |
| `TINYEMU_X86_WW_LO=lo` + `TINYEMU_X86_WW_HI=hi` | watch writes to the window `[lo,hi)` (both must be set) |

### Machine & devices (`machine/pc`)

| Variable | Effect |
|---|---|
| `TINYEMU_BIOS_DEBUG=path\|stderr` | route firmware (SeaBIOS / OVMF) port-0x402 debug output |
| `TINYEMU_BIOS_POST=1` | log BIOS POST codes (port 0x80) |
| `TINYEMU_CMOS_DEBUG=1` | log every CMOS/RTC register read (memory-sizing regs 0x34/0x35, 0x5B-0x5D) |
| `TINYEMU_FWCFG_DEBUG=1` | log fw_cfg selector + reads (file directory, `etc/e820`, …) |
| `TINYEMU_FDC_DEBUG=1` | log every floppy-controller command and READ |
| `TINYEMU_LAPIC_DEBUG=1` | log local-APIC MMIO register reads/writes (use with `-apic`) |
| `TINYEMU_VGA_CHAR_LOG=path\|stderr` | mirror VGA text-mode framebuffer writes |
| `TINYEMU_X86_VGA_RENDER=1` | render the VGA framebuffer for inspection |
| `TINYEMU_X86_ATA_DEBUG=1` | log ATA/IDE commands |
| `TINYEMU_X86_IDE=1` | enable the legacy IDE controller |
| `TINYEMU_X86_PCI_DEBUG=1` | log PCI config-space accesses |
| `TINYEMU_X86_IO_DEBUG=1` | log every port IN / OUT at the machine layer |

### virtio & networking

| Variable | Effect |
|---|---|
| `TINYEMU_VIRTIO_PCI_DEBUG=1` | log every virtio-pci register access |
| `TINYEMU_VIRTIO_NET_DEBUG=1` | log virtio-net queue / packet activity |
| `TINYEMU_VIRTIO_CONSUME_DEBUG=1` | log virtqueue descriptor consumption |
| `TINYEMU_X86_DMAWATCH=lo-hi` | log virtio DMA activity in an address range |
| `TINYEMU_SLIRP_TRACE=1` | trace user-mode network (slirp) packets |

### Profiling & test

| Variable | Effect |
|---|---|
| `TINYEMU_PROFILE` / `TINYEMU_X86_PROFILE` / `TINYEMU_X64_PROFILE=1` | write a Go CPU profile to `/tmp/temu.prof` |
| `TINYEMU_PERF` / `TINYEMU_X86_PERF` / `TINYEMU_X64_PERF=1` | print periodic cycles/sec on stderr |
| `TINYEMU_SKIP_BOOT_TESTS=1` | skip the slow OS-boot integration tests under `go test` |

---

## Appendix: the canonical session

The MenuetOS work followed this template end-to-end. Read it as a
worked example:

1. **Symptom:** `ERROR in Run: decoder feature not implemented yet:
   opcode 0xa6 ... pre=00 00 ... 00 a6 post=00 00 ...` — `CMPSB` in a
   zero-memory region. So something jumped wrong long before this.
2. **Sample:** `RIPSAMPLE=1` over the 2.3M cycles. Find the *first*
   sample whose surrounding bytes are zeros → "we wandered into junk
   at cycle 1.39M".
3. **Backtrace:** scan backward. Find the last *sane* RIP. The one
   before it executes the bad jump.
4. **Disassemble:** the bytes at that RIP. `cf` = `IRET`. So the jump
   target came from the IRET frame on the stack.
5. **Watchpoint:** the source stack location. See who wrote the bytes
   the IRET pops. Cross-reference with what *should* have been pushed.
6. **Find the asymmetry:** the push wrote half-width (`pushw` instead
   of `pushl`). That's the bug — `Group 5 /6` PUSH r/m using
   `stackSlotSize` instead of `pushPopOperandSize`.
7. **Fix.** One line: `c.pushStack(value, c.pushPopOperandSize(operandSize))`.
8. **Pin:** unit test that exercises `66 ff 70 20` in real mode.

The first time you run this loop it takes a day. By the fifth bug
it's a few hours. The tools — samplers, watchpoints, dumps — pay for
themselves immediately; build them once and use them on every guest.
