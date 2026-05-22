// Package virtio provides VirtIO device emulation for the TinyEMU RISC-V emulator.
// This file implements the VirtIO MMIO (Memory-Mapped I/O) transport.
//
// Reference: TinyEMU virtio.c
package virtio

import (
	"encoding/binary"
	"fmt"
	"os"
	"sync"

	"github.com/jtolio/tinyemu-go/mem"
)

// MMIO register offsets - from the Linux kernel / VirtIO spec
// Reference: virtio.c lines 37-64
const (
	MMIOMagicValue        = 0x000 // Magic value "virt"
	MMIOVersion           = 0x004 // VirtIO version
	MMIODeviceID          = 0x008 // Device type ID
	MMIOVendorID          = 0x00c // Vendor ID
	MMIODeviceFeatures    = 0x010 // Device features
	MMIODeviceFeaturesSel = 0x014 // Device features selector
	MMIODriverFeatures    = 0x020 // Driver features (write-only)
	MMIODriverFeaturesSel = 0x024 // Driver features selector
	MMIOGuestPageSize     = 0x028 // Guest page size (version 1 only)
	MMIOQueueSel          = 0x030 // Queue selector
	MMIOQueueNumMax       = 0x034 // Maximum queue size
	MMIOQueueNum          = 0x038 // Queue size
	MMIOQueueAlign        = 0x03c // Queue alignment (version 1 only)
	MMIOQueuePFN          = 0x040 // Queue page frame number (version 1 only)
	MMIOQueueReady        = 0x044 // Queue ready flag
	MMIOQueueNotify       = 0x050 // Queue notification
	MMIOInterruptStatus   = 0x060 // Interrupt status
	MMIOInterruptAck      = 0x064 // Interrupt acknowledgment
	MMIOStatus            = 0x070 // Device status
	MMIOQueueDescLow      = 0x080 // Descriptor table address (low 32 bits)
	MMIOQueueDescHigh     = 0x084 // Descriptor table address (high 32 bits)
	MMIOQueueAvailLow     = 0x090 // Available ring address (low 32 bits)
	MMIOQueueAvailHigh    = 0x094 // Available ring address (high 32 bits)
	MMIOQueueUsedLow      = 0x0a0 // Used ring address (low 32 bits)
	MMIOQueueUsedHigh     = 0x0a4 // Used ring address (high 32 bits)
	MMIOConfigGeneration  = 0x0fc // Configuration generation
	MMIOConfig            = 0x100 // Device-specific configuration starts here
)

// VirtIO magic value "virt" in little-endian
// Reference: virtio.c line 617-618
const MMIOMagic = 0x74726976

// VirtIO version (we implement version 2 = modern virtio)
// Reference: virtio.c line 620-621
const MMIOVersionVal = 2

// VirtIO device IDs
// Reference: virtio.c lines 231-251
const (
	DeviceIDNet     = 1  // Network card
	DeviceIDBlock   = 2  // Block device
	DeviceIDConsole = 3  // Console
	DeviceIDEntropy = 4  // Entropy source
	DeviceIDGPU     = 16 // GPU
	DeviceIDInput   = 18 // Input device
	DeviceID9P      = 9  // 9P filesystem
)

// Queue constants
// Reference: virtio.c lines 94-96
const (
	MaxQueue      = 8   // Maximum number of queues per device
	MaxConfigSize = 256 // Maximum configuration space size
	// MaxQueueNum: Linux's virtio_net needs num_free >= 2+MAX_SKB_FRAGS
	// (= 19 on x86_64) to re-wake netif_tx_subqueue after each xmit;
	// with anything below that threshold the TX queue stops forever
	// after the first packet. 256 is the standard ring size everyone
	// else uses and gives generous headroom. Must be a power of two so
	// the (idx & (Num-1)) ring-wrap mask works.
	MaxQueueNum = 256
)

// VirtIO descriptor flags
// Reference: virtio.c lines 108-110
const (
	VRingDescFNext     = 1 // Descriptor continues in next
	VRingDescFWrite    = 2 // Device writes (vs reads)
	VRingDescFIndirect = 4 // Buffer contains indirect descriptors
)

// PageSize is the VirtIO page size (4KB)
const PageSize = 4096

// QueueState represents the state of a single virtqueue.
// Reference: virtio.c lines 98-106
type QueueState struct {
	Ready        uint32 // 0 or 1 - queue is ready for use
	Num          uint32 // Queue size (number of descriptors)
	LastAvailIdx uint16 // Last available index processed
	DescAddr     uint64 // Descriptor table physical address
	AvailAddr    uint64 // Available ring physical address
	UsedAddr     uint64 // Used ring physical address
	ManualRecv   bool   // If true, device_recv callback is not called automatically
}

// Desc represents a VirtIO descriptor.
// Reference: virtio.c lines 112-117
type Desc struct {
	Addr  uint64 // Physical address of buffer
	Len   uint32 // Length of buffer
	Flags uint16 // VRING_DESC_F_* flags
	Next  uint16 // Next descriptor index (if NEXT flag set)
}

// DeviceRecvFunc is called when a descriptor chain is available.
// Returns < 0 to stop notification (must be manually restarted), 0 if OK.
// Reference: virtio.c lines 119-123
type DeviceRecvFunc func(dev *Device, queueIdx int, descIdx int, readSize int, writeSize int) int

// ConfigWriteFunc is called when configuration space is written.
type ConfigWriteFunc func(dev *Device)

// Device represents a VirtIO device using MMIO transport.
// Reference: virtio.c lines 128-153
type Device struct {
	mu sync.Mutex

	// Memory and interrupt
	MemMap   *mem.PhysMemoryMap
	MemRange *mem.PhysMemoryRange
	IRQ      *mem.IRQSignal

	// Device state
	IntStatus         uint32               // Interrupt status bits
	Status            uint32               // Device status
	DeviceFeaturesSel uint32               // Features selector (0 or 1)
	GuestFeaturesSel  uint32               // Driver-side features selector (0 or 1)
	QueueSel          uint32               // Currently selected queue
	Queues            [MaxQueue]QueueState // Queue states

	// Device identification
	DeviceID uint32 // Device type ID
	VendorID uint32 // Vendor ID (0xffff = unassigned)

	// Feature negotiation. `Features` (low 32 bits) and `FeaturesHi`
	// (bits 32-63) are the device-OFFERED features — the bits this
	// device implements. The driver reads these via the transport's
	// host-features register. The driver then writes back the subset
	// it wants to use via the guest-features register; we record that
	// subset in `GuestFeatures` / `GuestFeaturesHi`. The negotiated
	// set is `Features & GuestFeatures` — and transports MUST gate
	// behavior on the negotiated set, not the offered set, so that
	// drivers and devices never disagree about what's active.
	//
	// Legacy virtio-pci can only expose the low 32 bits; modern and
	// MMIO support both halves via the *Sel registers.
	Features         uint32
	FeaturesHi       uint32
	GuestFeatures    uint32
	GuestFeaturesHi  uint32

	// Callbacks
	DeviceRecv  DeviceRecvFunc  // Called when descriptors are available
	ConfigWrite ConfigWriteFunc // Called when config space is written

	// Configuration space
	ConfigSpaceSize uint32              // Size of config space (bytes, multiple of 4)
	ConfigSpace     [MaxConfigSize]byte // Device-specific configuration

	// Debug
	Debug int
}

// DebugFlags for debugging VirtIO
const (
	DebugIO = 1 << 0
	Debug9P = 1 << 1
)

// NewDevice creates a new VirtIO MMIO device.
// Reference: virtio.c lines 220-296 (virtio_init)
func NewDevice(memMap *mem.PhysMemoryMap, addr uint64, irq *mem.IRQSignal,
	deviceID uint32, configSpaceSize uint32, recvFunc DeviceRecvFunc) (*Device, error) {

	dev := NewDeviceCore(memMap, irq, deviceID, configSpaceSize, recvFunc)

	// Register MMIO region
	var err error
	dev.MemRange, err = memMap.RegisterDevice(
		addr, PageSize, dev,
		dev.read, dev.write,
		mem.DevIOSize8|mem.DevIOSize16|mem.DevIOSize32,
	)
	if err != nil {
		return nil, err
	}

	return dev, nil
}

// NewDeviceCore creates a VirtIO Device without registering an MMIO
// region. Use this when wiring the device to a non-MMIO transport
// (e.g. PCI legacy I/O ports via virtio/pci.go).
func NewDeviceCore(memMap *mem.PhysMemoryMap, irq *mem.IRQSignal,
	deviceID uint32, configSpaceSize uint32, recvFunc DeviceRecvFunc) *Device {

	dev := &Device{
		MemMap:          memMap,
		IRQ:             irq,
		DeviceID:        deviceID,
		VendorID:        0xffff, // Default vendor ID
		ConfigSpaceSize: configSpaceSize,
		DeviceRecv:      recvFunc,
	}
	dev.Reset()
	return dev
}

// Reset resets the device to initial state.
// Reference: virtio.c lines 162-179 (virtio_reset)
func (dev *Device) Reset() {
	dev.Status = 0
	dev.QueueSel = 0
	dev.DeviceFeaturesSel = 0
	dev.IntStatus = 0

	dev.GuestFeatures = 0
	dev.GuestFeaturesHi = 0
	dev.GuestFeaturesSel = 0

	for i := range dev.Queues {
		qs := &dev.Queues[i]
		qs.Ready = 0
		qs.Num = MaxQueueNum
		qs.DescAddr = 0
		qs.AvailAddr = 0
		qs.UsedAddr = 0
		qs.LastAvailIdx = 0
	}
}

// SetFeatures sets the device-OFFERED features (low 32 bits). Use
// SetFeaturesHi for bits 32-63. Both are bits the device knows how to
// implement; the driver picks a subset via the guest-features register.
func (dev *Device) SetFeatures(features uint32) {
	dev.mu.Lock()
	defer dev.mu.Unlock()
	dev.Features = features
}

// SetFeaturesHi sets the device-offered features (bits 32-63).
func (dev *Device) SetFeaturesHi(features uint32) {
	dev.mu.Lock()
	defer dev.mu.Unlock()
	dev.FeaturesHi = features
}

// NegotiatedFeatures returns the 64-bit set of features both the device
// and driver agreed to use. Transports and device backends MUST gate
// optional behavior on this — never on the offered set alone.
func (dev *Device) NegotiatedFeatures() uint64 {
	dev.mu.Lock()
	defer dev.mu.Unlock()
	lo := uint64(dev.Features & dev.GuestFeatures)
	hi := uint64(dev.FeaturesHi & dev.GuestFeaturesHi)
	return lo | hi<<32
}

// SetDebug sets debug flags.
func (dev *Device) SetDebug(flags int) {
	dev.mu.Lock()
	defer dev.mu.Unlock()
	dev.Debug = flags
}

// read handles MMIO register reads.
// Reference: virtio.c lines 606-700 (virtio_mmio_read)
// DebugMMIO enables unconditional MMIO debug output
var DebugMMIO bool

func (dev *Device) read(opaque any, offset uint32, sizeLog2 int) uint32 {
	dev.mu.Lock()
	defer dev.mu.Unlock()

	// Configuration space access
	if offset >= MMIOConfig {
		return dev.configRead(offset-MMIOConfig, sizeLog2)
	}

	// Only 32-bit reads are supported for most registers
	if sizeLog2 != 2 {
		return 0
	}

	var val uint32
	switch offset {
	case MMIOMagicValue:
		val = MMIOMagic

	case MMIOVersion:
		val = MMIOVersionVal

	case MMIODeviceID:
		val = dev.DeviceID

	case MMIOVendorID:
		val = dev.VendorID

	case MMIODeviceFeatures:
		// Return offered features for the selected half. MMIO version 2
		// expects bit 32 (VIRTIO_F_VERSION_1) to be set in the high
		// half; we OR that in unconditionally so the driver can complete
		// the modern handshake. Other high bits (e.g. STANDBY at 62)
		// are exposed only if the device backend explicitly set them
		// via FeaturesHi.
		switch dev.DeviceFeaturesSel {
		case 0:
			val = dev.Features
		case 1:
			val = dev.FeaturesHi | (1 << 0) // VIRTIO_F_VERSION_1 (bit 32 == hi bit 0)
		default:
			val = 0
		}

	case MMIODeviceFeaturesSel:
		val = dev.DeviceFeaturesSel

	case MMIOQueueSel:
		val = dev.QueueSel

	case MMIOQueueNumMax:
		val = MaxQueueNum

	case MMIOQueueNum:
		val = dev.Queues[dev.QueueSel].Num

	case MMIOQueueDescLow:
		val = uint32(dev.Queues[dev.QueueSel].DescAddr)

	case MMIOQueueAvailLow:
		val = uint32(dev.Queues[dev.QueueSel].AvailAddr)

	case MMIOQueueUsedLow:
		val = uint32(dev.Queues[dev.QueueSel].UsedAddr)

	case MMIOQueueDescHigh:
		val = uint32(dev.Queues[dev.QueueSel].DescAddr >> 32)

	case MMIOQueueAvailHigh:
		val = uint32(dev.Queues[dev.QueueSel].AvailAddr >> 32)

	case MMIOQueueUsedHigh:
		val = uint32(dev.Queues[dev.QueueSel].UsedAddr >> 32)

	case MMIOQueueReady:
		val = dev.Queues[dev.QueueSel].Ready

	case MMIOInterruptStatus:
		val = dev.IntStatus

	case MMIOStatus:
		val = dev.Status

	case MMIOConfigGeneration:
		val = 0 // Configuration has not changed

	default:
		val = 0
	}

	return val
}

// write handles MMIO register writes.
// Reference: virtio.c lines 719-792 (virtio_mmio_write)
func (dev *Device) write(opaque any, offset uint32, val uint32, sizeLog2 int) {
	dev.mu.Lock()
	defer dev.mu.Unlock()

	// Configuration space access
	if offset >= MMIOConfig {
		dev.configWrite(offset-MMIOConfig, val, sizeLog2)
		return
	}

	// Only 32-bit writes are supported for most registers
	if sizeLog2 != 2 {
		return
	}

	switch offset {
	case MMIODeviceFeaturesSel:
		dev.DeviceFeaturesSel = val

	case MMIODriverFeatures:
		// Driver acks features it wants to use. Mask against what we
		// actually offer — drivers MUST NOT enable bits we don't
		// advertise (per virtio spec). We still record the masked
		// value so reads can echo it back.
		switch dev.GuestFeaturesSel {
		case 0:
			dev.GuestFeatures = val & dev.Features
		case 1:
			dev.GuestFeaturesHi = val & dev.FeaturesHi
		}

	case MMIODriverFeaturesSel:
		dev.GuestFeaturesSel = val

	case MMIOQueueSel:
		if val < MaxQueue {
			dev.QueueSel = val
		}

	case MMIOQueueNum:
		// Queue size must be a power of 2 and > 0
		if isPowerOfTwo(val) && val > 0 {
			dev.Queues[dev.QueueSel].Num = val
		}

	case MMIOQueueDescLow:
		setLow32(&dev.Queues[dev.QueueSel].DescAddr, val)

	case MMIOQueueAvailLow:
		setLow32(&dev.Queues[dev.QueueSel].AvailAddr, val)

	case MMIOQueueUsedLow:
		setLow32(&dev.Queues[dev.QueueSel].UsedAddr, val)

	case MMIOQueueDescHigh:
		setHigh32(&dev.Queues[dev.QueueSel].DescAddr, val)

	case MMIOQueueAvailHigh:
		setHigh32(&dev.Queues[dev.QueueSel].AvailAddr, val)

	case MMIOQueueUsedHigh:
		setHigh32(&dev.Queues[dev.QueueSel].UsedAddr, val)

	case MMIOStatus:
		dev.Status = val
		if val == 0 {
			// Reset device
			if dev.IRQ != nil {
				dev.IRQ.Lower()
			}
			dev.Reset()
		}

	case MMIOQueueReady:
		dev.Queues[dev.QueueSel].Ready = val & 1
		// When queue becomes ready, ensure used ring flags are 0 (no notification suppression)
		if val&1 != 0 && dev.Queues[dev.QueueSel].UsedAddr != 0 {
			dev.write16(dev.Queues[dev.QueueSel].UsedAddr, 0)
		}

	case MMIOQueueNotify:
		if val < MaxQueue {
			dev.queueNotify(val)
		}

	case MMIOInterruptAck:
		dev.IntStatus &= ^val
		if dev.IntStatus == 0 && dev.IRQ != nil {
			dev.IRQ.Lower()
		}
	}
}

// configRead reads from device configuration space.
// Reference: tinyemu-2019-12-21/virtio.c:546-576 (virtio_config_read)
func (dev *Device) configRead(offset uint32, sizeLog2 int) uint32 {
	switch sizeLog2 {
	case 0: // 1 byte
		if offset < dev.ConfigSpaceSize {
			return uint32(dev.ConfigSpace[offset])
		}
	case 1: // 2 bytes
		if offset < dev.ConfigSpaceSize-1 {
			return uint32(binary.LittleEndian.Uint16(dev.ConfigSpace[offset:]))
		}
	case 2: // 4 bytes
		if offset < dev.ConfigSpaceSize-3 {
			return binary.LittleEndian.Uint32(dev.ConfigSpace[offset:])
		}
	}
	return 0
}

// configWrite writes to device configuration space.
// Reference: tinyemu-2019-12-21/virtio.c:578-604 (virtio_config_write)
func (dev *Device) configWrite(offset uint32, val uint32, sizeLog2 int) {
	switch sizeLog2 {
	case 0: // 1 byte
		if offset < dev.ConfigSpaceSize {
			dev.ConfigSpace[offset] = byte(val)
			if dev.ConfigWrite != nil {
				dev.ConfigWrite(dev)
			}
		}
	case 1: // 2 bytes
		if offset < dev.ConfigSpaceSize-1 {
			binary.LittleEndian.PutUint16(dev.ConfigSpace[offset:], uint16(val))
			if dev.ConfigWrite != nil {
				dev.ConfigWrite(dev)
			}
		}
	case 2: // 4 bytes
		if offset < dev.ConfigSpaceSize-3 {
			binary.LittleEndian.PutUint32(dev.ConfigSpace[offset:], val)
			if dev.ConfigWrite != nil {
				dev.ConfigWrite(dev)
			}
		}
	}
}

// queueNotify handles queue notification from the driver.
// Reference: tinyemu-2019-12-21/virtio.c:518-544 (queue_notify)
// Note: This function is called with the lock held, but releases it before
// calling the device callback to avoid deadlock.
func (dev *Device) queueNotify(queueIdx uint32) {
	qs := &dev.Queues[queueIdx]

	if qs.ManualRecv {
		return
	}

	// Read available index from guest memory
	availIdx := dev.read16(qs.AvailAddr + 2)

	for qs.LastAvailIdx != availIdx {
		// Get descriptor index from available ring
		descIdx := dev.read16(qs.AvailAddr + 4 + uint64(qs.LastAvailIdx&uint16(qs.Num-1))*2)

		// Calculate read and write sizes
		readSize, writeSize, ok := dev.getDescRWSize(int(queueIdx), int(descIdx))
		if ok {
			if dev.DeviceRecv != nil {
				// Release lock before calling callback to avoid deadlock
				dev.mu.Unlock()
				result := dev.DeviceRecv(dev, int(queueIdx), int(descIdx), readSize, writeSize)
				dev.mu.Lock()
				if result < 0 {
					break
				}
			}
		}
		qs.LastAvailIdx++
	}
}

// getDescRWSize calculates the total read and write sizes for a descriptor chain.
// Reference: tinyemu-2019-12-21/virtio.c:480-515 (get_desc_rw_size)
func (dev *Device) getDescRWSize(queueIdx int, descIdx int) (readSize, writeSize int, ok bool) {
	desc, err := dev.getDesc(queueIdx, descIdx)
	if err != nil {
		return 0, 0, false
	}

	// First, count all read descriptors (before any write descriptor)
	for {
		if desc.Flags&VRingDescFWrite != 0 {
			break
		}
		readSize += int(desc.Len)
		if desc.Flags&VRingDescFNext == 0 {
			return readSize, writeSize, true
		}
		descIdx = int(desc.Next)
		desc, err = dev.getDesc(queueIdx, descIdx)
		if err != nil {
			return 0, 0, false
		}
	}

	// Then, count all write descriptors
	for {
		if desc.Flags&VRingDescFWrite == 0 {
			return 0, 0, false // Read descriptor after write is invalid
		}
		writeSize += int(desc.Len)
		if desc.Flags&VRingDescFNext == 0 {
			break
		}
		descIdx = int(desc.Next)
		desc, err = dev.getDesc(queueIdx, descIdx)
		if err != nil {
			return 0, 0, false
		}
	}

	return readSize, writeSize, true
}

// getDesc reads a descriptor from the descriptor table.
// Reference: tinyemu-2019-12-21/virtio.c:371-378 (get_desc)
func (dev *Device) getDesc(queueIdx int, descIdx int) (Desc, error) {
	qs := &dev.Queues[queueIdx]
	descAddr := qs.DescAddr + uint64(descIdx)*16 // sizeof(VIRTIODesc) = 16

	var desc Desc
	buf := make([]byte, 16)
	if err := dev.memcpyFromRAM(buf, descAddr, 16); err != nil {
		return desc, err
	}

	desc.Addr = binary.LittleEndian.Uint64(buf[0:])
	desc.Len = binary.LittleEndian.Uint32(buf[8:])
	desc.Flags = binary.LittleEndian.Uint16(buf[12:])
	desc.Next = binary.LittleEndian.Uint16(buf[14:])

	return desc, nil
}

// ConsumeDesc signals that a descriptor has been consumed.
// This adds the descriptor to the used ring and raises an interrupt.
// Reference: tinyemu-2019-12-21/virtio.c:461-478 (virtio_consume_desc)
func (dev *Device) ConsumeDesc(queueIdx int, descIdx int, descLen int) {
	dev.mu.Lock()
	defer dev.mu.Unlock()
	dev.consumeDescLocked(queueIdx, descIdx, descLen)
}

// DebugConsumeDesc enables debug logging for used ring writes.
var DebugConsumeDesc bool

func init() {
	if os.Getenv("TINYEMU_VIRTIO_CONSUME_DEBUG") == "1" {
		DebugConsumeDesc = true
	}
}

// consumeDescLocked is the internal implementation of ConsumeDesc.
// Caller must hold dev.mu.
// Reference: tinyemu-2019-12-21/virtio.c:461-478 (virtio_consume_desc)
func (dev *Device) consumeDescLocked(queueIdx int, descIdx int, descLen int) {
	qs := &dev.Queues[queueIdx]

	// Read current used index and increment it immediately (matching C behavior)
	// Reference: tinyemu-2019-12-21/virtio.c:468-470
	usedIdxAddr := qs.UsedAddr + 2
	usedIdx := dev.read16(usedIdxAddr)
	dev.write16(usedIdxAddr, usedIdx+1)

	// Write used ring entry
	// Reference: tinyemu-2019-12-21/virtio.c:472-474
	entryAddr := qs.UsedAddr + 4 + uint64(usedIdx&uint16(qs.Num-1))*8
	dev.write32(entryAddr, uint32(descIdx))
	dev.write32(entryAddr+4, uint32(descLen))

	if DebugConsumeDesc {
		tag := "id?"
		switch dev.DeviceID {
		case DeviceIDNet:
			tag = "net"
		case DeviceIDBlock:
			tag = "blk"
		case DeviceIDConsole:
			tag = "con"
		}
		fmt.Fprintf(os.Stderr, "[virtio-%s] consumeDesc queue=%d usedAddr=%x usedIdx=%d->%d entryAddr=%x descIdx=%d descLen=%d\n",
			tag, queueIdx, qs.UsedAddr, usedIdx, usedIdx+1, entryAddr, descIdx, descLen)
	}

	// Signal interrupt
	// Reference: tinyemu-2019-12-21/virtio.c:476-477
	dev.IntStatus |= 1
	if dev.IRQ != nil {
		dev.IRQ.Raise()
	}
}

// DMA write watchpoint (TINYEMU_X86_DMAWATCH=lo:hi hex). Logs every virtio
// memcpyToRAM call that overlaps the watch range — useful for catching DMA
// that bypasses CPU writePhys hooks.
var (
	dmaWatchActive bool
	dmaWatchLo     uint32
	dmaWatchHi     uint32
)

func init() {
	if s := os.Getenv("TINYEMU_X86_DMAWATCH"); s != "" {
		if _, err := fmt.Sscanf(s, "%x:%x", &dmaWatchLo, &dmaWatchHi); err == nil && dmaWatchHi > dmaWatchLo {
			dmaWatchActive = true
		}
	}
}

func dmaWatchHook(addr, count uint32, buf []byte) {
	// Overlap check: [addr, addr+count) vs [dmaWatchLo, dmaWatchHi).
	if addr >= dmaWatchHi || addr+count <= dmaWatchLo {
		return
	}
	// Show the bytes that land inside the watch range.
	start := dmaWatchLo
	if addr > dmaWatchLo {
		start = addr
	}
	end := dmaWatchHi
	if addr+count < dmaWatchHi {
		end = addr + count
	}
	off := start - addr
	n := end - start
	if n > 16 {
		n = 16
	}
	fmt.Fprintf(os.Stderr, "[dmawatch] virtio memcpyToRAM dst=0x%08X len=%d watched=[0x%08X..0x%08X) bytes=%X\n", addr, count, start, end, buf[off:off+n])
}

// MemcpyFromQueue copies data from a descriptor chain (guest to host).
// Reference: tinyemu-2019-12-21/virtio.c:444-450 (memcpy_from_queue)
func (dev *Device) MemcpyFromQueue(buf []byte, queueIdx int, descIdx int, offset int, count int) error {
	return dev.memcpyToFromQueue(buf, queueIdx, descIdx, offset, count, false)
}

// MemcpyToQueue copies data to a descriptor chain (host to guest).
// Reference: tinyemu-2019-12-21/virtio.c:452-458 (memcpy_to_queue)
func (dev *Device) MemcpyToQueue(queueIdx int, descIdx int, offset int, buf []byte, count int) error {
	return dev.memcpyToFromQueue(buf, queueIdx, descIdx, offset, count, true)
}

// memcpyToFromQueue copies data to/from a descriptor chain.
// Reference: tinyemu-2019-12-21/virtio.c:380-442 (memcpy_to_from_queue)
func (dev *Device) memcpyToFromQueue(buf []byte, queueIdx int, descIdx int, offset int, count int, toQueue bool) error {
	if count == 0 {
		return nil
	}

	desc, err := dev.getDesc(queueIdx, descIdx)
	if err != nil {
		return err
	}

	// For writes to queue, find the first write descriptor
	if toQueue {
		for {
			if desc.Flags&VRingDescFWrite != 0 {
				break
			}
			if desc.Flags&VRingDescFNext == 0 {
				return ErrDescChainEnd
			}
			descIdx = int(desc.Next)
			desc, err = dev.getDesc(queueIdx, descIdx)
			if err != nil {
				return err
			}
		}
	}

	// Skip to the offset
	for {
		writeFlag := desc.Flags & VRingDescFWrite
		var expectedFlag uint16
		if toQueue {
			expectedFlag = VRingDescFWrite
		}
		if writeFlag != expectedFlag {
			return ErrDescWrongType
		}
		if offset < int(desc.Len) {
			break
		}
		if desc.Flags&VRingDescFNext == 0 {
			return ErrDescChainEnd
		}
		descIdx = int(desc.Next)
		offset -= int(desc.Len)
		desc, err = dev.getDesc(queueIdx, descIdx)
		if err != nil {
			return err
		}
	}

	// Copy data
	// Reference: tinyemu-2019-12-21/virtio.c:420-440 (memcpy_to_from_queue copy loop)
	bufOffset := 0
	for {
		l := min(count, int(desc.Len)-offset)
		if toQueue {
			if err := dev.memcpyToRAM(desc.Addr+uint64(offset), buf[bufOffset:bufOffset+l], l); err != nil {
				return err
			}
		} else {
			if err := dev.memcpyFromRAM(buf[bufOffset:bufOffset+l], desc.Addr+uint64(offset), l); err != nil {
				return err
			}
		}
		count -= l
		// Match C behavior: check count == 0 BEFORE following to next descriptor
		// This is critical because the next descriptor may have a different type
		// (e.g., read desc followed by write desc in a block request)
		if count == 0 {
			break
		}
		offset += l
		bufOffset += l
		if offset == int(desc.Len) {
			if desc.Flags&VRingDescFNext == 0 {
				return ErrDescChainEnd
			}
			descIdx = int(desc.Next)
			desc, err = dev.getDesc(queueIdx, descIdx)
			if err != nil {
				return err
			}
			writeFlag := desc.Flags & VRingDescFWrite
			var expectedFlag uint16
			if toQueue {
				expectedFlag = VRingDescFWrite
			}
			if writeFlag != expectedFlag {
				return ErrDescWrongType
			}
			offset = 0
		}
	}

	return nil
}

// Memory access helpers

// read16 reads a 16-bit value from guest memory.
// Reference: tinyemu-2019-12-21/virtio.c:298-307 (virtio_read16)
func (dev *Device) read16(addr uint64) uint16 {
	if addr&1 != 0 {
		return 0 // Unaligned access not supported
	}
	ptr := dev.MemMap.GetRAMPtr(addr, false)
	if ptr == nil {
		return 0
	}
	return binary.LittleEndian.Uint16(ptr)
}

// write16 writes a 16-bit value to guest memory.
// Reference: tinyemu-2019-12-21/virtio.c:309-319 (virtio_write16)
func (dev *Device) write16(addr uint64, val uint16) {
	if addr&1 != 0 {
		return // Unaligned access not supported
	}
	if dmaWatchActive && uint32(addr) >= dmaWatchLo && uint32(addr) < dmaWatchHi {
		fmt.Fprintf(os.Stderr, "[dmawatch] virtio write16 phys=0x%08X val=0x%04X\n", uint32(addr), val)
	}
	ptr := dev.MemMap.GetRAMPtr(addr, true)
	if ptr == nil {
		return
	}
	binary.LittleEndian.PutUint16(ptr, val)
}

// write32 writes a 32-bit value to guest memory.
// Reference: tinyemu-2019-12-21/virtio.c:321-331 (virtio_write32)
func (dev *Device) write32(addr uint64, val uint32) {
	if addr&3 != 0 {
		return // Unaligned access not supported
	}
	if dmaWatchActive && uint32(addr) >= dmaWatchLo && uint32(addr) < dmaWatchHi {
		fmt.Fprintf(os.Stderr, "[dmawatch] virtio write32 phys=0x%08X val=0x%08X\n", uint32(addr), val)
	}
	ptr := dev.MemMap.GetRAMPtr(addr, true)
	if ptr == nil {
		return
	}
	binary.LittleEndian.PutUint32(ptr, val)
}

// memcpyFromRAM copies data from guest memory to a buffer.
// Reference: tinyemu-2019-12-21/virtio.c:333-350 (virtio_memcpy_from_ram)
func (dev *Device) memcpyFromRAM(buf []byte, addr uint64, count int) error {
	offset := 0
	for count > 0 {
		// Copy up to one page at a time
		pageOffset := addr & (PageSize - 1)
		l := min(count, PageSize-int(pageOffset))
		ptr := dev.MemMap.GetRAMPtr(addr, false)
		if ptr == nil {
			return ErrNoRAM
		}
		copy(buf[offset:offset+l], ptr[:l])
		addr += uint64(l)
		offset += l
		count -= l
	}
	return nil
}

// memcpyToRAM copies data from a buffer to guest memory.
// Reference: tinyemu-2019-12-21/virtio.c:352-369 (virtio_memcpy_to_ram)
func (dev *Device) memcpyToRAM(addr uint64, buf []byte, count int) error {
	if dmaWatchActive {
		dmaWatchHook(uint32(addr), uint32(count), buf)
	}
	offset := 0
	for count > 0 {
		// Copy up to one page at a time
		pageOffset := addr & (PageSize - 1)
		l := min(count, PageSize-int(pageOffset))
		ptr := dev.MemMap.GetRAMPtr(addr, true)
		if ptr == nil {
			return ErrNoRAM
		}
		copy(ptr[:l], buf[offset:offset+l])
		addr += uint64(l)
		offset += l
		count -= l
	}
	return nil
}

// Helper functions

// setLow32 sets the low 32 bits of a 64-bit address.
// Reference: tinyemu-2019-12-21/virtio.c:703-706 (set_low32)
func setLow32(addr *uint64, val uint32) {
	*addr = (*addr & 0xFFFFFFFF00000000) | uint64(val)
}

// setHigh32 sets the high 32 bits of a 64-bit address.
// Reference: tinyemu-2019-12-21/virtio.c:708-711 (set_high32)
func setHigh32(addr *uint64, val uint32) {
	*addr = (*addr & 0x00000000FFFFFFFF) | (uint64(val) << 32)
}

// isPowerOfTwo checks if n is a power of 2.
func isPowerOfTwo(n uint32) bool {
	return n&(n-1) == 0
}
