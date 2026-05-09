package devices

import (
	"errors"
	"sort"
	"sync"
)

// SplitBlockDevice implements BlockDevice by combining multiple underlying
// block devices, each covering a range of sectors. This is useful for:
// - HTTP serving where large disk images are split into smaller chunks
// - Sparse disk images where not all sectors are backed
// - Combining multiple physical files into one logical device
type SplitBlockDevice struct {
	mu           sync.RWMutex
	chunks       []Chunk
	totalSectors int64
	mode         BlockDeviceMode
	closed       bool
}

// Chunk represents a portion of the disk backed by a BlockDevice.
type Chunk struct {
	// StartSector is the first sector this chunk covers in the virtual device.
	StartSector int64
	// SectorCount is the number of sectors this chunk covers.
	SectorCount int64
	// Device is the backing block device for this chunk.
	// Sector 0 of Device maps to StartSector of the virtual device.
	Device BlockDevice
}

// SplitBlockDeviceConfig configures a SplitBlockDevice.
type SplitBlockDeviceConfig struct {
	// TotalSectors is the total size of the virtual device in sectors.
	// Sectors not covered by any chunk will read as zeros.
	TotalSectors int64
	// Mode specifies the access mode.
	Mode BlockDeviceMode
	// Chunks is the list of chunks that back the device.
	// Chunks must not overlap.
	Chunks []Chunk
}

// NewSplitBlockDevice creates a new split block device from the given configuration.
func NewSplitBlockDevice(config SplitBlockDeviceConfig) (*SplitBlockDevice, error) {
	if config.TotalSectors <= 0 {
		return nil, errors.New("total sectors must be positive")
	}

	// Make a copy of chunks and sort by start sector
	chunks := make([]Chunk, len(config.Chunks))
	copy(chunks, config.Chunks)
	sort.Slice(chunks, func(i, j int) bool {
		return chunks[i].StartSector < chunks[j].StartSector
	})

	// Validate chunks don't overlap and are within bounds
	for i, chunk := range chunks {
		if chunk.StartSector < 0 {
			return nil, errors.New("chunk start sector must be non-negative")
		}
		if chunk.SectorCount <= 0 {
			return nil, errors.New("chunk sector count must be positive")
		}
		if chunk.Device == nil {
			return nil, errors.New("chunk device must not be nil")
		}
		endSector := chunk.StartSector + chunk.SectorCount
		if endSector > config.TotalSectors {
			return nil, errors.New("chunk extends beyond total sectors")
		}
		// Check for overlap with next chunk
		if i+1 < len(chunks) {
			if endSector > chunks[i+1].StartSector {
				return nil, errors.New("chunks overlap")
			}
		}
	}

	return &SplitBlockDevice{
		chunks:       chunks,
		totalSectors: config.TotalSectors,
		mode:         config.Mode,
	}, nil
}

// GetSectorCount returns the total number of sectors.
func (s *SplitBlockDevice) GetSectorCount() int64 {
	return s.totalSectors
}

// findChunk returns the chunk containing the given sector, or nil if the sector
// is not backed by any chunk (sparse region).
func (s *SplitBlockDevice) findChunk(sector int64) *Chunk {
	// Binary search for the chunk
	idx := sort.Search(len(s.chunks), func(i int) bool {
		return s.chunks[i].StartSector+s.chunks[i].SectorCount > sector
	})
	if idx < len(s.chunks) {
		chunk := &s.chunks[idx]
		if sector >= chunk.StartSector && sector < chunk.StartSector+chunk.SectorCount {
			return chunk
		}
	}
	return nil
}

// ReadSectors reads sectors from the split device.
func (s *SplitBlockDevice) ReadSectors(sectorNum uint64, buf []byte, n int) (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return 0, errors.New("device is closed")
	}

	if n <= 0 {
		return 0, nil
	}

	sector := int64(sectorNum)
	endSector := sector + int64(n)

	if sector < 0 || endSector > s.totalSectors {
		return 0, ErrSectorOutOfRange
	}

	if len(buf) < n*SectorSize {
		return 0, ErrBufferTooSmall
	}

	bytesRead := 0
	currentSector := sector

	for currentSector < endSector {
		chunk := s.findChunk(currentSector)

		if chunk == nil {
			// Sparse region - find how many sectors until next chunk or end
			var sectorsToZero int64
			nextChunkIdx := sort.Search(len(s.chunks), func(i int) bool {
				return s.chunks[i].StartSector > currentSector
			})
			if nextChunkIdx < len(s.chunks) {
				sectorsToZero = s.chunks[nextChunkIdx].StartSector - currentSector
			} else {
				sectorsToZero = endSector - currentSector
			}
			if currentSector+sectorsToZero > endSector {
				sectorsToZero = endSector - currentSector
			}

			// Zero out the buffer for sparse region
			start := bytesRead
			end := bytesRead + int(sectorsToZero)*SectorSize
			for i := start; i < end; i++ {
				buf[i] = 0
			}
			bytesRead = end
			currentSector += sectorsToZero
		} else {
			// Read from chunk
			chunkOffset := currentSector - chunk.StartSector
			chunkRemaining := chunk.SectorCount - chunkOffset
			sectorsToRead := endSector - currentSector
			if sectorsToRead > chunkRemaining {
				sectorsToRead = chunkRemaining
			}

			_, err := chunk.Device.ReadSectors(uint64(chunkOffset), buf[bytesRead:], int(sectorsToRead))
			if err != nil {
				return bytesRead, err
			}

			bytesRead += int(sectorsToRead) * SectorSize
			currentSector += sectorsToRead
		}
	}

	return bytesRead, nil
}

// WriteSectors writes sectors to the split device.
func (s *SplitBlockDevice) WriteSectors(sectorNum uint64, buf []byte, n int) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return 0, errors.New("device is closed")
	}

	if s.mode == ModeReadOnly {
		return 0, ErrReadOnly
	}

	if n <= 0 {
		return 0, nil
	}

	sector := int64(sectorNum)
	endSector := sector + int64(n)

	if sector < 0 || endSector > s.totalSectors {
		return 0, ErrSectorOutOfRange
	}

	if len(buf) < n*SectorSize {
		return 0, ErrBufferTooSmall
	}

	bytesWritten := 0
	currentSector := sector

	for currentSector < endSector {
		chunk := s.findChunk(currentSector)

		if chunk == nil {
			// Sparse region - writes to sparse regions are not allowed
			// unless we have a backing store (future enhancement)
			return bytesWritten, errors.New("cannot write to sparse region")
		}

		// Write to chunk
		chunkOffset := currentSector - chunk.StartSector
		chunkRemaining := chunk.SectorCount - chunkOffset
		sectorsToWrite := endSector - currentSector
		if sectorsToWrite > chunkRemaining {
			sectorsToWrite = chunkRemaining
		}

		_, err := chunk.Device.WriteSectors(uint64(chunkOffset), buf[bytesWritten:], int(sectorsToWrite))
		if err != nil {
			return bytesWritten, err
		}

		bytesWritten += int(sectorsToWrite) * SectorSize
		currentSector += sectorsToWrite
	}

	return bytesWritten, nil
}

// Flush flushes all underlying chunk devices.
func (s *SplitBlockDevice) Flush() error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return errors.New("device is closed")
	}

	for _, chunk := range s.chunks {
		if err := chunk.Device.Flush(); err != nil {
			return err
		}
	}
	return nil
}

// Close closes all underlying chunk devices.
func (s *SplitBlockDevice) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return nil
	}
	s.closed = true

	var firstErr error
	for _, chunk := range s.chunks {
		if err := chunk.Device.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// ChunkCount returns the number of chunks.
func (s *SplitBlockDevice) ChunkCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.chunks)
}

// GetChunk returns information about a chunk by index.
func (s *SplitBlockDevice) GetChunk(idx int) (startSector, sectorCount int64, ok bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if idx < 0 || idx >= len(s.chunks) {
		return 0, 0, false
	}
	chunk := s.chunks[idx]
	return chunk.StartSector, chunk.SectorCount, true
}

// Verify SplitBlockDevice implements BlockDevice
var _ BlockDevice = (*SplitBlockDevice)(nil)
