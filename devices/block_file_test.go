package devices

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestCreateFileBlockDevice(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "test.img")

	bd, err := CreateFileBlockDevice(path, 4096)
	if err != nil {
		t.Fatalf("CreateFileBlockDevice failed: %v", err)
	}
	defer bd.Close()

	if bd.GetSectorCount() != 8 {
		t.Errorf("sector count = %d, want 8", bd.GetSectorCount())
	}

	if bd.Mode() != ModeReadWrite {
		t.Errorf("mode = %d, want ModeReadWrite", bd.Mode())
	}

	// Verify file was created with correct size
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("failed to stat file: %v", err)
	}
	if info.Size() != 4096 {
		t.Errorf("file size = %d, want 4096", info.Size())
	}
}

func TestCreateFileBlockDeviceTooSmall(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "test.img")

	_, err := CreateFileBlockDevice(path, 100)
	if err == nil {
		t.Error("expected error for size < SectorSize")
	}
}

func TestOpenFileBlockDevice(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "test.img")

	// Create a test file
	data := make([]byte, 4096)
	for i := range data {
		data[i] = byte(i % 256)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	bd, err := OpenFileBlockDevice(path, ModeReadOnly)
	if err != nil {
		t.Fatalf("OpenFileBlockDevice failed: %v", err)
	}
	defer bd.Close()

	if bd.GetSectorCount() != 8 {
		t.Errorf("sector count = %d, want 8", bd.GetSectorCount())
	}

	// Read and verify data
	buf := make([]byte, SectorSize)
	n, err := bd.ReadSectors(0, buf, 1)
	if err != nil {
		t.Fatalf("ReadSectors failed: %v", err)
	}
	if n != SectorSize {
		t.Errorf("read %d bytes, want %d", n, SectorSize)
	}
	if !bytes.Equal(buf, data[:SectorSize]) {
		t.Error("data mismatch")
	}
}

func TestOpenFileBlockDeviceNonExistent(t *testing.T) {
	_, err := OpenFileBlockDevice("/nonexistent/path/file.img", ModeReadOnly)
	if err == nil {
		t.Error("expected error for non-existent file")
	}
}

func TestFileBlockDeviceReadWrite(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "test.img")

	bd, err := CreateFileBlockDevice(path, 4096)
	if err != nil {
		t.Fatalf("CreateFileBlockDevice failed: %v", err)
	}
	defer bd.Close()

	// Write test data
	writeData := make([]byte, SectorSize)
	for i := range writeData {
		writeData[i] = byte(i)
	}

	n, err := bd.WriteSectors(0, writeData, 1)
	if err != nil {
		t.Fatalf("WriteSectors failed: %v", err)
	}
	if n != SectorSize {
		t.Errorf("wrote %d bytes, want %d", n, SectorSize)
	}

	// Flush to ensure persistence
	if err := bd.Flush(); err != nil {
		t.Fatalf("Flush failed: %v", err)
	}

	// Read back
	readData := make([]byte, SectorSize)
	n, err = bd.ReadSectors(0, readData, 1)
	if err != nil {
		t.Fatalf("ReadSectors failed: %v", err)
	}
	if !bytes.Equal(readData, writeData) {
		t.Error("read data doesn't match written data")
	}
}

func TestFileBlockDeviceMultipleSectors(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "test.img")

	bd, err := CreateFileBlockDevice(path, 4096)
	if err != nil {
		t.Fatalf("CreateFileBlockDevice failed: %v", err)
	}
	defer bd.Close()

	// Write multiple sectors
	writeData := make([]byte, SectorSize*3)
	for i := range writeData {
		writeData[i] = byte(i % 256)
	}

	n, err := bd.WriteSectors(2, writeData, 3)
	if err != nil {
		t.Fatalf("WriteSectors failed: %v", err)
	}
	if n != SectorSize*3 {
		t.Errorf("wrote %d bytes, want %d", n, SectorSize*3)
	}

	// Read back
	readData := make([]byte, SectorSize*3)
	n, err = bd.ReadSectors(2, readData, 3)
	if err != nil {
		t.Fatalf("ReadSectors failed: %v", err)
	}
	if !bytes.Equal(readData, writeData) {
		t.Error("read data doesn't match written data")
	}
}

func TestFileBlockDeviceOutOfRange(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "test.img")

	bd, err := CreateFileBlockDevice(path, 4096)
	if err != nil {
		t.Fatalf("CreateFileBlockDevice failed: %v", err)
	}
	defer bd.Close()

	buf := make([]byte, SectorSize)

	// Read past end
	_, err = bd.ReadSectors(8, buf, 1)
	if err != ErrSectorOutOfRange {
		t.Errorf("expected ErrSectorOutOfRange, got %v", err)
	}

	// Write past end
	_, err = bd.WriteSectors(8, buf, 1)
	if err != ErrSectorOutOfRange {
		t.Errorf("expected ErrSectorOutOfRange, got %v", err)
	}
}

func TestFileBlockDeviceReadOnly(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "test.img")

	// Create file first
	if err := os.WriteFile(path, make([]byte, 4096), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	bd, err := OpenFileBlockDevice(path, ModeReadOnly)
	if err != nil {
		t.Fatalf("OpenFileBlockDevice failed: %v", err)
	}
	defer bd.Close()

	buf := make([]byte, SectorSize)

	// Writing should fail
	_, err = bd.WriteSectors(0, buf, 1)
	if err != ErrReadOnly {
		t.Errorf("expected ErrReadOnly, got %v", err)
	}

	// Reading should work
	_, err = bd.ReadSectors(0, buf, 1)
	if err != nil {
		t.Errorf("reading from read-only device failed: %v", err)
	}
}

func TestFileBlockDeviceSnapshot(t *testing.T) {
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

	bd, err := OpenFileBlockDevice(path, ModeSnapshot)
	if err != nil {
		t.Fatalf("OpenFileBlockDevice failed: %v", err)
	}
	defer bd.Close()

	// Write different data
	writeData := make([]byte, SectorSize)
	for i := range writeData {
		writeData[i] = 0xBB
	}
	n, err := bd.WriteSectors(0, writeData, 1)
	if err != nil {
		t.Fatalf("WriteSectors failed: %v", err)
	}
	if n != SectorSize {
		t.Errorf("wrote %d bytes, want %d", n, SectorSize)
	}

	// Close and reopen
	bd.Close()

	// Verify file was not modified
	fileData, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}
	if !bytes.Equal(fileData, originalData) {
		t.Error("snapshot mode modified the file")
	}
}

func TestFileBlockDevicePersistence(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "test.img")

	// Create and write data
	bd, err := CreateFileBlockDevice(path, 4096)
	if err != nil {
		t.Fatalf("CreateFileBlockDevice failed: %v", err)
	}

	writeData := make([]byte, SectorSize)
	for i := range writeData {
		writeData[i] = byte(i)
	}
	bd.WriteSectors(0, writeData, 1)
	bd.Close()

	// Reopen and verify
	bd2, err := OpenFileBlockDevice(path, ModeReadOnly)
	if err != nil {
		t.Fatalf("OpenFileBlockDevice failed: %v", err)
	}
	defer bd2.Close()

	readData := make([]byte, SectorSize)
	bd2.ReadSectors(0, readData, 1)
	if !bytes.Equal(readData, writeData) {
		t.Error("data was not persisted")
	}
}

func TestFileBlockDeviceNonSectorAlignedFile(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "test.img")

	// Create file with non-sector-aligned size
	if err := os.WriteFile(path, make([]byte, 1000), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	bd, err := OpenFileBlockDevice(path, ModeReadOnly)
	if err != nil {
		t.Fatalf("OpenFileBlockDevice failed: %v", err)
	}
	defer bd.Close()

	// Should report 1 sector (rounded down from 1000)
	if bd.GetSectorCount() != 1 {
		t.Errorf("sector count = %d, want 1", bd.GetSectorCount())
	}
}

func TestFileBlockDeviceBufferTooSmall(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "test.img")

	bd, err := CreateFileBlockDevice(path, 4096)
	if err != nil {
		t.Fatalf("CreateFileBlockDevice failed: %v", err)
	}
	defer bd.Close()

	smallBuf := make([]byte, SectorSize-1)

	_, err = bd.ReadSectors(0, smallBuf, 1)
	if err != ErrBufferTooSmall {
		t.Errorf("expected ErrBufferTooSmall, got %v", err)
	}

	_, err = bd.WriteSectors(0, smallBuf, 1)
	if err != ErrBufferTooSmall {
		t.Errorf("expected ErrBufferTooSmall, got %v", err)
	}
}

func TestFileBlockDeviceClosed(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "test.img")

	bd, err := CreateFileBlockDevice(path, 4096)
	if err != nil {
		t.Fatalf("CreateFileBlockDevice failed: %v", err)
	}

	bd.Close()

	buf := make([]byte, SectorSize)

	_, err = bd.ReadSectors(0, buf, 1)
	if err == nil {
		t.Error("expected error reading from closed device")
	}

	_, err = bd.WriteSectors(0, buf, 1)
	if err == nil {
		t.Error("expected error writing to closed device")
	}

	err = bd.Flush()
	if err == nil {
		t.Error("expected error flushing closed device")
	}

	// Double close should be safe
	if err := bd.Close(); err != nil {
		t.Errorf("double close failed: %v", err)
	}
}
