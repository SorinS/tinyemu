package devices

import (
	"sync"
)

// SnapshotBlockDevice wraps a read-only block device and provides
// copy-on-write semantics. Writes are stored in memory and reads
// check memory first before falling back to the underlying device.
//
// Reference: tinyemu-2019-12-21/temu.c:217-222 (BlockDeviceFile)
// Reference: tinyemu-2019-12-21/temu.c:248-265 (bf_read_async snapshot handling)
// Reference: tinyemu-2019-12-21/temu.c:284-298 (bf_write_async snapshot handling)
type SnapshotBlockDevice struct {
	mu          sync.Mutex
	backing     BlockDevice
	sectorTable map[uint64][]byte // Copy-on-write sector table
	closed      bool
}

// NewSnapshotBlockDevice creates a new snapshot block device wrapping the
// given backing device. The backing device is used for reads of unmodified
// sectors, while writes are stored in memory.
//
// Reference: tinyemu-2019-12-21/temu.c:337-340 (sector_table initialization)
func NewSnapshotBlockDevice(backing BlockDevice) *SnapshotBlockDevice {
	return &SnapshotBlockDevice{
		backing:     backing,
		sectorTable: make(map[uint64][]byte),
	}
}

// GetSectorCount returns the number of sectors in the device.
// Reference: tinyemu-2019-12-21/temu.c:224-228 (bf_get_sector_count)
func (s *SnapshotBlockDevice) GetSectorCount() int64 {
	return s.backing.GetSectorCount()
}

// ReadSectors reads n sectors starting at sectorNum.
// Modified sectors are read from memory, unmodified sectors from backing device.
//
// Reference: tinyemu-2019-12-21/temu.c:248-265 (bf_read_async BF_MODE_SNAPSHOT)
func (s *SnapshotBlockDevice) ReadSectors(sectorNum uint64, buf []byte, n int) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return 0, ErrDeviceClosed
	}

	if n <= 0 {
		return 0, nil
	}

	if len(buf) < n*SectorSize {
		return 0, ErrBufferTooSmall
	}

	// Check bounds
	if int64(sectorNum)+int64(n) > s.backing.GetSectorCount() {
		return 0, ErrSectorOutOfRange
	}

	// Read each sector, checking sector table first
	// Reference: tinyemu-2019-12-21/temu.c:250-259
	totalRead := 0
	for i := 0; i < n; i++ {
		sector := sectorNum + uint64(i)
		offset := i * SectorSize

		if data, ok := s.sectorTable[sector]; ok {
			// Sector has been modified, read from memory
			copy(buf[offset:offset+SectorSize], data)
		} else {
			// Read from backing device
			_, err := s.backing.ReadSectors(sector, buf[offset:offset+SectorSize], 1)
			if err != nil {
				return totalRead, err
			}
		}
		totalRead += SectorSize
	}

	return totalRead, nil
}

// WriteSectors writes n sectors starting at sectorNum.
// Data is stored in memory without modifying the backing device.
//
// Reference: tinyemu-2019-12-21/temu.c:284-298 (bf_write_async BF_MODE_SNAPSHOT)
func (s *SnapshotBlockDevice) WriteSectors(sectorNum uint64, buf []byte, n int) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return 0, ErrDeviceClosed
	}

	if n <= 0 {
		return 0, nil
	}

	if len(buf) < n*SectorSize {
		return 0, ErrBufferTooSmall
	}

	// Check bounds
	// Reference: tinyemu-2019-12-21/temu.c:287-288
	if int64(sectorNum)+int64(n) > s.backing.GetSectorCount() {
		return 0, ErrSectorOutOfRange
	}

	// Store each sector in the sector table
	// Reference: tinyemu-2019-12-21/temu.c:289-296
	totalWritten := 0
	for i := 0; i < n; i++ {
		sector := sectorNum + uint64(i)
		offset := i * SectorSize

		// Allocate sector if not already present
		if s.sectorTable[sector] == nil {
			s.sectorTable[sector] = make([]byte, SectorSize)
		}
		copy(s.sectorTable[sector], buf[offset:offset+SectorSize])
		totalWritten += SectorSize
	}

	return totalWritten, nil
}

// Flush is a no-op for snapshot devices since all writes are in memory.
func (s *SnapshotBlockDevice) Flush() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return ErrDeviceClosed
	}

	return nil
}

// Close closes the snapshot device and releases the sector table memory.
// The backing device is NOT closed (caller is responsible for that).
func (s *SnapshotBlockDevice) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return nil
	}

	s.closed = true
	s.sectorTable = nil
	return nil
}

// ModifiedSectorCount returns the number of sectors that have been modified.
// This is useful for testing and debugging.
func (s *SnapshotBlockDevice) ModifiedSectorCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.sectorTable)
}

// Verify SnapshotBlockDevice implements BlockDevice
var _ BlockDevice = (*SnapshotBlockDevice)(nil)
