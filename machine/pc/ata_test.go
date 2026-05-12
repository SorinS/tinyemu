package pc

import (
	"testing"

	"github.com/jtolio/tinyemu-go/cpu/x86"
	"github.com/jtolio/tinyemu-go/devices"
	"github.com/jtolio/tinyemu-go/mem"
)

// newATATestRig builds the minimum harness needed to exercise the ATA
// controller end-to-end: a CPU (only used to satisfy the PIC's wiring), a
// PIC8259 (so we can observe IRQ 14 being raised), an IOPortDispatcher
// (where the ATA controller registers its ports), and a backing
// MemoryBlockDevice. The harness returns both the dispatcher (for issuing
// IN/OUT) and the controller (for assertions).
func newATATestRig(t *testing.T, sectors int64) (*IOPortDispatcher, *PIC8259, *ATAController, *devices.MemoryBlockDevice) {
	t.Helper()
	mm := mem.NewPhysMemoryMap()
	if _, err := mm.RegisterRAM(0, 1<<20, 0); err != nil {
		t.Fatalf("register ram: %v", err)
	}
	cpu := x86.NewCPU(mm)
	pic := NewPIC8259Cascaded(cpu, 0x20, 0xA0)
	io := NewIOPortDispatcher()
	pic.Register(io)
	// Unmask the cascade (master IRQ 2) and slave's IRQ 6 (= overall IRQ 14)
	// so the tests can observe IRQs.
	pic.imr = 0xFB        // unmask master IRQ 2
	pic.slave.imr = 0xBF  // unmask slave IRQ 6 (= IRQ 14)
	// Configure the vector bases the way Linux would (master 0x20, slave 0x28).
	pic.icw2 = 0x20
	pic.slave.icw2 = 0x28

	bd, err := devices.NewMemoryBlockDevice(sectors * devices.SectorSize)
	if err != nil {
		t.Fatalf("new memory block device: %v", err)
	}
	ata := NewATAController(pic, bd)
	ata.Register(io)
	return io, pic, ata, bd
}

// TestATAIdentify exercises the IDENTIFY DEVICE flow: issue command 0xEC,
// poll status for DRQ, read 256 16-bit words from the data port, verify a
// few key fields of the identify response.
func TestATAIdentify(t *testing.T) {
	io, _, _, bd := newATATestRig(t, 1024)
	_ = bd

	// Select drive 0.
	io.Write8(0x1F6, 0x00)
	// Issue IDENTIFY DEVICE.
	io.Write8(0x1F7, 0xEC)

	// Status should now have DRQ set (data ready).
	st := io.Read8(0x1F7)
	if st&ataSR_DRQ == 0 {
		t.Fatalf("expected DRQ set after IDENTIFY, got status=%02X", st)
	}
	if st&ataSR_ERR != 0 {
		t.Fatalf("unexpected ERR after IDENTIFY, status=%02X", st)
	}

	// Read 256 words.
	words := make([]uint16, 256)
	for i := range words {
		words[i] = io.Read16(0x1F0)
	}

	// Word 0: bit 6 set (ATA fixed device).
	if words[0]&0x40 == 0 {
		t.Errorf("word[0] = %04X, expected bit 6 set", words[0])
	}
	// Word 49: bit 9 set (LBA supported).
	if words[49]&0x0200 == 0 {
		t.Errorf("word[49] = %04X, expected LBA bit set", words[49])
	}
	// Word 60-61: total sectors = LBA28 (1024 for this device).
	total := uint32(words[60]) | uint32(words[61])<<16
	if total != 1024 {
		t.Errorf("LBA28 sector count = %d, expected 1024", total)
	}

	// After consuming the identify buffer, DRQ should be clear.
	st = io.Read8(0x1F7)
	if st&ataSR_DRQ != 0 {
		t.Errorf("DRQ still set after consuming identify, status=%02X", st)
	}
}

// TestATAReadOneSector seeds the backing storage with a known pattern,
// issues READ SECTORS for sector 7, and verifies that the 256 words read
// from the data port match.
func TestATAReadOneSector(t *testing.T) {
	io, _, _, bd := newATATestRig(t, 16)

	// Write a recognisable pattern at sector 7: byte k = (7*256 + k) % 251.
	src := make([]byte, devices.SectorSize)
	for k := range src {
		src[k] = byte((7*256 + k) % 251)
	}
	if _, err := bd.WriteSectors(7, src, 1); err != nil {
		t.Fatalf("write seed sector: %v", err)
	}

	// Program the task file: LBA28=7, count=1.
	io.Write8(0x1F6, 0x40)        // drive 0 + LBA mode
	io.Write8(0x1F2, 1)            // sector count
	io.Write8(0x1F3, 7)            // LBA[7:0]
	io.Write8(0x1F4, 0)            // LBA[15:8]
	io.Write8(0x1F5, 0)            // LBA[23:16]
	io.Write8(0x1F7, 0x20)        // READ SECTORS

	st := io.Read8(0x1F7)
	if st&ataSR_DRQ == 0 || st&ataSR_ERR != 0 {
		t.Fatalf("bad status after READ command: %02X", st)
	}

	got := make([]byte, devices.SectorSize)
	for i := 0; i < devices.SectorSize/2; i++ {
		w := io.Read16(0x1F0)
		got[i*2] = byte(w)
		got[i*2+1] = byte(w >> 8)
	}
	for i := range got {
		if got[i] != src[i] {
			t.Fatalf("byte %d: got %02X, want %02X", i, got[i], src[i])
		}
	}
}

// TestATAReadMultiSector issues a 3-sector read and verifies it walks
// across sector boundaries with IRQ + DRQ-cycling.
func TestATAReadMultiSector(t *testing.T) {
	io, _, _, bd := newATATestRig(t, 32)

	// Seed sectors 2, 3, 4 with distinct patterns.
	for s := uint64(2); s <= 4; s++ {
		buf := make([]byte, devices.SectorSize)
		for k := range buf {
			buf[k] = byte(s*256 + uint64(k)%97)
		}
		if _, err := bd.WriteSectors(s, buf, 1); err != nil {
			t.Fatalf("seed sector %d: %v", s, err)
		}
	}

	io.Write8(0x1F6, 0x40)
	io.Write8(0x1F2, 3) // 3 sectors
	io.Write8(0x1F3, 2) // start LBA 2
	io.Write8(0x1F4, 0)
	io.Write8(0x1F5, 0)
	io.Write8(0x1F7, 0x20) // READ SECTORS

	for s := uint64(2); s <= 4; s++ {
		st := io.Read8(0x1F7)
		if st&ataSR_DRQ == 0 || st&ataSR_ERR != 0 {
			t.Fatalf("sector %d: bad status %02X before transfer", s, st)
		}
		want := make([]byte, devices.SectorSize)
		for k := range want {
			want[k] = byte(s*256 + uint64(k)%97)
		}
		for i := 0; i < devices.SectorSize/2; i++ {
			w := io.Read16(0x1F0)
			if want[i*2] != byte(w) || want[i*2+1] != byte(w>>8) {
				t.Fatalf("sector %d byte %d: got word %04X, want bytes %02X %02X",
					s, i*2, w, want[i*2], want[i*2+1])
			}
		}
	}

	// After the third sector, DRQ should be clear and command complete.
	st := io.Read8(0x1F7)
	if st&ataSR_DRQ != 0 {
		t.Errorf("DRQ still set after 3-sector read, status=%02X", st)
	}
	if st&ataSR_DRDY == 0 {
		t.Errorf("DRDY not set after completion, status=%02X", st)
	}
}

// TestATAWriteOneSector issues a WRITE SECTORS for sector 5 and verifies
// the backing device received the bytes.
func TestATAWriteOneSector(t *testing.T) {
	io, _, _, bd := newATATestRig(t, 16)

	io.Write8(0x1F6, 0x40)
	io.Write8(0x1F2, 1)
	io.Write8(0x1F3, 5)
	io.Write8(0x1F4, 0)
	io.Write8(0x1F5, 0)
	io.Write8(0x1F7, 0x30) // WRITE SECTORS

	// After WRITE PIO, DRQ should be set so the host can push data.
	st := io.Read8(0x1F7)
	if st&ataSR_DRQ == 0 {
		t.Fatalf("DRQ not set after WRITE command, status=%02X", st)
	}

	// Push 256 words of pattern.
	want := make([]byte, devices.SectorSize)
	for k := range want {
		want[k] = byte((5*256 + k) % 241)
	}
	for i := 0; i < devices.SectorSize/2; i++ {
		w := uint16(want[i*2]) | uint16(want[i*2+1])<<8
		io.Write16(0x1F0, w)
	}

	// After all 256 words pushed, DRQ should clear and the write must have
	// been flushed to the backing device.
	st = io.Read8(0x1F7)
	if st&ataSR_DRQ != 0 {
		t.Errorf("DRQ still set after pushing sector, status=%02X", st)
	}
	got := make([]byte, devices.SectorSize)
	if _, err := bd.ReadSectors(5, got, 1); err != nil {
		t.Fatalf("read back: %v", err)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("byte %d: got %02X, want %02X", i, got[i], want[i])
		}
	}
}

// TestATAIRQAssertedAndCleared confirms IRQ 14 is raised on command
// completion and cleared by reading the status register.
func TestATAIRQAssertedAndCleared(t *testing.T) {
	io, pic, _, _ := newATATestRig(t, 16)

	io.Write8(0x1F6, 0x00)
	io.Write8(0x1F7, 0xEC) // IDENTIFY

	// PIC should now have IRQ 14 pending in its IRR.
	if pic.PeekInterrupt() < 0 {
		t.Fatalf("expected IRQ pending after IDENTIFY")
	}

	// Reading status at 0x1F7 (the primary port, not 0x3F6) clears the IRQ.
	_ = io.Read8(0x1F7)
	// Drain any remaining bytes so we don't accidentally re-fire on next sector.
	for i := 0; i < devices.SectorSize/2; i++ {
		io.Read16(0x1F0)
	}
}

// TestATAReadOutOfRange triggers an IDNF abort by reading past the device.
func TestATAReadOutOfRange(t *testing.T) {
	io, _, _, _ := newATATestRig(t, 4) // device is exactly 4 sectors

	io.Write8(0x1F6, 0x40)
	io.Write8(0x1F2, 1)
	io.Write8(0x1F3, 100) // LBA 100 — way past 4
	io.Write8(0x1F4, 0)
	io.Write8(0x1F5, 0)
	io.Write8(0x1F7, 0x20) // READ SECTORS

	st := io.Read8(0x1F7)
	if st&ataSR_ERR == 0 {
		t.Errorf("expected ERR set on out-of-range read, status=%02X", st)
	}
	if err := io.Read8(0x1F1); err&ataER_IDNF == 0 {
		t.Errorf("expected IDNF in error reg, got %02X", err)
	}
}

// newATAPITestRig builds an ATAPI CD-ROM controller with a 16-CD-sector
// (32 KB) backing image and a working PIC cascade.
func newATAPITestRig(t *testing.T, cdSectors int64) (*IOPortDispatcher, *ATAController, *devices.MemoryBlockDevice) {
	t.Helper()
	mm := mem.NewPhysMemoryMap()
	if _, err := mm.RegisterRAM(0, 1<<20, 0); err != nil {
		t.Fatalf("register ram: %v", err)
	}
	cpu := x86.NewCPU(mm)
	pic := NewPIC8259Cascaded(cpu, 0x20, 0xA0)
	io := NewIOPortDispatcher()
	pic.Register(io)
	pic.imr = 0xFB
	pic.slave.imr = 0xBF
	pic.icw2 = 0x20
	pic.slave.icw2 = 0x28

	bd, err := devices.NewMemoryBlockDevice(cdSectors * cdSectorSize)
	if err != nil {
		t.Fatalf("new memory block device: %v", err)
	}
	cd := NewCDROMController(pic, bd)
	cd.Register(io)
	return io, cd, bd
}

// readATAPIBytes reads exactly `n` bytes from the data port (using word
// reads when n is even, then a final byte read if odd).
func readATAPIBytes(t *testing.T, io *IOPortDispatcher, n int) []byte {
	t.Helper()
	buf := make([]byte, n)
	i := 0
	for ; i+1 < n; i += 2 {
		w := io.Read16(0x1F0)
		buf[i] = byte(w)
		buf[i+1] = byte(w >> 8)
	}
	if i < n {
		buf[i] = io.Read8(0x1F0)
	}
	return buf
}

// writePacketCommand sends a 12-byte SCSI packet via the data port after
// the host has issued the PACKET command.
func writePacketCommand(io *IOPortDispatcher, cmd []byte) {
	full := make([]byte, 12)
	copy(full, cmd)
	for i := 0; i < 12; i += 2 {
		io.Write16(0x1F0, uint16(full[i])|uint16(full[i+1])<<8)
	}
}

// TestATAPIIdentifySignature confirms that a CD-ROM device exposes the
// ATAPI signature (lba_mid=0x14, lba_high=0xEB) after reset and that an
// ATA IDENTIFY DEVICE command aborts (rather than returning identify
// data) — both required for Linux's ATAPI probing.
func TestATAPIIdentifySignature(t *testing.T) {
	io, _, _ := newATAPITestRig(t, 16)

	// Read the signature from the task-file (post-reset state).
	if v := io.Read8(0x1F4); v != 0x14 {
		t.Errorf("lba_mid after reset = %02X, want 0x14", v)
	}
	if v := io.Read8(0x1F5); v != 0xEB {
		t.Errorf("lba_high after reset = %02X, want 0xEB", v)
	}

	// Issue ATA IDENTIFY DEVICE — must abort and leave signature intact.
	io.Write8(0x1F6, 0x00)
	io.Write8(0x1F7, 0xEC) // ATA IDENTIFY
	st := io.Read8(0x1F7)
	if st&ataSR_ERR == 0 {
		t.Errorf("expected ERR after ATA IDENTIFY on ATAPI device, status=%02X", st)
	}
	if v := io.Read8(0x1F4); v != 0x14 {
		t.Errorf("lba_mid after aborted IDENTIFY = %02X, want 0x14", v)
	}
}

// TestATAPIInquiry runs the standard MMC INQUIRY (op 0x12) and checks
// the response header.
func TestATAPIInquiry(t *testing.T) {
	io, _, _ := newATAPITestRig(t, 16)

	// Issue PACKET command.
	io.Write8(0x1F6, 0x00)
	io.Write8(0x1F7, 0xA0) // PACKET
	st := io.Read8(0x1F7)
	if st&ataSR_DRQ == 0 {
		t.Fatalf("DRQ not set after PACKET, status=%02X", st)
	}

	// Send 12-byte INQUIRY with allocation length 36.
	writePacketCommand(io, []byte{0x12, 0, 0, 0, 36, 0, 0, 0, 0, 0, 0, 0})

	// Device should now have data ready.
	st = io.Read8(0x1F7)
	if st&ataSR_DRQ == 0 {
		t.Fatalf("DRQ not set after INQUIRY cmd, status=%02X", st)
	}
	byteCount := uint16(io.Read8(0x1F4)) | uint16(io.Read8(0x1F5))<<8
	if byteCount != 36 {
		t.Fatalf("byte count = %d, want 36", byteCount)
	}

	buf := readATAPIBytes(t, io, int(byteCount))
	if buf[0] != 0x05 {
		t.Errorf("INQUIRY byte 0 = %02X, want 0x05 (CD-ROM)", buf[0])
	}
	if buf[1]&0x80 == 0 {
		t.Errorf("INQUIRY byte 1 = %02X, expected removable bit set", buf[1])
	}
	if got := string(buf[8:16]); got != "TINYEMU " {
		t.Errorf("INQUIRY vendor = %q", got)
	}
}

// TestATAPIReadCapacity verifies READ_CAPACITY returns the right total
// sector count (in CD sectors of 2048 bytes).
func TestATAPIReadCapacity(t *testing.T) {
	io, _, _ := newATAPITestRig(t, 100)

	io.Write8(0x1F6, 0x00)
	io.Write8(0x1F7, 0xA0)
	_ = io.Read8(0x1F7)
	writePacketCommand(io, []byte{0x25, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0})

	buf := readATAPIBytes(t, io, 8)
	lastLBA := uint32(buf[0])<<24 | uint32(buf[1])<<16 | uint32(buf[2])<<8 | uint32(buf[3])
	if lastLBA != 99 {
		t.Errorf("last LBA = %d, want 99 (100 sectors → 0..99)", lastLBA)
	}
	bs := uint32(buf[4])<<24 | uint32(buf[5])<<16 | uint32(buf[6])<<8 | uint32(buf[7])
	if bs != 2048 {
		t.Errorf("block size = %d, want 2048", bs)
	}
}

// TestATAPIRead10 seeds CD sector 3 with a pattern, issues READ(10), and
// verifies the bytes coming back.
func TestATAPIRead10(t *testing.T) {
	io, _, bd := newATAPITestRig(t, 16)

	// Seed CD sector 3 = underlying 512-byte sectors 12-15.
	expected := make([]byte, cdSectorSize)
	for i := range expected {
		expected[i] = byte((3*256 + i) % 251)
	}
	if _, err := bd.WriteSectors(12, expected, 4); err != nil {
		t.Fatalf("seed: %v", err)
	}

	io.Write8(0x1F6, 0x00)
	io.Write8(0x1F7, 0xA0)
	_ = io.Read8(0x1F7)
	// READ(10) LBA=3, count=1.
	writePacketCommand(io, []byte{0x28, 0, 0, 0, 0, 3, 0, 0, 1, 0, 0, 0})

	st := io.Read8(0x1F7)
	if st&ataSR_DRQ == 0 || st&ataSR_ERR != 0 {
		t.Fatalf("bad status after READ(10): %02X", st)
	}
	byteCount := uint16(io.Read8(0x1F4)) | uint16(io.Read8(0x1F5))<<8
	if byteCount != cdSectorSize {
		t.Fatalf("byte count = %d, want %d", byteCount, cdSectorSize)
	}

	got := readATAPIBytes(t, io, cdSectorSize)
	for i := range got {
		if got[i] != expected[i] {
			t.Fatalf("byte %d: got %02X, want %02X", i, got[i], expected[i])
		}
	}
}

// TestATAPIRead10Multi reads 2 CD sectors and verifies both come through.
func TestATAPIRead10Multi(t *testing.T) {
	io, _, bd := newATAPITestRig(t, 16)

	for cd := uint64(0); cd < 2; cd++ {
		buf := make([]byte, cdSectorSize)
		for i := range buf {
			buf[i] = byte((cd*1000 + uint64(i)) % 211)
		}
		if _, err := bd.WriteSectors(cd*4, buf, 4); err != nil {
			t.Fatalf("seed sector %d: %v", cd, err)
		}
	}

	io.Write8(0x1F6, 0x00)
	io.Write8(0x1F7, 0xA0)
	_ = io.Read8(0x1F7)
	writePacketCommand(io, []byte{0x28, 0, 0, 0, 0, 0, 0, 0, 2, 0, 0, 0})

	for cd := uint64(0); cd < 2; cd++ {
		st := io.Read8(0x1F7)
		if st&ataSR_DRQ == 0 {
			t.Fatalf("sector %d: no DRQ, status=%02X", cd, st)
		}
		byteCount := uint16(io.Read8(0x1F4)) | uint16(io.Read8(0x1F5))<<8
		if byteCount != cdSectorSize {
			t.Fatalf("sector %d byte count = %d", cd, byteCount)
		}
		got := readATAPIBytes(t, io, int(byteCount))
		for i := range got {
			want := byte((cd*1000 + uint64(i)) % 211)
			if got[i] != want {
				t.Fatalf("sector %d byte %d: got %02X, want %02X", cd, i, got[i], want)
			}
		}
	}

	st := io.Read8(0x1F7)
	if st&ataSR_DRQ != 0 {
		t.Errorf("DRQ still set after two-sector read, status=%02X", st)
	}
}

// TestATAPITestUnitReady runs the always-succeeds command and verifies it
// completes without DRQ.
func TestATAPITestUnitReady(t *testing.T) {
	io, _, _ := newATAPITestRig(t, 16)

	io.Write8(0x1F6, 0x00)
	io.Write8(0x1F7, 0xA0)
	_ = io.Read8(0x1F7)
	writePacketCommand(io, []byte{0x00, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0})

	st := io.Read8(0x1F7)
	if st&ataSR_DRQ != 0 {
		t.Errorf("DRQ set after TEST_UNIT_READY, status=%02X", st)
	}
	if st&ataSR_ERR != 0 {
		t.Errorf("ERR set after TEST_UNIT_READY, status=%02X", st)
	}
}

// TestATANoDeviceAborts confirms that with a nil backing device, any
// command aborts cleanly (the typical "channel present, drive absent"
// case Linux probes for at boot).
func TestATANoDeviceAborts(t *testing.T) {
	mm := mem.NewPhysMemoryMap()
	if _, err := mm.RegisterRAM(0, 1<<20, 0); err != nil {
		t.Fatalf("register ram: %v", err)
	}
	cpu := x86.NewCPU(mm)
	pic := NewPIC8259(cpu, 0x20)
	io := NewIOPortDispatcher()
	pic.Register(io)
	ata := NewATAController(pic, nil)
	ata.Register(io)

	io.Write8(0x1F6, 0x00)
	io.Write8(0x1F7, 0xEC) // IDENTIFY

	st := io.Read8(0x1F7)
	if st&ataSR_ERR == 0 {
		t.Errorf("expected ERR when no device attached, status=%02X", st)
	}
}
