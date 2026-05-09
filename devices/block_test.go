package devices

import (
	"bytes"
	"io"
	"testing"
)

func TestNewMemoryBlockDevice(t *testing.T) {
	tests := []struct {
		name    string
		size    int64
		wantErr bool
	}{
		{"valid size", 4096, false},
		{"minimum size", 512, false},
		{"large size", 1024 * 1024, false},
		{"zero size", 0, true},
		{"negative size", -512, true},
		{"non-sector-aligned", 1000, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			bd, err := NewMemoryBlockDevice(tc.size)
			if tc.wantErr {
				if err == nil {
					t.Errorf("expected error for size %d", tc.size)
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}
			if bd.GetSectorCount() != tc.size/SectorSize {
				t.Errorf("sector count = %d, want %d", bd.GetSectorCount(), tc.size/SectorSize)
			}
		})
	}
}

func TestMemoryBlockDeviceReadWrite(t *testing.T) {
	// Create a 4KB device (8 sectors)
	bd, err := NewMemoryBlockDevice(4096)
	if err != nil {
		t.Fatalf("failed to create device: %v", err)
	}

	// Write test data to first sector
	writeData := make([]byte, SectorSize)
	for i := range writeData {
		writeData[i] = byte(i)
	}

	n, err := bd.WriteSectors(0, writeData, 1)
	if err != nil {
		t.Fatalf("WriteSectors failed: %v", err)
	}
	if n != SectorSize {
		t.Errorf("WriteSectors returned %d, want %d", n, SectorSize)
	}

	// Read it back
	readData := make([]byte, SectorSize)
	n, err = bd.ReadSectors(0, readData, 1)
	if err != nil {
		t.Fatalf("ReadSectors failed: %v", err)
	}
	if n != SectorSize {
		t.Errorf("ReadSectors returned %d, want %d", n, SectorSize)
	}

	if !bytes.Equal(readData, writeData) {
		t.Error("read data doesn't match written data")
	}
}

func TestMemoryBlockDeviceMultipleSectors(t *testing.T) {
	// Create a 4KB device (8 sectors)
	bd, err := NewMemoryBlockDevice(4096)
	if err != nil {
		t.Fatalf("failed to create device: %v", err)
	}

	// Write to multiple sectors at once
	writeData := make([]byte, SectorSize*3)
	for i := range writeData {
		writeData[i] = byte(i % 256)
	}

	n, err := bd.WriteSectors(2, writeData, 3)
	if err != nil {
		t.Fatalf("WriteSectors failed: %v", err)
	}
	if n != SectorSize*3 {
		t.Errorf("WriteSectors returned %d, want %d", n, SectorSize*3)
	}

	// Read it back
	readData := make([]byte, SectorSize*3)
	n, err = bd.ReadSectors(2, readData, 3)
	if err != nil {
		t.Fatalf("ReadSectors failed: %v", err)
	}
	if !bytes.Equal(readData, writeData) {
		t.Error("read data doesn't match written data")
	}
}

func TestMemoryBlockDeviceOutOfRange(t *testing.T) {
	bd, err := NewMemoryBlockDevice(4096) // 8 sectors
	if err != nil {
		t.Fatalf("failed to create device: %v", err)
	}

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

	// Read spanning past end
	_, err = bd.ReadSectors(7, make([]byte, SectorSize*2), 2)
	if err != ErrSectorOutOfRange {
		t.Errorf("expected ErrSectorOutOfRange, got %v", err)
	}
}

func TestMemoryBlockDeviceBufferTooSmall(t *testing.T) {
	bd, err := NewMemoryBlockDevice(4096)
	if err != nil {
		t.Fatalf("failed to create device: %v", err)
	}

	// Buffer too small for read
	smallBuf := make([]byte, SectorSize-1)
	_, err = bd.ReadSectors(0, smallBuf, 1)
	if err != ErrBufferTooSmall {
		t.Errorf("expected ErrBufferTooSmall, got %v", err)
	}

	// Buffer too small for write
	_, err = bd.WriteSectors(0, smallBuf, 1)
	if err != ErrBufferTooSmall {
		t.Errorf("expected ErrBufferTooSmall, got %v", err)
	}
}

func TestMemoryBlockDeviceReadOnly(t *testing.T) {
	bd, err := NewMemoryBlockDevice(4096)
	if err != nil {
		t.Fatalf("failed to create device: %v", err)
	}

	bd.SetReadOnly(true)

	buf := make([]byte, SectorSize)
	_, err = bd.WriteSectors(0, buf, 1)
	if err != ErrReadOnly {
		t.Errorf("expected ErrReadOnly, got %v", err)
	}

	// Reading should still work
	_, err = bd.ReadSectors(0, buf, 1)
	if err != nil {
		t.Errorf("reading from read-only device failed: %v", err)
	}
}

func TestNewMemoryBlockDeviceFromData(t *testing.T) {
	data := []byte("hello world test data that spans multiple bytes")
	bd := NewMemoryBlockDeviceFromData(data)

	// Should round up to sector size
	if bd.GetSectorCount() != 1 {
		t.Errorf("sector count = %d, want 1", bd.GetSectorCount())
	}

	// Read back
	readBuf := make([]byte, SectorSize)
	_, err := bd.ReadSectors(0, readBuf, 1)
	if err != nil {
		t.Fatalf("ReadSectors failed: %v", err)
	}

	// First bytes should match original data
	if !bytes.Equal(readBuf[:len(data)], data) {
		t.Error("data doesn't match")
	}
}

func TestReadAt(t *testing.T) {
	// Create device with known data
	data := make([]byte, 4096)
	for i := range data {
		data[i] = byte(i % 256)
	}
	bd := NewMemoryBlockDeviceFromData(data)

	tests := []struct {
		name   string
		offset int64
		size   int
		want   []byte
		err    error
	}{
		{"start of sector", 0, 10, data[0:10], nil},
		{"middle of sector", 100, 50, data[100:150], nil},
		{"cross sector", 500, 50, data[500:550], nil},
		{"past end", 5000, 10, nil, io.EOF},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			buf := make([]byte, tc.size)
			n, err := ReadAt(bd, buf, tc.offset)

			if tc.err != nil {
				if err != tc.err {
					t.Errorf("expected error %v, got %v", tc.err, err)
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if n != len(tc.want) {
				t.Errorf("read %d bytes, want %d", n, len(tc.want))
			}

			if !bytes.Equal(buf[:n], tc.want) {
				t.Error("data doesn't match")
			}
		})
	}
}

func TestWriteAt(t *testing.T) {
	bd, err := NewMemoryBlockDevice(4096)
	if err != nil {
		t.Fatalf("failed to create device: %v", err)
	}

	// Write at non-sector-aligned offset
	data := []byte("test data")
	n, err := WriteAt(bd, data, 100)
	if err != nil {
		t.Fatalf("WriteAt failed: %v", err)
	}
	if n != len(data) {
		t.Errorf("wrote %d bytes, want %d", n, len(data))
	}

	// Read back
	readBuf := make([]byte, len(data))
	n, err = ReadAt(bd, readBuf, 100)
	if err != nil {
		t.Fatalf("ReadAt failed: %v", err)
	}
	if !bytes.Equal(readBuf, data) {
		t.Errorf("read %q, want %q", readBuf, data)
	}
}

func TestWriteAtCrossSector(t *testing.T) {
	bd, err := NewMemoryBlockDevice(4096)
	if err != nil {
		t.Fatalf("failed to create device: %v", err)
	}

	// Write data spanning two sectors
	data := make([]byte, 100)
	for i := range data {
		data[i] = byte(i)
	}

	// Start near end of first sector
	n, err := WriteAt(bd, data, 500)
	if err != nil {
		t.Fatalf("WriteAt failed: %v", err)
	}
	if n != len(data) {
		t.Errorf("wrote %d bytes, want %d", n, len(data))
	}

	// Read back
	readBuf := make([]byte, len(data))
	n, err = ReadAt(bd, readBuf, 500)
	if err != nil {
		t.Fatalf("ReadAt failed: %v", err)
	}
	if !bytes.Equal(readBuf, data) {
		t.Error("data doesn't match")
	}
}

func TestBlockDeviceSize(t *testing.T) {
	bd, _ := NewMemoryBlockDevice(4096)
	if BlockDeviceSize(bd) != 4096 {
		t.Errorf("BlockDeviceSize = %d, want 4096", BlockDeviceSize(bd))
	}
}

func TestFlushAndClose(t *testing.T) {
	bd, _ := NewMemoryBlockDevice(4096)

	if err := bd.Flush(); err != nil {
		t.Errorf("Flush failed: %v", err)
	}

	if err := bd.Close(); err != nil {
		t.Errorf("Close failed: %v", err)
	}
}
