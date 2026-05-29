package pc

import (
	"fmt"
	"os"

	"github.com/jtolio/tinyemu-go/mem"
)

// floppyDebug traces FDC/DMA activity when TINYEMU_FDC_DEBUG=1.
var floppyDebug = os.Getenv("TINYEMU_FDC_DEBUG") == "1"

// Standard 1.44 MB floppy geometry.
const (
	flpCylinders = 80
	flpHeads     = 2
	flpSectors   = 18 // sectors per track
	flpSecSize   = 512
)

// dmaController is a minimal 8237 slave DMA controller. We model only the
// state the floppy driver needs on channel 2: a 16-bit base address (low
// then high byte through a shared flip-flop), a 16-bit count, the page
// register, and the mask/mode bytes. The flip-flop is shared across all
// channels per the 8237 spec.
//
// We don't perform real cycle-stealing transfers; the FDC reads
// channel-2 state synchronously when it executes a READ and copies the
// bytes itself (see FDC.execRead). Ports: 0x00-0x0F (slave 8237),
// channel-2 page register at 0x81.
type dmaController struct {
	ch2Addr  uint16
	ch2Count uint16
	ch2Page  uint8
	flipFlop bool // false → next byte is low, true → high
}

// Register wires the slave 8237 ports for channel 2.
func (d *dmaController) Register(io *IOPortDispatcher) {
	// Channel 2 base address (port 0x04) and count (port 0x05) use the
	// shared flip-flop to sequence low/high bytes.
	io.RegisterWrite(0x04, 0x04, func(_ uint16, v uint32) {
		if !d.flipFlop {
			d.ch2Addr = (d.ch2Addr & 0xFF00) | uint16(v&0xFF)
		} else {
			d.ch2Addr = (d.ch2Addr & 0x00FF) | (uint16(v&0xFF) << 8)
		}
		d.flipFlop = !d.flipFlop
	})
	io.RegisterWrite(0x05, 0x05, func(_ uint16, v uint32) {
		if !d.flipFlop {
			d.ch2Count = (d.ch2Count & 0xFF00) | uint16(v&0xFF)
		} else {
			d.ch2Count = (d.ch2Count & 0x00FF) | (uint16(v&0xFF) << 8)
		}
		d.flipFlop = !d.flipFlop
	})
	// 0x0C: clear byte-pointer (flip-flop) flip-flop.
	io.RegisterWrite(0x0C, 0x0C, func(_ uint16, _ uint32) { d.flipFlop = false })
	// 0x0A mask, 0x0B mode — accepted and ignored (we transfer eagerly).
	io.RegisterWrite(0x0A, 0x0A, func(_ uint16, _ uint32) {})
	io.RegisterWrite(0x0B, 0x0B, func(_ uint16, _ uint32) {})
	// Channel-2 page register lives at 0x81 (DMA page register block).
	io.RegisterWrite(0x81, 0x81, func(_ uint16, v uint32) { d.ch2Page = uint8(v) })
	io.RegisterRead(0x81, 0x81, func(_ uint16) uint32 { return uint32(d.ch2Page) })
}

// target returns the physical address and byte count programmed for a
// channel-2 transfer. The 8237 count register holds (bytes - 1).
func (d *dmaController) target() (addr uint64, count int) {
	return (uint64(d.ch2Page) << 16) | uint64(d.ch2Addr), int(d.ch2Count) + 1
}

// FDC is a minimal 8272/82077 floppy disk controller. It models exactly
// the command set SeaBIOS's floppy.c driver issues (SPECIFY, RECALIBRATE,
// SEEK, SENSE_INTERRUPT, READ). The data transfer for READ is performed
// synchronously against the DMA controller's channel-2 target rather
// than streaming through the FIFO — SeaBIOS only observes the result via
// memory + the result bytes + IRQ6, so the shortcut is invisible.
type FDC struct {
	image  []byte // backing floppy image bytes
	dma    *dmaController
	pic    *PIC8259
	memMap *mem.PhysMemoryMap

	// Command/result FIFO state machine.
	cmd      []byte // bytes received so far this command phase (cmd + params)
	need     int    // total bytes expected in command phase (1 + params)
	result   []byte // result bytes pending in the result phase
	resPos   int    // next result byte to hand to the CPU
	pcn      uint8  // present cylinder number (set by seek/recalibrate)
	lastHead uint8  // head from the last seek/read (for sense-interrupt ST0)
	intPending bool // an operation raised IRQ6 and is awaiting sense-interrupt
	dor      uint8  // digital output register (motor/drive/reset)
}

// NewFDC creates a floppy controller backed by `image`.
func NewFDC(image []byte, pic *PIC8259, memMap *mem.PhysMemoryMap) *FDC {
	return &FDC{
		image:  image,
		dma:    &dmaController{},
		pic:    pic,
		memMap: memMap,
	}
}

// floppyIRQ is the legacy IRQ line for the floppy controller (IRQ6).
const floppyIRQ = 6

// Register wires the FDC ports (0x3F2 DOR, 0x3F4 MSR, 0x3F5 DATA,
// 0x3F7 DIR/CCR) and the DMA controller.
func (f *FDC) Register(io *IOPortDispatcher) {
	f.dma.Register(io)

	io.RegisterRead(0x3F4, 0x3F4, func(_ uint16) uint32 { return uint32(f.readMSR()) })
	io.RegisterRead(0x3F5, 0x3F5, func(_ uint16) uint32 { return uint32(f.readData()) })
	io.RegisterRead(0x3F7, 0x3F7, func(_ uint16) uint32 { return 0x00 }) // DIR: no disk-change
	io.RegisterRead(0x3F2, 0x3F2, func(_ uint16) uint32 { return uint32(f.dor) })

	io.RegisterWrite(0x3F2, 0x3F2, func(_ uint16, v uint32) { f.writeDOR(uint8(v)) })
	io.RegisterWrite(0x3F5, 0x3F5, func(_ uint16, v uint32) { f.writeData(uint8(v)) })
	io.RegisterWrite(0x3F7, 0x3F7, func(_ uint16, _ uint32) {}) // CCR: data rate, ignored
}

// readMSR returns the Main Status Register (port 0x3F4).
//   bit7 RQM  — controller ready to transfer a data byte
//   bit6 DIO  — direction: 1 = FDC→CPU (result phase), 0 = CPU→FDC
//   bit4 CB   — command in progress
func (f *FDC) readMSR() uint8 {
	const RQM, DIO, CB = 0x80, 0x40, 0x10
	if len(f.result) > 0 {
		return RQM | DIO | CB // result phase: byte available to read
	}
	if f.need > 0 {
		return RQM | CB // command phase: awaiting more bytes from CPU
	}
	return RQM // idle, ready for a new command
}

func (f *FDC) writeDOR(v uint8) {
	prev := f.dor
	f.dor = v
	// Bit 2 = /RESET (0 = held in reset). A 0→1 transition leaves reset
	// and, in the real controller, posts a reset interrupt. SeaBIOS's
	// reset path issues SENSE_INTERRUPT afterwards to clear it.
	if prev&0x04 == 0 && v&0x04 != 0 {
		f.cmd = nil
		f.need = 0
		f.result = nil
		f.resPos = 0
		f.raiseIRQ()
	}
}

func (f *FDC) raiseIRQ() {
	f.intPending = true
	if f.pic != nil {
		f.pic.RaiseIRQ(floppyIRQ)
	}
}

// readData returns the next result byte (port 0x3F5).
func (f *FDC) readData() uint8 {
	if f.resPos < len(f.result) {
		b := f.result[f.resPos]
		f.resPos++
		if f.resPos >= len(f.result) {
			f.result = nil
			f.resPos = 0
		}
		return b
	}
	return 0
}

// cmdParamCount returns how many parameter bytes follow the given
// command opcode (the low 5 bits select the command; high bits are MT/
// MFM/SK flags). -1 marks an unknown command.
func cmdParamCount(op uint8) int {
	switch op & 0x1F {
	case 0x03: // SPECIFY
		return 2
	case 0x07: // RECALIBRATE
		return 1
	case 0x08: // SENSE INTERRUPT STATUS
		return 0
	case 0x0F: // SEEK
		return 2
	case 0x06: // READ DATA (0xE6 masks to 0x06)
		return 8
	case 0x05: // WRITE DATA (0xC5 masks to 0x05)
		return 8
	case 0x0A: // READ ID
		return 1
	}
	return -1
}

// writeData accepts a command/parameter byte (port 0x3F5).
func (f *FDC) writeData(v uint8) {
	if f.need == 0 {
		// First byte of a new command.
		n := cmdParamCount(v)
		if n < 0 {
			if floppyDebug {
				fmt.Fprintf(os.Stderr, "[fdc] unknown command %#x\n", v)
			}
			return
		}
		f.cmd = []byte{v}
		f.need = 1 + n
		if f.need == 1 {
			f.execute()
		}
		return
	}
	f.cmd = append(f.cmd, v)
	if len(f.cmd) >= f.need {
		f.execute()
	}
}

// execute runs the fully-received command.
func (f *FDC) execute() {
	op := f.cmd[0] & 0x1F
	if floppyDebug {
		fmt.Fprintf(os.Stderr, "[fdc] exec cmd=% x\n", f.cmd)
	}
	switch op {
	case 0x03: // SPECIFY — timing params; nothing to do, no IRQ, no result.
	case 0x07: // RECALIBRATE — seek to cylinder 0.
		f.pcn = 0
		f.lastHead = 0
		f.raiseIRQ()
	case 0x0F: // SEEK — param[0]=drive/head, param[1]=cylinder.
		f.lastHead = (f.cmd[1] >> 2) & 1
		f.pcn = f.cmd[2]
		f.raiseIRQ()
	case 0x08: // SENSE INTERRUPT STATUS — return ST0 + PCN.
		st0 := uint8(0x20) | (f.lastHead << 2) // seek-end, head, drive 0
		if !f.intPending {
			// No interrupt pending: invalid command (polling mode end).
			st0 = 0x80
		}
		f.intPending = false
		f.result = []byte{st0, f.pcn}
		f.resPos = 0
	case 0x06: // READ DATA
		f.execRead()
	case 0x05: // WRITE DATA — not supported for read-only boot; report OK-ish.
		f.execWriteStub()
	case 0x0A: // READ ID
		f.result = []byte{(f.lastHead << 2), 0, 0, f.pcn, f.lastHead, 1, 2}
		f.resPos = 0
		f.raiseIRQ()
	}
	f.cmd = nil
	f.need = 0
}

// execRead performs the synchronous sector read into the DMA target.
// cmd layout: [0]=0xE6, [1]=(head<<2)|drive, [2]=cyl, [3]=head, [4]=sec,
// [5]=sizecode, [6]=EOT(last sector), [7]=gap, [8]=datalen.
func (f *FDC) execRead() {
	cyl := uint32(f.cmd[2])
	head := uint32(f.cmd[3])
	sec := uint32(f.cmd[4]) // 1-based
	f.lastHead = uint8(head)

	addr, count := f.dma.target()
	nsec := count / flpSecSize
	if nsec < 1 {
		nsec = 1
	}
	lba := (cyl*flpHeads+head)*flpSectors + (sec - 1)
	if floppyDebug {
		fmt.Fprintf(os.Stderr, "[fdc] READ chs=%d/%d/%d lba=%d nsec=%d -> phys=%#x (%d bytes)\n",
			cyl, head, sec, lba, nsec, addr, count)
	}

	off := int(lba) * flpSecSize
	for i := 0; i < count; i++ {
		var b uint8
		if off+i < len(f.image) {
			b = f.image[off+i]
		}
		if err := f.memMap.Write8(addr+uint64(i), b); err != nil {
			break
		}
	}

	// End position after the transfer (advance sector/head/cyl).
	endSec := sec + uint32(nsec)
	endCyl, endHead := cyl, head
	for endSec > flpSectors {
		endSec -= flpSectors
		endHead++
		if endHead >= flpHeads {
			endHead = 0
			endCyl++
		}
	}
	// ST0 bits 6-7 = 0 → success. ST1=ST2=0.
	st0 := uint8(head<<2) & 0x04
	f.result = []byte{st0, 0, 0, uint8(endCyl), uint8(endHead), uint8(endSec), f.cmd[5]}
	f.resPos = 0
	f.raiseIRQ()
}

func (f *FDC) execWriteStub() {
	// Writes aren't backed (boot is read-only). Report a generic error
	// (ST0 bit 6 set) so the guest doesn't think a write succeeded.
	f.result = []byte{0x40, 0x02, 0, f.cmd[2], f.cmd[3], f.cmd[4], f.cmd[5]}
	f.resPos = 0
	f.raiseIRQ()
}
