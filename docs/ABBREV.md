# Abbreviations & Acronyms

A glossary of the abbreviations that show up across this emulator (multi-ISA:
x86 / x86_64 / ARM64 / RISC-V, plus virtio, firmware, and boot plumbing).
Grouped by area; within a group, roughly alphabetical.

---

## General / CPU

| Abbr | Expansion | Notes |
|------|-----------|-------|
| ISA | Instruction Set Architecture | x86, x86_64, ARM64/AArch64, RISC-V |
| CPU | Central Processing Unit | one core in this emulator |
| PC | Program Counter | next instruction address (ARM64/RISC-V); x86 calls it RIP/EIP/IP |
| RIP / EIP / IP | Instruction Pointer | x86_64 / x86-32 / x86-16 program counter |
| SP | Stack Pointer | |
| LR | Link Register | ARM64 return address (X30) |
| FP | Frame Pointer | also "Floating Point" depending on context |
| GPR | General-Purpose Register | |
| EA | Effective Address | computed memory operand address (base+index+disp) |
| imm | Immediate | constant encoded in the instruction |
| MMIO | Memory-Mapped I/O | device registers accessed as memory |
| PIO | Port I/O | x86 `in`/`out` to I/O ports |
| DMA | Direct Memory Access | device reads/writes RAM without the CPU |
| SMP | Symmetric Multi-Processing | multiple CPUs (mostly single-core here) |
| BSP / AP | Bootstrap / Application Processor | x86 SMP: the first vs. secondary cores |
| JIT | Just-In-Time (compilation) | not used; we are a tree-walking interpreter |
| SMC | Self-Modifying Code | guest code that rewrites itself |
| MIPS | Millions of Instructions Per Second | rough interpreter throughput metric |

## Memory / MMU

| Abbr | Expansion | Notes |
|------|-----------|-------|
| MMU | Memory Management Unit | virtual→physical translation |
| TLB | Translation Lookaside Buffer | cached page translations |
| VA / PA | Virtual / Physical Address | |
| GVA / GPA | Guest Virtual / Physical Address | |
| PTE / PDE | Page Table / Page Directory Entry | |
| PML4 / PDPT / PD / PT | x86_64 4-level page-table tiers | PML4→PDPT→PD→PT |
| TTBR0 / TTBR1 | Translation Table Base Register | ARM64 user / kernel page-table roots |
| TCR / MAIR / SCTLR | Translation Control / Memory Attr Indirection / System Control | ARM64 MMU config |
| ASID / VMID | Address-Space / VM Identifier | TLB tagging |
| SATP | Supervisor Address Translation & Protection | RISC-V page-table root CSR |
| NX | No-eXecute | non-executable page bit |
| KPTI | Kernel Page-Table Isolation | Meltdown mitigation (disabled via `mitigations=off`) |

## Interrupts / Exceptions

| Abbr | Expansion | Notes |
|------|-----------|-------|
| IRQ | Interrupt ReQuest | maskable hardware interrupt |
| FIQ | Fast Interrupt reQuest | ARM64 high-priority interrupt |
| NMI | Non-Maskable Interrupt | x86 |
| ISR | Interrupt Service Routine *or* In-Service Register | context-dependent (handler / APIC reg) |
| IRR | Interrupt Request Register | APIC pending-interrupt bitmap |
| EOI | End Of Interrupt | acknowledge completion to the interrupt controller |
| GSI | Global System Interrupt | flat interrupt-number space (x86 IOAPIC inputs) |
| INTx / INTA | PCI legacy interrupt pins | INTA–INTD; level-triggered |
| MSI | Message-Signaled Interrupt | PCI interrupt via a memory write (not used yet) |

## x86 / x86_64

| Abbr | Expansion | Notes |
|------|-----------|-------|
| IDT / GDT / LDT | Interrupt / Global / Local Descriptor Table | |
| IDTR / GDTR | IDT / GDT Register (base+limit) | |
| TSS / TR | Task State Segment / Task Register | holds ring stacks (RSP0) |
| CPL / RPL / DPL | Current / Requested / Descriptor Privilege Level | rings 0–3 |
| CR0/CR2/CR3/CR4 | Control Registers | CR3 = page-table base, CR2 = fault addr |
| EFER | Extended Feature Enable Register | LME/LMA = long-mode enable/active |
| MSR | Model-Specific Register | `rdmsr`/`wrmsr` |
| RFLAGS | x86_64 flags register | CF/ZF/SF/OF/AF/PF + IF/DF |
| PE / PG / PAE | Protection Enable / Paging / Physical Address Extension | CR0/CR4 mode bits |
| LMA / LME | Long Mode Active / Enable | EFER bits |
| ModR/M, SIB | Mod-Reg-R/M, Scale-Index-Base | x86 instruction operand-encoding bytes |
| REX | Register EXtension prefix | x86_64 64-bit / extended-reg prefix |
| x87 / FPU | x87 Floating-Point Unit | legacy stack FP |
| MMX / SSE / SSE2 / AVX | SIMD extensions | packed integer/float |
| TSX | Transactional Sync eXtensions | XBEGIN/XABORT (stubbed) |
| PIC | Programmable Interrupt Controller | the 8259 pair |
| PIT | Programmable Interval Timer | the 8254 (IRQ0) |
| APIC / LAPIC / IOAPIC | Advanced PIC / Local APIC / I/O APIC | modern interrupt routing (`-apic`) |
| RTC / CMOS | Real-Time Clock / CMOS NVRAM | IRQ8, time + config bytes |
| UART / COM1 | serial controller / first serial port | 16550-style at 0x3F8 |
| FIFO | First-In-First-Out (buffer) | UART RX/TX queues |
| ATA / ATAPI / IDE / AHCI | disk interfaces | mostly bypassed via virtio-blk |
| FDC | Floppy Disk Controller | used for floppy-only images |

## ACPI / Firmware

| Abbr | Expansion | Notes |
|------|-----------|-------|
| ACPI | Advanced Configuration & Power Interface | |
| RSDP / RSDT / XSDT | Root System Description Pointer / Table / eXtended SDT | ACPI table roots |
| FADT / MADT / DSDT / HPET | Fixed ACPI Desc / Multiple APIC Desc / Differentiated System Desc / High-Precision Event Timer | ACPI tables |
| _PRT | PCI Routing Table | AML method mapping PCI INTx → GSI |
| BIOS | Basic Input/Output System | SeaBIOS here |
| UEFI / OVMF | Unified EFI / Open Virtual Machine Firmware | edk2-based UEFI build |
| fw_cfg | QEMU firmware-config channel | how SeaBIOS/UEFI fetch tables, cmdline |
| CFI | Common Flash Interface | NOR-flash protocol (UEFI pflash) |
| PSCI | Power State Coordination Interface | ARM64 CPU on/off/reset via SMC |
| DTB / FDT | Device Tree Blob / Flattened Device Tree | hardware description for ARM64/RISC-V |

## PCI

| Abbr | Expansion | Notes |
|------|-----------|-------|
| PCI | Peripheral Component Interconnect | |
| BAR | Base Address Register | a device's MMIO/PIO window |
| ECAM | Enhanced Configuration Access Mechanism | MMIO PCI config space |

## ARM64 / AArch64

| Abbr | Expansion | Notes |
|------|-----------|-------|
| EL0–EL3 | Exception Levels | user / kernel / hypervisor / secure-monitor |
| PSTATE | Processor State | the AArch64 condition/control flags |
| NZCV | Negative/Zero/Carry/oVerflow | condition flags |
| DAIF | Debug/SError/IRQ/FIQ mask bits | interrupt masking in PSTATE |
| SPSR / ELR | Saved Program Status Reg / Exception Link Reg | saved on exception entry |
| VBAR | Vector Base Address Register | exception-vector table base |
| ESR / FAR | Exception Syndrome / Fault Address Register | why/where a fault happened |
| SVC / HVC / SMC | SuperVisor / HyperVisor / Secure-Monitor Call | syscall-style traps |
| ERET | Exception RETurn | |
| WFI / WFE | Wait For Interrupt / Event | low-power park (drives idle fast-forward) |
| SEV | Send EVent | wakes WFE |
| GIC / GICv2 | Generic Interrupt Controller (v2) | ARM64 interrupt controller |
| SGI / PPI / SPI | Software-Generated / Private-Peripheral / Shared-Peripheral Interrupt | GIC interrupt classes (timer=PPI, virtio=SPI) |
| NEON / SIMD | Advanced SIMD | packed vector ops |
| VFP / FPCR / FPSR | Vector FP / FP Control / FP Status Register | scalar/vector FP config + flags |
| CNTPCT / CNTVCT | Physical / Virtual Counter | generic-timer counter (= retired-instruction count here) |
| CNTP_CVAL / CNTV_CVAL | Physical / Virtual Compare Value | timer fires when counter ≥ CVAL |
| CNTP_CTL / CNTV_CTL | timer Control (enable/imask/istatus) | |
| CNTFRQ | Counter Frequency | advertised timer rate |
| TVAL | Timer Value | signed countdown offset alias for CVAL |
| PL011 | ARM PrimeCell UART | the `virt` board serial port (ttyAMA0) |
| DC ZVA | Data Cache Zero by VA | zeroes a cache line |
| STTR / STNP / LDP / STP | unprivileged store / non-temporal pair / load-pair / store-pair | |

## RISC-V

| Abbr | Expansion | Notes |
|------|-----------|-------|
| HART | HARdware Thread | a RISC-V core |
| CSR | Control & Status Register | |
| SBI | Supervisor Binary Interface | firmware call layer |
| PLIC / CLINT | Platform-Level IC / Core-Local Interruptor | interrupt + timer controllers |
| MSTATUS / MEPC / MCAUSE | machine status / exc PC / exc cause | trap CSRs |

## virtio / Networking

| Abbr | Expansion | Notes |
|------|-----------|-------|
| virtio | virtual-I/O paravirtual device framework | blk / net / console |
| virtqueue / vring | the shared-memory ring a virtio device uses | |
| desc ring | Descriptor table | buffer descriptors (addr/len/flags/next) |
| avail ring | Available ring | driver→device: buffers offered |
| used ring | Used ring | device→driver: completed buffers (id+len) |
| MRG_RXBUF | Mergeable RX buffers | virtio-net feature |
| EVENT_IDX | used/avail event-index | interrupt/notify suppression (not offered) |
| slirp | user-mode TCP/IP stack | `-net-user` host networking |
| hostfwd | host port forwarding | expose a guest port on the host |
| NIC | Network Interface Controller | |
| OOB / URG | Out-Of-Band / Urgent (TCP) | |
| MTU | Maximum Transmission Unit | |

## Boot / Filesystem / OS

| Abbr | Expansion | Notes |
|------|-----------|-------|
| ELF | Executable and Linkable Format | kernel/userspace binary format |
| MBR | Master Boot Record | disk sector 0 + boot code |
| GRUB | GRand Unified Bootloader | OpenWrt x86-64 uses BIOS-GRUB |
| LZMA | Lempel-Ziv-Markov chain Algorithm | GRUB core.img compression |
| RS | Reed-Solomon | GRUB core.img error correction |
| initrd / initramfs | initial RAM disk / filesystem | early-userspace image |
| rootfs | root filesystem | |
| squashfs / ext4 / cd9660 (ISO9660) | filesystems | cd9660 = the CD/ISO filesystem |
| procd / init / getty | OpenWrt process daemon / PID 1 / terminal login | |
| OpenRC | Alpine init system | |
| GEOM | FreeBSD storage framework | tastes/labels disks |
| callout | FreeBSD timer-wheel deferred-work mechanism | driven by the timer tick |

---

*Add new entries here as they come up — keep it one row per abbreviation, with
the expansion and a one-line "where it bites in this codebase" note.*
