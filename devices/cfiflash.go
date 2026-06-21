package devices

import "github.com/sorins/tinyemu-go/mem"

// CFIFlash emulates an Intel-command-set parallel NOR flash ("P30" / the QEMU
// pflash_cfi01 model) as exposed by the QEMU "virt" board's UEFI variable store.
// edk2's VirtNorFlashDxe drives it by writing commands (erase, program, read
// status) and polling a status register for the "write state machine ready" bit
// before continuing — so backing it with plain RAM (which just echoes the
// command byte) wedges the firmware in an infinite status poll.
//
// The board maps the flash at bank-width 4 as two interleaved x16 devices, so a
// 32-bit command write carries the command byte replicated across both 16-bit
// lanes (0x00NN00NN) and a 32-bit status read returns the status replicated
// (0x00800080 = both chips' ready bit). Commands are decoded from the low byte;
// status reads are replicated to the access width.
type CFIFlash struct {
	mem  []byte
	mode cfiMode

	pendProgram bool   // program setup seen; next write is the data word
	pendErase   bool   // erase setup seen; next write must be the confirm
	eraseOff    uint32 // address within the block to erase
	pendLock    bool   // lock setup seen; next write is a lock sub-command (no-op)

	bufActive    bool // buffered-program sequence in progress
	bufNeedCount bool // next buffered write is the word count
	bufRemaining int  // data words still expected in the buffer
}

type cfiMode int

const (
	cfiReadArray  cfiMode = iota // reads return flash contents (default / power-on)
	cfiReadStatus                // reads return the status register
)

// Intel CFI command set (low byte of a command write).
const (
	cfiCmdReadArray   = 0xFF
	cfiCmdReadArray2  = 0xF0
	cfiCmdReadStatus  = 0x70
	cfiCmdClearStatus = 0x50
	cfiCmdEraseSetup  = 0x20
	cfiCmdProgSetup   = 0x40
	cfiCmdProgSetup2  = 0x10
	cfiCmdBufSetup    = 0xE8
	cfiCmdConfirm     = 0xD0
	cfiCmdLockSetup   = 0x60

	cfiStatusReady = 0x80 // SR.7: write state machine ready (no error bits set)

	cfiSectorSize = 0x40000 // 256 KiB erase block (QEMU virt VIRT_FLASH_SECTOR_SIZE)
)

// NewCFIFlash returns a flash of the given size with every byte set to fill
// (0xFF models an erased part).
func NewCFIFlash(size int, fill byte) *CFIFlash {
	m := make([]byte, size)
	if fill != 0 {
		for i := range m {
			m[i] = fill
		}
	}
	return &CFIFlash{mem: m}
}

// Init copies initial contents (e.g. firmware) into the flash starting at offset 0.
func (f *CFIFlash) Init(data []byte) { copy(f.mem, data) }

// Register maps the flash at base in the physical memory map.
func (f *CFIFlash) Register(memMap *mem.PhysMemoryMap, base uint64) error {
	_, err := memMap.RegisterDevice(base, uint64(len(f.mem)), f, f.read, f.write,
		mem.DevIOSize8|mem.DevIOSize16|mem.DevIOSize32)
	return err
}

func (f *CFIFlash) read(_ any, offset uint32, sizeLog2 int) uint32 {
	if f.mode == cfiReadStatus {
		switch sizeLog2 {
		case 0:
			return cfiStatusReady
		case 1:
			return cfiStatusReady // 0x0080: one 16-bit lane
		default:
			return cfiStatusReady<<16 | cfiStatusReady // 0x00800080: both lanes
		}
	}
	return f.load(offset, sizeLog2)
}

// load reads sizeLog2 bytes from the backing store, little-endian.
func (f *CFIFlash) load(offset uint32, sizeLog2 int) uint32 {
	o := int(offset)
	var v uint32
	for i := 0; i < 1<<sizeLog2; i++ {
		v |= uint32(f.mem[o+i]) << (8 * i)
	}
	return v
}

func (f *CFIFlash) write(_ any, offset uint32, val uint32, sizeLog2 int) {
	// A write following a setup command is raw data, not a new command.
	if f.pendProgram {
		f.program(offset, val, sizeLog2)
		f.pendProgram = false
		f.mode = cfiReadStatus
		return
	}
	if f.bufActive {
		f.writeBuffered(offset, val, sizeLog2)
		return
	}

	switch val & 0xFF { // commands arrive byte-replicated across the lanes
	case cfiCmdReadArray, cfiCmdReadArray2:
		f.mode = cfiReadArray
	case cfiCmdReadStatus:
		f.mode = cfiReadStatus
	case cfiCmdClearStatus:
		// Status is modelled as always "ready, no error"; nothing to clear.
	case cfiCmdProgSetup, cfiCmdProgSetup2:
		f.pendProgram = true
	case cfiCmdEraseSetup:
		f.pendErase = true
		f.eraseOff = offset
		f.mode = cfiReadStatus
	case cfiCmdBufSetup:
		f.bufActive = true
		f.bufNeedCount = true
		f.mode = cfiReadStatus
	case cfiCmdLockSetup:
		f.pendLock = true
		f.mode = cfiReadStatus
	case cfiCmdConfirm:
		switch {
		case f.pendErase:
			f.erase(f.eraseOff)
			f.pendErase = false
		case f.pendLock:
			f.pendLock = false // lock/unlock confirm: no-op
		}
		f.mode = cfiReadStatus
	default:
		if f.pendLock { // lock sub-commands (0x01 lock, 0x2F lock-down)
			f.pendLock = false
			f.mode = cfiReadStatus
		}
		// Unknown commands are ignored, leaving the current mode.
	}
}

// program applies a NOR word write: bits can only be cleared (1->0), so the new
// value is ANDed into the existing contents.
func (f *CFIFlash) program(offset, val uint32, sizeLog2 int) {
	o := int(offset)
	for i := 0; i < 1<<sizeLog2; i++ {
		f.mem[o+i] &= byte(val >> (8 * i))
	}
}

// erase resets the 256 KiB block containing offset to the erased state (0xFF).
func (f *CFIFlash) erase(offset uint32) {
	start := int(offset) &^ (cfiSectorSize - 1)
	end := min(start+cfiSectorSize, len(f.mem))
	for i := start; i < end; i++ {
		f.mem[i] = 0xFF
	}
}

// writeBuffered handles the buffered-program sequence: count word, then that
// many data words, then the confirm command.
func (f *CFIFlash) writeBuffered(offset, val uint32, sizeLog2 int) {
	switch {
	case f.bufNeedCount:
		// The count word holds (number of words - 1).
		f.bufRemaining = int(val&0xFFFF) + 1
		f.bufNeedCount = false
		f.mode = cfiReadStatus
	case f.bufRemaining > 0:
		f.program(offset, val, sizeLog2)
		f.bufRemaining--
	default:
		// The trailing confirm (0xD0); data is already committed.
		f.bufActive = false
		f.mode = cfiReadStatus
	}
}
