package virtio

import (
	"fmt"
	"os"
)

var pciTransportDebug = os.Getenv("TINYEMU_VIRTIO_PCI_DEBUG") == "1"

// Legacy VirtIO PCI transport — implements the I/O-port register layout
// used by virtio 0.9.5 / "legacy" devices. This is the layout the
// `virtio_pci_legacy` driver in Linux's drivers/virtio/ binds to when a
// PCI device advertises a virtio vendor (0x1AF4) with device IDs in the
// 0x1000-0x103F range. Modern (virtio 1.0+) requires a more elaborate
// 4-capability layout in PCI config space; legacy is a single 32-byte
// I/O BAR plus device-specific config tacked on after that.
//
// Register layout when MSI-X is *disabled* (which is our case — we don't
// emulate MSI-X):
//
//   +0x00  uint32  host features            RO
//   +0x04  uint32  guest features           RW
//   +0x08  uint32  queue address (PFN)      RW  — phys addr = PFN << 12
//   +0x0C  uint16  queue size               RO  — number of descriptors
//   +0x0E  uint16  queue select             RW
//   +0x10  uint16  queue notify             RW  — write = kick queue N
//   +0x12  uint8   device status            RW
//   +0x13  uint8   ISR status               R/W1C — read clears it
//   +0x14  ...     device-specific config   RW
//
// References:
//   - Linux drivers/virtio/virtio_pci_legacy.c
//   - virtio-v1.0 spec, §4.1.4 ("legacy interface")

const (
	pciHostFeatures   = 0x00
	pciGuestFeatures  = 0x04
	pciQueuePFN       = 0x08
	pciQueueSize      = 0x0C
	pciQueueSelect    = 0x0E
	pciQueueNotify    = 0x10
	pciDeviceStatus   = 0x12
	pciISRStatus      = 0x13
	pciConfigOffset   = 0x14 // device-specific config begins here
	pciQueuePFNShift  = 12   // PFN → phys: << 12
	pciQueueAlignment = 4096 // legacy queue alignment is PAGE_SIZE
)

// LegacyTransport wraps a *Device and exposes the legacy virtio-pci I/O
// register layout. Register it into a PC board's IOPortDispatcher by
// calling IORead8/16/32 and IOWrite8/16/32 from the dispatcher handlers,
// passing the offset within the I/O BAR.
type LegacyTransport struct {
	dev *Device
}

// NewLegacyTransport adapts an existing Device to the legacy virtio-pci
// register layout. The Device must have been created via NewDeviceCore
// (i.e. no MMIO region registered).
func NewLegacyTransport(dev *Device) *LegacyTransport {
	return &LegacyTransport{dev: dev}
}

// IOSize returns the number of I/O ports occupied: 0x14 of registers plus
// the device-specific config space.
func (t *LegacyTransport) IOSize() uint32 {
	return pciConfigOffset + t.dev.ConfigSpaceSize
}

// IORead handles an I/O port read at byte offset `off` into the BAR for
// `size` bytes (1, 2, or 4).
func (t *LegacyTransport) IORead(off uint16, size int) uint32 {
	dev := t.dev
	dev.mu.Lock()
	defer dev.mu.Unlock()
	val := t.ioReadLocked(off, size)
	if pciTransportDebug {
		fmt.Fprintf(os.Stderr, "[vpci] R off=%02x sz=%d -> %x  status=%02x intStatus=%x\n",
			off, size, val, dev.Status, dev.IntStatus)
	}
	return val
}

func (t *LegacyTransport) ioReadLocked(off uint16, size int) uint32 {
	dev := t.dev

	// Device-specific config space.
	if uint32(off) >= pciConfigOffset {
		cfgOff := uint32(off) - pciConfigOffset
		switch size {
		case 1:
			if cfgOff < dev.ConfigSpaceSize {
				return uint32(dev.ConfigSpace[cfgOff])
			}
		case 2:
			if cfgOff+2 <= dev.ConfigSpaceSize {
				return uint32(dev.ConfigSpace[cfgOff]) |
					uint32(dev.ConfigSpace[cfgOff+1])<<8
			}
		case 4:
			if cfgOff+4 <= dev.ConfigSpaceSize {
				return uint32(dev.ConfigSpace[cfgOff]) |
					uint32(dev.ConfigSpace[cfgOff+1])<<8 |
					uint32(dev.ConfigSpace[cfgOff+2])<<16 |
					uint32(dev.ConfigSpace[cfgOff+3])<<24
			}
		}
		return 0
	}

	switch off {
	case pciHostFeatures:
		// Legacy virtio-pci only exposes the low 32 bits of the
		// feature mask; the device's high bits are unreachable from a
		// legacy driver (per virtio spec §4.1.4.2). This is fine: any
		// high-bit feature requires modern transport anyway.
		return dev.Features
	case pciGuestFeatures:
		// Driver read-back of its own ACK. We store the (already-
		// masked) value in GuestFeatures.
		return dev.GuestFeatures
	case pciQueuePFN:
		// PFN is desc_addr >> 12. Legacy assumes desc_addr is page-aligned.
		return uint32(dev.Queues[dev.QueueSel].DescAddr >> pciQueuePFNShift)
	case pciQueueSize:
		return MaxQueueNum
	case pciQueueSelect:
		return dev.QueueSel
	case pciQueueNotify:
		return 0
	case pciDeviceStatus:
		return dev.Status
	case pciISRStatus:
		// Read-clears: return current ISR, then zero it and lower the line.
		v := dev.IntStatus
		dev.IntStatus = 0
		if dev.IRQ != nil {
			dev.IRQ.Lower()
		}
		return v
	}
	return 0
}

// IOWrite handles an I/O port write at byte offset `off` into the BAR for
// `size` bytes (1, 2, or 4).
func (t *LegacyTransport) IOWrite(off uint16, val uint32, size int) {
	if pciTransportDebug {
		fmt.Fprintf(os.Stderr, "[vpci] W off=%02x sz=%d <- %x\n", off, size, val)
	}
	dev := t.dev
	dev.mu.Lock()
	defer dev.mu.Unlock()

	// Device-specific config space.
	if uint32(off) >= pciConfigOffset {
		cfgOff := uint32(off) - pciConfigOffset
		switch size {
		case 1:
			if cfgOff < dev.ConfigSpaceSize {
				dev.ConfigSpace[cfgOff] = uint8(val)
			}
		case 2:
			if cfgOff+2 <= dev.ConfigSpaceSize {
				dev.ConfigSpace[cfgOff] = uint8(val)
				dev.ConfigSpace[cfgOff+1] = uint8(val >> 8)
			}
		case 4:
			if cfgOff+4 <= dev.ConfigSpaceSize {
				dev.ConfigSpace[cfgOff] = uint8(val)
				dev.ConfigSpace[cfgOff+1] = uint8(val >> 8)
				dev.ConfigSpace[cfgOff+2] = uint8(val >> 16)
				dev.ConfigSpace[cfgOff+3] = uint8(val >> 24)
			}
		}
		if dev.ConfigWrite != nil {
			// Unlock around the callback in case it touches the device.
			dev.mu.Unlock()
			dev.ConfigWrite(dev)
			dev.mu.Lock()
		}
		return
	}

	switch off {
	case pciGuestFeatures:
		// Mask against offered features — drivers are NOT permitted
		// to enable bits we don't advertise (virtio-v1.0 §3.1.1
		// driver-init step 5). Silently dropping unsupported bits
		// matches what real virtio devices do; the driver then
		// reads back the masked value and sees only what was
		// granted.
		dev.GuestFeatures = val & dev.Features
	case pciQueuePFN:
		// Driver writes PFN; we derive desc/avail/used from PFN + queue size.
		// Layout per legacy spec: desc table, then avail ring,
		// padded to PAGE_SIZE, then used ring.
		qs := &dev.Queues[dev.QueueSel]
		if val == 0 {
			qs.DescAddr = 0
			qs.AvailAddr = 0
			qs.UsedAddr = 0
			qs.Ready = 0
			return
		}
		descAddr := uint64(val) << pciQueuePFNShift
		num := uint64(qs.Num)
		if num == 0 {
			num = MaxQueueNum
		}
		availAddr := descAddr + 16*num
		// Used ring is page-aligned after the avail ring.
		usedStart := availAddr + 4 + 2*num
		usedAddr := (usedStart + pciQueueAlignment - 1) &^ uint64(pciQueueAlignment-1)
		qs.DescAddr = descAddr
		qs.AvailAddr = availAddr
		qs.UsedAddr = usedAddr
		qs.Ready = 1
		// Clear suppression bit in used ring flags so the device can
		// raise interrupts.
		if qs.UsedAddr != 0 {
			dev.write16(qs.UsedAddr, 0)
		}
		if pciTransportDebug {
			fmt.Fprintf(os.Stderr, "[vpci] PFN write q=%d pfn=%08x num=%d desc=%016x avail=%016x used=%016x\n",
				dev.QueueSel, val, num, descAddr, availAddr, usedAddr)
		}
	case pciQueueSelect:
		if val < MaxQueue {
			dev.QueueSel = val
		}
	case pciQueueNotify:
		if val < MaxQueue {
			if pciTransportDebug {
				qs := &dev.Queues[val]
				fmt.Fprintf(os.Stderr, "[vpci] notify q=%d avail.idx=%d lastAvail=%d\n",
					val, dev.read16(qs.AvailAddr+2), qs.LastAvailIdx)
			}
			dev.queueNotify(val)
		}
	case pciDeviceStatus:
		dev.Status = val
		if val == 0 {
			// Reset.
			if dev.IRQ != nil {
				dev.IRQ.Lower()
			}
			dev.Reset()
		}
	}
}
