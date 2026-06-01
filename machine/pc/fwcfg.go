package pc

import (
	"encoding/binary"
	"fmt"
	"os"
)

var fwCfgDebug = os.Getenv("TINYEMU_FWCFG_DEBUG") == "1"

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
)

// fwCfgFile is a named blob the firmware can read end-to-end. select
// is the 16-bit selector we hand to readers (so they don't have to
// know the directory order).
type fwCfgFile struct {
	name string
	data []byte
	sel  uint16
}

// fwCfg is the per-machine fw_cfg state. The selector field tracks
// the currently-selected "file" (or one of the well-known direct
// selectors); offset is the next byte index inside that file.
type fwCfg struct {
	selector uint16
	offset   uint32
	files    []fwCfgFile
	dirCache []byte // assembled on first access; rebuilt if files change
}

// newFWCfg creates an fw_cfg device with the standard signature/ID
// entries already in place. Callers add their own files (ACPI tables,
// kernel cmdline, etc.) via addFile *before* registering the device
// on the I/O bus — once SeaBIOS sees the file directory it caches
// names → selectors, so new files added after boot won't be picked up.
func newFWCfg() *fwCfg {
	return &fwCfg{}
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
		// Feature bits. Bit 0 = fw_cfg present (we set it), bit 1 =
		// DMA support (we don't implement DMA; SeaBIOS falls back to
		// the slow port path when this is clear). The field is 4
		// bytes little-endian; we hand back exactly the LSB.
		return []byte{0x01, 0x00, 0x00, 0x00}
	case fwCfgSelFileDir:
		return f.fileDirectory()
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
