// Package riscv implements the RISC-V CPU core for the TinyEMU emulator.
package riscv

// Privilege levels
const (
	PrivUser       = 0 // U-mode
	PrivSupervisor = 1 // S-mode
	PrivHypervisor = 2 // H-mode (reserved)
	PrivMachine    = 3 // M-mode
)

// Exception/interrupt causes
const (
	CauseMisalignedFetch = 0x0
	CauseFaultFetch      = 0x1
	CauseIllegalInsn     = 0x2
	CauseBreakpoint      = 0x3
	CauseMisalignedLoad  = 0x4
	CauseFaultLoad       = 0x5
	CauseMisalignedStore = 0x6
	CauseFaultStore      = 0x7
	CauseUserEcall       = 0x8
	CauseSupervisorEcall = 0x9
	CauseHypervisorEcall = 0xa
	CauseMachineEcall    = 0xb
	CauseFetchPageFault  = 0xc
	CauseLoadPageFault   = 0xd
	CauseStorePageFault  = 0xf

	// Interrupt flag - set in high bit of cause
	CauseInterrupt = 1 << 63
)

// MIP/MIE bits - Machine Interrupt Pending/Enable
const (
	MipUSIP = 1 << 0  // User Software Interrupt Pending
	MipSSIP = 1 << 1  // Supervisor Software Interrupt Pending
	MipHSIP = 1 << 2  // Hypervisor Software Interrupt Pending (reserved)
	MipMSIP = 1 << 3  // Machine Software Interrupt Pending
	MipUTIP = 1 << 4  // User Timer Interrupt Pending
	MipSTIP = 1 << 5  // Supervisor Timer Interrupt Pending
	MipHTIP = 1 << 6  // Hypervisor Timer Interrupt Pending (reserved)
	MipMTIP = 1 << 7  // Machine Timer Interrupt Pending
	MipUEIP = 1 << 8  // User External Interrupt Pending
	MipSEIP = 1 << 9  // Supervisor External Interrupt Pending
	MipHEIP = 1 << 10 // Hypervisor External Interrupt Pending (reserved)
	MipMEIP = 1 << 11 // Machine External Interrupt Pending
)

// MISA extension bits
const (
	MisaA = 1 << ('A' - 'A') // Atomic extension
	MisaB = 1 << ('B' - 'A') // Bit manipulation
	MisaC = 1 << ('C' - 'A') // Compressed extension
	MisaD = 1 << ('D' - 'A') // Double-precision floating-point
	MisaE = 1 << ('E' - 'A') // RV32E base ISA
	MisaF = 1 << ('F' - 'A') // Single-precision floating-point
	MisaG = 1 << ('G' - 'A') // Reserved
	MisaH = 1 << ('H' - 'A') // Hypervisor extension
	MisaI = 1 << ('I' - 'A') // RV32I/64I/128I base ISA
	MisaJ = 1 << ('J' - 'A') // Reserved
	MisaK = 1 << ('K' - 'A') // Reserved
	MisaL = 1 << ('L' - 'A') // Reserved
	MisaM = 1 << ('M' - 'A') // Integer Multiply/Divide extension
	MisaN = 1 << ('N' - 'A') // User-level interrupts
	MisaO = 1 << ('O' - 'A') // Reserved
	MisaP = 1 << ('P' - 'A') // Reserved
	MisaQ = 1 << ('Q' - 'A') // Quad-precision floating-point
	MisaR = 1 << ('R' - 'A') // Reserved
	MisaS = 1 << ('S' - 'A') // Supervisor mode
	MisaT = 1 << ('T' - 'A') // Reserved
	MisaU = 1 << ('U' - 'A') // User mode
	MisaV = 1 << ('V' - 'A') // Vector extension
	MisaW = 1 << ('W' - 'A') // Reserved
	MisaX = 1 << ('X' - 'A') // Non-standard extensions
	MisaY = 1 << ('Y' - 'A') // Reserved
	MisaZ = 1 << ('Z' - 'A') // Reserved
)

// MXL values for MISA register (bits 63:62 for RV64, bits 31:30 for RV32)
const (
	MxlRV32  = 1
	MxlRV64  = 2
	MxlRV128 = 3
)

// MSTATUS bit positions
const (
	MstatusUIE  = 1 << 0  // User Interrupt Enable
	MstatusSIE  = 1 << 1  // Supervisor Interrupt Enable
	MstatusHIE  = 1 << 2  // Hypervisor Interrupt Enable (reserved)
	MstatusMIE  = 1 << 3  // Machine Interrupt Enable
	MstatusUPIE = 1 << 4  // User Previous Interrupt Enable
	MstatusSPIE = 1 << 5  // Supervisor Previous Interrupt Enable
	MstatusHPIE = 1 << 6  // Hypervisor Previous Interrupt Enable (reserved)
	MstatusMPIE = 1 << 7  // Machine Previous Interrupt Enable
	MstatusSPP  = 1 << 8  // Supervisor Previous Privilege
	MstatusHPP  = 3 << 9  // Hypervisor Previous Privilege (reserved)
	MstatusMPP  = 3 << 11 // Machine Previous Privilege
	MstatusFS   = 3 << 13 // Floating-point Status
	MstatusXS   = 3 << 15 // Extension Status
	MstatusMPRV = 1 << 17 // Modify Privilege (for loads/stores)
	MstatusSUM  = 1 << 18 // Supervisor User Memory access
	MstatusMXR  = 1 << 19 // Make eXecutable Readable
	MstatusTVM  = 1 << 20 // Trap Virtual Memory
	MstatusTW   = 1 << 21 // Timeout Wait
	MstatusTSR  = 1 << 22 // Trap SRET
)

// MSTATUS bit shifts
const (
	MstatusSPIEShift = 5
	MstatusMPIEShift = 7
	MstatusSPPShift  = 8
	MstatusMPPShift  = 11
	MstatusFSShift   = 13
	MstatusXSShift   = 15
	MstatusUXLShift  = 32
	MstatusSXLShift  = 34
)

// MSTATUS masks
const (
	MstatusUXLMask = uint64(3) << MstatusUXLShift
	MstatusSXLMask = uint64(3) << MstatusSXLShift
)

// Floating-point status values (FS field in mstatus)
const (
	FSOff     = 0 // FP unit is off
	FSInitial = 1 // FP unit is in initial state
	FSClean   = 2 // FP state is clean
	FSDirty   = 3 // FP state is dirty
)

// SATP modes (address translation modes)
const (
	SatpModeBare = 0  // No translation
	SatpModeSv32 = 1  // 32-bit virtual addressing (RV32 only)
	SatpModeSv39 = 8  // 39-bit virtual addressing (RV64)
	SatpModeSv48 = 9  // 48-bit virtual addressing (RV64)
	SatpModeSv57 = 10 // 57-bit virtual addressing (RV64)
	SatpModeSv64 = 11 // 64-bit virtual addressing (RV64, reserved)
)

// Page table constants
const (
	PageShift = 12
	PageSize  = 1 << PageShift
	PageMask  = PageSize - 1
)

// TLB constants
const (
	TLBSize = 256
)

// CSR addresses
const (
	// User-level CSRs
	CSRFflags   = 0x001 // Floating-Point Accrued Exceptions
	CSRFrm      = 0x002 // Floating-Point Dynamic Rounding Mode
	CSRFcsr     = 0x003 // Floating-Point Control and Status
	CSRCycle    = 0xC00 // Cycle counter
	CSRTime     = 0xC01 // Timer
	CSRInstret  = 0xC02 // Instructions retired
	CSRCycleh   = 0xC80 // Cycle counter high (RV32 only)
	CSRTimeh    = 0xC81 // Timer high (RV32 only)
	CSRInstreth = 0xC82 // Instructions retired high (RV32 only)

	// Supervisor-level CSRs
	CSRSstatus    = 0x100 // Supervisor status
	CSRSie        = 0x104 // Supervisor interrupt enable
	CSRStvec      = 0x105 // Supervisor trap vector
	CSRScounteren = 0x106 // Supervisor counter enable
	CSRSscratch   = 0x140 // Supervisor scratch register
	CSRSepc       = 0x141 // Supervisor exception PC
	CSRScause     = 0x142 // Supervisor trap cause
	CSRStval      = 0x143 // Supervisor trap value
	CSRSip        = 0x144 // Supervisor interrupt pending
	CSRSatp       = 0x180 // Supervisor address translation and protection

	// Machine-level CSRs
	CSRMstatus    = 0x300 // Machine status
	CSRMisa       = 0x301 // Machine ISA
	CSRMedeleg    = 0x302 // Machine exception delegation
	CSRMideleg    = 0x303 // Machine interrupt delegation
	CSRMie        = 0x304 // Machine interrupt enable
	CSRMtvec      = 0x305 // Machine trap vector
	CSRMcounteren = 0x306 // Machine counter enable
	CSRMscratch   = 0x340 // Machine scratch register
	CSRMepc       = 0x341 // Machine exception PC
	CSRMcause     = 0x342 // Machine trap cause
	CSRMtval      = 0x343 // Machine trap value
	CSRMip        = 0x344 // Machine interrupt pending
	CSRMcycle     = 0xB00 // Machine cycle counter
	CSRMinstret   = 0xB02 // Machine instructions retired
	CSRMcycleh    = 0xB80 // Machine cycle counter high (RV32 only)
	CSRMinstreth  = 0xB82 // Machine instructions retired high (RV32 only)
	CSRMvendorid  = 0xF11 // Machine Vendor ID (read-only)
	CSRMarchid    = 0xF12 // Machine Architecture ID (read-only)
	CSRMimpid     = 0xF13 // Machine Implementation ID (read-only)
	CSRMhartid    = 0xF14 // Machine hardware thread ID
)

// Floating-point rounding modes
const (
	RoundNearestEven = 0 // RNE - Round to Nearest, ties to Even
	RoundToZero      = 1 // RTZ - Round towards Zero
	RoundDown        = 2 // RDN - Round Down (towards -infinity)
	RoundUp          = 3 // RUP - Round Up (towards +infinity)
	RoundNearestMax  = 4 // RMM - Round to Nearest, ties to Max Magnitude
	RoundDynamic     = 7 // DYN - Use frm register
)

// Floating-point exception flags
const (
	FFlagNX = 1 << 0 // Inexact
	FFlagUF = 1 << 1 // Underflow
	FFlagOF = 1 << 2 // Overflow
	FFlagDZ = 1 << 3 // Divide by Zero
	FFlagNV = 1 << 4 // Invalid Operation
)
