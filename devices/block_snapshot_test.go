package devices

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// TestSnapshotBlockDeviceCopyOnWrite tests that writes are stored in memory
// and reads return the written data, matching C behavior.
// Reference: tinyemu-2019-12-21/temu.c:248-265 (bf_read_async BF_MODE_SNAPSHOT)
// Reference: tinyemu-2019-12-21/temu.c:284-298 (bf_write_async BF_MODE_SNAPSHOT)
func TestSnapshotBlockDeviceCopyOnWrite(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "test.img")

	// Create file with known data
	originalData := make([]byte, 4096)
	for i := range originalData {
		originalData[i] = 0xAA
	}
	if err := os.WriteFile(path, originalData, 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	// Open as read-only backing device
	backing, err := OpenFileBlockDevice(path, ModeReadOnly)
	if err != nil {
		t.Fatalf("OpenFileBlockDevice failed: %v", err)
	}
	defer backing.Close()

	// Wrap with snapshot device
	snap := NewSnapshotBlockDevice(backing)
	defer snap.Close()

	// Write different data
	writeData := make([]byte, SectorSize)
	for i := range writeData {
		writeData[i] = 0xBB
	}
	n, err := snap.WriteSectors(0, writeData, 1)
	if err != nil {
		t.Fatalf("WriteSectors failed: %v", err)
	}
	if n != SectorSize {
		t.Errorf("wrote %d bytes, want %d", n, SectorSize)
	}

	// Read back - should return the written data (0xBB), not original (0xAA)
	readData := make([]byte, SectorSize)
	n, err = snap.ReadSectors(0, readData, 1)
	if err != nil {
		t.Fatalf("ReadSectors failed: %v", err)
	}
	if n != SectorSize {
		t.Errorf("read %d bytes, want %d", n, SectorSize)
	}
	if !bytes.Equal(readData, writeData) {
		t.Errorf("snapshot read returned original data, expected written data")
	}

	// Read unmodified sector - should return original data
	readData2 := make([]byte, SectorSize)
	n, err = snap.ReadSectors(1, readData2, 1)
	if err != nil {
		t.Fatalf("ReadSectors failed: %v", err)
	}
	if !bytes.Equal(readData2, originalData[SectorSize:SectorSize*2]) {
		t.Error("unmodified sector should return original data")
	}

	// Verify original file was not modified
	fileData, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}
	if !bytes.Equal(fileData, originalData) {
		t.Error("snapshot mode modified the underlying file")
	}
}

func TestSnapshotBlockDeviceGetSectorCount(t *testing.T) {
	backing, err := NewMemoryBlockDevice(4096)
	if err != nil {
		t.Fatalf("NewMemoryBlockDevice failed: %v", err)
	}

	snap := NewSnapshotBlockDevice(backing)
	defer snap.Close()

	if got := snap.GetSectorCount(); got != 8 {
		t.Errorf("GetSectorCount() = %d, want 8", got)
	}
}

func TestSnapshotBlockDeviceMultipleSectors(t *testing.T) {
	// Create backing device with known data
	backing, err := NewMemoryBlockDevice(4096)
	if err != nil {
		t.Fatalf("NewMemoryBlockDevice failed: %v", err)
	}

	// Initialize with pattern
	for i := 0; i < int(backing.GetSectorCount()); i++ {
		data := make([]byte, SectorSize)
		for j := range data {
			data[j] = byte(i)
		}
		backing.WriteSectors(uint64(i), data, 1)
	}

	snap := NewSnapshotBlockDevice(backing)
	defer snap.Close()

	// Write to sectors 2 and 4
	writeData := make([]byte, SectorSize)
	for i := range writeData {
		writeData[i] = 0xFF
	}
	snap.WriteSectors(2, writeData, 1)
	snap.WriteSectors(4, writeData, 1)

	// Read all sectors and verify
	for i := 0; i < int(backing.GetSectorCount()); i++ {
		readData := make([]byte, SectorSize)
		_, err := snap.ReadSectors(uint64(i), readData, 1)
		if err != nil {
			t.Fatalf("ReadSectors(%d) failed: %v", i, err)
		}

		if i == 2 || i == 4 {
			// Modified sectors should return 0xFF
			if readData[0] != 0xFF {
				t.Errorf("sector %d: got %d, want 0xFF", i, readData[0])
			}
		} else {
			// Unmodified sectors should return original pattern
			if readData[0] != byte(i) {
				t.Errorf("sector %d: got %d, want %d", i, readData[0], i)
			}
		}
	}
}

func TestSnapshotBlockDeviceModifiedSectorCount(t *testing.T) {
	backing, err := NewMemoryBlockDevice(4096)
	if err != nil {
		t.Fatalf("NewMemoryBlockDevice failed: %v", err)
	}

	snap := NewSnapshotBlockDevice(backing)
	defer snap.Close()

	if snap.ModifiedSectorCount() != 0 {
		t.Error("expected 0 modified sectors initially")
	}

	data := make([]byte, SectorSize)
	snap.WriteSectors(0, data, 1)
	if snap.ModifiedSectorCount() != 1 {
		t.Error("expected 1 modified sector after write")
	}

	// Writing same sector again shouldn't increase count
	snap.WriteSectors(0, data, 1)
	if snap.ModifiedSectorCount() != 1 {
		t.Error("expected 1 modified sector after rewrite")
	}

	snap.WriteSectors(3, data, 1)
	if snap.ModifiedSectorCount() != 2 {
		t.Error("expected 2 modified sectors")
	}
}

func TestSnapshotBlockDeviceBoundaryChecks(t *testing.T) {
	backing, err := NewMemoryBlockDevice(4096)
	if err != nil {
		t.Fatalf("NewMemoryBlockDevice failed: %v", err)
	}

	snap := NewSnapshotBlockDevice(backing)
	defer snap.Close()

	buf := make([]byte, SectorSize)

	// Read past end
	_, err = snap.ReadSectors(8, buf, 1)
	if err != ErrSectorOutOfRange {
		t.Errorf("expected ErrSectorOutOfRange, got %v", err)
	}

	// Write past end
	_, err = snap.WriteSectors(8, buf, 1)
	if err != ErrSectorOutOfRange {
		t.Errorf("expected ErrSectorOutOfRange, got %v", err)
	}

	// Buffer too small
	smallBuf := make([]byte, SectorSize-1)
	_, err = snap.ReadSectors(0, smallBuf, 1)
	if err != ErrBufferTooSmall {
		t.Errorf("expected ErrBufferTooSmall, got %v", err)
	}

	_, err = snap.WriteSectors(0, smallBuf, 1)
	if err != ErrBufferTooSmall {
		t.Errorf("expected ErrBufferTooSmall, got %v", err)
	}
}

func TestSnapshotBlockDeviceClose(t *testing.T) {
	backing, err := NewMemoryBlockDevice(4096)
	if err != nil {
		t.Fatalf("NewMemoryBlockDevice failed: %v", err)
	}

	snap := NewSnapshotBlockDevice(backing)

	// Write some data
	data := make([]byte, SectorSize)
	snap.WriteSectors(0, data, 1)

	// Close
	if err := snap.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// Operations should fail on closed device
	_, err = snap.ReadSectors(0, data, 1)
	if err != ErrDeviceClosed {
		t.Errorf("expected ErrDeviceClosed, got %v", err)
	}

	_, err = snap.WriteSectors(0, data, 1)
	if err != ErrDeviceClosed {
		t.Errorf("expected ErrDeviceClosed, got %v", err)
	}

	err = snap.Flush()
	if err != ErrDeviceClosed {
		t.Errorf("expected ErrDeviceClosed, got %v", err)
	}

	// Double close should be safe
	if err := snap.Close(); err != nil {
		t.Errorf("double close failed: %v", err)
	}
}

func TestSnapshotBlockDeviceFlush(t *testing.T) {
	backing, err := NewMemoryBlockDevice(4096)
	if err != nil {
		t.Fatalf("NewMemoryBlockDevice failed: %v", err)
	}

	snap := NewSnapshotBlockDevice(backing)
	defer snap.Close()

	// Flush should succeed (no-op)
	if err := snap.Flush(); err != nil {
		t.Errorf("Flush failed: %v", err)
	}
}

func TestSnapshotBlockDeviceZeroLengthOperations(t *testing.T) {
	backing, err := NewMemoryBlockDevice(4096)
	if err != nil {
		t.Fatalf("NewMemoryBlockDevice failed: %v", err)
	}

	snap := NewSnapshotBlockDevice(backing)
	defer snap.Close()

	// Zero-length operations should succeed
	n, err := snap.ReadSectors(0, nil, 0)
	if err != nil || n != 0 {
		t.Errorf("zero-length read: got (%d, %v), want (0, nil)", n, err)
	}

	n, err = snap.WriteSectors(0, nil, 0)
	if err != nil || n != 0 {
		t.Errorf("zero-length write: got (%d, %v), want (0, nil)", n, err)
	}
}

func TestSnapshotBlockDeviceConsecutiveSectors(t *testing.T) {
	backing, err := NewMemoryBlockDevice(4096)
	if err != nil {
		t.Fatalf("NewMemoryBlockDevice failed: %v", err)
	}

	// Initialize backing with known pattern
	for i := 0; i < int(backing.GetSectorCount()); i++ {
		data := make([]byte, SectorSize)
		for j := range data {
			data[j] = byte(i * 10)
		}
		backing.WriteSectors(uint64(i), data, 1)
	}

	snap := NewSnapshotBlockDevice(backing)
	defer snap.Close()

	// Write consecutive sectors with different pattern
	writeData := make([]byte, SectorSize*3)
	for i := range writeData {
		writeData[i] = 0xAB
	}
	n, err := snap.WriteSectors(2, writeData, 3)
	if err != nil {
		t.Fatalf("WriteSectors failed: %v", err)
	}
	if n != SectorSize*3 {
		t.Errorf("wrote %d bytes, want %d", n, SectorSize*3)
	}

	// Read them back
	readData := make([]byte, SectorSize*3)
	n, err = snap.ReadSectors(2, readData, 3)
	if err != nil {
		t.Fatalf("ReadSectors failed: %v", err)
	}
	if !bytes.Equal(readData, writeData) {
		t.Error("consecutive sector read doesn't match written data")
	}
}
