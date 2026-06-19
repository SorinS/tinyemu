package pc

import (
	"encoding/binary"
	"fmt"
	"math/bits"
	"os"

	"github.com/sorins/tinyemu-go/mem"
)

var fwCfgDebug = os.Getenv("TINYEMU_FWCFG_DEBUG") == "1"

// fwCfgDMAEnabled gates the (newer) DMA interface. Default off: with it
// clear, ID reports only bit 0 and OVMF/SeaBIOS use the byte-at-a-time
// port path — the exact behaviour that boots today. Set TINYEMU_FWCFG_DMA=1
// to advertise + service DMA (faster bulk reads, matches QEMU). Flag-gated
// so a DMA bug can't regress a currently-working firmware boot; remove the
// gate once it's proven in the field.
var fwCfgDMAEnabled = os.Getenv("TINYEMU_FWCFG_DMA") == "1"

// fwCfgRamfb gates a ramfb framebuffer (QEMU's etc/ramfb). When set,
// fw_cfg exposes a writable etc/ramfb item; OVMF's QemuRamfbDxe writes the
// framebuffer config there (over fw_cfg DMA) and publishes a GOP, which is
// what GOP-requiring guests (e.g. go-boot's screenInfo) need to proceed.
// temu is headless, so the framebuffer is write-only (held in RAM). ramfb
// is configured over the DMA interface, so enabling it implies DMA.
var fwCfgRamfb = os.Getenv("TINYEMU_RAMFB") == "1"

// ramfbCfgSize is the size of QEMU's RAMFBCfg struct written to etc/ramfb:
// addr(8) + fourcc(4) + flags(4) + width(4) + height(4) + stride(4).
const ramfbCfgSize = 28

// fw_cfg — QEMU's hypervisor-to-firmware paravirt channel. SeaBIOS,
// OVMF/EDK2, and the various coreboot SeaBIOS payloads all consume it.
// The host writes a 16-bit "selector" to I/O port 0x510 and reads
// bytes back from I/O port 0x511; reads auto-increment a per-selector
// offset so a multi-byte structure can be drained byte-by-byte. We
// don't implement the DMA interface (0x514) because SeaBIOS falls
// back to the port-IO path when the DMA selector reads zero.
//
// Why we're implementing this: SeaBIOS without fw_cfg writes a
// stub RSDP into 0xE0000..0xFFFFF with a placeholder XsdtAddress
// it expects fw_cfg to populate. Anything that walks the BIOS-ROM
// area for an ACPI RSDP (Pure64, BareMetal, future Linux-on-SeaBIOS
// paths that need ACPI) then halts because the published RSDP fails
// its checksum. With fw_cfg in place SeaBIOS publishes a real RSDP,
// in a place SeaBIOS chooses, with the BiosLinker-patched pointers
// and checksums it computes itself. The whole "stamp tables into
// the BIOS shadow" race goes away.

const (
	fwCfgSelectorPort = 0x510 // 16-bit write
	fwCfgDataPort     = 0x511 // 8-bit read, auto-increment offset

	// QEMU-defined "well-known" selectors that SeaBIOS probes before
	// it starts using the file directory. Anything else lives in the
	// file directory and gets a per-file selector at runtime.
	fwCfgSelSignature   = 0x0000
	fwCfgSelID          = 0x0001
	fwCfgSelFileDir     = 0x0019
	fwCfgSelFirstCustom = 0x0020

	// fw_cfg "file directory" entry layout (network/big-endian on the
	// wire) per docs/specs/fw_cfg.rst in the qemu tree:
	//   4 bytes  size
	//   2 bytes  select
	//   2 bytes  reserved
	//  56 bytes  name (NUL-padded ASCII)
	fwCfgFileNameSize  = 56
	fwCfgFileEntrySize = 4 + 2 + 2 + fwCfgFileNameSize // = 64

	// DMA interface (QEMU docs/specs/fw_cfg.rst). The 64-bit, big-endian
	// control-structure address register sits at 0x514 (high 32 bits) and
	// 0x518 (low 32 bits); writing the low half triggers the transfer.
	fwCfgDMAAddrHi = 0x514
	fwCfgDMAAddrLo = 0x518

	// FWCfgDmaAccess.control bits (the struct is 16 bytes, all big-endian:
	// control@0, length@4, address@8).
	fwCfgDmaError  = 0x01
	fwCfgDmaRead   = 0x02
	fwCfgDmaSkip   = 0x04
	fwCfgDmaSelect = 0x08
	fwCfgDmaWrite  = 0x10
)

// fwCfgFile is a named blob the firmware can read end-to-end. select
// is the 16-bit selector we hand to readers (so they don't have to
// know the directory order).
type fwCfgFile struct {
	name     string
	data     []byte
	sel      uint16
	writable bool // guest may write it back over the DMA WRITE path
}

// fwCfg is the per-machine fw_cfg state. The selector field tracks
// the currently-selected "file" (or one of the well-known direct
// selectors); offset is the next byte index inside that file.
type fwCfg struct {
	selector uint16
	offset   uint32
	files    []fwCfgFile
	dirCache []byte // assembled on first access; rebuilt if files change

	// DMA interface state (only used when fwCfgDMAEnabled). mem is the
	// guest physical memory the DMA transfers read from / write to; it is
	// wired by the machine before Register. dmaAddr accumulates the 64-bit
	// control-structure address across the two 32-bit register writes.
	mem     *mem.PhysMemoryMap
	dma     bool
	ramfb   bool
	dmaAddr uint64
}

// newFWCfg creates an fw_cfg device with the standard signature/ID
// entries already in place. Callers add their own files (ACPI tables,
// kernel cmdline, etc.) via addFile *before* registering the device
// on the I/O bus — once SeaBIOS sees the file directory it caches
// names → selectors, so new files added after boot won't be picked up.
func newFWCfg() *fwCfg {
	// ramfb is configured over the DMA WRITE path, so it implies DMA.
	return &fwCfg{dma: fwCfgDMAEnabled || fwCfgRamfb, ramfb: fwCfgRamfb}
}

// addFile appends a named file to the directory and assigns it the
// next available selector. The returned selector is mostly for
// tests — the actual lookup at runtime is by name through the file
// directory at selector 0x19.
func (f *fwCfg) addFile(name string, data []byte) uint16 {
	if len(name) >= fwCfgFileNameSize {
		// Truncate rather than panic — fw_cfg names are NUL-padded
		// in 56 bytes and longer names would silently get cut on the
		// wire anyway. A panic here would just move the failure to
		// the wrong layer.
		name = name[:fwCfgFileNameSize-1]
	}
	sel := uint16(fwCfgSelFirstCustom) + uint16(len(f.files))
	f.files = append(f.files, fwCfgFile{name: name, data: data, sel: sel})
	f.dirCache = nil
	return sel
}

// addFileWritable is addFile for an item the guest may write back over the
// DMA WRITE path (e.g. etc/ramfb, where the firmware writes the framebuffer
// config). The backing slice is mutated in place by the write.
func (f *fwCfg) addFileWritable(name string, data []byte) uint16 {
	sel := f.addFile(name, data)
	f.files[len(f.files)-1].writable = true
	return sel
}

// writableForSelector returns the backing slice of a writable item, or nil
// if the selector doesn't map to one (so DMA WRITE can't touch read-only
// items or the inlined well-known selectors).
func (f *fwCfg) writableForSelector(sel uint16) []byte {
	for i := range f.files {
		if f.files[i].sel == sel && f.files[i].writable {
			return f.files[i].data
		}
	}
	return nil
}

// writeSelector implements writes to port 0x510. SeaBIOS issues
// 16-bit OUTs but some BIOSes split into two 8-bit OUTs; we accept
// either via the standard I/O bus dispatch (the caller already
// composes the 16-bit value).
func (f *fwCfg) writeSelector(val uint32) {
	f.selector = uint16(val)
	f.offset = 0
	if fwCfgDebug {
		name := "<unknown>"
		switch f.selector {
		case fwCfgSelSignature:
			name = "<signature>"
		case fwCfgSelID:
			name = "<id>"
		case fwCfgSelFileDir:
			name = "<file-dir>"
		default:
			for _, fl := range f.files {
				if fl.sel == f.selector {
					name = fl.name
					break
				}
			}
		}
		fmt.Fprintf(os.Stderr, "[fwcfg] select %#x (%s)\n", f.selector, name)
	}
}

// readData implements byte reads from port 0x511. Returns zero past
// the end of the selected file — matches QEMU's behaviour and lets
// SeaBIOS use known-end signatures rather than a separate length
// query.
func (f *fwCfg) readData() uint32 {
	data := f.dataForSelector(f.selector)
	if int(f.offset) >= len(data) {
		if fwCfgDebug {
			fmt.Fprintf(os.Stderr, "[fwcfg] read sel=%#x off=%d → 0 (past end, len=%d)\n",
				f.selector, f.offset, len(data))
		}
		return 0
	}
	b := data[f.offset]
	if fwCfgDebug {
		fmt.Fprintf(os.Stderr, "[fwcfg] read sel=%#x off=%d → %#02x\n",
			f.selector, f.offset, b)
	}
	f.offset++
	return uint32(b)
}

// dataForSelector returns the byte slice associated with `sel`. The
// well-known selectors are inlined here; everything else falls
// through to the file directory lookup. Selectors that don't map to
// anything return nil (which readData turns into a zero byte).
func (f *fwCfg) dataForSelector(sel uint16) []byte {
	switch sel {
	case fwCfgSelSignature:
		// SeaBIOS probes this at boot to decide whether fw_cfg is
		// present. The four bytes must be exactly "QEMU"; anything
		// else and SeaBIOS treats the channel as absent.
		return []byte("QEMU")
	case fwCfgSelID:
		// Feature bits, 4 bytes little-endian. Bit 0 = fw_cfg present;
		// bit 1 = DMA interface. With DMA advertised, OVMF/SeaBIOS use
		// the bulk DMA path; otherwise they fall back to the slow
		// byte-at-a-time port reads.
		if f.dma {
			return []byte{0x03, 0x00, 0x00, 0x00}
		}
		return []byte{0x01, 0x00, 0x00, 0x00}
	case fwCfgSelFileDir:
		return f.fileDirectory()
	case 0x0005: // FW_CFG_NB_CPUS — 16-bit LE, present CPU count
		return []byte{0x01, 0x00}
	case 0x000F: // FW_CFG_MAX_CPUS — 16-bit LE
		return []byte{0x01, 0x00}
	case 0x000E: // FW_CFG_BOOT_MENU — 16-bit LE flag (disabled)
		return []byte{0x00, 0x00}
	}
	for i := range f.files {
		if f.files[i].sel == sel {
			return f.files[i].data
		}
	}
	return nil
}

// Register wires fw_cfg into the I/O port dispatcher. After this
// returns, writes to port 0x510 select a file and reads from 0x511
// drain the selected file byte by byte. SeaBIOS issues a single
// 16-bit OUT to 0x510, so the selector handler is registered via
// RegisterWrite16; the data port is byte-only.
func (f *fwCfg) Register(io *IOPortDispatcher) {
	io.RegisterWrite16(fwCfgSelectorPort, fwCfgSelectorPort, func(_ uint16, v uint32) {
		f.writeSelector(v)
	})
	// Some firmware (and our own test helpers) may also OUT 8 bits
	// at a time across the 16-bit port; treat the second byte write
	// as the high half of the selector. SeaBIOS doesn't need this
	// today but a future BIOS port might.
	io.RegisterWrite(fwCfgSelectorPort, fwCfgSelectorPort+1, func(port uint16, v uint32) {
		shift := uint16(port-fwCfgSelectorPort) * 8
		f.selector = (f.selector &^ (0xFF << shift)) | (uint16(v&0xFF) << shift)
		f.offset = 0
	})
	io.RegisterRead(fwCfgDataPort, fwCfgDataPort, func(_ uint16) uint32 {
		return f.readData()
	})

	// DMA interface: the 64-bit big-endian control-structure address is
	// written as two 32-bit halves; the low-half write triggers. Only
	// registered when DMA is advertised so the default port path is
	// untouched. Completion is signalled by writing the struct's control
	// field back in guest memory (synchronous here), so no read handler
	// is needed.
	if f.dma {
		io.RegisterWrite32(fwCfgDMAAddrHi, fwCfgDMAAddrHi, func(_ uint16, v uint32) {
			f.dmaAddr = (f.dmaAddr & 0x00000000FFFFFFFF) | (uint64(bits.ReverseBytes32(v)) << 32)
		})
		io.RegisterWrite32(fwCfgDMAAddrLo, fwCfgDMAAddrLo, func(_ uint16, v uint32) {
			f.dmaAddr = (f.dmaAddr &^ 0x00000000FFFFFFFF) | uint64(bits.ReverseBytes32(v))
			f.dmaProcess(f.dmaAddr)
		})
	}
}

// dmaProcess services one FWCfgDmaAccess request whose 16-byte control
// structure lives at guest-physical addr. All struct fields are
// big-endian: control@0, length@4, address@8. It selects (if requested),
// then reads/skips/writes, advancing the per-selector offset, and writes
// the control field back to 0 on success or the ERROR bit on failure —
// which is how the firmware learns the transfer is complete.
func (f *fwCfg) dmaProcess(addr uint64) {
	if f.mem == nil {
		return
	}
	hdr := f.mem.GetRAMPtr(addr, true)
	if hdr == nil || len(hdr) < 16 {
		// Control struct not in mapped RAM — can't even report an error
		// (no writeback target). Firmware would treat this as a stuck
		// transfer; OVMF always places it in RAM, so this is unexpected.
		return
	}
	control := binary.BigEndian.Uint32(hdr[0:4])
	length := binary.BigEndian.Uint32(hdr[4:8])
	bufAddr := binary.BigEndian.Uint64(hdr[8:16])

	if control&fwCfgDmaSelect != 0 {
		f.selector = uint16(control >> 16)
		f.offset = 0
	}

	dmaErr := false
	switch {
	case control&fwCfgDmaRead != 0:
		data := f.dataForSelector(f.selector)
		buf := f.mem.GetRAMPtr(bufAddr, true)
		if buf == nil || uint64(len(buf)) < uint64(length) {
			dmaErr = true
			break
		}
		for i := uint32(0); i < length; i++ {
			if int(f.offset) < len(data) {
				buf[i] = data[f.offset]
			} else {
				buf[i] = 0 // zero-fill past end, matching the port path
			}
			f.offset++
		}
	case control&fwCfgDmaSkip != 0:
		f.offset += length
	case control&fwCfgDmaWrite != 0:
		// Copy guest memory into a writable item (e.g. etc/ramfb). Refused
		// for read-only items so a stray WRITE can't corrupt a backing
		// slice.
		dst := f.writableForSelector(f.selector)
		src := f.mem.GetRAMPtr(bufAddr, false)
		if dst == nil || src == nil || uint64(len(src)) < uint64(length) {
			dmaErr = true
			break
		}
		for i := uint32(0); i < length; i++ {
			if int(f.offset) < len(dst) {
				dst[f.offset] = src[i]
			}
			f.offset++
		}
	}

	if fwCfgDebug {
		fmt.Fprintf(os.Stderr, "[fwcfg] dma ctrl=%#x len=%d buf=%#x sel=%#x off=%d err=%v\n",
			control, length, bufAddr, f.selector, f.offset, dmaErr)
	}

	if dmaErr {
		binary.BigEndian.PutUint32(hdr[0:4], fwCfgDmaError)
	} else {
		binary.BigEndian.PutUint32(hdr[0:4], 0)
	}
}

// fileDirectory assembles the QEMU-defined file directory: a 4-byte
// big-endian count followed by one big-endian entry per file. SeaBIOS
// reads this once early in boot, indexes it by name, and uses the
// per-entry selector for subsequent file fetches.
func (f *fwCfg) fileDirectory() []byte {
	if f.dirCache != nil {
		return f.dirCache
	}
	buf := make([]byte, 4+len(f.files)*fwCfgFileEntrySize)
	binary.BigEndian.PutUint32(buf[0:4], uint32(len(f.files)))
	off := 4
	for i := range f.files {
		e := buf[off : off+fwCfgFileEntrySize]
		binary.BigEndian.PutUint32(e[0:4], uint32(len(f.files[i].data)))
		binary.BigEndian.PutUint16(e[4:6], f.files[i].sel)
		// e[6:8] reserved, leave zero
		copy(e[8:8+fwCfgFileNameSize], f.files[i].name)
		// remaining bytes inside name are already zero from make()
		off += fwCfgFileEntrySize
	}
	f.dirCache = buf
	return buf
}
