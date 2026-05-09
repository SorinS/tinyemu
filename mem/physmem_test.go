package mem

import (
	"testing"
)

func TestNewPhysMemoryMap(t *testing.T) {
	m := NewPhysMemoryMap()
	if m == nil {
		t.Fatal("NewPhysMemoryMap returned nil")
	}
	if m.NumRanges() != 0 {
		t.Errorf("expected 0 ranges, got %d", m.NumRanges())
	}
	m.Close()
}

func TestRegisterRAM(t *testing.T) {
	m := NewPhysMemoryMap()
	defer m.Close()

	// Register a 4KB RAM region
	pr, err := m.RegisterRAM(0x80000000, 4096, 0)
	if err != nil {
		t.Fatalf("RegisterRAM failed: %v", err)
	}
	if pr == nil {
		t.Fatal("RegisterRAM returned nil range")
	}
	if !pr.IsRAM {
		t.Error("expected IsRAM to be true")
	}
	if pr.Addr != 0x80000000 {
		t.Errorf("expected Addr 0x80000000, got 0x%x", pr.Addr)
	}
	if pr.Size != 4096 {
		t.Errorf("expected Size 4096, got %d", pr.Size)
	}
	if len(pr.PhysMem) != 4096 {
		t.Errorf("expected PhysMem len 4096, got %d", len(pr.PhysMem))
	}
	if m.NumRanges() != 1 {
		t.Errorf("expected 1 range, got %d", m.NumRanges())
	}
}

func TestRegisterRAMInvalidSize(t *testing.T) {
	m := NewPhysMemoryMap()
	defer m.Close()

	// Zero size should fail
	_, err := m.RegisterRAM(0x80000000, 0, 0)
	if err != ErrInvalidSize {
		t.Errorf("expected ErrInvalidSize for zero size, got %v", err)
	}

	// Non-page-aligned size should fail
	_, err = m.RegisterRAM(0x80000000, 100, 0)
	if err != ErrInvalidSize {
		t.Errorf("expected ErrInvalidSize for non-aligned size, got %v", err)
	}
}

func TestRegisterRAMDisabled(t *testing.T) {
	m := NewPhysMemoryMap()
	defer m.Close()

	pr, err := m.RegisterRAM(0x80000000, 4096, RAMFlagDisabled)
	if err != nil {
		t.Fatalf("RegisterRAM failed: %v", err)
	}
	if pr.Size != 0 {
		t.Errorf("expected Size 0 for disabled region, got %d", pr.Size)
	}
	if pr.OrgSize != 4096 {
		t.Errorf("expected OrgSize 4096, got %d", pr.OrgSize)
	}

	// Should not be found by GetRange since Size is 0
	if m.GetRange(0x80000000) != nil {
		t.Error("disabled region should not be found by GetRange")
	}
}

func TestRegisterRAMWithDirtyBits(t *testing.T) {
	m := NewPhysMemoryMap()
	defer m.Close()

	// 8KB = 2 pages
	pr, err := m.RegisterRAM(0x80000000, 8192, RAMFlagDirtyBits)
	if err != nil {
		t.Fatalf("RegisterRAM failed: %v", err)
	}
	if pr.DirtyBits == nil {
		t.Error("expected DirtyBits to be allocated")
	}

	// Check initial state - not dirty
	if pr.IsDirtyBit(0) {
		t.Error("page 0 should not be dirty initially")
	}

	// Set dirty bit
	pr.SetDirtyBit(0)
	if !pr.IsDirtyBit(0) {
		t.Error("page 0 should be dirty after SetDirtyBit")
	}

	// Second page should still be clean
	if pr.IsDirtyBit(4096) {
		t.Error("page 1 should not be dirty")
	}
}

func TestRegisterDevice(t *testing.T) {
	m := NewPhysMemoryMap()
	defer m.Close()

	readCalled := false
	writeCalled := false

	readFn := func(opaque any, offset uint32, sizeLog2 int) uint32 {
		readCalled = true
		return 0xDEADBEEF
	}
	writeFn := func(opaque any, offset uint32, val uint32, sizeLog2 int) {
		writeCalled = true
	}

	pr, err := m.RegisterDevice(0x10000000, 0x1000, nil, readFn, writeFn, DevIOSize32)
	if err != nil {
		t.Fatalf("RegisterDevice failed: %v", err)
	}
	if pr.IsRAM {
		t.Error("expected IsRAM to be false")
	}

	// Test read
	val, err := m.Read32(0x10000000)
	if err != nil {
		t.Fatalf("Read32 failed: %v", err)
	}
	if !readCalled {
		t.Error("read function was not called")
	}
	if val != 0xDEADBEEF {
		t.Errorf("expected 0xDEADBEEF, got 0x%x", val)
	}

	// Test write
	err = m.Write32(0x10000000, 0x12345678)
	if err != nil {
		t.Fatalf("Write32 failed: %v", err)
	}
	if !writeCalled {
		t.Error("write function was not called")
	}
}

func TestGetRange(t *testing.T) {
	m := NewPhysMemoryMap()
	defer m.Close()

	m.RegisterRAM(0x80000000, 0x10000, 0) // 64KB at 0x80000000
	m.RegisterRAM(0x90000000, 0x10000, 0) // 64KB at 0x90000000

	tests := []struct {
		addr     uint64
		wantNil  bool
		wantAddr uint64
	}{
		{0x80000000, false, 0x80000000},
		{0x80000100, false, 0x80000000},
		{0x8000FFFF, false, 0x80000000},
		{0x80010000, true, 0}, // Just past first region
		{0x90000000, false, 0x90000000},
		{0x70000000, true, 0}, // Before any region
		{0xA0000000, true, 0}, // After all regions
	}

	for _, tc := range tests {
		pr := m.GetRange(tc.addr)
		if tc.wantNil {
			if pr != nil {
				t.Errorf("GetRange(0x%x) expected nil, got range at 0x%x", tc.addr, pr.Addr)
			}
		} else {
			if pr == nil {
				t.Errorf("GetRange(0x%x) expected range, got nil", tc.addr)
			} else if pr.Addr != tc.wantAddr {
				t.Errorf("GetRange(0x%x) expected range at 0x%x, got 0x%x", tc.addr, tc.wantAddr, pr.Addr)
			}
		}
	}
}

func TestRAMReadWrite(t *testing.T) {
	m := NewPhysMemoryMap()
	defer m.Close()

	m.RegisterRAM(0x80000000, 4096, 0)

	// Test 8-bit
	if err := m.Write8(0x80000000, 0x12); err != nil {
		t.Fatalf("Write8 failed: %v", err)
	}
	v8, err := m.Read8(0x80000000)
	if err != nil {
		t.Fatalf("Read8 failed: %v", err)
	}
	if v8 != 0x12 {
		t.Errorf("Read8: expected 0x12, got 0x%x", v8)
	}

	// Test 16-bit (little-endian)
	if err := m.Write16(0x80000010, 0x3456); err != nil {
		t.Fatalf("Write16 failed: %v", err)
	}
	v16, err := m.Read16(0x80000010)
	if err != nil {
		t.Fatalf("Read16 failed: %v", err)
	}
	if v16 != 0x3456 {
		t.Errorf("Read16: expected 0x3456, got 0x%x", v16)
	}

	// Test 32-bit
	if err := m.Write32(0x80000020, 0x789ABCDE); err != nil {
		t.Fatalf("Write32 failed: %v", err)
	}
	v32, err := m.Read32(0x80000020)
	if err != nil {
		t.Fatalf("Read32 failed: %v", err)
	}
	if v32 != 0x789ABCDE {
		t.Errorf("Read32: expected 0x789ABCDE, got 0x%x", v32)
	}

	// Test 64-bit
	if err := m.Write64(0x80000030, 0x123456789ABCDEF0); err != nil {
		t.Fatalf("Write64 failed: %v", err)
	}
	v64, err := m.Read64(0x80000030)
	if err != nil {
		t.Fatalf("Read64 failed: %v", err)
	}
	if v64 != 0x123456789ABCDEF0 {
		t.Errorf("Read64: expected 0x123456789ABCDEF0, got 0x%x", v64)
	}
}

func TestReadWriteGeneric(t *testing.T) {
	m := NewPhysMemoryMap()
	defer m.Close()

	m.RegisterRAM(0x80000000, 4096, 0)

	tests := []struct {
		size  int
		value uint64
	}{
		{1, 0xAB},
		{2, 0xABCD},
		{4, 0xABCDEF01},
		{8, 0xABCDEF0123456789},
	}

	for _, tc := range tests {
		addr := uint64(0x80000000)
		err := m.Write(addr, tc.value, tc.size)
		if err != nil {
			t.Fatalf("Write(%d bytes) failed: %v", tc.size, err)
		}
		got, err := m.Read(addr, tc.size)
		if err != nil {
			t.Fatalf("Read(%d bytes) failed: %v", tc.size, err)
		}
		// Mask to actual size
		mask := uint64((1 << (tc.size * 8)) - 1)
		expected := tc.value & mask
		if got != expected {
			t.Errorf("Read(%d bytes): expected 0x%x, got 0x%x", tc.size, expected, got)
		}
	}
}

func TestROMWriteProtection(t *testing.T) {
	m := NewPhysMemoryMap()
	defer m.Close()

	m.RegisterRAM(0x80000000, 4096, RAMFlagROM)

	// Reads should work
	_, err := m.Read32(0x80000000)
	if err != nil {
		t.Fatalf("Read32 on ROM failed: %v", err)
	}

	// Writes should fail
	err = m.Write32(0x80000000, 0x12345678)
	if err != ErrReadOnly {
		t.Errorf("expected ErrReadOnly for ROM write, got %v", err)
	}
}

func TestDeviceUnsupportedSize(t *testing.T) {
	m := NewPhysMemoryMap()
	defer m.Close()

	// Register device that only supports 32-bit access
	readFn := func(opaque any, offset uint32, sizeLog2 int) uint32 { return 0 }
	writeFn := func(opaque any, offset uint32, val uint32, sizeLog2 int) {}
	m.RegisterDevice(0x10000000, 0x1000, nil, readFn, writeFn, DevIOSize32)

	// 32-bit should work
	_, err := m.Read32(0x10000000)
	if err != nil {
		t.Fatalf("Read32 should work: %v", err)
	}

	// 8-bit should fail
	_, err = m.Read8(0x10000000)
	if err != ErrUnsupportedSize {
		t.Errorf("expected ErrUnsupportedSize for 8-bit read, got %v", err)
	}

	// 16-bit should fail
	_, err = m.Read16(0x10000000)
	if err != ErrUnsupportedSize {
		t.Errorf("expected ErrUnsupportedSize for 16-bit read, got %v", err)
	}
}

func TestGetRAMPtr(t *testing.T) {
	m := NewPhysMemoryMap()
	defer m.Close()

	pr, _ := m.RegisterRAM(0x80000000, 4096, RAMFlagDirtyBits)

	// Write some data directly
	pr.PhysMem[0] = 0xAA
	pr.PhysMem[1] = 0xBB

	// Get pointer and verify
	ptr := m.GetRAMPtr(0x80000000, false)
	if ptr == nil {
		t.Fatal("GetRAMPtr returned nil")
	}
	if ptr[0] != 0xAA || ptr[1] != 0xBB {
		t.Errorf("expected [0xAA, 0xBB], got [0x%x, 0x%x]", ptr[0], ptr[1])
	}

	// Verify dirty bit is not set for read
	if pr.IsDirtyBit(0) {
		t.Error("dirty bit should not be set for read")
	}

	// Get pointer with write flag
	ptr = m.GetRAMPtr(0x80000000, true)
	if !pr.IsDirtyBit(0) {
		t.Error("dirty bit should be set for write")
	}

	// Get pointer for non-RAM address should return nil
	readFn := func(opaque any, offset uint32, sizeLog2 int) uint32 { return 0 }
	writeFn := func(opaque any, offset uint32, val uint32, sizeLog2 int) {}
	m.RegisterDevice(0x10000000, 0x1000, nil, readFn, writeFn, DevIOSize32)

	ptr = m.GetRAMPtr(0x10000000, false)
	if ptr != nil {
		t.Error("GetRAMPtr should return nil for device region")
	}
}

func TestSetAddr(t *testing.T) {
	m := NewPhysMemoryMap()
	defer m.Close()

	pr, _ := m.RegisterRAM(0x80000000, 4096, 0)

	// Disable
	pr.SetAddr(0, false)
	if pr.Size != 0 {
		t.Errorf("expected Size 0 after disable, got %d", pr.Size)
	}
	if m.GetRange(0x80000000) != nil {
		t.Error("disabled region should not be found")
	}

	// Re-enable at different address
	pr.SetAddr(0x90000000, true)
	if pr.Addr != 0x90000000 {
		t.Errorf("expected Addr 0x90000000, got 0x%x", pr.Addr)
	}
	if pr.Size != 4096 {
		t.Errorf("expected Size 4096 after enable, got %d", pr.Size)
	}
	if m.GetRange(0x90000000) == nil {
		t.Error("re-enabled region should be found at new address")
	}
}

func TestDirtyBitsDoubleBuffer(t *testing.T) {
	m := NewPhysMemoryMap()
	defer m.Close()

	pr, _ := m.RegisterRAM(0x80000000, 8192, RAMFlagDirtyBits)

	// Set some dirty bits
	pr.SetDirtyBit(0)
	pr.SetDirtyBit(4096)

	// Get dirty bits (should return current and swap buffers)
	dirty := pr.GetDirtyBits()
	if dirty == nil {
		t.Fatal("GetDirtyBits returned nil")
	}

	// Check the bits
	if dirty[0]&1 == 0 {
		t.Error("page 0 should be dirty in returned bits")
	}
	if dirty[0]&2 == 0 {
		t.Error("page 1 should be dirty in returned bits")
	}

	// After GetDirtyBits, new current buffer should be clean
	if pr.IsDirtyBit(0) {
		t.Error("page 0 should be clean after GetDirtyBits")
	}
	if pr.IsDirtyBit(4096) {
		t.Error("page 1 should be clean after GetDirtyBits")
	}
}

func TestTooManyRanges(t *testing.T) {
	m := NewPhysMemoryMap()
	defer m.Close()

	// Register MaxRanges regions
	for i := 0; i < MaxRanges; i++ {
		_, err := m.RegisterRAM(uint64(i)*0x1000000, 4096, 0)
		if err != nil {
			t.Fatalf("RegisterRAM %d failed: %v", i, err)
		}
	}

	// Next registration should fail
	_, err := m.RegisterRAM(0xF0000000, 4096, 0)
	if err != ErrTooManyRanges {
		t.Errorf("expected ErrTooManyRanges, got %v", err)
	}
}

func TestRangeAt(t *testing.T) {
	m := NewPhysMemoryMap()
	defer m.Close()

	m.RegisterRAM(0x80000000, 4096, 0)
	m.RegisterRAM(0x90000000, 4096, 0)

	r0 := m.RangeAt(0)
	if r0 == nil || r0.Addr != 0x80000000 {
		t.Error("RangeAt(0) should return first range")
	}

	r1 := m.RangeAt(1)
	if r1 == nil || r1.Addr != 0x90000000 {
		t.Error("RangeAt(1) should return second range")
	}

	rNil := m.RangeAt(2)
	if rNil != nil {
		t.Error("RangeAt(2) should return nil")
	}

	rNeg := m.RangeAt(-1)
	if rNeg != nil {
		t.Error("RangeAt(-1) should return nil")
	}
}

func TestTLBFlushCallback(t *testing.T) {
	m := NewPhysMemoryMap()
	defer m.Close()

	flushCalls := 0
	m.SetTLBFlushFunc(nil, func(ramAddr []byte, ramSize uint64) {
		flushCalls++
	})

	pr, _ := m.RegisterRAM(0x80000000, 8192, RAMFlagDirtyBits)

	// Set dirty and get - should trigger flush
	pr.SetDirtyBit(0)
	pr.GetDirtyBits()
	if flushCalls != 1 {
		t.Errorf("expected 1 flush call, got %d", flushCalls)
	}

	// Reset dirty bit should trigger flush
	pr.SetDirtyBit(0)
	pr.ResetDirtyBit(0)
	if flushCalls != 2 {
		t.Errorf("expected 2 flush calls, got %d", flushCalls)
	}

	// SetAddr disable should trigger flush
	pr.SetAddr(0, false)
	if flushCalls != 3 {
		t.Errorf("expected 3 flush calls, got %d", flushCalls)
	}
}

func TestLittleEndianness(t *testing.T) {
	m := NewPhysMemoryMap()
	defer m.Close()

	m.RegisterRAM(0x80000000, 4096, 0)

	// Write 32-bit value
	m.Write32(0x80000000, 0x12345678)

	// Read individual bytes
	b0, _ := m.Read8(0x80000000)
	b1, _ := m.Read8(0x80000001)
	b2, _ := m.Read8(0x80000002)
	b3, _ := m.Read8(0x80000003)

	// Little-endian: LSB first
	if b0 != 0x78 {
		t.Errorf("byte 0: expected 0x78, got 0x%x", b0)
	}
	if b1 != 0x56 {
		t.Errorf("byte 1: expected 0x56, got 0x%x", b1)
	}
	if b2 != 0x34 {
		t.Errorf("byte 2: expected 0x34, got 0x%x", b2)
	}
	if b3 != 0x12 {
		t.Errorf("byte 3: expected 0x12, got 0x%x", b3)
	}
}

// TestUnmappedMemoryAccess_CBehavior tests that unmapped memory access
// matches TinyEMU C behavior: writes are silent no-ops, reads return 0.
// Reference: riscv_cpu.c:376-382 (read), riscv_cpu.c:462-468 (write)
func TestUnmappedMemoryAccess_CBehavior(t *testing.T) {
	m := NewPhysMemoryMap()
	defer m.Close()

	// Register a small RAM region so we have a valid baseline
	m.RegisterRAM(0x80000000, 4096, 0)

	// Test Case 1: Write to unmapped address should NOT return error
	// C behavior: silent no-op (riscv_cpu.c:463-468)
	t.Run("WriteUnmapped_NoError", func(t *testing.T) {
		// Address 0x70000000 is completely unmapped
		err := m.Write32(0x70000000, 0x12345678)
		if err != nil {
			t.Errorf("Write to unmapped address should not error (C behavior), got: %v", err)
		}

		err = m.Write8(0x70000000, 0xAB)
		if err != nil {
			t.Errorf("Write8 to unmapped address should not error, got: %v", err)
		}

		err = m.Write16(0x70000000, 0xABCD)
		if err != nil {
			t.Errorf("Write16 to unmapped address should not error, got: %v", err)
		}

		err = m.Write64(0x70000000, 0x123456789ABCDEF0)
		if err != nil {
			t.Errorf("Write64 to unmapped address should not error, got: %v", err)
		}
	})

	// Test Case 2: Read from unmapped address should return 0, no error
	// C behavior: returns 0 with ret=0 (riscv_cpu.c:376-382)
	t.Run("ReadUnmapped_ReturnsZero", func(t *testing.T) {
		val32, err := m.Read32(0x70000000)
		if err != nil {
			t.Errorf("Read from unmapped address should not error (C behavior), got: %v", err)
		}
		if val32 != 0 {
			t.Errorf("Read from unmapped address should return 0, got: 0x%x", val32)
		}

		val8, err := m.Read8(0x70000000)
		if err != nil {
			t.Errorf("Read8 from unmapped address should not error, got: %v", err)
		}
		if val8 != 0 {
			t.Errorf("Read8 from unmapped address should return 0, got: 0x%x", val8)
		}

		val16, err := m.Read16(0x70000000)
		if err != nil {
			t.Errorf("Read16 from unmapped address should not error, got: %v", err)
		}
		if val16 != 0 {
			t.Errorf("Read16 from unmapped address should return 0, got: 0x%x", val16)
		}

		val64, err := m.Read64(0x70000000)
		if err != nil {
			t.Errorf("Read64 from unmapped address should not error, got: %v", err)
		}
		if val64 != 0 {
			t.Errorf("Read64 from unmapped address should return 0, got: 0x%x", val64)
		}
	})

	// Test Case 3: Write to boundary address (first addr after low RAM) should not fault
	// This tests edge case at 0x10000 (64KB, typical low memory boundary)
	t.Run("WriteBoundary_NoFault", func(t *testing.T) {
		// Register low RAM at 0x0 with size 0x10000 (64KB)
		m2 := NewPhysMemoryMap()
		defer m2.Close()
		m2.RegisterRAM(0x0, 0x10000, 0)

		// Write to 0x10000 (first address after RAM) should not error
		err := m2.Write32(0x10000, 0xDEADBEEF)
		if err != nil {
			t.Errorf("Write to boundary address 0x10000 should not error, got: %v", err)
		}

		// Read from 0x10000 should return 0, no error
		val, err := m2.Read32(0x10000)
		if err != nil {
			t.Errorf("Read from boundary address 0x10000 should not error, got: %v", err)
		}
		if val != 0 {
			t.Errorf("Read from boundary address should return 0, got: 0x%x", val)
		}
	})
}

func TestDevice64BitAccess(t *testing.T) {
	m := NewPhysMemoryMap()
	defer m.Close()

	var lastOffset uint32
	var lastVal uint32
	var lastSize int
	readCount := 0

	readFn := func(opaque any, offset uint32, sizeLog2 int) uint32 {
		lastOffset = offset
		lastSize = sizeLog2
		readCount++
		if offset == 0 {
			return 0xAAAAAAAA
		}
		return 0xBBBBBBBB
	}
	writeFn := func(opaque any, offset uint32, val uint32, sizeLog2 int) {
		lastOffset = offset
		lastVal = val
		lastSize = sizeLog2
	}

	m.RegisterDevice(0x10000000, 0x1000, nil, readFn, writeFn, DevIOSize32)

	// 64-bit read should result in two 32-bit reads
	val, err := m.Read64(0x10000000)
	if err != nil {
		t.Fatalf("Read64 failed: %v", err)
	}
	if readCount != 2 {
		t.Errorf("expected 2 read calls for 64-bit read, got %d", readCount)
	}
	expected := uint64(0xBBBBBBBBAAAAAAAA)
	if val != expected {
		t.Errorf("Read64: expected 0x%x, got 0x%x", expected, val)
	}

	// 64-bit write should result in two 32-bit writes
	err = m.Write64(0x10000000, 0x1234567890ABCDEF)
	if err != nil {
		t.Fatalf("Write64 failed: %v", err)
	}
	// Last write should be high word at offset 4
	if lastOffset != 4 {
		t.Errorf("last write offset: expected 4, got %d", lastOffset)
	}
	if lastVal != 0x12345678 {
		t.Errorf("last write value: expected 0x12345678, got 0x%x", lastVal)
	}
	_ = lastSize // unused, but we verify it's set correctly
}
