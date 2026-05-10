# Plan: boot Alpine Linux to interactive shell + vim under `cmd/temu` (x86)

## Context

The repo is a Go port of TinyEMU. The RISC-V CPU and machine boot Linux to a shell, including vim. The x86 CPU at `cpu/x86/` and PC board at `machine/pc/` were added later and don't yet get the kernel running. The user wants the x86 path to reach an interactive shell on `cmd/temu` and run vim, with the existing Alpine x86 images in `bin/` (`vmlinuz-alpine-x86`, `initrd-alpine-x86`).

The audit (read of `cpu/x86/*.go`, `machine/pc/*.go`, `cmd/temu/main.go`) revealed three classes of problems:

1. **Run-loop / IRQ wiring is incomplete on x86**: `PIC.RaiseIRQ` does not assert `cpu.SetINTR` directly — only `pc.CheckTimer()` updates INTR via `pic.PeekInterrupt()`. `PIT8254.Tick()` advances by a fixed amount per host call rather than being driven by CPU cycles. The standalone `longboot_test.go` calls `cpu.Step()` in a tight loop and never calls `CheckTimer/PollDevices`, so timer IRQs never reach the kernel and PIT calibration loops forever. (`cmd/temu/main.go:574-634` does drive the loop correctly for the interactive case, so this primarily affects the test harness — but the PIC→INTR gap affects both.)
2. **Instruction-set holes the kernel will hit**: in the 0F dispatcher (`cpu/x86/exec.go:1546`) the CMOVcc family `0F 40-4F`, BSF/BSR `0F BC/BD`, XADD `0F C0/C1`, CMPXCHG8B `0F C7`, multi-byte NOP `0F 1F`, and LSS/LFS/LGS `0F B2/B4/B5` are missing. CLTS `0F 06`, WAIT `9B`, x87 `D8-DF` will also be hit. MSR access is stubbed (`exec.go:1790-1799` — WRMSR ignores writes, RDMSR returns 0), which can mislead kernel feature detection. CPUID (`system.go:292-311`) advertises FPU=1 and nothing else.
3. **PAE paging is unimplemented**: `mmu.go:91-154` walks 32-bit two-level only; PAE prints a warning and bails. Modern Alpine x86 builds expect PAE.

Other issues: page-fault `Fprintf` to stderr on every PF (`mmu.go:40-45`) will flood; `LoadSegmentProtected` has a TODO at `system.go:99` for privilege/type checks; division-by-zero never raises `#DE` (8 TODOs in `arith.go`); unimplemented opcodes return Go errors instead of raising `#UD`; `longboot_test.go` doesn't supply an initrd (kernel will rootfs-panic).

The user has confirmed: target is interactive `cmd/temu` to shell + vim; we **implement** PAE rather than avoid it; we do a **comprehensive ISA sweep first** before iterating on kernel-driven failures.

---

## Phase 0 — Diagnostic harness and event-loop wiring

Goal: make IRQs flow into the CPU under any harness, silence PF spam, give us a fast iteration target.

### 0.1 PIC owns the CPU INTR line

- File: `machine/pc/pic.go`
- After `RaiseIRQ` sets the IRR bit, recompute `pending = irr & ^imr`; if non-zero call `p.cpu.SetINTR(1)`. After `LowerIRQ` and `DeliverInterrupt`, recompute and call `p.cpu.SetINTR(0)` when no interrupt remains.
- File: `machine/pc/pc.go:231-238` — `CheckTimer` no longer needs the `PeekInterrupt → SetINTR` block; keep `pit.Tick(...)`.

### 0.2 Couple PIT advance to CPU cycles

- File: `machine/pc/pit.go` — change `Tick` to take a `delta uint64` and decrement the channel counters by `delta / pitCyclesPerTick` (pick a constant such that PIT calibration converges in a reasonable wall-clock budget; 100 is a starting point).
- File: `machine/pc/pc.go` — add `lastTickCycles uint64` to `PC`; in `CheckTimer` compute `delta = cpu.GetCycles() - p.lastTickCycles`, update the field, pass to `pit.Tick(delta)`.

### 0.3 Suppress page-fault spam

- File: `cpu/x86/mmu.go:40-45, 101, 115` — replace unconditional `fmt.Fprintf` with `if pfDebug { ... }` where `pfDebug` is initialized once from `os.Getenv("TINYEMU_X86_PF_DEBUG") == "1"`.

### 0.4 Reshape `longboot_test.go`

- File: `machine/pc/longboot_test.go` — load `bin/initrd-alpine-x86` and pass to `LoadBIOS`; replace the `cpu.Step()` loop with `for !done { p.CheckTimer(); p.PollDevices(); cpu.Run(50_000); ... }`; check the UART buffer for forward progress every ~5s wall, log `EIP/CR0/CR3/CR4/EFLAGS/ESP` on stalls.
- Add a `TestStage1Boot` (≈10M cycles) that asserts UART contains `"Linux version"`.

### Verification (Phase 0)

- `go test ./machine/pc -run TestStage1Boot -v -count=1`
- `TINYEMU_X86_PF_DEBUG=1 go test ./machine/pc -run TestStage1Boot` should be the only noisy mode.

---

## Phase 1 — Comprehensive ISA sweep

All edits in `cpu/x86/exec.go` 0F dispatcher (starts at line 1546). New tests under `cpu/x86/` mirroring `bit_test.go` / `group0f00_test.go`.

### 1.1 Verified-present (no work)

`SLDT/STR/LLDT/LTR/VERR/VERW (0F 00)`, `LGDT/LIDT/LMSW/SMSW/INVLPG (0F 01)`, MOV CR/DR (`0F 20/22`, `0F 21/23`), Jcc near (`0F 80-8F`), SETcc (`0F 90-9F`), `PUSH/POP FS/GS`, CPUID `0F A2`, BT/BTS/BTR memory (`0F A3/AB/B3`), BT/BTS/BTR/BTC immediate `0F BA`, BTC reg/mem `0F BB`, SHLD/SHRD (`0F A4/A5/AC/AD`), WRMSR/RDTSC/RDMSR (`0F 30/31/32`), IMUL `0F AF`, CMPXCHG (`0F B0/B1`), MOVZX/MOVSX (`0F B6/B7/BE/BF`), BSWAP (`0F C8-CF`).

### 1.2 Missing — must add

Each as a new `case 0xNN:` inside the 0F switch:

- **`0F 40..4F` CMOVcc** — entire family missing. Add a helper that mirrors the condition test from the existing Jcc-near handler at `exec.go:1557`, then conditionally writes `r/m → r`. Linux uses CMOV in scheduling, locking, and atomic paths.
- **`0F BC` BSF, `0F BD` BSR** — set ZF if source is zero; otherwise write the bit index of the lowest/highest set bit. Pattern: similar shape to BT (`exec.go:1879`).
- **`0F C0/C1` XADD** — `parseModRM`; tmp=dst; dst=dst+src (flags via existing ADD helper); src=tmp.
- **`0F C7 /1` CMPXCHG8B** — group-encoded; only `/1` (and `/6`/`/7` RDRAND/RDSEED — skip). Compare `EDX:EAX` with 8-byte memory; equal → write `ECX:EBX`, set ZF; not equal → load `EDX:EAX`, clear ZF.
- **`0F 1F /0..7` multi-byte NOP** — `parseModRM` (consume bytes), do nothing.
- **`0F B2` LSS, `0F B4` LFS, `0F B5` LGS** — same shape as existing `LES/LDS` (`exec.go:1385-1425`). LSS is critical: kernel uses it for the SS:ESP atomic load on stack switch. After the segment load, set `c.interruptsBlocked = true` for SS to mirror the `MOV SS, ...` shadow.
- **`0F 06` CLTS** — clear `CR0.TS`; `#GP` if `CPL>0`.
- **`0F 08` INVD, `0F 09` WBINVD** — NOP (cache ops); `#GP` if `CPL>0`.

### 1.3 One-byte gaps

- **`0x9B` WAIT/FWAIT** — add `case 0x9B:` as NOP. Currently falls through to "unimplemented".

### 1.4 x87 `D8-DF`

- Decision: drop FPU=1 from CPUID (Phase 2.1) and stub the entire `D8-DF` range to consume a ModRM where applicable. Recognize `FNINIT`/`FNCLEX`/`FNSTSW` patterns explicitly as NOPs. With FPU bit clear in CPUID, the kernel uses soft-FP and won't issue x87 instructions on the fast path.

### 1.5 Fault model

- Add panic types `divideError struct{}` and `invalidOpcodeError struct{}` next to `pageFaultError` in `mmu.go`.
- In `exec.go` `Step()` defer (lines 129-145), add cases routing to `handleInterrupt(0x00, false)` and `handleInterrupt(0x06, false)`.
- Replace the two `fmt.Errorf("unimplemented opcode...")` returns with `panic(invalidOpcodeError{})`.
- In `arith.go`, replace the 8 division TODOs with `panic(divideError{})`.

### 1.6 PUSHFD/POPFD privilege filtering

- File: `cpu/x86/exec.go` POPFD path. When `IsProtectedMode() && CPL>0`, mask out attempts to modify `IOPL`, `IF` (when `CPL>IOPL`), `VM`, `VIP`, `VIF` — preserve current EFLAGS bits for those.

### Verification (Phase 1)

- New unit tests: `cmov_test.go`, `bsf_bsr_test.go`, `xadd_test.go`, `cmpxchg8b_test.go`, `lss_test.go`, `nop_test.go`.
- `go test ./cpu/x86 -count=1` — all green.
- Re-run `TestStage1Boot`; expect kernel boot progress past CPU detect.

---

## Phase 2 — MSR allow-list, CPUID polish, SYSENTER policy

### 2.1 CPUID (`cpu/x86/system.go:292-311`)

Extend `handleCPUID` to handle leaves `0, 1, 2, 4, 0x80000000..0x80000004`.

- Leaf 0: max standard = 4; vendor (already correct).
- Leaf 1 EDX feature bits to set: TSC(4), MSR(5), CX8(8), CMOV(15), PGE(13), PAT(16), PSE(3). After Phase 4 lands, also set PAE(6). Clear: FPU(0), VME(1), DE(2), MCE(7), APIC(9), SEP(11), MTRR(12), MCA(14), PSE36(17), CLFLUSH(19), DS(21), ACPI(22), MMX(23), FXSR(24), SSE(25), SSE2(26).
- Leaf 1 ECX = 0.
- Leaves 2, 4: zero-filled.
- Leaf 0x80000000: max ext = 0x80000004; vendor.
- Leaves 0x80000002-0x80000004: brand string `"tinyemu-go x86 CPU @ 1.0GHz"`.

### 2.2 MSR handler (`cpu/x86/exec.go:1790-1799`)

Add CPU fields in `cpu.go`: `msrSysenterCS, msrSysenterESP, msrSysenterEIP uint32; msrFSBase, msrGSBase uint32; msrMiscEnable uint64; efer uint64`.

Replace WRMSR/RDMSR stubs with switch on `ECX`:

- `0x10 IA32_TSC` — write absorbs; read returns `c.cycles`.
- `0x174/175/176 IA32_SYSENTER_*` — store/load.
- `0x1A0 IA32_MISC_ENABLE` — store/load 64-bit.
- `0x1B IA32_APIC_BASE` — read returns `0xFEE00000` (enable bit clear); write absorbs.
- `0xC0000080 IA32_EFER` — store/load; recognize SCE(0), NXE(11); reject LME(1) with `#GP`.
- `0xC0000100/101 IA32_FS_BASE/GS_BASE` — write also reflects into `c.segBase[FS]`/`c.segBase[GS]`.
- `0x200..0x20F` MTRR base/mask, `0x250..0x258` MTRR fixed: silently absorb writes; reads return 0.
- default: `raiseGeneralProtectionFault(0)`.

### 2.3 SYSENTER/SYSEXIT

Not implemented. CPUID.1.EDX SEP=0 ensures kernel uses `INT 0x80`.

### Verification (Phase 2)

- Extend `cpu/x86/msr_test.go` to cover the allow-list and `#GP` for unknown.
- Re-run stage1 test — boot should reach early kernel device detect.

---

## Phase 3 — Page-fault and fault-handling polish

### 3.1 Error code completeness

- File: `cpu/x86/mmu.go` `raisePageFault` (line 31).
- Add parameters `fetch bool, rsvd bool`. Set bit 4 (I/D) for fetch faults; set bit 3 (RSVD) for reserved-bit faults.
- Plumb `fetch` through `translateAddress` by adding the parameter; introduce `fetchMem8/16/32` (or pass a flag from `fetch8/16/32` at `exec.go:85-109`) so instruction fetches translate with `fetch=true`.

### 3.2 Segment-load privilege/type checks (`system.go:99` TODO)

- Null selector into DS/ES/FS/GS at any CPL: zero out (mark not-usable).
- Null selector into CS or SS: `raiseGeneralProtectionFault(0)`.
- Data segment: require `RPL ≤ DPL && CPL ≤ DPL`. SS additionally requires writable.
- Non-conforming code into CS: require `CPL == DPL`. Conforming: `CPL ≥ DPL`.
- Type checks: enforce S=1; data has type-bit-3 = 0; code has type-bit-3 = 1.
- On failure: `raiseGeneralProtectionFault(uint32(selector & 0xFFFC))`.

### 3.3 Defer cases

- Already covered in 1.5: `divideError → INT 0`, `invalidOpcodeError → INT 6`.

### Verification (Phase 3)

- `go test ./cpu/x86 -count=1` — existing `interrupt_test.go` still green; add cases for new error-code bits and segment-load `#GP`.

---

## Phase 4 — PAE 3-level paging

### 4.1 State

- Add to `cpu/x86/cpu.go`: `pdpte [4]uint64`, `paeActive bool`.
- Add helper `c.refreshPDPTEs()` that reads four 64-bit PDPTEs from `CR3 & ~0x1F` (using two `readPhys32` per entry, or a new `readPhys64`).

### 4.2 Dispatcher

File: `cpu/x86/mmu.go` `translateAddress`:

```
if !pagingEnabled() { return lin }
if cr4 & CR4_PAE != 0 { return translatePAE(lin, write, user, fetch) }
return translateLegacy(lin, write, user, fetch)
```

Rename the existing 32-bit walk as `translateLegacy`; preserve the A/D bit update pattern at lines 140-150.

### 4.3 `translatePAE`

- Index: `pdptIdx = (lin>>30) & 3`; load `c.pdpte[pdptIdx]`. Bit 0 (P) must be 1.
- `pdAddr = (PDPTE[51:12]) << 12 | ((lin>>21) & 0x1FF) << 3`. Read 64-bit PDE.
- If PDE.PS (bit 7) = 1: 2 MB page; phys = `PDE[51:21] | (lin & 0x1FFFFF)`.
- Else `ptAddr = (PDE[51:12]) << 12 | ((lin>>12) & 0x1FF) << 3`; read 64-bit PTE; phys = `PTE[51:12] | (lin & 0xFFF)`.
- Permission combine: U/S = AND of bit 2 across PDPTE/PDE/PTE; R/W = AND of bit 1; subject to CR0.WP for supervisor.
- A/D bit updates: write A on each level walked; write D on PTE for write access.
- NX (bit 63): honored only if `EFER.NXE=1`. On instruction-fetch violation, raise `#PF` with bit 4 set.

### 4.4 PDPTE cache invalidation

- File: `cpu/x86/system.go` `handleMovCR`. After write to `CR3` while `paeActive`: `refreshPDPTEs()`.
- After write to `CR0` enabling PG while `CR4.PAE=1`: set `paeActive=true`, `refreshPDPTEs()`.
- After write to `CR4` enabling PAE while `CR0.PG=1`: same.

### 4.5 CPUID

After PAE works, set CPUID.1.EDX bit 6 (PAE).

### 4.6 Tests

New file `cpu/x86/pae_test.go`:

- Build a 3-level table mapping `0x0010_0000_0000` → `0x0000_2000`. Set CR3=PDPT phys; CR4.PAE=1; CR0.PG|PE=1. `ReadMem8` should read from `0x2000`.
- 2 MB page mapping case.
- U/S violation raises PF with `error_code = 0x05` (P=1, U=1).
- NX violation with `EFER.NXE=1` raises PF with bit 4 set on fetch.

### Verification (Phase 4)

- `go test ./cpu/x86 -run TestPAE -v -count=1`
- Stage1 boot test: kernel should reach device init.

---

## Phase 5 — Console RX path and interactive shell input

### 5.1 UART receive (`machine/pc/uart.go`)

- Add: `rxFIFO []byte`, `rxMu sync.Mutex`, `Push(b []byte)` method that appends and raises IRQ on PIC if `IER & 0x01`.
- Read at port `0x3F8` (DLAB=0): pop one byte from FIFO; if FIFO becomes empty, lower IRQ.
- Update LSR (port `0x3FD`): set bit 0 (Data Ready) when FIFO non-empty.
- Update IIR (port `0x3FA`): return `0x04` (recv data available) when FIFO non-empty AND `IER & 0x01`; `0xC1` when THR empty AND `IER & 0x02`; else `0x01`.

### 5.2 Wire stdin → UART (`cmd/temu/main.go`)

- In `pc.go`: add `(p *PC) UART() *UART16550`.
- In `cmd/temu/main.go runEmulator` around line 591 (existing `console.Read(inputBuf)`): when the machine is `*pc.PC`, type-assert and call `pcM.UART().Push(inputBuf[:n])`.

### Verification (Phase 5)

- Manual: `go run ./cmd/temu -machine x86 -kernel bin/vmlinuz-alpine-x86 -initrd bin/initrd-alpine-x86 -m 128 -append "console=ttyS0,115200 noapic nolapic acpi=off"` and confirm typing reaches the kernel.

---

## Phase 6 — Rootfs and userspace

- Initramfs path is the chosen rootfs; `bin/initrd-alpine-x86` already supported by `bzimage.go:170-183`. Verify `cmd/temu/main.go:461` passes initrd through to `LoadBIOS`.
- Cmdline default for headless boot: `console=ttyS0,115200 noapic nolapic acpi=off pci=noacpi nosmp earlyprintk=serial,ttyS0,115200 nokaslr`. Avoid `quiet` until iteration is done.
- virtio-blk: deferred.

### Verification (Phase 6)

- Boot reaches `Welcome to Alpine` or busybox `init` prompt.

---

## Phase 7 — Verify shell, run vim

- `go run ./cmd/temu -machine x86 -kernel bin/vmlinuz-alpine-x86 -initrd bin/initrd-alpine-x86 -m 128 -append "console=ttyS0,115200 noapic nolapic acpi=off"`.
- Smoke: `ls /`, `uname -a`, `cat /etc/os-release`.
- Editor: `vi /tmp/test.txt`, type `iHello<Esc>:wq`, confirm via `cat`. `vim` requires `apk add vim` which needs network; busybox `vi` is sufficient for the success criterion.
- Likely workarounds in-guest: `stty rows 24 cols 80`, `stty erase ^?`.

---

## Cross-phase verification

- Per-phase unit tests under `cpu/x86/`.
- `TestStage1Boot` (≈10M cycles, asserts `"Linux version"` in UART) — fast iteration target.
- `TestStage2Boot` (≈200M cycles, asserts `"Welcome to Alpine"` or `"~ #"`).
- Final: interactive `cmd/temu` run with smoke + vi test.

## Critical files to touch

- `cpu/x86/exec.go` — opcode sweep, ISA gaps, MSR allow-list, fault dispatch.
- `cpu/x86/mmu.go` — PAE walk, error-code bits, debug gating, panic types.
- `cpu/x86/system.go` — CPUID polish, segment-load privilege checks, CR write hooks for PAE.
- `cpu/x86/cpu.go` — new MSR/EFER/PDPTE fields.
- `cpu/x86/arith.go` — divide-by-zero `#DE` panics.
- `machine/pc/pic.go` — direct INTR assertion.
- `machine/pc/pit.go` — cycle-driven Tick.
- `machine/pc/pc.go` — lastTickCycles, UART accessor, drop redundant INTR set.
- `machine/pc/uart.go` — RX FIFO, LSR/IIR updates, `Push`.
- `machine/pc/longboot_test.go` — proper run loop, initrd, stage1/stage2 split.
- `cmd/temu/main.go` — stdin → UART forwarding for `*pc.PC`.

## Risks / unknowns

- 32-bit IO ports in `pc.go:125-129` are split into two 16-bit accesses; not on critical path for initramfs-only boot.
- `interruptsBlocked` is cleared once at `exec.go:156` after the IRQ check; verify it isn't reset mid-prefix-loop (current control flow only enters `executeOpcode` once per Step).
- IRET v8086 path is TODO at `exec.go:854-856`. Not hit for our cmdline.
- LOCK prefix ignored — safe for SMP=1.
- TSC calibration: if PIT calibration succeeds, kernel converts to TSC. With `nolapic` we avoid lapic-timer entirely.
- Reserved-bit checks in PAE PDPTE/PDE/PTE: start permissive (only check P bit); tighten if kernel actually relies on the check.
