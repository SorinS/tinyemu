package devices

import (
	"testing"
)

func TestNewSplitBlockDevice(t *testing.T) {
	chunk1, _ := NewMemoryBlockDevice(SectorSize * 10)
	chunk2, _ := NewMemoryBlockDevice(SectorSize * 10)

	tests := []struct {
		name    string
		config  SplitBlockDeviceConfig
		wantErr bool
	}{
		{
			name: "valid single chunk",
			config: SplitBlockDeviceConfig{
				TotalSectors: 10,
				Mode:         ModeReadWrite,
				Chunks: []Chunk{
					{StartSector: 0, SectorCount: 10, Device: chunk1},
				},
			},
			wantErr: false,
		},
		{
			name: "valid multiple chunks",
			config: SplitBlockDeviceConfig{
				TotalSectors: 30,
				Mode:         ModeReadWrite,
				Chunks: []Chunk{
					{StartSector: 0, SectorCount: 10, Device: chunk1},
					{StartSector: 20, SectorCount: 10, Device: chunk2},
				},
			},
			wantErr: false,
		},
		{
			name: "valid with sparse gap",
			config: SplitBlockDeviceConfig{
				TotalSectors: 100,
				Mode:         ModeReadOnly,
				Chunks: []Chunk{
					{StartSector: 10, SectorCount: 10, Device: chunk1},
					{StartSector: 50, SectorCount: 10, Device: chunk2},
				},
			},
			wantErr: false,
		},
		{
			name: "invalid zero total sectors",
			config: SplitBlockDeviceConfig{
				TotalSectors: 0,
				Chunks:       []Chunk{},
			},
			wantErr: true,
		},
		{
			name: "invalid negative total sectors",
			config: SplitBlockDeviceConfig{
				TotalSectors: -1,
				Chunks:       []Chunk{},
			},
			wantErr: true,
		},
		{
			name: "invalid chunk extends beyond total",
			config: SplitBlockDeviceConfig{
				TotalSectors: 5,
				Chunks: []Chunk{
					{StartSector: 0, SectorCount: 10, Device: chunk1},
				},
			},
			wantErr: true,
		},
		{
			name: "invalid overlapping chunks",
			config: SplitBlockDeviceConfig{
				TotalSectors: 20,
				Chunks: []Chunk{
					{StartSector: 0, SectorCount: 10, Device: chunk1},
					{StartSector: 5, SectorCount: 10, Device: chunk2},
				},
			},
			wantErr: true,
		},
		{
			name: "invalid nil device",
			config: SplitBlockDeviceConfig{
				TotalSectors: 10,
				Chunks: []Chunk{
					{StartSector: 0, SectorCount: 10, Device: nil},
				},
			},
			wantErr: true,
		},
		{
			name: "invalid zero sector count",
			config: SplitBlockDeviceConfig{
				TotalSectors: 10,
				Chunks: []Chunk{
					{StartSector: 0, SectorCount: 0, Device: chunk1},
				},
			},
			wantErr: true,
		},
		{
			name: "invalid negative start sector",
			config: SplitBlockDeviceConfig{
				TotalSectors: 10,
				Chunks: []Chunk{
					{StartSector: -1, SectorCount: 5, Device: chunk1},
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewSplitBlockDevice(tt.config)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewSplitBlockDevice() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestSplitBlockDeviceGetSectorCount(t *testing.T) {
	chunk, _ := NewMemoryBlockDevice(SectorSize * 10)

	sd, err := NewSplitBlockDevice(SplitBlockDeviceConfig{
		TotalSectors: 100,
		Chunks: []Chunk{
			{StartSector: 0, SectorCount: 10, Device: chunk},
		},
	})
	if err != nil {
		t.Fatalf("failed to create split device: %v", err)
	}

	if sd.GetSectorCount() != 100 {
		t.Errorf("GetSectorCount() = %d, want 100", sd.GetSectorCount())
	}
}

func TestSplitBlockDeviceReadSingleChunk(t *testing.T) {
	// Create a chunk with known data
	chunk, _ := NewMemoryBlockDevice(SectorSize * 10)
	for i := 0; i < 10*SectorSize; i++ {
		chunk.data[i] = byte(i % 256)
	}

	sd, err := NewSplitBlockDevice(SplitBlockDeviceConfig{
		TotalSectors: 10,
		Chunks: []Chunk{
			{StartSector: 0, SectorCount: 10, Device: chunk},
		},
	})
	if err != nil {
		t.Fatalf("failed to create split device: %v", err)
	}

	// Read from device
	buf := make([]byte, SectorSize*2)
	n, err := sd.ReadSectors(0, buf, 2)
	if err != nil {
		t.Fatalf("ReadSectors failed: %v", err)
	}
	if n != 2*SectorSize {
		t.Errorf("ReadSectors returned %d bytes, want %d", n, 2*SectorSize)
	}

	// Verify data
	for i := 0; i < 2*SectorSize; i++ {
		if buf[i] != byte(i%256) {
			t.Errorf("buf[%d] = %d, want %d", i, buf[i], byte(i%256))
			break
		}
	}
}

func TestSplitBlockDeviceReadSparseRegion(t *testing.T) {
	chunk, _ := NewMemoryBlockDevice(SectorSize * 10)
	for i := range chunk.data {
		chunk.data[i] = 0xFF
	}

	// Device with sparse region at start
	sd, err := NewSplitBlockDevice(SplitBlockDeviceConfig{
		TotalSectors: 20,
		Chunks: []Chunk{
			{StartSector: 10, SectorCount: 10, Device: chunk},
		},
	})
	if err != nil {
		t.Fatalf("failed to create split device: %v", err)
	}

	// Read from sparse region (should be zeros)
	buf := make([]byte, SectorSize)
	n, err := sd.ReadSectors(0, buf, 1)
	if err != nil {
		t.Fatalf("ReadSectors failed: %v", err)
	}
	if n != SectorSize {
		t.Errorf("ReadSectors returned %d bytes, want %d", n, SectorSize)
	}

	// Verify all zeros
	for i, b := range buf {
		if b != 0 {
			t.Errorf("sparse region buf[%d] = %d, want 0", i, b)
			break
		}
	}

	// Read from backed region (should be 0xFF)
	n, err = sd.ReadSectors(10, buf, 1)
	if err != nil {
		t.Fatalf("ReadSectors failed: %v", err)
	}
	for i, b := range buf {
		if b != 0xFF {
			t.Errorf("backed region buf[%d] = %d, want 0xFF", i, b)
			break
		}
	}
}

func TestSplitBlockDeviceReadAcrossChunks(t *testing.T) {
	// Create two chunks with distinct data
	chunk1, _ := NewMemoryBlockDevice(SectorSize * 5)
	for i := range chunk1.data {
		chunk1.data[i] = 0xAA
	}
	chunk2, _ := NewMemoryBlockDevice(SectorSize * 5)
	for i := range chunk2.data {
		chunk2.data[i] = 0xBB
	}

	// Adjacent chunks
	sd, err := NewSplitBlockDevice(SplitBlockDeviceConfig{
		TotalSectors: 10,
		Chunks: []Chunk{
			{StartSector: 0, SectorCount: 5, Device: chunk1},
			{StartSector: 5, SectorCount: 5, Device: chunk2},
		},
	})
	if err != nil {
		t.Fatalf("failed to create split device: %v", err)
	}

	// Read across chunk boundary (sectors 4-5)
	buf := make([]byte, SectorSize*2)
	n, err := sd.ReadSectors(4, buf, 2)
	if err != nil {
		t.Fatalf("ReadSectors failed: %v", err)
	}
	if n != 2*SectorSize {
		t.Errorf("ReadSectors returned %d bytes, want %d", n, 2*SectorSize)
	}

	// First sector should be 0xAA (from chunk1)
	for i := 0; i < SectorSize; i++ {
		if buf[i] != 0xAA {
			t.Errorf("buf[%d] = 0x%02X, want 0xAA", i, buf[i])
			break
		}
	}
	// Second sector should be 0xBB (from chunk2)
	for i := SectorSize; i < 2*SectorSize; i++ {
		if buf[i] != 0xBB {
			t.Errorf("buf[%d] = 0x%02X, want 0xBB", i, buf[i])
			break
		}
	}
}

func TestSplitBlockDeviceReadAcrossSparseAndBacked(t *testing.T) {
	chunk, _ := NewMemoryBlockDevice(SectorSize * 5)
	for i := range chunk.data {
		chunk.data[i] = 0xCC
	}

	// Sparse region followed by backed region
	sd, err := NewSplitBlockDevice(SplitBlockDeviceConfig{
		TotalSectors: 10,
		Chunks: []Chunk{
			{StartSector: 5, SectorCount: 5, Device: chunk},
		},
	})
	if err != nil {
		t.Fatalf("failed to create split device: %v", err)
	}

	// Read across sparse/backed boundary (sectors 4-6)
	buf := make([]byte, SectorSize*3)
	n, err := sd.ReadSectors(4, buf, 3)
	if err != nil {
		t.Fatalf("ReadSectors failed: %v", err)
	}
	if n != 3*SectorSize {
		t.Errorf("ReadSectors returned %d bytes, want %d", n, 3*SectorSize)
	}

	// First sector (4) should be zeros (sparse)
	for i := 0; i < SectorSize; i++ {
		if buf[i] != 0 {
			t.Errorf("sparse buf[%d] = 0x%02X, want 0x00", i, buf[i])
			break
		}
	}
	// Sectors 5-6 should be 0xCC (backed)
	for i := SectorSize; i < 3*SectorSize; i++ {
		if buf[i] != 0xCC {
			t.Errorf("backed buf[%d] = 0x%02X, want 0xCC", i, buf[i])
			break
		}
	}
}

func TestSplitBlockDeviceWriteSingleChunk(t *testing.T) {
	chunk, _ := NewMemoryBlockDevice(SectorSize * 10)

	sd, err := NewSplitBlockDevice(SplitBlockDeviceConfig{
		TotalSectors: 10,
		Mode:         ModeReadWrite,
		Chunks: []Chunk{
			{StartSector: 0, SectorCount: 10, Device: chunk},
		},
	})
	if err != nil {
		t.Fatalf("failed to create split device: %v", err)
	}

	// Write data
	buf := make([]byte, SectorSize)
	for i := range buf {
		buf[i] = 0xDD
	}
	n, err := sd.WriteSectors(5, buf, 1)
	if err != nil {
		t.Fatalf("WriteSectors failed: %v", err)
	}
	if n != SectorSize {
		t.Errorf("WriteSectors returned %d bytes, want %d", n, SectorSize)
	}

	// Verify via read
	readBuf := make([]byte, SectorSize)
	_, err = sd.ReadSectors(5, readBuf, 1)
	if err != nil {
		t.Fatalf("ReadSectors failed: %v", err)
	}
	for i, b := range readBuf {
		if b != 0xDD {
			t.Errorf("readBuf[%d] = 0x%02X, want 0xDD", i, b)
			break
		}
	}
}

func TestSplitBlockDeviceWriteAcrossChunks(t *testing.T) {
	chunk1, _ := NewMemoryBlockDevice(SectorSize * 5)
	chunk2, _ := NewMemoryBlockDevice(SectorSize * 5)

	sd, err := NewSplitBlockDevice(SplitBlockDeviceConfig{
		TotalSectors: 10,
		Mode:         ModeReadWrite,
		Chunks: []Chunk{
			{StartSector: 0, SectorCount: 5, Device: chunk1},
			{StartSector: 5, SectorCount: 5, Device: chunk2},
		},
	})
	if err != nil {
		t.Fatalf("failed to create split device: %v", err)
	}

	// Write across chunk boundary
	buf := make([]byte, SectorSize*2)
	for i := range buf {
		buf[i] = 0xEE
	}
	n, err := sd.WriteSectors(4, buf, 2)
	if err != nil {
		t.Fatalf("WriteSectors failed: %v", err)
	}
	if n != 2*SectorSize {
		t.Errorf("WriteSectors returned %d bytes, want %d", n, 2*SectorSize)
	}

	// Verify chunk1 sector 4
	for i := 4 * SectorSize; i < 5*SectorSize; i++ {
		if chunk1.data[i] != 0xEE {
			t.Errorf("chunk1.data[%d] = 0x%02X, want 0xEE", i, chunk1.data[i])
			break
		}
	}
	// Verify chunk2 sector 0
	for i := 0; i < SectorSize; i++ {
		if chunk2.data[i] != 0xEE {
			t.Errorf("chunk2.data[%d] = 0x%02X, want 0xEE", i, chunk2.data[i])
			break
		}
	}
}

func TestSplitBlockDeviceWriteToSparseRegionFails(t *testing.T) {
	chunk, _ := NewMemoryBlockDevice(SectorSize * 5)

	sd, err := NewSplitBlockDevice(SplitBlockDeviceConfig{
		TotalSectors: 10,
		Mode:         ModeReadWrite,
		Chunks: []Chunk{
			{StartSector: 5, SectorCount: 5, Device: chunk},
		},
	})
	if err != nil {
		t.Fatalf("failed to create split device: %v", err)
	}

	// Write to sparse region should fail
	buf := make([]byte, SectorSize)
	_, err = sd.WriteSectors(0, buf, 1)
	if err == nil {
		t.Error("WriteSectors to sparse region should fail")
	}
}

func TestSplitBlockDeviceReadOnlyMode(t *testing.T) {
	chunk, _ := NewMemoryBlockDevice(SectorSize * 10)

	sd, err := NewSplitBlockDevice(SplitBlockDeviceConfig{
		TotalSectors: 10,
		Mode:         ModeReadOnly,
		Chunks: []Chunk{
			{StartSector: 0, SectorCount: 10, Device: chunk},
		},
	})
	if err != nil {
		t.Fatalf("failed to create split device: %v", err)
	}

	// Write should fail
	buf := make([]byte, SectorSize)
	_, err = sd.WriteSectors(0, buf, 1)
	if err != ErrReadOnly {
		t.Errorf("WriteSectors on read-only device: error = %v, want ErrReadOnly", err)
	}
}

func TestSplitBlockDeviceOutOfRange(t *testing.T) {
	chunk, _ := NewMemoryBlockDevice(SectorSize * 10)

	sd, err := NewSplitBlockDevice(SplitBlockDeviceConfig{
		TotalSectors: 10,
		Chunks: []Chunk{
			{StartSector: 0, SectorCount: 10, Device: chunk},
		},
	})
	if err != nil {
		t.Fatalf("failed to create split device: %v", err)
	}

	buf := make([]byte, SectorSize)

	// Read past end
	_, err = sd.ReadSectors(10, buf, 1)
	if err != ErrSectorOutOfRange {
		t.Errorf("ReadSectors past end: error = %v, want ErrSectorOutOfRange", err)
	}

	// Write past end
	_, err = sd.WriteSectors(10, buf, 1)
	if err != ErrSectorOutOfRange {
		t.Errorf("WriteSectors past end: error = %v, want ErrSectorOutOfRange", err)
	}
}

func TestSplitBlockDeviceFlush(t *testing.T) {
	chunk, _ := NewMemoryBlockDevice(SectorSize * 10)

	sd, err := NewSplitBlockDevice(SplitBlockDeviceConfig{
		TotalSectors: 10,
		Chunks: []Chunk{
			{StartSector: 0, SectorCount: 10, Device: chunk},
		},
	})
	if err != nil {
		t.Fatalf("failed to create split device: %v", err)
	}

	// Flush should succeed
	if err := sd.Flush(); err != nil {
		t.Errorf("Flush() error = %v", err)
	}
}

func TestSplitBlockDeviceClose(t *testing.T) {
	chunk, _ := NewMemoryBlockDevice(SectorSize * 10)

	sd, err := NewSplitBlockDevice(SplitBlockDeviceConfig{
		TotalSectors: 10,
		Chunks: []Chunk{
			{StartSector: 0, SectorCount: 10, Device: chunk},
		},
	})
	if err != nil {
		t.Fatalf("failed to create split device: %v", err)
	}

	// Close should succeed
	if err := sd.Close(); err != nil {
		t.Errorf("Close() error = %v", err)
	}

	// Operations after close should fail
	buf := make([]byte, SectorSize)
	_, err = sd.ReadSectors(0, buf, 1)
	if err == nil {
		t.Error("ReadSectors after Close should fail")
	}
}

func TestSplitBlockDeviceChunkInfo(t *testing.T) {
	chunk1, _ := NewMemoryBlockDevice(SectorSize * 10)
	chunk2, _ := NewMemoryBlockDevice(SectorSize * 20)

	sd, err := NewSplitBlockDevice(SplitBlockDeviceConfig{
		TotalSectors: 50,
		Chunks: []Chunk{
			{StartSector: 5, SectorCount: 10, Device: chunk1},
			{StartSector: 30, SectorCount: 20, Device: chunk2},
		},
	})
	if err != nil {
		t.Fatalf("failed to create split device: %v", err)
	}

	if sd.ChunkCount() != 2 {
		t.Errorf("ChunkCount() = %d, want 2", sd.ChunkCount())
	}

	start, count, ok := sd.GetChunk(0)
	if !ok || start != 5 || count != 10 {
		t.Errorf("GetChunk(0) = %d, %d, %v; want 5, 10, true", start, count, ok)
	}

	start, count, ok = sd.GetChunk(1)
	if !ok || start != 30 || count != 20 {
		t.Errorf("GetChunk(1) = %d, %d, %v; want 30, 20, true", start, count, ok)
	}

	_, _, ok = sd.GetChunk(2)
	if ok {
		t.Error("GetChunk(2) should return ok=false")
	}
}

func TestSplitBlockDeviceChunksSorted(t *testing.T) {
	chunk1, _ := NewMemoryBlockDevice(SectorSize * 5)
	for i := range chunk1.data {
		chunk1.data[i] = 0x11
	}
	chunk2, _ := NewMemoryBlockDevice(SectorSize * 5)
	for i := range chunk2.data {
		chunk2.data[i] = 0x22
	}
	chunk3, _ := NewMemoryBlockDevice(SectorSize * 5)
	for i := range chunk3.data {
		chunk3.data[i] = 0x33
	}

	// Provide chunks in non-sorted order
	sd, err := NewSplitBlockDevice(SplitBlockDeviceConfig{
		TotalSectors: 15,
		Chunks: []Chunk{
			{StartSector: 10, SectorCount: 5, Device: chunk3},
			{StartSector: 0, SectorCount: 5, Device: chunk1},
			{StartSector: 5, SectorCount: 5, Device: chunk2},
		},
	})
	if err != nil {
		t.Fatalf("failed to create split device: %v", err)
	}

	// Read from each sector region and verify data
	buf := make([]byte, SectorSize)

	// Sector 0 should be 0x11
	sd.ReadSectors(0, buf, 1)
	if buf[0] != 0x11 {
		t.Errorf("sector 0: got 0x%02X, want 0x11", buf[0])
	}

	// Sector 5 should be 0x22
	sd.ReadSectors(5, buf, 1)
	if buf[0] != 0x22 {
		t.Errorf("sector 5: got 0x%02X, want 0x22", buf[0])
	}

	// Sector 10 should be 0x33
	sd.ReadSectors(10, buf, 1)
	if buf[0] != 0x33 {
		t.Errorf("sector 10: got 0x%02X, want 0x33", buf[0])
	}
}

func TestSplitBlockDeviceEmptyChunks(t *testing.T) {
	// Device with no chunks (fully sparse)
	sd, err := NewSplitBlockDevice(SplitBlockDeviceConfig{
		TotalSectors: 100,
		Mode:         ModeReadOnly,
		Chunks:       []Chunk{},
	})
	if err != nil {
		t.Fatalf("failed to create split device: %v", err)
	}

	// Read should return zeros
	buf := make([]byte, SectorSize)
	n, err := sd.ReadSectors(50, buf, 1)
	if err != nil {
		t.Fatalf("ReadSectors failed: %v", err)
	}
	if n != SectorSize {
		t.Errorf("ReadSectors returned %d bytes, want %d", n, SectorSize)
	}
	for i, b := range buf {
		if b != 0 {
			t.Errorf("buf[%d] = %d, want 0", i, b)
			break
		}
	}
}
