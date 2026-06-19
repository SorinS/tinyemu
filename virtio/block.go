// Package virtio provides VirtIO device emulation.
// This file implements the VirtIO block device.
//
// Reference: TinyEMU virtio.c lines 991-1132

package virtio

import (
	"encoding/binary"

	"github.com/sorins/tinyemu-go/devices"
	"github.com/sorins/tinyemu-go/mem"
)

// VirtIO block request types
const (
	VirtIOBlkTIn       = 0 // Read request
	VirtIOBlkTOut      = 1 // Write request
	VirtIOBlkTFlush    = 4 // Flush request
	VirtIOBlkTFlushOut = 5 // Flush out request
)

// VirtIO block status codes
const (
	VirtIOBlkSOK     = 0 // Success
	VirtIOBlkSIOErr  = 1 // I/O error
	VirtIOBlkSUnsupp = 2 // Unsupported operation
)

// BlockDevice is a VirtIO block device that wraps a devices.BlockDevice.
type BlockDevice struct {
	dev *Device
	bs  devices.BlockDevice
}

// NewBlockDevice creates a new VirtIO block device backed by an MMIO
// transport. For PCI transport use NewBlockDeviceCore + your own
// PCI wiring.
func NewBlockDevice(memMap *mem.PhysMemoryMap, addr uint64, irq *mem.IRQSignal, bs devices.BlockDevice) (*BlockDevice, error) {
	bd := &BlockDevice{bs: bs}

	var err error
	bd.dev, err = NewDevice(memMap, addr, irq, DeviceIDBlock, 8, bd.recvRequest)
	if err != nil {
		return nil, err
	}
	binary.LittleEndian.PutUint64(bd.dev.ConfigSpace[:], uint64(bs.GetSectorCount()))
	return bd, nil
}

// NewBlockDeviceCore creates a VirtIO block device whose underlying
// Device is *not* registered to any transport. The caller is responsible
// for wiring the returned device's Device() to a transport (e.g. PCI
// via LegacyTransport). The block backend's request handler and config
// space (capacity) are set up here.
func NewBlockDeviceCore(memMap *mem.PhysMemoryMap, irq *mem.IRQSignal, bs devices.BlockDevice) *BlockDevice {
	bd := &BlockDevice{bs: bs}
	bd.dev = NewDeviceCore(memMap, irq, DeviceIDBlock, 8, bd.recvRequest)
	binary.LittleEndian.PutUint64(bd.dev.ConfigSpace[:], uint64(bs.GetSectorCount()))
	return bd
}

// Device returns the underlying VirtIO device.
func (bd *BlockDevice) Device() *Device {
	return bd.dev
}

// recvRequest handles incoming block device requests.
// Reference: virtio.c lines 1063-1115 (virtio_block_recv_request)
func (bd *BlockDevice) recvRequest(dev *Device, queueIdx int, descIdx int, readSize int, writeSize int) int {
	// Read request header (16 bytes)
	var header [16]byte
	if err := dev.MemcpyFromQueue(header[:], queueIdx, descIdx, 0, 16); err != nil {
		return 0
	}

	reqType := binary.LittleEndian.Uint32(header[0:4])
	// ioprio := binary.LittleEndian.Uint32(header[4:8])  // Unused
	sectorNum := binary.LittleEndian.Uint64(header[8:16])

	switch reqType {
	case VirtIOBlkTIn:
		bd.handleRead(dev, queueIdx, descIdx, sectorNum, writeSize)

	case VirtIOBlkTOut:
		bd.handleWrite(dev, queueIdx, descIdx, sectorNum, readSize)

	case VirtIOBlkTFlush, VirtIOBlkTFlushOut:
		bd.handleFlush(dev, queueIdx, descIdx)

	default:
		// Unsupported operation - return error status
		status := []byte{VirtIOBlkSUnsupp}
		dev.MemcpyToQueue(queueIdx, descIdx, 0, status, 1)
		dev.ConsumeDesc(queueIdx, descIdx, 1)
	}

	return 0
}

// handleRead handles a read request (VIRTIO_BLK_T_IN).
func (bd *BlockDevice) handleRead(dev *Device, queueIdx int, descIdx int, sectorNum uint64, writeSize int) {
	// writeSize includes the status byte at the end
	dataLen := writeSize - 1
	numSectors := dataLen / devices.SectorSize

	// Allocate buffer for data + status
	buf := make([]byte, writeSize)

	// Read from block device
	_, err := bd.bs.ReadSectors(sectorNum, buf[:dataLen], numSectors)
	if err != nil {
		buf[writeSize-1] = VirtIOBlkSIOErr
	} else {
		buf[writeSize-1] = VirtIOBlkSOK
	}

	// Copy data to guest
	dev.MemcpyToQueue(queueIdx, descIdx, 0, buf, writeSize)
	dev.ConsumeDesc(queueIdx, descIdx, writeSize)
}

// handleWrite handles a write request (VIRTIO_BLK_T_OUT).
func (bd *BlockDevice) handleWrite(dev *Device, queueIdx int, descIdx int, sectorNum uint64, readSize int) {
	// readSize includes the 16-byte header
	dataLen := readSize - 16
	numSectors := dataLen / devices.SectorSize

	// Read data from guest (after header)
	buf := make([]byte, dataLen)
	if err := dev.MemcpyFromQueue(buf, queueIdx, descIdx, 16, dataLen); err != nil {
		// Error reading from guest
		status := []byte{VirtIOBlkSIOErr}
		dev.MemcpyToQueue(queueIdx, descIdx, 0, status, 1)
		dev.ConsumeDesc(queueIdx, descIdx, 1)
		return
	}

	// Write to block device
	var status byte
	_, err := bd.bs.WriteSectors(sectorNum, buf, numSectors)
	if err != nil {
		status = VirtIOBlkSIOErr
	} else {
		status = VirtIOBlkSOK
	}

	// Send status to guest
	statusBuf := []byte{status}
	dev.MemcpyToQueue(queueIdx, descIdx, 0, statusBuf, 1)
	dev.ConsumeDesc(queueIdx, descIdx, 1)
}

// handleFlush handles a flush request (VIRTIO_BLK_T_FLUSH).
func (bd *BlockDevice) handleFlush(dev *Device, queueIdx int, descIdx int) {
	var status byte
	err := bd.bs.Flush()
	if err != nil {
		status = VirtIOBlkSIOErr
	} else {
		status = VirtIOBlkSOK
	}

	// Send status to guest
	statusBuf := []byte{status}
	dev.MemcpyToQueue(queueIdx, descIdx, 0, statusBuf, 1)
	dev.ConsumeDesc(queueIdx, descIdx, 1)
}

// GetCapacity returns the capacity of the block device in sectors.
func (bd *BlockDevice) GetCapacity() int64 {
	return bd.bs.GetSectorCount()
}

// Close closes the underlying block device.
func (bd *BlockDevice) Close() error {
	return bd.bs.Close()
}
