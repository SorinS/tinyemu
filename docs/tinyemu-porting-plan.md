# TinyEMU RISC-V to Go Porting Plan

## Overview

This document outlines a plan to port the RISC-V emulation components of TinyEMU to pure Go (with optional assembly for performance-critical paths). The goal is a single statically-linked Go binary that provides full RISC-V system emulation capable of booting Linux.

### Project Goals

1. **Pure Go implementation** - No cgo dependencies, single static binary
2. **Feature parity** - Support RV32/RV64 IMAFDC instruction sets
3. **Linux boot capability** - Boot full Linux distributions with VirtIO devices
4. **SLIRP user-mode networking** - Booted Linux distributions will be able to reach the internet
5. **Maintainability** - Clean, idiomatic Go code with good test coverage
6. **RV128 support**

### Non-Goals (Initial Release)

- x86/KVM emulation (Linux-specific, requires kernel interface)
- Emscripten/WebAssembly target (future consideration)

---

## Source Analysis

### TinyEMU Codebase Statistics

| Category | Files | Lines of C | Purpose |
|----------|-------|------------|---------|
| RISC-V CPU | 4 | 3,713 | Core instruction emulation |
| Software FP | 4 | 3,551 | IEEE754 floating-point |
| Memory/IO | 2 | 412 | Physical memory mapping |
| RISC-V Machine | 1 | 1,053 | Platform devices (CLINT, PLIC, HTIF) |
| VirtIO | 2 | 2,796 | Virtual device framework |
| Machine Framework | 2 | 836 | Configuration, abstraction |
| Utilities | 4 | 778 | JSON, helpers |
| Main/UI | 2 | 1,110 | Entry point, terminal handling |
| SLIRP Networking | 30+ | 9,975 | User-mode TCP/IP |
| **Total** | **51+** | **24,224** | |

### Key Source Files

```
riscv_cpu.c          (1,377 lines) - CPU state, TLB, CSR handling, memory ops
riscv_cpu_template.h (1,739 lines) - Instruction decode/execute (included 3x for RV32/64/128)
riscv_cpu_fp_template.h (304 lines) - FP instruction implementations
riscv_cpu_priv.h       (293 lines) - Privileged architecture constants
riscv_machine.c      (1,053 lines) - RISC-V platform (CLINT, PLIC, HTIF, device init)
softfp.c             (2,070 lines) - Software FP core
softfp_template.h    (1,129 lines) - FP operations (included 3x for F32/64/128)
virtio.c             (2,650 lines) - VirtIO queue handling, block/net/console/9p devices
iomem.c                (264 lines) - Physical memory range management
machine.c              (640 lines) - Machine configuration, JSON loading
temu.c                 (835 lines) - Main loop, terminal I/O, SDL integration
```

---

## Technical Challenges

### 1. C Preprocessor Template Expansion

**Problem:** TinyEMU uses C preprocessor macros extensively to generate type-specific code:

```c
// riscv_cpu.c includes the template 3 times with different XLEN values
#define XLEN 32
#include "riscv_cpu_template.h"
#define XLEN 64
#include "riscv_cpu_template.h"
#define XLEN 128
#include "riscv_cpu_template.h"
```

The template contains ~1,700 lines of instruction decode/execute logic that gets compiled three times with different integer widths.

**Go Solution Options:**

| Approach | Pros | Cons |
|----------|------|------|
| **Go Generics** | Type-safe, single source | Limited to Go 1.18+, some operations awkward |
| **Code Generation** | Full control, optimizable | Maintenance burden, generated code in repo |
| **Manual Expansion** | Simple, explicit | Code duplication, 3x maintenance |
| **Interface + Type Switch** | Flexible | Runtime overhead, less optimizable |

**Recommendation:** Hybrid approach:
- Use Go generics for register file operations and type-safe arithmetic
- Use code generation (`go generate`) for instruction decode tables
- Keep RV32/RV64 as separate execution paths for performance

```go
// Example: Generic register operations
type UintX interface {
    uint32 | uint64
}

type CPU[T UintX] struct {
    reg [32]T
    pc  T
    // ...
}

func (c *CPU[T]) executeADD(rd, rs1, rs2 int) {
    c.reg[rd] = c.reg[rs1] + c.reg[rs2]
}
```

### 2. 128-bit Integer Support

**Problem:** C code uses GCC's `__int128` extension for RV128 and some FP operations. Go has no native 128-bit integer type.

**Go Solution:**

Use `lukechampine.com/uint128` which provides a well-tested implementation.

### 3. Software Floating-Point (IEEE754)

**Problem:** RISC-V requires exact IEEE754 semantics with configurable rounding modes and exception flags. Go's `math` package doesn't expose these details.

**Source:** `softfp.c` + `softfp_template.h` (~3,500 lines) implements:
- Pack/unpack floating-point bit representations
- Add, subtract, multiply, divide, square root
- Fused multiply-add (FMA)
- Comparisons with NaN handling
- Integer ↔ float conversions
- Rounding modes: RNE, RTZ, RDN, RUP, RMM
- Exception flags: invalid, divide-by-zero, overflow, underflow, inexact

**Go Implementation:**

```go
// softfp/softfp.go

type RoundingMode int

const (
    RoundNearestEven RoundingMode = iota  // RNE
    RoundToZero                            // RTZ
    RoundDown                              // RDN
    RoundUp                                // RUP
    RoundNearestMax                        // RMM
)

type ExceptionFlags uint32

const (
    FlagInvalidOp   ExceptionFlags = 1 << 0
    FlagDivByZero   ExceptionFlags = 1 << 1
    FlagOverflow    ExceptionFlags = 1 << 2
    FlagUnderflow   ExceptionFlags = 1 << 3
    FlagInexact     ExceptionFlags = 1 << 4
)

// F32 operations
func AddF32(a, b uint32, rm RoundingMode, flags *ExceptionFlags) uint32
func MulF32(a, b uint32, rm RoundingMode, flags *ExceptionFlags) uint32
func DivF32(a, b uint32, rm RoundingMode, flags *ExceptionFlags) uint32
func SqrtF32(a uint32, rm RoundingMode, flags *ExceptionFlags) uint32
func FmaF32(a, b, c uint32, rm RoundingMode, flags *ExceptionFlags) uint32

// F64 operations (same pattern)
// F128 operations (uses Uint128)
```

**Testing:** Use RISC-V floating-point compliance tests to verify correctness.

### 4. Memory Access Patterns

**Problem:** C code uses direct pointer arithmetic and casting for memory-mapped I/O:

```c
// C: Direct memory access
*(uint32_t *)(pr->phys_mem + offset) = val;
val = *(uint32_t *)(pr->phys_mem + offset);
```

**Go Solutions:**

```go
// Option 1: encoding/binary (safe, slower)
binary.LittleEndian.PutUint32(mem[offset:], val)
val := binary.LittleEndian.Uint32(mem[offset:])

// Option 2: unsafe (fast, requires care)
*(*uint32)(unsafe.Pointer(&mem[offset])) = val
val := *(*uint32)(unsafe.Pointer(&mem[offset]))

// Option 3: Hybrid with bounds checking in debug mode
func (m *Memory) WriteU32(offset uint64, val uint32) {
    if debugMode && offset+4 > uint64(len(m.data)) {
        panic("memory write out of bounds")
    }
    *(*uint32)(unsafe.Pointer(&m.data[offset])) = val
}
```

**Recommendation:** Use `unsafe` for the memory hot path with optional bounds checking. Go's `unsafe` is acceptable for emulator memory operations since we're explicitly managing a byte array as typed memory.

### 5. TLB and Page Table Walking

**Problem:** The CPU maintains a software TLB (Translation Lookaside Buffer) for virtual→physical address translation.

**Source Structure:**

```c
// From riscv_cpu_priv.h
#define TLB_SIZE 256
#define PG_SHIFT 12

typedef struct {
    target_ulong vaddr;
    uintptr_t mem_addend;  // Allows direct pointer computation
} TLBEntry;

// Three TLBs: read, write, code
TLBEntry tlb_read[TLB_SIZE];
TLBEntry tlb_write[TLB_SIZE];
TLBEntry tlb_code[TLB_SIZE];
```

**Go Implementation:**

```go
// cpu/mmu.go

const (
    TLBSize  = 256
    PageShift = 12
    PageSize  = 1 << PageShift
    PageMask  = PageSize - 1
)

type TLBEntry struct {
    VAddr    uint64  // Virtual address (page-aligned)
    MemBase  []byte  // Backing memory slice
    PAddr    uint64  // Physical address for this mapping
}

type MMU struct {
    tlbRead  [TLBSize]TLBEntry
    tlbWrite [TLBSize]TLBEntry
    tlbCode  [TLBSize]TLBEntry

    mem      *PhysMemoryMap
    satp     uint64  // Page table base register
}

func (m *MMU) Translate(vaddr uint64, access AccessType) (paddr uint64, mem []byte, err error) {
    tlbIdx := (vaddr >> PageShift) & (TLBSize - 1)

    var tlb *[TLBSize]TLBEntry
    switch access {
    case AccessRead:
        tlb = &m.tlbRead
    case AccessWrite:
        tlb = &m.tlbWrite
    case AccessCode:
        tlb = &m.tlbCode
    }

    // TLB hit?
    if tlb[tlbIdx].VAddr == vaddr & ^uint64(PageMask) {
        offset := vaddr & PageMask
        return tlb[tlbIdx].PAddr + offset, tlb[tlbIdx].MemBase[offset:], nil
    }

    // TLB miss - walk page table
    return m.walkPageTable(vaddr, access)
}
```

### 6. Interrupt and Exception Handling

**Problem:** RISC-V has a complex privilege model with M/S/U modes and delegatable interrupts.

**Key Components:**
- Machine-mode interrupt enable (MIE) and pending (MIP)
- Interrupt delegation (mideleg, medeleg)
- Trap vectors (mtvec, stvec)
- Exception causes and handling

**Go Implementation:**

```go
// cpu/trap.go

type PrivilegeLevel int

const (
    PrivUser       PrivilegeLevel = 0
    PrivSupervisor PrivilegeLevel = 1
    PrivMachine    PrivilegeLevel = 3
)

type ExceptionCause uint64

const (
    CauseInsnAddrMisalign ExceptionCause = 0
    CauseInsnAccessFault  ExceptionCause = 1
    CauseIllegalInsn      ExceptionCause = 2
    CauseBreakpoint       ExceptionCause = 3
    CauseLoadAddrMisalign ExceptionCause = 4
    CauseLoadAccessFault  ExceptionCause = 5
    // ... etc

    CauseInterrupt        ExceptionCause = 1 << 63  // MSB set for interrupts
)

func (c *CPU) RaiseException(cause ExceptionCause, tval uint64) {
    // Determine if exception should be delegated to S-mode
    deleg := false
    if c.priv <= PrivSupervisor {
        if cause&CauseInterrupt != 0 {
            deleg = (c.mideleg >> (cause & 0x3F) & 1) != 0
        } else {
            deleg = (c.medeleg >> cause & 1) != 0
        }
    }

    if deleg {
        c.scause = cause
        c.sepc = c.pc
        c.stval = tval
        // Update mstatus.SPIE, mstatus.SPP
        c.priv = PrivSupervisor
        c.pc = c.stvec
    } else {
        c.mcause = cause
        c.mepc = c.pc
        c.mtval = tval
        // Update mstatus.MPIE, mstatus.MPP
        c.priv = PrivMachine
        c.pc = c.mtvec
    }
}
```

---

## Go Package Architecture

**Module:** `github.com/sorins/tinyemu-go`

The module is structured as a library with packages at the root level. This allows importing as `github.com/sorins/tinyemu-go/cpu`, `github.com/sorins/tinyemu-go/mem`, etc.

```
tinyemu-go/
├── cmd/
│   └── temu/
│       └── main.go                 # CLI entry point
│
├── cpu/
│   ├── cpu.go                      # RISCVCPUState, main CPU interface
│   ├── cpu_test.go                 # CPU unit tests
│   ├── decode.go                   # Instruction decoder
│   ├── exec.go                     # Common execution helpers
│   ├── exec_rv32.go                # RV32-specific execution
│   ├── exec_rv64.go                # RV64-specific execution
│   ├── csr.go                      # CSR read/write operations
│   ├── csr_test.go
│   ├── mmu.go                      # TLB and page table walk
│   ├── mmu_test.go
│   ├── trap.go                     # Exception/interrupt handling
│   └── fp.go                       # FP instruction dispatch
│
├── softfp/
│   ├── softfp.go                   # Common types and helpers
│   ├── f32.go                      # 32-bit float operations
│   ├── f32_test.go
│   ├── f64.go                      # 64-bit float operations
│   ├── f64_test.go
│   ├── f128.go                     # 128-bit float operations
│   └── convert.go                  # Float ↔ int conversions
│
├── machine/
│   ├── machine.go                  # VirtMachine interface
│   ├── riscv.go                    # RISC-V machine implementation
│   ├── clint.go                    # Core Local Interruptor (timer)
│   ├── plic.go                     # Platform-Level Interrupt Controller
│   ├── htif.go                     # Host-Target Interface
│   └── config.go                   # JSON configuration loader
│
├── mem/
│   ├── physmem.go                  # PhysMemoryMap, range management
│   ├── physmem_test.go
│   └── iomem.go                    # Memory-mapped I/O interface
│
├── virtio/
│   ├── virtio.go                   # VirtIO queue handling (virtqueue)
│   ├── virtio_test.go
│   ├── mmio.go                     # VirtIO MMIO transport
│   ├── block.go                    # VirtIO block device
│   ├── console.go                  # VirtIO console
│   ├── net.go                      # VirtIO network
│   ├── input.go                    # VirtIO input (keyboard/mouse)
│   └── p9.go                       # VirtIO 9P filesystem
│
├── device/
│   ├── simplefb.go                 # Simple framebuffer
│   └── block.go                    # Block device interface
│
├── fs/
│   ├── fs.go                       # Filesystem interface
│   ├── disk.go                     # Disk-backed filesystem
│   └── p9fs.go                     # 9P protocol implementation
│
├── net/
│   ├── tap.go                      # TAP network backend (Linux)
│   └── slirp.go                    # User-mode networking
│
├── internal/
│   └── gen/
│       └── decode_gen.go           # Instruction table generator
│
├── testdata/
│   ├── compliance/                 # RISC-V compliance test binaries
│   └── images/                     # Test disk images
│
├── go.mod
├── go.sum
└── Makefile
```

---

## Implementation Phases

### Phase 1: Minimal Boot (MVP)

**Goal:** Boot Linux kernel to shell prompt via serial console

**Duration:** 4-6 weeks

#### 1.1 Memory Subsystem (`mem/`)

```go
// physmem.go

const MaxMemRanges = 32

type PhysMemoryRange struct {
    Addr     uint64
    Size     uint64
    IsRAM    bool
    PhysMem  []byte          // Backing memory for RAM
    ReadFn   DeviceReadFunc  // For MMIO
    WriteFn  DeviceWriteFunc
    Opaque   interface{}
}

type PhysMemoryMap struct {
    ranges    [MaxMemRanges]PhysMemoryRange
    numRanges int
}

func NewPhysMemoryMap() *PhysMemoryMap
func (m *PhysMemoryMap) RegisterRAM(addr, size uint64) ([]byte, error)
func (m *PhysMemoryMap) RegisterDevice(addr, size uint64, read DeviceReadFunc, write DeviceWriteFunc, opaque interface{}) error
func (m *PhysMemoryMap) GetRange(addr uint64) *PhysMemoryRange
func (m *PhysMemoryMap) Read(addr uint64, size int) (uint64, error)
func (m *PhysMemoryMap) Write(addr uint64, val uint64, size int) error
```

**Tasks:**
- [ ] Implement `PhysMemoryMap` with range registration
- [ ] Implement RAM allocation and backing store
- [ ] Implement device I/O dispatch
- [ ] Add read/write methods for 8/16/32/64-bit access
- [ ] Unit tests for memory operations

#### 1.2 CPU Core - Integer Instructions (`cpu/`)

```go
// cpu.go

type CPU struct {
    // Integer registers
    reg [32]uint64
    pc  uint64

    // Privilege level
    priv PrivilegeLevel

    // CSRs (Control and Status Registers)
    mstatus, misa, mie, mip       uint64
    mtvec, mscratch, mepc, mcause uint64
    mtval, medeleg, mideleg       uint64
    mcounteren                    uint64

    // S-mode CSRs
    sstatus, sie, sip             uint64
    stvec, sscratch, sepc, scause uint64
    stval, satp, scounteren       uint64

    // Counter
    insnCounter uint64

    // MMU
    mmu *MMU

    // Memory map
    mem *PhysMemoryMap

    // Current XLEN (32 or 64)
    xlen int

    // Pending exception
    pendingException int
    pendingTval      uint64

    // Power down flag (WFI)
    powerDown bool
}

func NewCPU(mem *PhysMemoryMap, xlen int) *CPU
func (c *CPU) Step() error                    // Execute one instruction
func (c *CPU) Run(cycles int) error           // Execute multiple cycles
func (c *CPU) SetMIP(mask uint32)             // Set interrupt pending
func (c *CPU) ResetMIP(mask uint32)           // Clear interrupt pending
func (c *CPU) GetMIP() uint32
```

**RV64I Instructions to Implement:**

| Category | Instructions |
|----------|-------------|
| Arithmetic | ADD, SUB, ADDI, ADDW, SUBW, ADDIW |
| Logical | AND, OR, XOR, ANDI, ORI, XORI |
| Shift | SLL, SRL, SRA, SLLI, SRLI, SRAI, SLLW, SRLW, SRAW, SLLIW, SRLIW, SRAIW |
| Compare | SLT, SLTU, SLTI, SLTIU |
| Load | LB, LH, LW, LD, LBU, LHU, LWU |
| Store | SB, SH, SW, SD |
| Branch | BEQ, BNE, BLT, BGE, BLTU, BGEU |
| Jump | JAL, JALR |
| Upper Imm | LUI, AUIPC |
| System | ECALL, EBREAK, FENCE, FENCE.I |
| CSR | CSRRW, CSRRS, CSRRC, CSRRWI, CSRRSI, CSRRCI |
| Privilege | MRET, SRET, WFI, SFENCE.VMA |

**Tasks:**
- [ ] Implement CPU state structure
- [ ] Implement instruction fetch with TLB
- [ ] Implement instruction decoder (opcode extraction)
- [ ] Implement RV64I integer instructions
- [ ] Implement CSR read/write operations
- [ ] Implement exception raising
- [ ] Implement interrupt checking
- [ ] Implement privilege mode transitions
- [ ] Unit tests for each instruction category

#### 1.3 MMU and Page Table Walk (`cpu/mmu.go`)

**Supported modes:**
- Bare (no translation, M-mode)
- Sv39 (39-bit virtual address, 3-level page table)
- Sv48 (48-bit virtual address, 4-level page table)

```go
// mmu.go

type AccessType int

const (
    AccessRead AccessType = iota
    AccessWrite
    AccessExecute
)

type MMU struct {
    cpu      *CPU
    tlbRead  [TLBSize]TLBEntry
    tlbWrite [TLBSize]TLBEntry
    tlbCode  [TLBSize]TLBEntry
}

func (m *MMU) Translate(vaddr uint64, access AccessType) (paddr uint64, err error)
func (m *MMU) WalkPageTable(vaddr uint64, access AccessType) (paddr uint64, err error)
func (m *MMU) FlushTLB()
func (m *MMU) FlushTLBEntry(vaddr uint64)
```

**Tasks:**
- [ ] Implement TLB structure and lookup
- [ ] Implement Sv39 page table walk
- [ ] Implement Sv48 page table walk (optional for MVP)
- [ ] Implement access permission checking
- [ ] Implement dirty/accessed bit updates
- [ ] Unit tests with known page table structures

#### 1.4 RISC-V Machine Platform (`machine/`)

**Memory Map:**
```
0x00000000 - 0x0000FFFF : Low RAM (64KB, boot code)
0x02000000 - 0x020BFFFF : CLINT (Core Local Interruptor)
0x40008000 - 0x40008FFF : HTIF (Host-Target Interface)
0x40010000 - 0x40010FFF : VirtIO device 0
0x40011000 - 0x40011FFF : VirtIO device 1
...
0x40100000 - 0x404FFFFF : PLIC (Platform-Level Interrupt Controller)
0x80000000 - ...        : Main RAM
```

**CLINT (Core Local Interruptor):**
```go
// clint.go

type CLINT struct {
    machine  *RISCVMachine
    mtime    uint64  // Machine timer
    mtimecmp uint64  // Timer compare
}

func (c *CLINT) Read(offset uint32, sizeLog2 int) uint32
func (c *CLINT) Write(offset uint32, val uint32, sizeLog2 int)
func (c *CLINT) Tick()  // Called periodically to update timer
```

**PLIC (Platform-Level Interrupt Controller):**
```go
// plic.go

type PLIC struct {
    machine     *RISCVMachine
    pending     uint32      // Pending interrupts (bitmap)
    served      uint32      // Currently served interrupt
    priority    [32]uint32  // Interrupt priorities
    enable      [32]uint32  // Enable bits per context
    threshold   [2]uint32   // Threshold per context (M/S mode)
}

func (p *PLIC) Read(offset uint32, sizeLog2 int) uint32
func (p *PLIC) Write(offset uint32, val uint32, sizeLog2 int)
func (p *PLIC) SetPending(irq int)
func (p *PLIC) GetPending() uint32
```

**Tasks:**
- [ ] Implement CLINT with timer and software interrupt
- [ ] Implement PLIC with interrupt routing
- [ ] Implement HTIF for console I/O
- [ ] Implement machine initialization and memory layout
- [ ] Wire up interrupt delivery to CPU

#### 1.5 VirtIO Console (`virtio/`)

```go
// virtio.go

type VirtQueue struct {
    desc      []VirtQueueDesc  // Descriptor table
    avail     *VirtQueueAvail  // Available ring
    used      *VirtQueueUsed   // Used ring
    queueSize uint16
    lastAvail uint16

    // Memory for queue access
    mem       *PhysMemoryMap
    descAddr  uint64
    availAddr uint64
    usedAddr  uint64
}

type VirtIODevice interface {
    GetDeviceID() uint32
    GetFeatures() uint64
    SetFeatures(features uint64)
    GetConfig(offset int) uint32
    SetConfig(offset int, val uint32)
    ProcessQueue(queueIdx int)
}

// console.go

type VirtIOConsole struct {
    base      VirtIOBase
    rxQueue   *VirtQueue  // Receive queue
    txQueue   *VirtQueue  // Transmit queue

    // Host I/O
    readData  func(buf []byte) int
    writeData func(buf []byte)
}

func NewVirtIOConsole(mem *PhysMemoryMap, addr uint64, irq *IRQSignal) *VirtIOConsole
func (c *VirtIOConsole) CanWriteData() bool
func (c *VirtIOConsole) WriteData(buf []byte) int  // Host → Guest
func (c *VirtIOConsole) ProcessTx()                // Guest → Host
```

**Tasks:**
- [ ] Implement VirtIO MMIO transport
- [ ] Implement VirtQueue descriptor parsing
- [ ] Implement available/used ring handling
- [ ] Implement VirtIO console device
- [ ] Wire console to stdin/stdout
- [ ] Test with Linux virtio-console driver

#### 1.6 Main Emulator Loop (`cmd/temu/`)

```go
// main.go

func main() {
    // Parse arguments
    cfg := parseConfig()

    // Create machine
    machine, err := machine.NewRISCVMachine(cfg)
    if err != nil {
        log.Fatal(err)
    }

    // Load kernel/BIOS
    if err := machine.LoadImage(cfg.BIOSPath, 0x1000); err != nil {
        log.Fatal(err)
    }
    if cfg.KernelPath != "" {
        if err := machine.LoadKernel(cfg.KernelPath); err != nil {
            log.Fatal(err)
        }
    }

    // Set up terminal
    term := setupTerminal()
    defer term.Restore()

    // Main loop
    for !machine.PoweredDown() {
        // Run CPU for a time slice
        machine.Run(10000)

        // Poll devices
        machine.PollDevices()

        // Handle terminal I/O
        handleTerminalIO(machine, term)
    }
}
```

**Tasks:**
- [ ] Implement command-line argument parsing
- [ ] Implement JSON configuration loading
- [ ] Implement kernel/BIOS image loading
- [ ] Implement terminal raw mode handling
- [ ] Implement main emulation loop
- [ ] Test booting Linux to shell

#### Phase 1 Deliverables

- [ ] Boot RISC-V Linux kernel with BusyBox
- [ ] Interactive shell via serial console
- [ ] Basic system calls working
- [ ] ~6,000 lines of Go code

---

### Phase 2: Full ISA Support

**Goal:** Complete RISC-V instruction set, pass compliance tests

**Duration:** 3-4 weeks

#### 2.1 M Extension (Multiply/Divide)

```go
// Instructions: MUL, MULH, MULHSU, MULHU, DIV, DIVU, REM, REMU
// Also: MULW, DIVW, DIVUW, REMW, REMUW (RV64)

func (c *CPU) execMUL(rd, rs1, rs2 int) {
    c.reg[rd] = c.reg[rs1] * c.reg[rs2]
}

func (c *CPU) execMULH(rd, rs1, rs2 int) {
    // High 64 bits of signed 64×64 → 128
    a := int64(c.reg[rs1])
    b := int64(c.reg[rs2])
    hi, _ := bits.Mul64(uint64(a), uint64(b))
    // Handle signs...
    c.reg[rd] = hi
}

func (c *CPU) execDIV(rd, rs1, rs2 int) {
    if c.reg[rs2] == 0 {
        c.reg[rd] = ^uint64(0)  // -1
    } else if int64(c.reg[rs1]) == math.MinInt64 && int64(c.reg[rs2]) == -1 {
        c.reg[rd] = c.reg[rs1]  // Overflow case
    } else {
        c.reg[rd] = uint64(int64(c.reg[rs1]) / int64(c.reg[rs2]))
    }
}
```

**Tasks:**
- [ ] Implement MUL, MULH, MULHSU, MULHU
- [ ] Implement DIV, DIVU, REM, REMU
- [ ] Implement W variants for RV64
- [ ] Handle edge cases (divide by zero, overflow)
- [ ] Unit tests

#### 2.2 A Extension (Atomics)

```go
// Load-Reserved / Store-Conditional
func (c *CPU) execLR_D(rd, rs1 int, aq, rl bool) error {
    addr := c.reg[rs1]
    val, err := c.loadU64(addr)
    if err != nil {
        return err
    }
    c.reg[rd] = val
    c.reservation = addr
    c.reservationValid = true
    return nil
}

func (c *CPU) execSC_D(rd, rs1, rs2 int, aq, rl bool) error {
    addr := c.reg[rs1]
    if c.reservationValid && c.reservation == addr {
        if err := c.storeU64(addr, c.reg[rs2]); err != nil {
            return err
        }
        c.reg[rd] = 0  // Success
    } else {
        c.reg[rd] = 1  // Failure
    }
    c.reservationValid = false
    return nil
}

// Atomic Memory Operations (AMO)
func (c *CPU) execAMOADD_D(rd, rs1, rs2 int, aq, rl bool) error {
    addr := c.reg[rs1]
    val, err := c.loadU64(addr)
    if err != nil {
        return err
    }
    c.reg[rd] = val
    return c.storeU64(addr, val + c.reg[rs2])
}
```

**AMO Instructions:** AMOSWAP, AMOADD, AMOAND, AMOOR, AMOXOR, AMOMAX, AMOMIN, AMOMAXU, AMOMINU

**Tasks:**
- [ ] Implement LR.W, LR.D
- [ ] Implement SC.W, SC.D
- [ ] Implement all AMO operations
- [ ] Handle acquire/release ordering (aq, rl bits)
- [ ] Unit tests

#### 2.3 Software Floating-Point (`softfp/`)

Port the complete softfp implementation:

```go
// f32.go

const (
    F32MantSize = 23
    F32ExpSize  = 8
    F32ExpMask  = (1 << F32ExpSize) - 1
    F32MantMask = (1 << F32MantSize) - 1
    F32SignMask = 1 << 31
    F32QNaN     = 0x7FC00000
)

func packF32(sign uint32, exp int32, mant uint32) uint32 {
    return (sign << 31) | (uint32(exp) << F32MantSize) | (mant & F32MantMask)
}

func unpackF32(a uint32) (sign uint32, exp int32, mant uint32) {
    sign = a >> 31
    exp = int32((a >> F32MantSize) & F32ExpMask)
    mant = a & F32MantMask
    return
}

func AddF32(a, b uint32, rm RoundingMode, flags *ExceptionFlags) uint32 {
    // ... ~100 lines of IEEE754 addition logic
}

func MulF32(a, b uint32, rm RoundingMode, flags *ExceptionFlags) uint32 {
    // ... ~80 lines of IEEE754 multiplication logic
}

func DivF32(a, b uint32, rm RoundingMode, flags *ExceptionFlags) uint32 {
    // ... ~100 lines of IEEE754 division logic
}

func SqrtF32(a uint32, rm RoundingMode, flags *ExceptionFlags) uint32 {
    // ... ~80 lines of square root logic
}

func FmaF32(a, b, c uint32, rm RoundingMode, flags *ExceptionFlags) uint32 {
    // Fused multiply-add: (a * b) + c with single rounding
    // ... ~150 lines
}
```

**Operations to implement for F32, F64, F128:**
- Add, Sub, Mul, Div, Sqrt
- FMA (Fused Multiply-Add)
- Min, Max (with NaN handling)
- Comparisons (EQ, LT, LE)
- Sign injection (FSGNJ, FSGNJN, FSGNJX)
- Classification (FCLASS)
- Conversions (FCVT between sizes, FCVT to/from integer)

**Tasks:**
- [ ] Port F32 operations
- [ ] Port F64 operations
- [ ] Port F128 operations (if supporting Q extension)
- [ ] Port integer ↔ float conversions
- [ ] Implement all rounding modes
- [ ] Implement exception flag handling
- [ ] Extensive testing against reference implementation

#### 2.4 F/D Extensions

```go
// fp.go

// FP register file (64 entries, upper bits for NaN boxing)
type FPRegFile struct {
    reg [32]uint64
}

func (f *FPRegFile) GetF32(idx int) uint32 {
    // NaN-boxing check
    if f.reg[idx] >> 32 != 0xFFFFFFFF {
        return F32QNaN  // Return canonical NaN if not properly boxed
    }
    return uint32(f.reg[idx])
}

func (f *FPRegFile) SetF32(idx int, val uint32) {
    f.reg[idx] = 0xFFFFFFFF00000000 | uint64(val)  // NaN-box
}

func (f *FPRegFile) GetF64(idx int) uint64 {
    return f.reg[idx]
}

func (f *FPRegFile) SetF64(idx int, val uint64) {
    f.reg[idx] = val
}
```

**F Extension Instructions:**
- FLW, FSW (load/store)
- FADD.S, FSUB.S, FMUL.S, FDIV.S, FSQRT.S
- FMADD.S, FMSUB.S, FNMADD.S, FNMSUB.S
- FMIN.S, FMAX.S
- FCVT.W.S, FCVT.WU.S, FCVT.S.W, FCVT.S.WU
- FCVT.L.S, FCVT.LU.S, FCVT.S.L, FCVT.S.LU (RV64)
- FMV.X.W, FMV.W.X
- FEQ.S, FLT.S, FLE.S
- FCLASS.S
- FSGNJ.S, FSGNJN.S, FSGNJX.S

**D Extension:** Same pattern with .D suffix for double precision

**Tasks:**
- [ ] Implement FP register file with NaN-boxing
- [ ] Implement F extension instructions
- [ ] Implement D extension instructions
- [ ] Wire up to softfp library
- [ ] Handle FP CSRs (frm, fflags, fcsr)
- [ ] Unit tests

#### 2.5 C Extension (Compressed Instructions)

16-bit instruction formats that expand to 32-bit equivalents:

```go
// decode.go

func (c *CPU) decodeCompressed(insn uint16) (expanded uint32, err error) {
    quadrant := insn & 0x3
    funct3 := (insn >> 13) & 0x7

    switch quadrant {
    case 0:
        return c.decodeC0(insn, funct3)
    case 1:
        return c.decodeC1(insn, funct3)
    case 2:
        return c.decodeC2(insn, funct3)
    default:
        return 0, ErrIllegalInstruction
    }
}

func (c *CPU) decodeC0(insn uint16, funct3 uint16) (uint32, error) {
    switch funct3 {
    case 0: // C.ADDI4SPN
        rd := ((insn >> 2) & 7) + 8
        imm := /* extract immediate */
        // Expand to: ADDI rd', x2, imm
        return encodeI(0x13, rd, 0, 2, imm), nil
    // ... other C0 instructions
    }
}
```

**Tasks:**
- [ ] Implement C extension decoder
- [ ] Map each compressed instruction to base equivalent
- [ ] Handle 16-bit instruction fetch
- [ ] Unit tests for all compressed instructions

#### Phase 2 Deliverables

- [ ] Full RV64IMAFDC support
- [ ] Pass RISC-V ISA compliance tests
- [ ] Run complex userspace programs
- [ ] ~5,000 additional lines of Go code

---

### Phase 3: Storage & Filesystem

**Goal:** Persistent storage for full Linux distribution

**Duration:** 2-3 weeks

#### 3.1 VirtIO Block Device

```go
// block.go

type VirtIOBlock struct {
    base     VirtIOBase
    backend  BlockDevice
    capacity uint64  // In 512-byte sectors
}

type BlockDevice interface {
    GetSectorCount() int64
    ReadSectors(sector uint64, buf []byte, count int) error
    WriteSectors(sector uint64, buf []byte, count int) error
    Flush() error
}

type FileBlockDevice struct {
    file     *os.File
    sectors  int64
    readonly bool
}

func (d *VirtIOBlock) ProcessQueue(queueIdx int) {
    for d.queue.HasAvailable() {
        desc := d.queue.GetDescriptor()

        // Parse block request header
        hdr := parseBlockHeader(desc)

        switch hdr.Type {
        case VIRTIO_BLK_T_IN:  // Read
            data := make([]byte, hdr.SectorCount * 512)
            d.backend.ReadSectors(hdr.Sector, data, hdr.SectorCount)
            d.queue.WriteToGuest(desc.DataAddr, data)

        case VIRTIO_BLK_T_OUT:  // Write
            data := d.queue.ReadFromGuest(desc.DataAddr, hdr.SectorCount * 512)
            d.backend.WriteSectors(hdr.Sector, data, hdr.SectorCount)
        }

        d.queue.PushUsed(desc)
    }
}
```

**Tasks:**
- [ ] Implement BlockDevice interface
- [ ] Implement FileBlockDevice (raw disk image)
- [ ] Implement VirtIO block device
- [ ] Support read/write/flush operations
- [ ] Test with Linux virtio-blk driver

#### 3.2 VirtIO 9P Filesystem

```go
// p9.go

type VirtIO9P struct {
    base     VirtIOBase
    fs       FSDevice
    mountTag string
}

type FSDevice interface {
    Attach(fid uint32, uname, aname string) error
    Walk(fid, newFid uint32, names []string) ([]Qid, error)
    Open(fid uint32, mode uint8) (Qid, uint32, error)
    Read(fid uint32, offset uint64, count uint32) ([]byte, error)
    Write(fid uint32, offset uint64, data []byte) (uint32, error)
    Clunk(fid uint32) error
    Stat(fid uint32) (*Stat, error)
    // ... other 9P operations
}

type HostFSDevice struct {
    root string  // Host directory to expose
    fids map[uint32]*openFile
}
```

**Tasks:**
- [ ] Implement 9P protocol message parsing
- [ ] Implement HostFSDevice for host directory access
- [ ] Implement VirtIO 9P device
- [ ] Test mounting host directories in guest

#### 3.3 Disk Image Support

```go
// disk.go

// Support for split disk images (for HTTP serving)
type SplitBlockDevice struct {
    files   []*os.File
    offsets []int64
}

// Support for compressed images
type CompressedBlockDevice struct {
    // ...
}
```

**Tasks:**
- [ ] Implement split image support
- [ ] Implement image format detection
- [ ] Add disk image creation utility

#### Phase 3 Deliverables

- [ ] Boot from disk images
- [ ] Mount host directories via 9P
- [ ] Full Linux distribution support
- [ ] ~3,000 additional lines of Go code

---

### Phase 4: Networking & Graphics

**Goal:** Full-featured emulation with networking

**Duration:** 2-3 weeks

#### 4.1 VirtIO Network with TAP Backend

```go
// net.go

type VirtIONet struct {
    base     VirtIOBase
    backend  NetBackend
    macAddr  [6]byte
    rxQueue  *VirtQueue
    txQueue  *VirtQueue
}

type NetBackend interface {
    ReadPacket() ([]byte, error)
    WritePacket([]byte) error
    GetMAC() [6]byte
}

// tap.go (Linux only)

type TAPBackend struct {
    fd      int
    name    string
    macAddr [6]byte
}

func NewTAPBackend(name string) (*TAPBackend, error) {
    fd, err := unix.Open("/dev/net/tun", unix.O_RDWR, 0)
    if err != nil {
        return nil, err
    }

    // Configure TAP interface
    var ifr unix.Ifreq
    copy(ifr.Name[:], name)
    ifr.Flags = unix.IFF_TAP | unix.IFF_NO_PI

    if err := unix.IoctlSetIfreq(fd, unix.TUNSETIFF, &ifr); err != nil {
        unix.Close(fd)
        return nil, err
    }

    return &TAPBackend{fd: fd, name: name}, nil
}
```

**Tasks:**
- [ ] Implement VirtIO network device
- [ ] Implement TAP backend (Linux)
- [ ] Handle packet receive/transmit
- [ ] Add MAC address configuration
- [ ] Test with Linux networking


#### Phase 4 Deliverables

- [ ] Network connectivity

---

### Phase 5: Performance & Polish

**Goal:** Production-ready emulator

**Duration:** 1-2 weeks

#### 5.1 Performance Optimization

**Profiling Focus Areas:**
1. Instruction decode (switch vs table lookup)
2. Memory access (TLB hit rate)
3. Floating-point operations
4. VirtIO queue handling

**Optimization Techniques:**
```go
// Use computed goto equivalent (function table)
var opTable = [128]func(*CPU, uint32){
    0x03: (*CPU).execLOAD,
    0x13: (*CPU).execOPIMM,
    0x23: (*CPU).execSTORE,
    0x33: (*CPU).execOP,
    // ...
}

func (c *CPU) Step() error {
    insn := c.fetch()
    opcode := insn & 0x7F
    if handler := opTable[opcode]; handler != nil {
        handler(c, insn)
    } else {
        return ErrIllegalInstruction
    }
    return nil
}

// Inline critical paths
//go:noinline is avoided for hot functions

// Use unsafe for memory access
func (c *CPU) loadU64Fast(addr uint64) uint64 {
    return *(*uint64)(unsafe.Pointer(&c.mem[addr]))
}
```

**Tasks:**
- [ ] Profile with `pprof`
- [ ] Optimize instruction decode path
- [ ] Optimize memory access path
- [ ] Reduce allocations in hot paths
- [ ] Benchmark against C version

#### 5.2 RV32 Support

```go
// Build tags for architecture selection
// +build rv32

// Or runtime selection
func NewCPU(mem *PhysMemoryMap, xlen int) *CPU {
    switch xlen {
    case 32:
        return newCPU32(mem)
    case 64:
        return newCPU64(mem)
    default:
        panic("unsupported XLEN")
    }
}
```

**Tasks:**
- [ ] Implement RV32 instruction variants
- [ ] Test with 32-bit Linux
- [ ] Add build configuration

#### 5.3 Testing & CI

```go
// compliance_test.go

func TestRISCVCompliance(t *testing.T) {
    tests := []string{
        "rv64ui-p-add",
        "rv64ui-p-addi",
        // ... all compliance tests
    }

    for _, test := range tests {
        t.Run(test, func(t *testing.T) {
            cpu := setupTestCPU(t)
            loadTestProgram(cpu, testdata(test))

            result := cpu.RunUntilHalt()
            if result != 0 {
                t.Errorf("test %s failed with code %d", test, result)
            }
        })
    }
}
```

**Tasks:**
- [ ] Set up CI pipeline
- [ ] Run RISC-V compliance tests
- [ ] Run Linux boot tests
- [ ] Performance regression tests

#### Phase 5 Deliverables

- [ ] Optimized performance
- [ ] RV32 support (optional)
- [ ] Comprehensive test suite
- [ ] Documentation

---

## Testing Strategy

Testing is the most critical aspect of this project. A RISC-V emulator must execute instructions with bit-exact correctness—any deviation will cause Linux to crash or behave incorrectly. This section details a comprehensive testing strategy using `go test ./...` as the primary validation method.

### Testing Philosophy

1. **Test Early, Test Often** - Every instruction implementation should have tests before moving to the next
2. **Layered Testing** - Unit tests → ISA compliance → Integration → Full boot
3. **Golden Reference** - Compare against known-correct signatures from reference implementations
4. **Embedded Test Data** - All test binaries and golden signatures live in `testdata/` for reproducibility

### Test Layers

#### Layer 1: Unit Tests (*_test.go)

Fine-grained tests for individual components:

```go
// cpu/alu_test.go
func TestADD(t *testing.T) {
    cpu := NewTestCPU()
    cpu.reg[1] = 0x7FFFFFFFFFFFFFFF
    cpu.reg[2] = 1
    cpu.executeADD(3, 1, 2)
    // Should overflow to negative
    assert.Equal(t, uint64(0x8000000000000000), cpu.reg[3])
}

func TestSRA(t *testing.T) {
    cpu := NewTestCPU()
    cpu.reg[1] = 0x8000000000000000 // -2^63
    cpu.reg[2] = 4
    cpu.executeSRA(3, 1, 2)
    // Arithmetic shift preserves sign
    assert.Equal(t, uint64(0xF800000000000000), cpu.reg[3])
}
```

**Coverage targets:**
- All RV64I instructions with edge cases (overflow, sign extension, zero)
- All CSR read/write operations with privilege checks
- TLB lookup and page table walking
- Soft float operations with all rounding modes
- VirtIO queue handling

#### Layer 2: ISA Compliance Tests (testdata/riscv-tests/)

The [riscv-software-src/riscv-tests](https://github.com/riscv-software-src/riscv-tests) repository provides assembly tests for each instruction. These tests:

1. Execute a sequence of instructions
2. Write results to a "signature" memory region (`begin_signature` to `end_signature`)
3. Pass/fail is determined by comparing the signature to a golden reference

**Test Categories (TVMs):**

| TVM | Description | Priority |
|-----|-------------|----------|
| `rv64ui-p-*` | RV64 user-mode integer, bare metal | Critical |
| `rv64um-p-*` | RV64 M extension (mul/div) | Critical |
| `rv64ua-p-*` | RV64 A extension (atomics) | Critical |
| `rv64uf-p-*` | RV64 F extension (float32) | High |
| `rv64ud-p-*` | RV64 D extension (float64) | High |
| `rv64uc-p-*` | RV64 C extension (compressed) | High |
| `rv64si-p-*` | RV64 supervisor-mode | Medium |
| `rv64mi-p-*` | RV64 machine-mode | Medium |

**Test Structure:**

```
testdata/
├── riscv-tests/
│   ├── isa/
│   │   ├── rv64ui-p-add.bin       # Prebuilt ELF binary
│   │   ├── rv64ui-p-add.sig       # Golden signature (hex dump)
│   │   ├── rv64ui-p-addi.bin
│   │   ├── rv64ui-p-addi.sig
│   │   ├── rv64um-p-mul.bin
│   │   ├── rv64um-p-mul.sig
│   │   └── ...
│   └── README.md                   # Build instructions, source commit
```

**Go Test Integration:**

```go
// cpu/compliance_test.go

//go:embed testdata/riscv-tests/isa/*.bin
var testBinaries embed.FS

//go:embed testdata/riscv-tests/isa/*.sig
var testSignatures embed.FS

func TestRISCVCompliance(t *testing.T) {
    tests := []string{
        "rv64ui-p-add", "rv64ui-p-addi", "rv64ui-p-and", // ... all tests
    }

    for _, name := range tests {
        t.Run(name, func(t *testing.T) {
            // Load test binary
            bin, _ := testBinaries.ReadFile("testdata/riscv-tests/isa/" + name + ".bin")
            golden, _ := testSignatures.ReadFile("testdata/riscv-tests/isa/" + name + ".sig")

            // Create minimal machine (RAM + CPU, no devices)
            cpu := newTestMachine(bin)

            // Run until ECALL (test exit) or timeout
            for i := 0; i < 100000 && !cpu.halted; i++ {
                cpu.Step()
            }

            // Extract signature from memory
            sig := cpu.DumpSignature()

            // Compare
            if !bytes.Equal(sig, golden) {
                t.Errorf("signature mismatch:\ngot:  %x\nwant: %x", sig, golden)
            }
        })
    }
}
```

**Building Test Binaries:**

The test binaries must be built with the RISC-V GNU toolchain. We'll provide a script and commit prebuilt binaries:

```bash
# scripts/build-riscv-tests.sh
git clone https://github.com/riscv-software-src/riscv-tests
cd riscv-tests
git submodule update --init --recursive
autoconf
./configure --prefix=$PWD/build
make

# Copy binaries and generate signatures using Spike
for test in build/share/riscv-tests/isa/rv64ui-p-*; do
    cp "$test" ../testdata/riscv-tests/isa/
    spike --isa=rv64gc "$test" 2>&1 | extract_signature > "../testdata/riscv-tests/isa/$(basename $test).sig"
done
```

#### Layer 3: Integration Tests (testdata/boot/)

Test full system functionality by booting minimal Linux environments.

**Test Resources from TinyEMU:**

Download from [bellard.org/tinyemu](https://bellard.org/tinyemu/):
- `diskimage-linux-riscv-2018-09-23.tar.gz` - Contains:
  - `bbl64.bin` - Berkeley Boot Loader (RISC-V bootloader)
  - `kernel-riscv64.bin` - Linux kernel
  - `root-riscv64.bin` - Root filesystem with BusyBox

```
testdata/
├── boot/
│   ├── bbl64.bin                  # BBL bootloader
│   ├── kernel-riscv64.bin         # Linux kernel
│   ├── rootfs-minimal.bin         # Minimal BusyBox rootfs
│   └── README.md                  # Source URLs, versions
```

**Boot Test Implementation:**

```go
// machine/boot_test.go

func TestLinuxBoot(t *testing.T) {
    if testing.Short() {
        t.Skip("skipping boot test in short mode")
    }

    machine := NewRISCVMachine(MachineConfig{
        RAMSize:    256 * 1024 * 1024,
        BIOSPath:   "testdata/boot/bbl64.bin",
        KernelPath: "testdata/boot/kernel-riscv64.bin",
    })

    // Capture console output
    var console bytes.Buffer
    machine.SetConsoleWriter(&console)

    // Run for up to 30 seconds
    timeout := time.After(30 * time.Second)
    bootComplete := make(chan bool)

    go func() {
        for {
            machine.Run(10000)
            if strings.Contains(console.String(), "~ #") {
                bootComplete <- true
                return
            }
        }
    }()

    select {
    case <-bootComplete:
        t.Log("Linux boot successful")
    case <-timeout:
        t.Fatalf("Boot timeout. Console output:\n%s", console.String())
    }
}

func TestLinuxCommands(t *testing.T) {
    if testing.Short() {
        t.Skip("skipping command test in short mode")
    }

    machine := bootLinux(t) // Helper that boots and waits for shell

    // Test basic commands
    tests := []struct {
        cmd      string
        expected string
    }{
        {"echo hello", "hello"},
        {"uname -m", "riscv64"},
        {"cat /proc/cpuinfo | grep isa", "rv64"},
    }

    for _, tc := range tests {
        output := machine.RunCommand(tc.cmd)
        if !strings.Contains(output, tc.expected) {
            t.Errorf("command %q: got %q, want substring %q", tc.cmd, output, tc.expected)
        }
    }
}
```

#### Layer 4: Soft Float Validation

IEEE754 compliance is critical. Test against reference implementations:

```go
// softfp/softfp_test.go

func TestF64Add(t *testing.T) {
    // Test vectors from Berkeley TestFloat or generated via reference
    tests := []struct {
        a, b     uint64
        rm       RoundingMode
        expected uint64
        flags    ExceptionFlags
    }{
        // Normal addition
        {0x3FF0000000000000, 0x3FF0000000000000, RoundNearestEven, 0x4000000000000000, 0},
        // Infinity + finite
        {0x7FF0000000000000, 0x3FF0000000000000, RoundNearestEven, 0x7FF0000000000000, 0},
        // NaN propagation
        {0x7FF8000000000000, 0x3FF0000000000000, RoundNearestEven, 0x7FF8000000000000, 0},
        // Overflow
        {0x7FEFFFFFFFFFFFFF, 0x7FEFFFFFFFFFFFFF, RoundNearestEven, 0x7FF0000000000000, FlagOverflow | FlagInexact},
        // ... extensive test vectors
    }

    for _, tc := range tests {
        var flags ExceptionFlags
        result := AddF64(tc.a, tc.b, tc.rm, &flags)
        if result != tc.expected || flags != tc.flags {
            t.Errorf("AddF64(%x, %x, %v) = %x, flags=%v; want %x, flags=%v",
                tc.a, tc.b, tc.rm, result, flags, tc.expected, tc.flags)
        }
    }
}
```

### Test Data Sources

| Resource | URL | Contents |
|----------|-----|----------|
| riscv-tests | [github.com/riscv-software-src/riscv-tests](https://github.com/riscv-software-src/riscv-tests) | ISA unit tests (assembly) |
| riscv-arch-test | [github.com/riscv-non-isa/riscv-arch-test](https://github.com/riscv-non-isa/riscv-arch-test) | Official architectural compliance tests |
| TinyEMU disk images | [bellard.org/tinyemu](https://bellard.org/tinyemu/) | BBL, Linux kernel, BusyBox rootfs |
| JSLinux configs | [bellard.org/jslinux](https://bellard.org/jslinux/) | Working VM configurations |
| Berkeley TestFloat | [github.com/ucb-bar/berkeley-testfloat-3](https://github.com/ucb-bar/berkeley-testfloat-3) | IEEE754 test vectors |

### Test Execution

All tests run via standard Go testing:

```bash
# Run all tests
go test ./...

# Run only unit tests (fast)
go test -short ./...

# Run specific package
go test ./cpu/...

# Run compliance tests with verbose output
go test -v -run TestRISCVCompliance ./cpu/

# Run boot test (slow, requires test data)
go test -v -run TestLinuxBoot ./machine/

# Run with race detector
go test -race ./...

# Generate coverage report
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out
```

### Test Data Management

Test binaries and golden signatures are committed to the repository under `testdata/`. This ensures:

1. **Reproducibility** - Tests work offline without external dependencies
2. **Versioning** - Test data changes are tracked in git
3. **CI-ready** - No need to build riscv-tests in CI (though CI is not a current goal)

**Initial test data setup:**

```bash
# Download TinyEMU boot images
wget https://bellard.org/tinyemu/diskimage-linux-riscv-2018-09-23.tar.gz
tar xzf diskimage-linux-riscv-2018-09-23.tar.gz -C testdata/boot/

# Build riscv-tests (requires RISC-V toolchain)
./scripts/build-riscv-tests.sh
```

### Success Criteria for Testing

| Test Layer | Pass Criteria |
|------------|--------------|
| Unit tests | 100% pass, >80% code coverage |
| ISA compliance (rv64ui) | 100% pass (48 tests) |
| ISA compliance (rv64um) | 100% pass (8 tests) |
| ISA compliance (rv64ua) | 100% pass |
| ISA compliance (rv64uf/ud) | 100% pass |
| Boot test | Linux shell prompt within 30s |
| Command test | Basic commands execute correctly |

---

## Risk Assessment

| Risk | Probability | Impact | Mitigation |
|------|-------------|--------|------------|
| Performance significantly worse than C | Medium | High | Profile early, use unsafe where needed, consider future JIT |
| IEEE754 compliance failures | Medium | High | Port softfp exactly, extensive testing |
| VirtIO protocol bugs | Medium | Medium | Test with multiple Linux versions |
| 128-bit arithmetic issues | Low | Medium | Use proven uint128 package |
| Memory model issues (atomics) | Low | Medium | Study RISC-V memory model spec |
| Linux boot hangs | Medium | High | Enable debug tracing, use known-working images |

---

## Resource Requirements

### Development Environment

- Go 1.21+ (for generics improvements)
- Linux development machine (for TAP networking, testing)
- RISC-V toolchain (for building test programs)
- QEMU (for comparison testing)

### Test Resources

- RISC-V ISA compliance test suite
- Linux kernel images (riscv64)
- BusyBox/Alpine Linux root filesystem
- RISC-V GCC/LLVM for building tests

### Documentation

- RISC-V ISA specifications (Volume I & II)
- VirtIO specification
- 9P protocol specification
- TinyEMU source code (reference implementation)

---

## Success Criteria

### MVP (Phase 1)
- [ ] Boot Linux to shell via serial console
- [ ] Run basic commands (ls, cat, echo)
- [ ] Stable operation for >1 hour

### Full Release
- [ ] Pass 100% of RISC-V compliance tests
- [ ] Boot major distributions (Debian, Alpine)
- [ ] Network connectivity
- [ ] Performance within 3x of C version
- [ ] Single static binary under 20MB

---

## Timeline Summary

| Phase | Duration | Deliverable |
|-------|----------|-------------|
| Phase 1 | 4-6 weeks | Boot Linux to shell |
| Phase 2 | 3-4 weeks | Full ISA, compliance tests |
| Phase 3 | 2-3 weeks | Disk & filesystem |
| Phase 4 | 2-3 weeks | Network & graphics |
| Phase 5 | 1-2 weeks | Optimization & polish |
| **Total** | **12-18 weeks** | Production-ready emulator |

---

## Appendix A: Instruction Reference

### RV64I Base Instructions

| Instruction | Format | Operation |
|-------------|--------|-----------|
| ADD | R | rd = rs1 + rs2 |
| SUB | R | rd = rs1 - rs2 |
| AND | R | rd = rs1 & rs2 |
| OR | R | rd = rs1 \| rs2 |
| XOR | R | rd = rs1 ^ rs2 |
| SLL | R | rd = rs1 << rs2[5:0] |
| SRL | R | rd = rs1 >> rs2[5:0] (logical) |
| SRA | R | rd = rs1 >> rs2[5:0] (arithmetic) |
| SLT | R | rd = (rs1 < rs2) ? 1 : 0 (signed) |
| SLTU | R | rd = (rs1 < rs2) ? 1 : 0 (unsigned) |
| ADDI | I | rd = rs1 + imm |
| ANDI | I | rd = rs1 & imm |
| ORI | I | rd = rs1 \| imm |
| XORI | I | rd = rs1 ^ imm |
| SLTI | I | rd = (rs1 < imm) ? 1 : 0 (signed) |
| SLTIU | I | rd = (rs1 < imm) ? 1 : 0 (unsigned) |
| SLLI | I | rd = rs1 << imm[5:0] |
| SRLI | I | rd = rs1 >> imm[5:0] (logical) |
| SRAI | I | rd = rs1 >> imm[5:0] (arithmetic) |
| LB | I | rd = sign_ext(mem[rs1+imm][7:0]) |
| LH | I | rd = sign_ext(mem[rs1+imm][15:0]) |
| LW | I | rd = sign_ext(mem[rs1+imm][31:0]) |
| LD | I | rd = mem[rs1+imm][63:0] |
| LBU | I | rd = zero_ext(mem[rs1+imm][7:0]) |
| LHU | I | rd = zero_ext(mem[rs1+imm][15:0]) |
| LWU | I | rd = zero_ext(mem[rs1+imm][31:0]) |
| SB | S | mem[rs1+imm][7:0] = rs2[7:0] |
| SH | S | mem[rs1+imm][15:0] = rs2[15:0] |
| SW | S | mem[rs1+imm][31:0] = rs2[31:0] |
| SD | S | mem[rs1+imm][63:0] = rs2[63:0] |
| BEQ | B | if (rs1 == rs2) pc += imm |
| BNE | B | if (rs1 != rs2) pc += imm |
| BLT | B | if (rs1 < rs2) pc += imm (signed) |
| BGE | B | if (rs1 >= rs2) pc += imm (signed) |
| BLTU | B | if (rs1 < rs2) pc += imm (unsigned) |
| BGEU | B | if (rs1 >= rs2) pc += imm (unsigned) |
| JAL | J | rd = pc + 4; pc += imm |
| JALR | I | rd = pc + 4; pc = (rs1 + imm) & ~1 |
| LUI | U | rd = imm << 12 |
| AUIPC | U | rd = pc + (imm << 12) |
| ECALL | I | Environment call |
| EBREAK | I | Breakpoint |

### RV64M Extension

| Instruction | Operation |
|-------------|-----------|
| MUL | rd = (rs1 * rs2)[63:0] |
| MULH | rd = (rs1 * rs2)[127:64] (signed×signed) |
| MULHSU | rd = (rs1 * rs2)[127:64] (signed×unsigned) |
| MULHU | rd = (rs1 * rs2)[127:64] (unsigned×unsigned) |
| DIV | rd = rs1 / rs2 (signed) |
| DIVU | rd = rs1 / rs2 (unsigned) |
| REM | rd = rs1 % rs2 (signed) |
| REMU | rd = rs1 % rs2 (unsigned) |

### RV64A Extension

| Instruction | Operation |
|-------------|-----------|
| LR.D | rd = mem[rs1]; reserve(rs1) |
| SC.D | if reserved(rs1): mem[rs1] = rs2, rd = 0; else: rd = 1 |
| AMOSWAP.D | rd = mem[rs1]; mem[rs1] = rs2 |
| AMOADD.D | rd = mem[rs1]; mem[rs1] = rd + rs2 |
| AMOAND.D | rd = mem[rs1]; mem[rs1] = rd & rs2 |
| AMOOR.D | rd = mem[rs1]; mem[rs1] = rd \| rs2 |
| AMOXOR.D | rd = mem[rs1]; mem[rs1] = rd ^ rs2 |
| AMOMAX.D | rd = mem[rs1]; mem[rs1] = max(rd, rs2) |
| AMOMIN.D | rd = mem[rs1]; mem[rs1] = min(rd, rs2) |
| AMOMAXU.D | rd = mem[rs1]; mem[rs1] = maxu(rd, rs2) |
| AMOMINU.D | rd = mem[rs1]; mem[rs1] = minu(rd, rs2) |

---

## Appendix B: CSR Reference

### Machine-Level CSRs

| CSR | Address | Description |
|-----|---------|-------------|
| mstatus | 0x300 | Machine status |
| misa | 0x301 | ISA and extensions |
| medeleg | 0x302 | Exception delegation |
| mideleg | 0x303 | Interrupt delegation |
| mie | 0x304 | Interrupt enable |
| mtvec | 0x305 | Trap vector base |
| mcounteren | 0x306 | Counter enable |
| mscratch | 0x340 | Scratch register |
| mepc | 0x341 | Exception PC |
| mcause | 0x342 | Exception cause |
| mtval | 0x343 | Trap value |
| mip | 0x344 | Interrupt pending |
| mcycle | 0xB00 | Cycle counter |
| minstret | 0xB02 | Instruction counter |
| mhartid | 0xF14 | Hardware thread ID |

### Supervisor-Level CSRs

| CSR | Address | Description |
|-----|---------|-------------|
| sstatus | 0x100 | Supervisor status |
| sie | 0x104 | Interrupt enable |
| stvec | 0x105 | Trap vector base |
| scounteren | 0x106 | Counter enable |
| sscratch | 0x140 | Scratch register |
| sepc | 0x141 | Exception PC |
| scause | 0x142 | Exception cause |
| stval | 0x143 | Trap value |
| sip | 0x144 | Interrupt pending |
| satp | 0x180 | Address translation |

---

## Appendix C: Memory Map

### RISC-V Machine Memory Layout

```
0x0000_0000 - 0x0000_FFFF : Low RAM (64KB) - Boot ROM/Reset vector
0x0200_0000 - 0x020B_FFFF : CLINT (Core Local Interruptor)
    0x0200_0000 : msip (Machine Software Interrupt Pending)
    0x0200_4000 : mtimecmp (Machine Timer Compare)
    0x0200_BFF8 : mtime (Machine Timer)
0x4000_8000 - 0x4000_8FFF : HTIF (Host-Target Interface)
0x4000_9000 - 0x4000_9FFF : IDE Controller (optional)
0x4001_0000 - 0x4001_0FFF : VirtIO Device 0
0x4001_1000 - 0x4001_1FFF : VirtIO Device 1
0x4001_2000 - 0x4001_2FFF : VirtIO Device 2
...
0x4010_0000 - 0x404F_FFFF : PLIC (Platform-Level Interrupt Controller)
    0x4010_0004 : Source 1 priority
    0x4010_1000 : Pending bits
    0x4010_2000 : Enable bits (context 0)
    0x4020_0000 : Priority threshold (context 0)
    0x4020_0004 : Claim/complete (context 0)
0x4100_0000 - 0x41FF_FFFF : Framebuffer (optional)
0x8000_0000 - ...         : Main RAM
```
