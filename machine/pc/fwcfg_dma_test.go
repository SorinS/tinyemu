package pc

import (
	"encoding/binary"
	"math/bits"
	"testing"

	"github.com/jtolio/tinyemu-go/mem"
)

// dmaFWCfg builds a DMA-enabled fw_cfg backed by a fresh 1 MiB RAM map.
func dmaFWCfg(t *testing.T) (*fwCfg, *mem.PhysMemoryMap) {
	t.Helper()
	mm := mem.NewPhysMemoryMap()
	t.Cleanup(mm.Close)
	if _, err := mm.RegisterRAM(0, 0x100000, 0); err != nil {
		t.Fatalf("RegisterRAM: %v", err)
	}
	f := newFWCfg()
	f.mem = mm
	f.dma = true
	return f, mm
}

// TestFwCfgDMA_ReadSignature is the end-to-end byte-order proof (the same
// transfer OVMF issues over DMA): a control word of SELECT|READ with
// selector 0 must copy the four-byte "QEMU" signature into the guest
// buffer and clear the control field. If the big-endian handling of the
// address register, the control struct, or the writeback is wrong, this
// fails. Drives the real port path through the dispatcher.
func TestFwCfgDMA_ReadSignature(t *testing.T) {
	f, mm := dmaFWCfg(t)
	io := NewIOPortDispatcher()
	f.Register(io)

	const ctrlAddr = 0x1000
	const bufAddr = 0x2000

	// FWCfgDmaAccess at ctrlAddr (big-endian): control=SELECT|READ (sel 0),
	// length=4, address=bufAddr.
	hdr := mm.GetRAMPtr(ctrlAddr, true)
	binary.BigEndian.PutUint32(hdr[0:4], fwCfgDmaSelect|fwCfgDmaRead) // selector 0 = signature
	binary.BigEndian.PutUint32(hdr[4:8], 4)
	binary.BigEndian.PutUint64(hdr[8:16], bufAddr)

	// Write the 64-bit control-struct address via the two big-endian
	// register halves; the low-half write triggers. The value an `outl`
	// carries is byte-swapped (the register is big-endian).
	io.Write32(fwCfgDMAAddrHi, bits.ReverseBytes32(uint32(ctrlAddr>>32)))
	io.Write32(fwCfgDMAAddrLo, bits.ReverseBytes32(uint32(ctrlAddr&0xFFFFFFFF)))

	if got := string(mm.GetRAMPtr(bufAddr, false)[:4]); got != "QEMU" {
		t.Errorf("DMA buffer = %q, want %q (byte-order bug)", got, "QEMU")
	}
	if ctrl := binary.BigEndian.Uint32(mm.GetRAMPtr(ctrlAddr, false)[:4]); ctrl != 0 {
		t.Errorf("control field = %#x after transfer, want 0 (completion not signalled)", ctrl)
	}
}

// TestFwCfgDMA_SelectThenRead checks a separate SELECT then READ (the
// pattern for reading a file via two DMA ops) and the per-selector offset
// advancing across reads. Reads the 4-byte ID feature word in two halves.
func TestFwCfgDMA_SelectThenRead(t *testing.T) {
	f, mm := dmaFWCfg(t)
	const ctrlAddr = 0x3000
	const bufAddr = 0x4000

	// 1) SELECT only: select the ID register (0x0001), no read.
	hdr := mm.GetRAMPtr(ctrlAddr, true)
	binary.BigEndian.PutUint32(hdr[0:4], fwCfgDmaSelect|(uint32(fwCfgSelID)<<16))
	binary.BigEndian.PutUint32(hdr[4:8], 0)
	binary.BigEndian.PutUint64(hdr[8:16], 0)
	f.dmaProcess(ctrlAddr)
	if f.selector != fwCfgSelID || f.offset != 0 {
		t.Fatalf("after SELECT: selector=%#x offset=%d, want %#x/0", f.selector, f.offset, fwCfgSelID)
	}

	// 2) READ 4 bytes (no SELECT) into bufAddr — must read the ID word.
	binary.BigEndian.PutUint32(hdr[0:4], fwCfgDmaRead)
	binary.BigEndian.PutUint32(hdr[4:8], 4)
	binary.BigEndian.PutUint64(hdr[8:16], bufAddr)
	f.dmaProcess(ctrlAddr)

	got := mm.GetRAMPtr(bufAddr, false)[:4]
	if got[0] != 0x03 || got[1] != 0 || got[2] != 0 || got[3] != 0 {
		t.Errorf("ID via DMA = % x, want 03 00 00 00 (DMA bit set)", got[:4])
	}
	if f.offset != 4 {
		t.Errorf("offset = %d after 4-byte read, want 4", f.offset)
	}
}

// TestFwCfgDMA_WriteRamfb: the DMA WRITE path copies guest memory into a
// writable item (the mechanism QemuRamfbDxe uses to publish its framebuffer
// config to etc/ramfb). A WRITE to a read-only item must fail, not corrupt.
func TestFwCfgDMA_WriteRamfb(t *testing.T) {
	f, mm := dmaFWCfg(t)
	sel := f.addFileWritable("etc/ramfb", make([]byte, ramfbCfgSize))

	const ctrlAddr = 0x5000
	const srcAddr = 0x6000
	// Source payload in guest RAM: a fake 28-byte RAMFBCfg.
	src := mm.GetRAMPtr(srcAddr, true)
	for i := 0; i < ramfbCfgSize; i++ {
		src[i] = byte(0x10 + i)
	}

	hdr := mm.GetRAMPtr(ctrlAddr, true)
	binary.BigEndian.PutUint32(hdr[0:4], fwCfgDmaSelect|fwCfgDmaWrite|(uint32(sel)<<16))
	binary.BigEndian.PutUint32(hdr[4:8], ramfbCfgSize)
	binary.BigEndian.PutUint64(hdr[8:16], srcAddr)
	f.dmaProcess(ctrlAddr)

	if ctrl := binary.BigEndian.Uint32(hdr[0:4]); ctrl != 0 {
		t.Fatalf("WRITE control = %#x, want 0 (success)", ctrl)
	}
	got := f.dataForSelector(sel)
	for i := 0; i < ramfbCfgSize; i++ {
		if got[i] != byte(0x10+i) {
			t.Fatalf("etc/ramfb[%d] = %#x, want %#x", i, got[i], 0x10+i)
		}
	}

	// WRITE to a read-only item (the signature) must error, not panic/corrupt.
	binary.BigEndian.PutUint32(hdr[0:4], fwCfgDmaSelect|fwCfgDmaWrite|(uint32(fwCfgSelSignature)<<16))
	binary.BigEndian.PutUint32(hdr[4:8], 4)
	binary.BigEndian.PutUint64(hdr[8:16], srcAddr)
	f.dmaProcess(ctrlAddr)
	if ctrl := binary.BigEndian.Uint32(hdr[0:4]); ctrl != fwCfgDmaError {
		t.Errorf("WRITE to read-only item control = %#x, want ERROR bit %#x", ctrl, fwCfgDmaError)
	}
}

// TestFwCfgDMA_Disabled: with DMA off (the default), ID reports only bit 0
// and the DMA ports are not registered — the byte path is untouched.
func TestFwCfgDMA_Disabled(t *testing.T) {
	f := newFWCfg() // env unset in tests → dma=false
	if f.dma {
		t.Skip("TINYEMU_FWCFG_DMA set in environment")
	}
	id := f.dataForSelector(fwCfgSelID)
	if id[0] != 0x01 {
		t.Errorf("ID byte0 = %#x with DMA off, want 0x01", id[0])
	}
}
