package pc

import (
	"encoding/binary"
	"fmt"
	"os"

	"github.com/jtolio/tinyemu-go/devices"
)

// ATAController implements a minimal IDE primary-channel controller in PIO
// mode, supporting BOTH plain ATA hard disks and ATAPI CD-ROMs.
//
// The kernel talks to it via the legacy I/O ports 0x1F0-0x1F7 (task-file
// registers) + 0x3F6 (alternate-status / device-control), and the
// controller raises IRQ 14 when commands complete or data is ready.
//
// The same struct handles both kinds; `isCDROM` selects which command
// dispatch path runs. For ATA (isCDROM=false): READ/WRITE SECTORS, LBA28,
// IDENTIFY, CACHE FLUSH. For ATAPI (isCDROM=true): the SCSI MMC subset
// behind the PACKET command — INQUIRY, READ_CAPACITY, READ(10), MODE_SENSE,
// READ_TOC, GET_CONFIGURATION, TEST_UNIT_READY, REQUEST_SENSE.
//
// Scope still excludes: slave drive, DMA, LBA48. Enough to boot a Linux
// i386 kernel from a HDD image OR from a CD-ROM ISO.
//
// References:
//   - ATA/ATAPI-5 spec
//   - MMC-2 (Multi-Media Commands)
//   - Linux drivers/ide/, drivers/scsi/sr.c
type ATAController struct {
	pic    *PIC8259
	device devices.BlockDevice

	// Task-file registers. Names match ATA spec.
	features    uint8 // write at 0x1F1
	errReg      uint8 // read  at 0x1F1
	sectorCount uint8 // 0x1F2 — ATA: count; ATAPI: interrupt-reason (C/D, I/O, REL)
	lbaLow      uint8 // 0x1F3
	lbaMid      uint8 // 0x1F4 — ATAPI: byte count low (per transfer phase)
	lbaHigh     uint8 // 0x1F5 — ATAPI: byte count high
	drvHead     uint8 // 0x1F6 — bit 4 selects drive, bit 6 = LBA mode, bits 3:0 = LBA[27:24]
	status      uint8 // 0x1F7 read; 0x3F6 read returns the same value without clearing IRQ
	control     uint8 // 0x3F6 write — bit 1 = nIEN (mask IRQ), bit 2 = SRST

	// ATA in-flight transfer state (used for disk READ/WRITE SECTORS).
	buffer    [devices.SectorSize]byte
	bufPos    int
	remaining int
	currentLBA uint64
	transferRead bool

	// Whether we have asserted IRQ 14 since the last status-read.
	irqAsserted bool

	// Flag for IDENTIFY's one-shot 512-byte buffer (so we don't try to
	// advance to a "next sector" after the host drains it).
	identifyPending bool

	// ATAPI extension.
	isCDROM       bool
	awaitingPacket bool      // PACKET command issued; host owes us 12 bytes
	packetCmd     [12]byte
	packetCmdPos  int
	// Pending ATAPI data, either response data (e.g. INQUIRY reply) or
	// one CD-ROM sector being streamed to the host as part of a READ(10).
	atapiData     []byte
	atapiDataPos  int
	// For READ(10): remaining CD sectors after atapiData is exhausted.
	atapiSectorsRemaining int
	atapiCurrentLBA       uint32
}

// ATA status register bits.
const (
	ataSR_BSY  = 0x80 // busy executing a command
	ataSR_DRDY = 0x40 // drive ready
	ataSR_DF   = 0x20 // drive write fault
	ataSR_DSC  = 0x10 // drive seek complete
	ataSR_DRQ  = 0x08 // data request — device wants host to transfer a sector
	ataSR_ERR  = 0x01 // an error occurred; details in `errReg`
)

// ATA error register bits.
const (
	ataER_UNC  = 0x40 // uncorrectable data error
	ataER_IDNF = 0x10 // sector ID not found
	ataER_ABRT = 0x04 // command aborted
)

// ATA commands (subset needed for Linux boot).
const (
	ataCMD_READ_PIO        = 0x20
	ataCMD_WRITE_PIO       = 0x30
	ataCMD_VERIFY          = 0x40
	ataCMD_INIT_DEV_PARAMS = 0x91
	ataCMD_PACKET          = 0xA0 // ATAPI: 12-byte SCSI packet command follows
	ataCMD_IDENTIFY_PACKET = 0xA1 // ATAPI IDENTIFY DEVICE
	ataCMD_SET_MULT        = 0xC6
	ataCMD_CACHE_FLUSH     = 0xE7
	ataCMD_IDENTIFY        = 0xEC
	ataCMD_SET_FEAT        = 0xEF
)

// CD-ROM sector size — fixed at 2048 bytes per Mode-1 data CD layout.
const cdSectorSize = 2048

// ATAPI sector-count register bit definitions (the "interrupt reason"
// register on ATAPI). Linux's drivers/scsi/sr.c consults these to know
// whether the upcoming transfer is command-out, data-in, or data-out.
const (
	atapiIR_CD  = 0x01 // 1 = command, 0 = data
	atapiIR_IO  = 0x02 // 1 = device → host, 0 = host → device
)

// MMC (SCSI multi-media commands) — the subset Linux's optical-drive code
// invokes during probe/mount.
const (
	mmcTEST_UNIT_READY   = 0x00
	mmcREQUEST_SENSE     = 0x03
	mmcINQUIRY           = 0x12
	mmcSTART_STOP_UNIT   = 0x1B
	mmcPREVENT_REMOVAL   = 0x1E
	mmcREAD_CAPACITY     = 0x25
	mmcREAD_10           = 0x28
	mmcSEEK_10           = 0x2B
	mmcREAD_TOC          = 0x43
	mmcGET_CONFIGURATION = 0x46
	mmcGET_EVENT_STATUS  = 0x4A
	mmcMODE_SENSE_10     = 0x5A
	mmcREAD_12           = 0xA8
)

// ATA hardware constants.
const (
	ATAPrimaryBase   = 0x1F0 // task-file base for the primary channel
	ATAControlPort   = 0x3F6 // alternate status / device control
	ATAIRQ           = 14    // primary channel IRQ
	ATASecondaryBase = 0x170 // task-file base for the secondary channel
	ATASecondaryCtl  = 0x376 // alt-status / device-control for secondary
	ATASecondaryIRQ  = 15    // secondary channel IRQ
)

// RegisterEmptySecondary stubs out the secondary IDE channel at port 0x170
// so reads return a "no device, link offline" pattern instead of the
// dispatcher's default 0xFF (which has BSY set and makes Linux's libata
// wait forever for the channel to settle). Call this once per PC after
// the primary controller is attached so libata's async probe for both
// channels can complete.
func RegisterEmptySecondary(iod *IOPortDispatcher) {
	// Status registers at 0x177 and 0x376 read as 0 ("link offline,
	// nothing on the bus"). Linux's libata sees status == 0 and aborts
	// the channel without further probing.
	iod.RegisterRead(0x170, 0x177, func(uint16) uint32 { return 0 })
	iod.RegisterWrite(0x170, 0x177, func(uint16, uint32) {})
	iod.RegisterRead(0x376, 0x376, func(uint16) uint32 { return 0 })
	iod.RegisterWrite(0x376, 0x376, func(uint16, uint32) {})
}

// ataDebug logs each port access. Enable with TINYEMU_X86_ATA_DEBUG=1.
var ataDebug = os.Getenv("TINYEMU_X86_ATA_DEBUG") == "1"

// NewATAController creates an ATA hard-disk controller backed by `bd`. If
// bd is nil, the controller reports "no drive" — any access returns 0xFF
// and commands abort. This is the legitimate "channel exists, drive
// absent" configuration that Linux probes for.
func NewATAController(pic *PIC8259, bd devices.BlockDevice) *ATAController {
	a := &ATAController{
		pic:    pic,
		device: bd,
	}
	if bd != nil {
		a.status = ataSR_DRDY | ataSR_DSC
	}
	return a
}

// NewCDROMController creates an ATAPI CD-ROM controller backed by `bd`,
// which must expose the ISO image as a normal 512-byte-sector BlockDevice
// (CD sectors are 2048 bytes; we read four underlying sectors per CD
// sector). The reset signature (sector_count=1, lba_low=1, lba_mid=0x14,
// lba_high=0xEB) is what Linux uses to identify the device as ATAPI.
func NewCDROMController(pic *PIC8259, bd devices.BlockDevice) *ATAController {
	a := NewATAController(pic, bd)
	a.isCDROM = true
	a.applyATAPISignature()
	return a
}

// applyATAPISignature sets the post-reset task-file values that identify
// the device as ATAPI to a probing kernel.
func (a *ATAController) applyATAPISignature() {
	a.sectorCount = 1
	a.lbaLow = 1
	a.lbaMid = 0x14
	a.lbaHigh = 0xEB
}

// Register wires the controller's eight task-file ports plus the control
// port into the I/O dispatcher. Data port reads/writes get a 16-bit-aware
// handler so a single `insw 0x1F0` advances by one word; everything else
// uses 8-bit handlers.
func (a *ATAController) Register(io *IOPortDispatcher) {
	io.RegisterRead(0x1F0, 0x1F0, func(uint16) uint32 { return uint32(a.readDataByte()) })
	io.RegisterWrite(0x1F0, 0x1F0, func(_ uint16, v uint32) { a.writeDataByte(uint8(v)) })
	io.RegisterRead16(0x1F0, 0x1F0, func(uint16) uint32 { return uint32(a.readDataWord()) })
	io.RegisterWrite16(0x1F0, 0x1F0, func(_ uint16, v uint32) { a.writeDataWord(uint16(v)) })

	io.RegisterRead(0x1F1, 0x1F1, func(uint16) uint32 {
		if ataDebug {
			fmt.Fprintf(os.Stderr, "[ata] err0x1F1 read → %02x\n", a.errReg)
		}
		return uint32(a.errReg)
	})
	io.RegisterWrite(0x1F1, 0x1F1, func(_ uint16, v uint32) {
		if ataDebug {
			fmt.Fprintf(os.Stderr, "[ata] feat0x1F1 write %02x\n", uint8(v))
		}
		a.features = uint8(v)
	})

	io.RegisterRead(0x1F2, 0x1F2, func(uint16) uint32 {
		if ataDebug {
			fmt.Fprintf(os.Stderr, "[ata] sc0x1F2 read → %02x\n", a.sectorCount)
		}
		return uint32(a.sectorCount)
	})
	io.RegisterWrite(0x1F2, 0x1F2, func(_ uint16, v uint32) {
		if ataDebug {
			fmt.Fprintf(os.Stderr, "[ata] sc0x1F2 write %02x\n", uint8(v))
		}
		a.sectorCount = uint8(v)
	})

	io.RegisterRead(0x1F3, 0x1F3, func(uint16) uint32 {
		if ataDebug {
			fmt.Fprintf(os.Stderr, "[ata] lbal0x1F3 read → %02x\n", a.lbaLow)
		}
		return uint32(a.lbaLow)
	})
	io.RegisterWrite(0x1F3, 0x1F3, func(_ uint16, v uint32) {
		if ataDebug {
			fmt.Fprintf(os.Stderr, "[ata] lbal0x1F3 write %02x\n", uint8(v))
		}
		a.lbaLow = uint8(v)
	})

	io.RegisterRead(0x1F4, 0x1F4, func(uint16) uint32 {
		if ataDebug {
			fmt.Fprintf(os.Stderr, "[ata] lbam0x1F4 read → %02x\n", a.lbaMid)
		}
		return uint32(a.lbaMid)
	})
	io.RegisterWrite(0x1F4, 0x1F4, func(_ uint16, v uint32) {
		if ataDebug {
			fmt.Fprintf(os.Stderr, "[ata] lbam0x1F4 write %02x\n", uint8(v))
		}
		a.lbaMid = uint8(v)
	})

	io.RegisterRead(0x1F5, 0x1F5, func(uint16) uint32 {
		if ataDebug {
			fmt.Fprintf(os.Stderr, "[ata] lbah0x1F5 read → %02x\n", a.lbaHigh)
		}
		return uint32(a.lbaHigh)
	})
	io.RegisterWrite(0x1F5, 0x1F5, func(_ uint16, v uint32) {
		if ataDebug {
			fmt.Fprintf(os.Stderr, "[ata] lbah0x1F5 write %02x\n", uint8(v))
		}
		a.lbaHigh = uint8(v)
	})

	io.RegisterRead(0x1F6, 0x1F6, func(uint16) uint32 {
		if ataDebug {
			fmt.Fprintf(os.Stderr, "[ata] drv0x1F6 read → %02x\n", a.drvHead)
		}
		return uint32(a.drvHead)
	})
	io.RegisterWrite(0x1F6, 0x1F6, func(_ uint16, v uint32) {
		if ataDebug {
			fmt.Fprintf(os.Stderr, "[ata] drv0x1F6 write %02x\n", uint8(v))
		}
		a.drvHead = uint8(v)
	})

	io.RegisterRead(0x1F7, 0x1F7, func(uint16) uint32 { return uint32(a.readStatusAndClearIRQ()) })
	io.RegisterWrite(0x1F7, 0x1F7, func(_ uint16, v uint32) { a.writeCommand(uint8(v)) })

	// Alternate status / device control. Reading 0x3F6 returns the same
	// status byte WITHOUT clearing the IRQ (per spec) — used by polling
	// loops that don't want to side-effect the IRQ line.
	io.RegisterRead(0x3F6, 0x3F6, func(uint16) uint32 {
		s := a.currentStatus()
		if ataDebug {
			fmt.Fprintf(os.Stderr, "[ata] altstatus0x3F6 read drv=%02x → %02x\n", a.drvHead, s)
		}
		return uint32(s)
	})
	io.RegisterWrite(0x3F6, 0x3F6, func(_ uint16, v uint32) { a.writeControl(uint8(v)) })
}

// --------------------------------------------------------------------------
// Status & IRQ
// --------------------------------------------------------------------------

func (a *ATAController) readStatusAndClearIRQ() uint8 {
	// Reading the status register (NOT the alternate-status register at
	// 0x3F6) acknowledges and clears the pending IRQ.
	if a.irqAsserted {
		a.pic.LowerIRQ(uint8(ATAIRQ))
		a.irqAsserted = false
	}
	s := a.currentStatus()
	if ataDebug {
		fmt.Fprintf(os.Stderr, "[ata] status0x1F7 read drv=%02x → %02x\n", a.drvHead, s)
	}
	return s
}

// currentStatus returns the status byte for whichever drive is currently
// selected. We only model the master (drive 0); when drive 1 is selected
// the channel must respond as "no device" — otherwise Linux's probe of
// the slave gets confused and aborts the entire channel.
func (a *ATAController) currentStatus() uint8 {
	if a.drvHead&0x10 != 0 {
		// Slave selected but we don't have one. Real hardware would
		// either float the bus (read 0xFF) or report 0; libata
		// treats 0x00 as "definitely no device", 0x7F or 0xFF as
		// "pull-up reading from a floating bus = no device".
		return 0x00
	}
	return a.status
}

func (a *ATAController) raiseIRQ() {
	if a.control&0x02 != 0 {
		return // nIEN set: interrupts masked at the device level
	}
	a.pic.RaiseIRQ(uint8(ATAIRQ))
	a.irqAsserted = true
}

// --------------------------------------------------------------------------
// Command path
// --------------------------------------------------------------------------

func (a *ATAController) writeCommand(cmd uint8) {
	if ataDebug {
		fmt.Fprintf(os.Stderr, "[ata] cmd=%02x sc=%d lba=%07x drv=%02x cdrom=%v\n",
			cmd, a.sectorCount, a.currentLBA28(), a.drvHead, a.isCDROM)
	}

	if a.device == nil {
		a.abort(ataER_ABRT)
		return
	}
	if a.drvHead&0x10 != 0 {
		// Drive 1 (slave) selected. We don't have one.
		a.abort(ataER_ABRT)
		return
	}

	// ATAPI command dispatch: the kernel issues IDENTIFY first (which
	// must abort with the ATAPI signature in the task-file), then learns
	// to use PACKET.
	if a.isCDROM {
		switch cmd {
		case ataCMD_IDENTIFY:
			// ATAPI spec: an IDENTIFY DEVICE to a packet device aborts
			// and leaves the ATAPI signature in lbaMid/lbaHigh — the
			// kernel uses this to detect the device type.
			a.applyATAPISignature()
			a.abort(ataER_ABRT)
			return
		case ataCMD_IDENTIFY_PACKET:
			a.fillIdentifyPacketBuffer()
			a.bufPos = 0
			a.remaining = 0
			a.transferRead = true
			a.identifyPending = true
			a.errReg = 0
			a.status = ataSR_DRDY | ataSR_DSC | ataSR_DRQ
			a.raiseIRQ()
			return
		case ataCMD_PACKET:
			a.awaitingPacket = true
			a.packetCmdPos = 0
			a.transferRead = false // host writes the 12-byte command first
			// Interrupt reason: command (C/D=1), host-to-device (I/O=0).
			a.sectorCount = atapiIR_CD
			a.errReg = 0
			a.status = ataSR_DRDY | ataSR_DSC | ataSR_DRQ
			// No IRQ at PACKET issue — the host pushes the 12-byte
			// command immediately; the IRQ comes after data is ready.
			return
		case ataCMD_SET_FEAT, ataCMD_INIT_DEV_PARAMS:
			a.status = ataSR_DRDY | ataSR_DSC
			a.errReg = 0
			a.raiseIRQ()
			return
		default:
			a.abort(ataER_ABRT)
			return
		}
	}

	switch cmd {
	case ataCMD_IDENTIFY:
		a.fillIdentifyBuffer()
		a.bufPos = 0
		a.remaining = 0
		a.transferRead = true
		a.identifyPending = true
		a.errReg = 0
		a.status = ataSR_DRDY | ataSR_DSC | ataSR_DRQ
		a.raiseIRQ()

	case ataCMD_READ_PIO:
		n := int(a.sectorCount)
		if n == 0 {
			n = 256
		}
		a.currentLBA = a.currentLBA28()
		a.remaining = n
		a.transferRead = true
		a.identifyPending = false
		a.errReg = 0
		a.loadCurrentSector()

	case ataCMD_WRITE_PIO:
		n := int(a.sectorCount)
		if n == 0 {
			n = 256
		}
		a.currentLBA = a.currentLBA28()
		a.remaining = n
		a.transferRead = false
		a.identifyPending = false
		a.errReg = 0
		a.bufPos = 0
		// WRITE PIO does NOT raise an IRQ for the first sector — the host
		// just pushes data immediately. IRQ raises after each completed
		// sector. Status: DRQ asserted, host writes the buffer.
		a.status = ataSR_DRDY | ataSR_DSC | ataSR_DRQ

	case ataCMD_CACHE_FLUSH:
		if err := a.device.Flush(); err != nil {
			a.abort(ataER_ABRT)
			return
		}
		a.status = ataSR_DRDY | ataSR_DSC
		a.errReg = 0
		a.raiseIRQ()

	case ataCMD_SET_FEAT, ataCMD_SET_MULT, ataCMD_INIT_DEV_PARAMS, ataCMD_VERIFY:
		// Accept silently. SET_FEATURES is used to enable/disable read-ahead,
		// caching, etc. — for our minimal PIO controller these are no-ops.
		a.status = ataSR_DRDY | ataSR_DSC
		a.errReg = 0
		a.raiseIRQ()

	default:
		a.abort(ataER_ABRT)
	}
}

func (a *ATAController) writeControl(v uint8) {
	prev := a.control
	a.control = v
	if ataDebug {
		fmt.Fprintf(os.Stderr, "[ata] ctrl=%02x prev=%02x cdrom=%v\n", v, prev, a.isCDROM)
	}
	if v&0x04 != 0 && prev&0x04 == 0 {
		// Software-reset edge.
		a.reset()
	}
}

func (a *ATAController) reset() {
	a.errReg = 0x01 // diagnostic passed
	a.drvHead &= 0x10
	a.bufPos = 0
	a.remaining = 0
	a.identifyPending = false
	a.awaitingPacket = false
	a.packetCmdPos = 0
	a.atapiData = nil
	a.atapiDataPos = 0
	a.atapiSectorsRemaining = 0
	if a.isCDROM {
		a.applyATAPISignature()
	} else {
		a.sectorCount = 1
		a.lbaLow = 1
		a.lbaMid = 0
		a.lbaHigh = 0
	}
	if a.device != nil {
		a.status = ataSR_DRDY | ataSR_DSC
	} else {
		a.status = 0
	}
	if a.irqAsserted {
		a.pic.LowerIRQ(uint8(ATAIRQ))
		a.irqAsserted = false
	}
}

func (a *ATAController) abort(errBits uint8) {
	a.errReg = errBits
	a.status = ataSR_DRDY | ataSR_ERR
	a.bufPos = 0
	a.remaining = 0
	a.raiseIRQ()
}

func (a *ATAController) currentLBA28() uint64 {
	return uint64(a.lbaLow) |
		(uint64(a.lbaMid) << 8) |
		(uint64(a.lbaHigh) << 16) |
		(uint64(a.drvHead&0x0F) << 24)
}

// --------------------------------------------------------------------------
// Data transfer
// --------------------------------------------------------------------------

// loadCurrentSector reads one sector from the backing device into the
// internal buffer and signals "ready for the host to read it" (DRQ + IRQ).
func (a *ATAController) loadCurrentSector() {
	if int64(a.currentLBA) >= a.device.GetSectorCount() {
		a.abort(ataER_IDNF)
		return
	}
	n, err := a.device.ReadSectors(a.currentLBA, a.buffer[:], 1)
	if err != nil || n != devices.SectorSize {
		a.abort(ataER_UNC)
		return
	}
	a.bufPos = 0
	a.status = ataSR_DRDY | ataSR_DSC | ataSR_DRQ
	a.raiseIRQ()
}

// flushCurrentSector writes the buffer back to the device after the host
// has filled it.
func (a *ATAController) flushCurrentSector() {
	if int64(a.currentLBA) >= a.device.GetSectorCount() {
		a.abort(ataER_IDNF)
		return
	}
	n, err := a.device.WriteSectors(a.currentLBA, a.buffer[:], 1)
	if err != nil || n != devices.SectorSize {
		a.abort(ataER_UNC)
		return
	}
	a.currentLBA++
	a.remaining--
	a.bufPos = 0
	if a.remaining == 0 {
		a.status = ataSR_DRDY | ataSR_DSC
	} else {
		a.status = ataSR_DRDY | ataSR_DSC | ataSR_DRQ
	}
	a.raiseIRQ()
}

// readDataByte returns the next byte the host is requesting.
func (a *ATAController) readDataByte() uint8 {
	if a.status&ataSR_DRQ == 0 || !a.transferRead {
		return 0xFF
	}
	if a.atapiData != nil {
		b := a.atapiData[a.atapiDataPos]
		a.atapiDataPos++
		if a.atapiDataPos >= len(a.atapiData) {
			a.afterATAPIChunk()
		}
		return b
	}
	b := a.buffer[a.bufPos]
	a.bufPos++
	if a.bufPos >= devices.SectorSize {
		a.afterSectorRead()
	}
	return b
}

// readDataWord returns the next 2 bytes (little-endian) and advances by 2.
func (a *ATAController) readDataWord() uint16 {
	if a.status&ataSR_DRQ == 0 || !a.transferRead {
		return 0xFFFF
	}
	if a.atapiData != nil {
		w := uint16(a.atapiData[a.atapiDataPos]) | uint16(a.atapiData[a.atapiDataPos+1])<<8
		a.atapiDataPos += 2
		if a.atapiDataPos >= len(a.atapiData) {
			a.afterATAPIChunk()
		}
		return w
	}
	w := uint16(a.buffer[a.bufPos]) | uint16(a.buffer[a.bufPos+1])<<8
	a.bufPos += 2
	if a.bufPos >= devices.SectorSize {
		a.afterSectorRead()
	}
	return w
}

func (a *ATAController) afterSectorRead() {
	if a.identifyPending {
		a.identifyPending = false
		a.status = ataSR_DRDY | ataSR_DSC
		return
	}
	a.currentLBA++
	a.remaining--
	if a.remaining > 0 {
		a.loadCurrentSector()
	} else {
		a.status = ataSR_DRDY | ataSR_DSC
	}
}

func (a *ATAController) writeDataByte(b uint8) {
	if a.status&ataSR_DRQ == 0 {
		return
	}
	if a.awaitingPacket {
		a.packetCmd[a.packetCmdPos] = b
		a.packetCmdPos++
		if a.packetCmdPos >= 12 {
			a.dispatchPacketCommand()
		}
		return
	}
	if a.transferRead {
		return
	}
	a.buffer[a.bufPos] = b
	a.bufPos++
	if a.bufPos >= devices.SectorSize {
		a.flushCurrentSector()
	}
}

func (a *ATAController) writeDataWord(w uint16) {
	if a.status&ataSR_DRQ == 0 {
		return
	}
	if a.awaitingPacket {
		a.packetCmd[a.packetCmdPos] = uint8(w)
		a.packetCmd[a.packetCmdPos+1] = uint8(w >> 8)
		a.packetCmdPos += 2
		if a.packetCmdPos >= 12 {
			a.dispatchPacketCommand()
		}
		return
	}
	if a.transferRead {
		return
	}
	a.buffer[a.bufPos] = uint8(w)
	a.buffer[a.bufPos+1] = uint8(w >> 8)
	a.bufPos += 2
	if a.bufPos >= devices.SectorSize {
		a.flushCurrentSector()
	}
}

// --------------------------------------------------------------------------
// IDENTIFY DEVICE
// --------------------------------------------------------------------------

// fillIdentifyBuffer populates the 512-byte buffer with the 256-word
// IDENTIFY DEVICE response — what Linux reads to learn the drive's geometry
// and capabilities.
func (a *ATAController) fillIdentifyBuffer() {
	for i := range a.buffer {
		a.buffer[i] = 0
	}
	setW := func(idx int, v uint16) {
		binary.LittleEndian.PutUint16(a.buffer[idx*2:], v)
	}
	// ATA strings are stored with byte-swapped pairs within each word.
	setStr := func(start, words int, s string) {
		buf := []byte(s)
		// Pad with spaces to exactly `words` words.
		full := make([]byte, words*2)
		for i := range full {
			full[i] = ' '
		}
		copy(full, buf)
		for i := 0; i < words; i++ {
			a.buffer[(start+i)*2+0] = full[i*2+1]
			a.buffer[(start+i)*2+1] = full[i*2+0]
		}
	}

	sectors := a.device.GetSectorCount()

	// Word 0: bit 6 set → ATA fixed device; bit 15 clear → ATA (not ATAPI).
	setW(0, 0x0040)
	// Word 1: cylinders (legacy CHS).
	setW(1, 16383)
	// Word 3: heads.
	setW(3, 16)
	// Word 6: sectors per track.
	setW(6, 63)
	// Words 10-19: serial number (20 chars).
	setStr(10, 10, "TINYEMU-0000000001  ")
	// Words 23-26: firmware revision (8 chars).
	setStr(23, 4, "1.0     ")
	// Words 27-46: model name (40 chars).
	setStr(27, 20, "tinyemu-go ATA disk                     ")
	// Word 47: bit 15 must be 1; low byte = max sectors per transfer for
	// READ/WRITE MULTIPLE. We support 1.
	setW(47, 0x8001)
	// Word 49: capabilities — bit 9 = LBA supported.
	setW(49, 0x0200)
	// Word 53: 0x0006 → words 64-70 and 88 are valid.
	setW(53, 0x0006)
	// Words 60-61: total user-addressable sectors (LBA28).
	lba28 := uint32(sectors)
	if lba28 > 0x0FFFFFFF {
		lba28 = 0x0FFFFFFF
	}
	setW(60, uint16(lba28))
	setW(61, uint16(lba28>>16))
	// Word 80: ATA major version bits — bit 5 = ATA-5.
	setW(80, 0x0020)
	// Word 82: command-set support — bit 14 = NOP supported.
	setW(82, 0x4000)
	// Word 83: bit 14 always 1; bit 10 = LBA48 supported (we don't).
	setW(83, 0x4000)
}

// --------------------------------------------------------------------------
// ATAPI / SCSI MMC
// --------------------------------------------------------------------------

// fillIdentifyPacketBuffer populates the IDENTIFY PACKET DEVICE response
// (used by ATAPI CD-ROM). Differs from the ATA IDENTIFY chiefly in word 0,
// where bits 14:13 = 01 (10 = "command packet device") and bit 7 = 1.
func (a *ATAController) fillIdentifyPacketBuffer() {
	for i := range a.buffer {
		a.buffer[i] = 0
	}
	setW := func(idx int, v uint16) {
		binary.LittleEndian.PutUint16(a.buffer[idx*2:], v)
	}
	setStr := func(start, words int, s string) {
		full := make([]byte, words*2)
		for i := range full {
			full[i] = ' '
		}
		copy(full, s)
		for i := 0; i < words; i++ {
			a.buffer[(start+i)*2+0] = full[i*2+1]
			a.buffer[(start+i)*2+1] = full[i*2+0]
		}
	}
	// Word 0: bits 15:14 = 10 (ATAPI), bits 12:8 = 00101 (CD-ROM device),
	// bits 7:6 = 11 (removable), bits 1:0 = 00 (12-byte packet).
	setW(0, 0x8580)
	setStr(10, 10, "TINYEMU-CDROM-00000 ")
	setStr(23, 4, "1.0     ")
	setStr(27, 20, "tinyemu-go ATAPI CD-ROM                 ")
	// Word 49: bit 9 = LBA, bit 8 = DMA (we don't support DMA).
	setW(49, 0x0200)
	// Word 53: 0x0006 → words 64-70 and 88 are valid.
	setW(53, 0x0006)
	// Word 80: ATA-5 supported.
	setW(80, 0x0020)
}

// dispatchPacketCommand parses the 12-byte SCSI command the host just
// pushed into packetCmd and produces the appropriate response. Most
// commands set a.atapiData to bytes the host must then drain.
func (a *ATAController) dispatchPacketCommand() {
	a.awaitingPacket = false
	op := a.packetCmd[0]
	if ataDebug {
		fmt.Fprintf(os.Stderr, "[atapi] op=%02x cmd=% x\n", op, a.packetCmd[:])
	}

	switch op {
	case mmcTEST_UNIT_READY:
		a.atapiComplete()

	case mmcREQUEST_SENSE:
		// Linux's drives.sr.c calls this after a failed command. Return
		// "no error" (sense key = 0). Allocation length is byte 4 of cmd.
		alloc := int(a.packetCmd[4])
		buf := make([]byte, 18)
		buf[0] = 0x70                  // current error code
		buf[2] = 0x00                  // sense key = NO SENSE
		buf[7] = 0x0A                  // additional sense length
		a.startATAPIDataIn(truncate(buf, alloc))

	case mmcINQUIRY:
		alloc := int(a.packetCmd[3])<<8 | int(a.packetCmd[4])
		buf := make([]byte, 36)
		buf[0] = 0x05                  // CD-ROM device
		buf[1] = 0x80                  // removable
		buf[2] = 0x00                  // ANSI version (0 — pre-SCSI)
		buf[3] = 0x21                  // response format
		buf[4] = byte(len(buf) - 5)    // additional length
		copy(buf[8:16], "TINYEMU ")    // vendor (8 bytes, space-padded)
		copy(buf[16:32], "TINYEMU CD-ROM  ") // product (16 bytes)
		copy(buf[32:36], "1.00")       // revision (4 bytes)
		a.startATAPIDataIn(truncate(buf, alloc))

	case mmcREAD_CAPACITY:
		// 8-byte response: last LBA (big-endian) + block size (big-endian).
		lastLBA := uint32(a.device.GetSectorCount()/4) - 1 // sectors are 2048 = 4×512
		buf := []byte{
			byte(lastLBA >> 24), byte(lastLBA >> 16),
			byte(lastLBA >> 8), byte(lastLBA),
			0x00, 0x00, 0x08, 0x00, // block size = 2048
		}
		a.startATAPIDataIn(buf)

	case mmcREAD_10:
		lba := uint32(a.packetCmd[2])<<24 | uint32(a.packetCmd[3])<<16 |
			uint32(a.packetCmd[4])<<8 | uint32(a.packetCmd[5])
		count := uint32(a.packetCmd[7])<<8 | uint32(a.packetCmd[8])
		if count == 0 {
			a.atapiComplete()
			return
		}
		a.atapiCurrentLBA = lba
		a.atapiSectorsRemaining = int(count)
		a.loadNextATAPISector()

	case mmcREAD_12:
		lba := uint32(a.packetCmd[2])<<24 | uint32(a.packetCmd[3])<<16 |
			uint32(a.packetCmd[4])<<8 | uint32(a.packetCmd[5])
		count := uint32(a.packetCmd[6])<<24 | uint32(a.packetCmd[7])<<16 |
			uint32(a.packetCmd[8])<<8 | uint32(a.packetCmd[9])
		if count == 0 {
			a.atapiComplete()
			return
		}
		a.atapiCurrentLBA = lba
		a.atapiSectorsRemaining = int(count)
		a.loadNextATAPISector()

	case mmcMODE_SENSE_10:
		// Build a minimal mode-sense response with no actual mode pages —
		// the kernel just needs the parameter list header (8 bytes) +
		// optionally a block-descriptor (8 bytes). We return the header
		// indicating "no pages, no block descriptor."
		alloc := int(a.packetCmd[7])<<8 | int(a.packetCmd[8])
		buf := []byte{
			0x00, 0x06, // mode data length = 6 (excluding this field itself)
			0x00,       // medium type
			0x00,       // device-specific (write-protect = 0)
			0x00, 0x00, // reserved
			0x00, 0x00, // block descriptor length = 0
		}
		a.startATAPIDataIn(truncate(buf, alloc))

	case mmcREAD_TOC:
		alloc := int(a.packetCmd[7])<<8 | int(a.packetCmd[8])
		// Build a minimal single-session TOC: header + one track descriptor.
		lastLBA := uint32(a.device.GetSectorCount()/4) - 1
		buf := []byte{
			0x00, 0x12, // TOC data length = 18
			0x01, 0x01, // first / last track
			// Track 1 descriptor (8 bytes)
			0x00,       // reserved
			0x14,       // ADR/control (data track)
			0x01,       // track number
			0x00,       // reserved
			0x00, 0x00, 0x00, 0x00, // track start LBA (track 1 starts at LBA 0)
			// Leadout descriptor (8 bytes)
			0x00,
			0x14,
			0xAA, // leadout track number
			0x00,
			byte(lastLBA >> 24), byte(lastLBA >> 16),
			byte(lastLBA >> 8), byte(lastLBA),
		}
		a.startATAPIDataIn(truncate(buf, alloc))

	case mmcGET_CONFIGURATION:
		alloc := int(a.packetCmd[7])<<8 | int(a.packetCmd[8])
		// Feature-header (8 bytes) + Profile-list feature (4 + 4 bytes)
		// claiming CD-ROM (profile 0x0008).
		buf := []byte{
			0x00, 0x00, 0x00, 0x10, // data length
			0x00, 0x00,             // reserved
			0x00, 0x08,             // current profile = CD-ROM
			// Profile-list feature descriptor
			0x00, 0x00,             // feature code = 0 (Profile List)
			0x03,                   // version 0, persistent, current
			0x04,                   // additional length
			0x00, 0x08, 0x01, 0x00, // profile 0x0008 (CD-ROM), current
		}
		a.startATAPIDataIn(truncate(buf, alloc))

	case mmcGET_EVENT_STATUS:
		// 4-byte header reporting "no events".
		buf := []byte{0x00, 0x02, 0x00, 0x00}
		a.startATAPIDataIn(buf)

	case mmcSTART_STOP_UNIT, mmcPREVENT_REMOVAL, mmcSEEK_10:
		a.atapiComplete()

	default:
		// Unknown command — abort with sense ABRT.
		a.errReg = ataER_ABRT | 0x40 // sense key 4 in upper nibble = ABORTED_COMMAND
		a.sectorCount = atapiIR_CD | atapiIR_IO
		a.status = ataSR_DRDY | ataSR_ERR
		a.raiseIRQ()
	}
}

// startATAPIDataIn sets up `data` to be drained by the host. The byte
// count is reflected in lbaMid/lbaHigh as ATAPI requires.
func (a *ATAController) startATAPIDataIn(data []byte) {
	a.atapiData = data
	a.atapiDataPos = 0
	a.transferRead = true
	// Interrupt reason: I/O=1 (device → host), C/D=0 (data).
	a.sectorCount = atapiIR_IO
	a.lbaMid = uint8(len(data))
	a.lbaHigh = uint8(len(data) >> 8)
	if len(data) == 0 {
		a.atapiComplete()
		return
	}
	a.errReg = 0
	a.status = ataSR_DRDY | ataSR_DSC | ataSR_DRQ
	a.raiseIRQ()
}

// loadNextATAPISector reads one 2048-byte CD sector from the backing
// device (as 4 underlying 512-byte sectors) and stages it for the host.
func (a *ATAController) loadNextATAPISector() {
	totalCDSectors := uint32(a.device.GetSectorCount() / 4)
	if a.atapiCurrentLBA >= totalCDSectors {
		a.errReg = ataER_IDNF | 0x50 // sense key 5 = ILLEGAL_REQUEST
		a.sectorCount = atapiIR_CD | atapiIR_IO
		a.status = ataSR_DRDY | ataSR_ERR
		a.raiseIRQ()
		return
	}
	buf := make([]byte, cdSectorSize)
	baseLBA := uint64(a.atapiCurrentLBA) * 4
	n, err := a.device.ReadSectors(baseLBA, buf, 4)
	if err != nil || n != cdSectorSize {
		a.errReg = ataER_UNC | 0x30 // sense key 3 = MEDIUM_ERROR
		a.sectorCount = atapiIR_CD | atapiIR_IO
		a.status = ataSR_DRDY | ataSR_ERR
		a.raiseIRQ()
		return
	}
	a.atapiCurrentLBA++
	a.atapiSectorsRemaining--
	a.startATAPIDataIn(buf)
}

// afterATAPIChunk runs after the host has drained the current atapiData
// slice. For READ(10), this means loading the next CD sector; for one-shot
// commands (INQUIRY, READ_CAPACITY, …) it completes the command.
func (a *ATAController) afterATAPIChunk() {
	a.atapiData = nil
	a.atapiDataPos = 0
	if a.atapiSectorsRemaining > 0 {
		a.loadNextATAPISector()
		return
	}
	a.atapiComplete()
}

// atapiComplete signals end-of-command on the ATAPI bus and raises IRQ 14.
func (a *ATAController) atapiComplete() {
	a.atapiData = nil
	a.atapiDataPos = 0
	a.atapiSectorsRemaining = 0
	a.sectorCount = atapiIR_CD | atapiIR_IO // command complete
	a.errReg = 0
	a.status = ataSR_DRDY | ataSR_DSC
	a.raiseIRQ()
}

// truncate clamps a buffer to `alloc` bytes (a SCSI allocation-length is
// the maximum the host wants to receive; the device must not send more).
func truncate(buf []byte, alloc int) []byte {
	if alloc > 0 && alloc < len(buf) {
		return buf[:alloc]
	}
	return buf
}

// --------------------------------------------------------------------------
// Cleanup
// --------------------------------------------------------------------------

// Close flushes and releases the backing block device.
func (a *ATAController) Close() error {
	if a.device == nil {
		return nil
	}
	_ = a.device.Flush()
	return a.device.Close()
}
