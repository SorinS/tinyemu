// Package devices provides device interfaces and implementations for the TinyEMU emulator.
package devices

import (
	"errors"
	"io"
)

// Standard sector size for block devices.
// Reference: tinyemu-2019-12-21/temu.c:215
const SectorSize = 512

// BlockDevice is the interface for block storage devices.
// All operations use sector-based addressing where each sector is 512 bytes.
type BlockDevice interface {
	// GetSectorCount returns the total number of sectors on the device.
	GetSectorCount() int64

	// ReadSectors reads n sectors starting at sector_num into buf.
	// buf must be at least n * SectorSize bytes.
	// Returns the number of bytes read and any error.
	ReadSectors(sectorNum uint64, buf []byte, n int) (int, error)

	// WriteSectors writes n sectors starting at sector_num from buf.
	// buf must be at least n * SectorSize bytes.
	// Returns the number of bytes written and any error.
	WriteSectors(sectorNum uint64, buf []byte, n int) (int, error)

	// Flush ensures all pending writes are persisted.
	Flush() error

	// Close closes the block device and releases resources.
	Close() error
}

// Block device errors.
var (
	ErrSectorOutOfRange = errors.New("sector out of range")
	ErrBufferTooSmall   = errors.New("buffer too small")
	ErrReadOnly         = errors.New("device is read-only")
	ErrDeviceClosed     = errors.New("device is closed")
)

// BlockDeviceMode specifies the access mode for a block device.
// Reference: tinyemu-2019-12-21/temu.c:209-213 (BlockDeviceModeEnum)
type BlockDeviceMode int

const (
	// ModeReadWrite allows both reading and writing.
	// Reference: tinyemu-2019-12-21/temu.c:211 (BF_MODE_RW)
	ModeReadWrite BlockDeviceMode = iota
	// ModeReadOnly only allows reading.
	// Reference: tinyemu-2019-12-21/temu.c:210 (BF_MODE_RO)
	ModeReadOnly
	// ModeSnapshot allows writes but doesn't persist them.
	// Writes are stored in memory and reads check memory first.
	// Reference: tinyemu-2019-12-21/temu.c:212 (BF_MODE_SNAPSHOT)
	ModeSnapshot
)

// ReadAt implements io.ReaderAt for a BlockDevice.
func ReadAt(bd BlockDevice, buf []byte, off int64) (int, error) {
	if off < 0 {
		return 0, errors.New("negative offset")
	}

	// Calculate sector and offset within sector
	sectorNum := uint64(off / SectorSize)
	sectorOff := int(off % SectorSize)

	// Calculate how many sectors we need to read
	totalBytes := len(buf)
	if totalBytes == 0 {
		return 0, nil
	}

	// Check bounds
	sectorCount := bd.GetSectorCount()
	if int64(sectorNum) >= sectorCount {
		return 0, io.EOF
	}

	// Read sectors
	bytesRead := 0
	for bytesRead < totalBytes {
		if int64(sectorNum) >= sectorCount {
			return bytesRead, io.EOF
		}

		// Read one sector at a time to handle partial reads
		sectorBuf := make([]byte, SectorSize)
		_, err := bd.ReadSectors(sectorNum, sectorBuf, 1)
		if err != nil {
			return bytesRead, err
		}

		// Copy relevant portion to output buffer
		toCopy := SectorSize - sectorOff
		if toCopy > totalBytes-bytesRead {
			toCopy = totalBytes - bytesRead
		}
		copy(buf[bytesRead:], sectorBuf[sectorOff:sectorOff+toCopy])
		bytesRead += toCopy

		sectorNum++
		sectorOff = 0
	}

	return bytesRead, nil
}

// WriteAt implements io.WriterAt for a BlockDevice.
func WriteAt(bd BlockDevice, buf []byte, off int64) (int, error) {
	if off < 0 {
		return 0, errors.New("negative offset")
	}

	// Calculate sector and offset within sector
	sectorNum := uint64(off / SectorSize)
	sectorOff := int(off % SectorSize)

	// Calculate how many sectors we need to write
	totalBytes := len(buf)
	if totalBytes == 0 {
		return 0, nil
	}

	// Check bounds
	sectorCount := bd.GetSectorCount()
	if int64(sectorNum) >= sectorCount {
		return 0, ErrSectorOutOfRange
	}

	// Write sectors
	bytesWritten := 0
	for bytesWritten < totalBytes {
		if int64(sectorNum) >= sectorCount {
			return bytesWritten, ErrSectorOutOfRange
		}

		// Handle partial writes by reading-modify-writing
		sectorBuf := make([]byte, SectorSize)
		if sectorOff != 0 || totalBytes-bytesWritten < SectorSize {
			// Partial sector write - read first
			_, err := bd.ReadSectors(sectorNum, sectorBuf, 1)
			if err != nil {
				return bytesWritten, err
			}
		}

		// Copy data to sector buffer
		toCopy := SectorSize - sectorOff
		if toCopy > totalBytes-bytesWritten {
			toCopy = totalBytes - bytesWritten
		}
		copy(sectorBuf[sectorOff:], buf[bytesWritten:bytesWritten+toCopy])

		// Write sector
		_, err := bd.WriteSectors(sectorNum, sectorBuf, 1)
		if err != nil {
			return bytesWritten, err
		}
		bytesWritten += toCopy

		sectorNum++
		sectorOff = 0
	}

	return bytesWritten, nil
}

// BlockDeviceSize returns the size of the block device in bytes.
func BlockDeviceSize(bd BlockDevice) int64 {
	return bd.GetSectorCount() * SectorSize
}

// MemoryBlockDevice implements BlockDevice using an in-memory buffer.
// This is useful for testing.
type MemoryBlockDevice struct {
	data     []byte
	readOnly bool
}

// NewMemoryBlockDevice creates a new in-memory block device with the given size.
// Size must be a multiple of SectorSize.
func NewMemoryBlockDevice(sizeBytes int64) (*MemoryBlockDevice, error) {
	if sizeBytes%SectorSize != 0 {
		return nil, errors.New("size must be a multiple of sector size")
	}
	if sizeBytes <= 0 {
		return nil, errors.New("size must be positive")
	}
	return &MemoryBlockDevice{
		data: make([]byte, sizeBytes),
	}, nil
}

// NewMemoryBlockDeviceFromData creates a memory block device from existing data.
// The data is copied, and size is rounded up to sector boundary.
func NewMemoryBlockDeviceFromData(data []byte) *MemoryBlockDevice {
	// Round up to sector boundary
	size := (int64(len(data)) + SectorSize - 1) / SectorSize * SectorSize
	bd := &MemoryBlockDevice{
		data: make([]byte, size),
	}
	copy(bd.data, data)
	return bd
}

// SetReadOnly sets whether the device is read-only.
func (m *MemoryBlockDevice) SetReadOnly(readOnly bool) {
	m.readOnly = readOnly
}

// GetSectorCount returns the number of sectors.
func (m *MemoryBlockDevice) GetSectorCount() int64 {
	return int64(len(m.data)) / SectorSize
}

// ReadSectors reads sectors from the memory buffer.
func (m *MemoryBlockDevice) ReadSectors(sectorNum uint64, buf []byte, n int) (int, error) {
	if n <= 0 {
		return 0, nil
	}

	offset := int64(sectorNum) * SectorSize
	endOffset := offset + int64(n)*SectorSize

	if offset < 0 || endOffset > int64(len(m.data)) {
		return 0, ErrSectorOutOfRange
	}

	if len(buf) < n*SectorSize {
		return 0, ErrBufferTooSmall
	}

	copied := copy(buf[:n*SectorSize], m.data[offset:endOffset])
	return copied, nil
}

// WriteSectors writes sectors to the memory buffer.
func (m *MemoryBlockDevice) WriteSectors(sectorNum uint64, buf []byte, n int) (int, error) {
	if m.readOnly {
		return 0, ErrReadOnly
	}

	if n <= 0 {
		return 0, nil
	}

	offset := int64(sectorNum) * SectorSize
	endOffset := offset + int64(n)*SectorSize

	if offset < 0 || endOffset > int64(len(m.data)) {
		return 0, ErrSectorOutOfRange
	}

	if len(buf) < n*SectorSize {
		return 0, ErrBufferTooSmall
	}

	copied := copy(m.data[offset:endOffset], buf[:n*SectorSize])
	return copied, nil
}

// Flush is a no-op for memory devices.
func (m *MemoryBlockDevice) Flush() error {
	return nil
}

// Close is a no-op for memory devices.
func (m *MemoryBlockDevice) Close() error {
	return nil
}

// Data returns the underlying data buffer.
// This is useful for testing and inspection.
func (m *MemoryBlockDevice) Data() []byte {
	return m.data
}

// Verify MemoryBlockDevice implements BlockDevice
var _ BlockDevice = (*MemoryBlockDevice)(nil)
