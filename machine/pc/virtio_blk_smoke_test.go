package pc

import (
	"encoding/binary"
	"testing"

	"github.com/jtolio/tinyemu-go/devices"
)

// This test exercises the full virtio-blk-pci data path end-to-end at
// unit-test speed (sub-second): create a PC machine, attach a
// memory-backed virtio-blk drive, simulate Linux's PCI BAR sizing
// sequence (write 0xFFFFFFFF then write the original BAR back), then
// drive the legacy virtio init flow, post a read request to the
// virtqueue, kick via the notify port, and verify the data ends up at
// the target address. If any link in that chain regresses — BAR
// relocation, queueNotify dispatch, descriptor parsing, recvRequest
// data transfer — this test fails loudly.
//
// The "Mounting boot media" hang that Alpine ran into would have been
// caught here if this test had existed.

const (
	// Where the virtqueue lives in guest RAM.
	vqPFN  = 0x2_0000 >> 12 // page-frame number
	vqDesc = 0x2_0000
	// SeaBIOS-style legacy layout: desc, then avail, padded to page,
	// then used. We use queue size 256.
	vqSize  = 256
	vqAvail = vqDesc + 16*vqSize
	vqUsed  = (vqAvail + 4 + 2*vqSize + 4095) &^ 4095

	// Where the read result + status byte live.
	readHdrAddr = 0x4_0000
	readDataDst = 0x4_1000
	readStatus  = 0x4_2000
)

// readVirtioPCIBAR0 reads the device's BAR0 base by walking the PCI
// host bridge through ports 0xCF8/0xCFC.
func readVirtioPCIBAR0(t *testing.T, p *PC) uint32 {
	t.Helper()
	// Bus 0, dev 3, fn 0, BAR0 register at offset 0x10. Enable bit (31) set.
	const addr = uint32(0x80000000 | (3 << 11) | 0x10)
	p.io.Write32(0xCF8, addr)
	return p.io.Read32(0xCFC)
}

func writeVirtioPCIBAR0(t *testing.T, p *PC, v uint32) {
	t.Helper()
	const addr = uint32(0x80000000 | (3 << 11) | 0x10)
	p.io.Write32(0xCF8, addr)
	p.io.Write32(0xCFC, v)
}

// TestVirtioBlk_PCISizingDoesNotBreakAccess simulates Linux's
// BAR-sizing dance and asserts that virtio I/O still works after.
// Catches the class of regression where our BAR-change handler does
// the wrong thing on the temporary 0xFFFFFFFF probe.
func TestVirtioBlk_PCISizingDoesNotBreakAccess(t *testing.T) {
	p := newTestPC(t, 32)

	// Attach a 1 MiB ramdisk as virtio-blk.
	bd, err := devices.NewMemoryBlockDevice(1 << 20)
	if err != nil {
		t.Fatalf("NewMemoryBlockDevice: %v", err)
	}
	// Write a known marker into sector 0 so we can verify the read.
	marker := []byte("VIRTIO-BLK-OK-12") // 16 bytes
	if _, err := bd.WriteSectors(0, paddedSector(marker), 1); err != nil {
		t.Fatalf("seed sector 0: %v", err)
	}
	if err := p.AttachVirtioBlock(bd); err != nil {
		t.Fatalf("AttachVirtioBlock: %v", err)
	}

	// Read the assigned BAR0 (set by AttachVirtioBlock).
	original := readVirtioPCIBAR0(t, p) &^ 0x1
	if original == 0 {
		t.Fatal("virtio-blk BAR0 is zero — device not registered?")
	}

	// Simulate Linux's BAR-sizing probe: write all-ones, then write
	// the original back. If our BAR-change handler is wrong it'll
	// strand the ports somewhere bogus.
	writeVirtioPCIBAR0(t, p, 0xFFFFFFFF)
	_ = readVirtioPCIBAR0(t, p) // discard the mask
	writeVirtioPCIBAR0(t, p, original|0x1)

	// Now drive the virtio init + post a read through the same port
	// range. If the BAR machinery left the I/O ports stranded, the
	// reads below will return 0xFFFF and the test fails.
	driveReadAndAssert(t, p, uint16(original), marker)
}

// TestVirtioBlk_RelocatedBAR exercises the SeaBIOS path: firmware
// MOVES the BAR to a new base. The handler should clear the old range
// and re-register at the new base; subsequent I/O at the new base
// should work, and I/O at the OLD base should fall through.
func TestVirtioBlk_RelocatedBAR(t *testing.T) {
	p := newTestPC(t, 32)
	bd, err := devices.NewMemoryBlockDevice(1 << 20)
	if err != nil {
		t.Fatalf("NewMemoryBlockDevice: %v", err)
	}
	marker := []byte("RELOCATED-BAR-OK") // 16 bytes
	if _, err := bd.WriteSectors(0, paddedSector(marker), 1); err != nil {
		t.Fatalf("seed sector 0: %v", err)
	}
	if err := p.AttachVirtioBlock(bd); err != nil {
		t.Fatalf("AttachVirtioBlock: %v", err)
	}

	// Relocate BAR0 to a new address (mimics SeaBIOS reassignment).
	const newBase uint16 = 0xC500
	writeVirtioPCIBAR0(t, p, uint32(newBase)|0x1)

	driveReadAndAssert(t, p, newBase, marker)
}

// driveReadAndAssert does a minimal virtio-blk init at `base` and
// reads sector 0 into guest RAM, asserting it equals `expect`.
func driveReadAndAssert(t *testing.T, p *PC, base uint16, expect []byte) {
	t.Helper()

	const (
		// Legacy virtio-pci port offsets (see virtio/pci.go).
		regHostFeatures  = 0x00
		regGuestFeatures = 0x04
		regQueuePFN      = 0x08
		regQueueSize     = 0x0C
		regQueueSelect   = 0x0E
		regQueueNotify   = 0x10
		regDeviceStatus  = 0x12
	)

	// Reset, then ACK | DRIVER.
	p.io.Write8(base+regDeviceStatus, 0)
	p.io.Write8(base+regDeviceStatus, 0x03)
	// Mirror back zero features.
	p.io.Write32(base+regGuestFeatures, 0)
	// Sanity: queue size must be sane (the 0xFFFF regression class).
	qs := p.io.Read16(base + regQueueSize)
	if qs == 0 || qs > 1024 {
		t.Fatalf("queue size read at port %#x = %d (want sane <=256); BAR likely stranded",
			base+regQueueSize, qs)
	}
	// Select queue 0 and program its PFN.
	p.io.Write16(base+regQueueSelect, 0)
	p.io.Write32(base+regQueuePFN, vqPFN)

	// Build a 3-descriptor read chain in guest RAM:
	//   [0] header (16 bytes, read by device)
	//   [1] data (512 bytes, written by device)
	//   [2] status (1 byte, written by device)
	header := make([]byte, 16)
	binary.LittleEndian.PutUint32(header[0:4], 0)  // type = VIRTIO_BLK_T_IN
	binary.LittleEndian.PutUint32(header[4:8], 0)  // ioprio
	binary.LittleEndian.PutUint64(header[8:16], 0) // sector 0
	for i, b := range header {
		_ = p.memMap.Write8(uint64(readHdrAddr+i), b)
	}
	// Clear the status byte.
	_ = p.memMap.Write8(uint64(readStatus), 0xFF)

	// Descriptor 0: header, NEXT flag, points to 1.
	writeDesc(t, p, 0, readHdrAddr, 16, 0x1 /*NEXT*/, 1)
	// Descriptor 1: data, NEXT | WRITE, points to 2.
	writeDesc(t, p, 1, readDataDst, 512, 0x1|0x2, 2)
	// Descriptor 2: status, WRITE, no next.
	writeDesc(t, p, 2, readStatus, 1, 0x2, 0)

	// Avail ring: ring[0] = head desc index (0), idx = 1.
	_ = p.memMap.Write16(uint64(vqAvail+4), 0)
	_ = p.memMap.Write16(uint64(vqAvail+2), 1) // idx

	// Kick.
	p.io.Write16(base+regQueueNotify, 0)

	// Status should now be SOK = 0.
	st, err := p.memMap.Read8(uint64(readStatus))
	if err != nil {
		t.Fatalf("read status byte: %v", err)
	}
	if st != 0 {
		t.Fatalf("status byte = %#x, want 0 (SOK)", st)
	}

	// Verify the data we expect lives at readDataDst.
	got := make([]byte, len(expect))
	for i := range got {
		b, err := p.memMap.Read8(uint64(readDataDst + i))
		if err != nil {
			t.Fatalf("read data byte %d: %v", i, err)
		}
		got[i] = b
	}
	if string(got) != string(expect) {
		t.Fatalf("data mismatch.\n got: %q\nwant: %q", string(got), string(expect))
	}
}

// writeDesc writes a virtio descriptor at index i.
func writeDesc(t *testing.T, p *PC, i int, addr uint64, length uint32, flags uint16, next uint16) {
	t.Helper()
	base := uint64(vqDesc + 16*i)
	_ = p.memMap.Write64(base+0, addr)
	_ = p.memMap.Write32(base+8, length)
	_ = p.memMap.Write16(base+12, flags)
	_ = p.memMap.Write16(base+14, next)
}

// paddedSector pads `s` out to 512 bytes with zeros so it's a valid sector.
func paddedSector(s []byte) []byte {
	out := make([]byte, 512)
	copy(out, s)
	return out
}

// newTestPC constructs a PC machine with the given RAM size (MiB) for tests.
func newTestPC(t *testing.T, ramMB int) *PC {
	t.Helper()
	p, err := New(Config{
		RAMSize:     uint64(ramMB) * 1024 * 1024,
		MachineType: "x86_64",
	})
	if err != nil {
		t.Fatalf("New PC: %v", err)
	}
	t.Cleanup(p.Close)
	return p
}
