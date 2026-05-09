package devices

import (
	"fmt"
	"os"
	"sync"
)

// FileBlockDevice implements BlockDevice using a file as backing storage.
// It supports raw disk images accessed at the sector level.
//
// Reference: tinyemu-2019-12-21/temu.c:217-222 (BlockDeviceFile)
type FileBlockDevice struct {
	mu          sync.Mutex
	file        *os.File
	sectorCount int64
	mode        BlockDeviceMode
	closed      bool
}

// OpenFileBlockDevice opens a file as a block device.
// The mode parameter controls read/write access.
//
// Note: For ModeSnapshot, use SnapshotBlockDevice wrapping a read-only
// FileBlockDevice instead, to get proper copy-on-write semantics matching
// the C implementation.
//
// Reference: tinyemu-2019-12-21/temu.c:307-347 (block_device_init)
func OpenFileBlockDevice(path string, mode BlockDeviceMode) (*FileBlockDevice, error) {
	var flag int
	switch mode {
	case ModeReadWrite:
		flag = os.O_RDWR
	case ModeReadOnly, ModeSnapshot:
		flag = os.O_RDONLY
	default:
		return nil, fmt.Errorf("invalid mode: %d", mode)
	}

	f, err := os.OpenFile(path, flag, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to open block device: %w", err)
	}

	// Get file size
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("failed to stat block device: %w", err)
	}

	size := info.Size()
	if size%SectorSize != 0 {
		// Round down to sector boundary
		size = (size / SectorSize) * SectorSize
	}

	return &FileBlockDevice{
		file:        f,
		sectorCount: size / SectorSize,
		mode:        mode,
	}, nil
}

// CreateFileBlockDevice creates a new file block device with the given size.
// The file is created if it doesn't exist, or truncated if it does.
func CreateFileBlockDevice(path string, sizeBytes int64) (*FileBlockDevice, error) {
	if sizeBytes < SectorSize {
		return nil, fmt.Errorf("size must be at least %d bytes", SectorSize)
	}

	// Round down to sector boundary
	sizeBytes = (sizeBytes / SectorSize) * SectorSize

	f, err := os.Create(path)
	if err != nil {
		return nil, fmt.Errorf("failed to create block device: %w", err)
	}

	// Extend file to desired size
	if err := f.Truncate(sizeBytes); err != nil {
		f.Close()
		os.Remove(path)
		return nil, fmt.Errorf("failed to resize block device: %w", err)
	}

	return &FileBlockDevice{
		file:        f,
		sectorCount: sizeBytes / SectorSize,
		mode:        ModeReadWrite,
	}, nil
}

// GetSectorCount returns the number of sectors in the device.
// Reference: tinyemu-2019-12-21/temu.c:224-228 (bf_get_sector_count)
func (f *FileBlockDevice) GetSectorCount() int64 {
	return f.sectorCount
}

// ReadSectors reads n sectors starting at sectorNum.
// Reference: tinyemu-2019-12-21/temu.c:232-266 (bf_read_async)
func (f *FileBlockDevice) ReadSectors(sectorNum uint64, buf []byte, n int) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.closed {
		return 0, ErrDeviceClosed
	}

	if n <= 0 {
		return 0, nil
	}

	offset := int64(sectorNum) * SectorSize
	endOffset := offset + int64(n)*SectorSize

	if offset < 0 || endOffset > f.sectorCount*SectorSize {
		return 0, ErrSectorOutOfRange
	}

	if len(buf) < n*SectorSize {
		return 0, ErrBufferTooSmall
	}

	totalRead := 0
	for totalRead < n*SectorSize {
		bytesRead, err := f.file.ReadAt(buf[totalRead:n*SectorSize], offset+int64(totalRead))
		totalRead += bytesRead
		if err != nil {
			return totalRead, err
		}
	}

	return totalRead, nil
}

// WriteSectors writes n sectors starting at sectorNum.
// Reference: tinyemu-2019-12-21/temu.c:268-305 (bf_write_async)
func (f *FileBlockDevice) WriteSectors(sectorNum uint64, buf []byte, n int) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.closed {
		return 0, ErrDeviceClosed
	}

	if f.mode == ModeReadOnly {
		return 0, ErrReadOnly
	}

	if f.mode == ModeSnapshot {
		// In snapshot mode, writes are discarded
		return n * SectorSize, nil
	}

	if n <= 0 {
		return 0, nil
	}

	offset := int64(sectorNum) * SectorSize
	endOffset := offset + int64(n)*SectorSize

	if offset < 0 || endOffset > f.sectorCount*SectorSize {
		return 0, ErrSectorOutOfRange
	}

	if len(buf) < n*SectorSize {
		return 0, ErrBufferTooSmall
	}

	totalWritten := 0
	for totalWritten < n*SectorSize {
		bytesWritten, err := f.file.WriteAt(buf[totalWritten:n*SectorSize], offset+int64(totalWritten))
		totalWritten += bytesWritten
		if err != nil {
			return totalWritten, err
		}
	}

	return totalWritten, nil
}

// Flush syncs any pending writes to disk.
func (f *FileBlockDevice) Flush() error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.closed {
		return ErrDeviceClosed
	}

	if f.mode == ModeReadOnly || f.mode == ModeSnapshot {
		return nil
	}

	return f.file.Sync()
}

// Close closes the block device file.
func (f *FileBlockDevice) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.closed {
		return nil
	}

	f.closed = true
	return f.file.Close()
}

// Mode returns the access mode of the device.
func (f *FileBlockDevice) Mode() BlockDeviceMode {
	return f.mode
}

// Path returns the file path of the device.
func (f *FileBlockDevice) Path() string {
	return f.file.Name()
}

// Verify FileBlockDevice implements BlockDevice
var _ BlockDevice = (*FileBlockDevice)(nil)
