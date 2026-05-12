// Package virtio provides VirtIO device emulation for the TinyEMU RISC-V emulator.
// This file implements the VirtIO Network device.
//
// Reference: tinyemu-2019-12-21/virtio.c:1134-1258
package virtio

import (
	"fmt"

	"github.com/jtolio/tinyemu-go/mem"
)

// Net queue indices
// Reference: virtio.c - queue 0 is RX (network->guest), queue 1 is TX (guest->network)
const (
	NetQueueRX = 0 // Receive queue (network writes to guest)
	NetQueueTX = 1 // Transmit queue (guest writes to network)
)

// Net feature bits
// Reference: tinyemu-2019-12-21/virtio.c:1243
const (
	NetFeatureMAC    = 1 << 5  // VIRTIO_NET_F_MAC - device has MAC address
	NetFeatureStatus = 1 << 16 // VIRTIO_NET_F_STATUS - device has status field (not enabled)
)

// Net configuration space layout
// Reference: tinyemu-2019-12-21/virtio.c:1246-1249
const (
	NetConfigMAC       = 0 // 6 bytes: MAC address
	NetConfigStatus    = 6 // 2 bytes: status
	NetConfigSpaceSize = 8 // 6 + 2 bytes
)

// NetHeaderSize is the size of the VirtIO net header.
// Reference: tinyemu-2019-12-21/virtio.c:1143-1151
const NetHeaderSize = 12

// NetHeader is the VirtIO network header prepended to packets.
// Reference: tinyemu-2019-12-21/virtio.c:1143-1151
type NetHeader struct {
	Flags      uint8
	GSOType    uint8
	HdrLen     uint16
	GSOSize    uint16
	CsumStart  uint16
	CsumOffset uint16
	NumBuffers uint16
}

// EthernetDevice represents the backend network interface.
// This is the external network connection (e.g., TAP device, slirp).
//
// Reference: tinyemu-2019-12-21/virtio.h:85-104
type EthernetDevice struct {
	// MACAddr is the MAC address of the interface (6 bytes).
	MACAddr [6]byte

	// WritePacket is called to send a packet to the network.
	// This is called when the guest transmits a packet.
	WritePacket func(buf []byte)

	// Opaque is user data for the backend.
	Opaque interface{}

	// The following are set by the VirtIO device:

	// DeviceOpaque points to the VirtIO Net device.
	DeviceOpaque interface{}

	// DeviceCanWritePacket checks if the guest has a buffer ready to receive.
	DeviceCanWritePacket func() bool

	// DeviceWritePacket writes a packet to the guest.
	DeviceWritePacket func(buf []byte)

	// DeviceSetCarrier sets the carrier state.
	DeviceSetCarrier func(carrierState bool)
}

// Net represents a VirtIO network device.
//
// Queue 0 (RX): Network writes packets to guest
// Queue 1 (TX): Guest writes packets to network
//
// Reference: tinyemu-2019-12-21/virtio.c:1137-1141
type Net struct {
	dev        *Device
	es         *EthernetDevice
	headerSize int
}

// NewNet creates a new VirtIO network device backed by an MMIO transport.
// For PCI transport, use NewNetCore + your own PCI wiring.
//
// Reference: tinyemu-2019-12-21/virtio.c:1235-1258 (virtio_net_init)
func NewNet(memMap *mem.PhysMemoryMap, addr uint64, irq *mem.IRQSignal,
	es *EthernetDevice) (*Net, error) {

	n := &Net{es: es, headerSize: NetHeaderSize}
	var err error
	n.dev, err = NewDevice(memMap, addr, irq, DeviceIDNet, NetConfigSpaceSize, n.recvRequest)
	if err != nil {
		return nil, err
	}
	n.setup(es)
	return n, nil
}

// NewNetCore creates a VirtIO net device whose underlying Device is not
// registered to any transport. The caller wires the returned device's
// Device() to a transport (e.g. PCI via LegacyTransport).
func NewNetCore(memMap *mem.PhysMemoryMap, irq *mem.IRQSignal, es *EthernetDevice) *Net {
	n := &Net{es: es, headerSize: NetHeaderSize}
	n.dev = NewDeviceCore(memMap, irq, DeviceIDNet, NetConfigSpaceSize, n.recvRequest)
	n.setup(es)
	return n
}

// setup configures device features, the manual-RX queue, MAC, status,
// and EthernetDevice callbacks. Shared between MMIO and PCI constructors.
//
// We explicitly advertise ONLY the features we implement:
//   - VIRTIO_NET_F_MAC    (bit 5)  — the device-configured MAC address
//                                   is exposed via config space.
//
// We do NOT advertise:
//   - VIRTIO_NET_F_STATUS  (bit 16) — would require tracking and
//                                    pushing config-change interrupts
//                                    on carrier transitions.
//   - VIRTIO_NET_F_MRG_RXBUF (bit 15) — needs multi-buffer RX support
//                                      we don't implement.
//   - VIRTIO_NET_F_STANDBY (bit 62) — failover semantics; pulls in
//                                    the net_failover kernel module
//                                    which we have no business
//                                    pretending to support.
//   - VIRTIO_NET_F_CTRL_VQ  (bit 17) — control queue, not implemented.
//   - All offload features (CSUM, GSO, …)             — host-side
//                                                       offload, not
//                                                       implemented.
func (n *Net) setup(es *EthernetDevice) {
	n.dev.SetFeatures(NetFeatureMAC)
	n.dev.SetFeaturesHi(0) // no high-bit features (no STANDBY, etc.)
	n.dev.Queues[NetQueueRX].ManualRecv = true
	copy(n.dev.ConfigSpace[NetConfigMAC:], es.MACAddr[:])
	n.dev.ConfigSpace[NetConfigStatus] = 0
	n.dev.ConfigSpace[NetConfigStatus+1] = 0
	es.DeviceOpaque = n
	es.DeviceCanWritePacket = n.CanWritePacket
	es.DeviceWritePacket = n.WritePacket
	es.DeviceSetCarrier = n.SetCarrier
}

// Device returns the underlying VirtIO device.
func (n *Net) Device() *Device {
	return n.dev
}

// recvRequest handles incoming requests from the guest.
// For the network device, this handles TX queue (guest -> network) packets.
//
// Reference: tinyemu-2019-12-21/virtio.c:1153-1175 (virtio_net_recv_request)
func (n *Net) recvRequest(dev *Device, queueIdx int, descIdx int, readSize int, writeSize int) int {
	if queueIdx == NetQueueTX {
		// Guest is sending a packet to the network
		// Skip the VirtIO net header
		if readSize <= n.headerSize {
			// No actual packet data
			dev.ConsumeDesc(queueIdx, descIdx, 0)
			return 0
		}

		// Read the packet data (after header)
		packetLen := readSize - n.headerSize
		buf := make([]byte, packetLen)
		if err := dev.MemcpyFromQueue(buf, queueIdx, descIdx, n.headerSize, packetLen); err != nil {
			dev.ConsumeDesc(queueIdx, descIdx, 0)
			return 0
		}

		// Send to the network backend
		if n.es != nil && n.es.WritePacket != nil {
			n.es.WritePacket(buf)
		}

		// Consume the descriptor
		dev.ConsumeDesc(queueIdx, descIdx, 0)
	}
	return 0
}

// CanWritePacket checks if the guest has provided a buffer for receiving packets.
// Returns true if there are available descriptors in the RX queue.
//
// Reference: tinyemu-2019-12-21/virtio.c:1177-1187 (virtio_net_can_write_packet)
func (n *Net) CanWritePacket() bool {
	n.dev.mu.Lock()
	defer n.dev.mu.Unlock()

	qs := &n.dev.Queues[NetQueueRX]

	if qs.Ready == 0 {
		if DebugNetRX {
			println("[VIRTIO-NET] CanWritePacket: queue not ready")
		}
		return false
	}

	// Read available index from guest memory
	availIdx := n.dev.read16(qs.AvailAddr + 2)
	canWrite := qs.LastAvailIdx != availIdx
	if DebugNetRX && !canWrite {
		println("[VIRTIO-NET] CanWritePacket: no buffers available, lastAvail:", qs.LastAvailIdx, "availIdx:", availIdx)
	}
	return canWrite
}

// DebugNetRX enables debug logging for network RX operations.
var DebugNetRX bool

// WritePacket writes a packet from the network to the guest via the RX queue.
//
// Reference: tinyemu-2019-12-21/virtio.c:1189-1217 (virtio_net_write_packet)
func (n *Net) WritePacket(buf []byte) {
	if DebugNetRX {
		println("[VIRTIO-NET] WritePacket: called with", len(buf), "bytes")
	}
	n.dev.mu.Lock()
	defer n.dev.mu.Unlock()

	qs := &n.dev.Queues[NetQueueRX]

	if qs.Ready == 0 {
		if DebugNetRX {
			println("[VIRTIO-NET] WritePacket: RX queue not ready, dropping packet")
		}
		return
	}

	// Read available index from guest memory
	availIdx := n.dev.read16(qs.AvailAddr + 2)
	if DebugNetRX {
		println("[VIRTIO-NET] WritePacket: availIdx:", availIdx, "lastAvailIdx:", qs.LastAvailIdx, "diff:", availIdx-qs.LastAvailIdx)
	}
	if qs.LastAvailIdx == availIdx {
		if DebugNetRX {
			println("[VIRTIO-NET] WritePacket: no available RX buffer, dropping packet. lastAvail:", qs.LastAvailIdx, "availIdx:", availIdx)
		}
		return
	}

	// Get the descriptor index from the available ring
	descIdx := n.dev.read16(qs.AvailAddr + 4 + uint64(qs.LastAvailIdx&uint16(qs.Num-1))*2)

	if DebugNetRX {
		// Debug: print the descriptor details
		desc, _ := n.dev.getDesc(NetQueueRX, int(descIdx))
		println("[VIRTIO-NET] Descriptor", descIdx, "addr:", desc.Addr, "len:", desc.Len, "flags:", desc.Flags)
	}

	// Check descriptor size
	readSize, writeSize, ok := n.dev.getDescRWSize(NetQueueRX, int(descIdx))
	if !ok {
		if DebugNetRX {
			println("[VIRTIO-NET] getDescRWSize failed for desc", descIdx)
		}
		return
	}
	_ = readSize // unused for RX

	// Total length = header + packet
	totalLen := n.headerSize + len(buf)
	if totalLen > writeSize {
		return // packet too large for buffer
	}

	// Write zero header
	// Reference: tinyemu-2019-12-21/virtio.c:1212-1213
	header := make([]byte, n.headerSize)
	if err := n.dev.MemcpyToQueue(NetQueueRX, int(descIdx), 0, header, n.headerSize); err != nil {
		return
	}

	// Write packet data after header
	if err := n.dev.MemcpyToQueue(NetQueueRX, int(descIdx), n.headerSize, buf, len(buf)); err != nil {
		return
	}

	// Consume descriptor and advance index
	n.dev.consumeDescLocked(NetQueueRX, int(descIdx), totalLen)
	qs.LastAvailIdx++
	if DebugNetRX {
		println("[VIRTIO-NET] WritePacket: wrote", len(buf), "bytes to desc", descIdx, "lastAvail now", qs.LastAvailIdx)
		// Verify what we wrote by reading back the used ring
		usedIdx := n.dev.read16(qs.UsedAddr + 2)
		entryAddr := qs.UsedAddr + 4 + uint64((usedIdx-1)&uint16(qs.Num-1))*8
		ptr := n.dev.MemMap.GetRAMPtr(entryAddr, false)
		if ptr != nil {
			entryId := uint32(ptr[0]) | uint32(ptr[1])<<8 | uint32(ptr[2])<<16 | uint32(ptr[3])<<24
			entryLen := uint32(ptr[4]) | uint32(ptr[5])<<8 | uint32(ptr[6])<<16 | uint32(ptr[7])<<24
			println("[VIRTIO-NET] Used ring verify: usedIdx:", usedIdx, "entry id:", entryId, "len:", entryLen)
		}
		// Verify packet data in descriptor buffer
		desc, _ := n.dev.getDesc(NetQueueRX, int(descIdx))
		pktPtr := n.dev.MemMap.GetRAMPtr(desc.Addr, false)
		if pktPtr != nil && len(buf) >= 14 {
			// Skip 12-byte VirtIO header, show packet data in hex
			print("[VIRTIO-NET] Packet in memory hex: ")
			for i := 12; i < 12+len(buf) && i < len(pktPtr); i++ {
				if pktPtr[i] < 16 {
					print("0")
				}
				print(fmt.Sprintf("%x", pktPtr[i]))
			}
			println()
		}
	}
}

// SetCarrier sets the network carrier state.
// This is currently a no-op matching the C implementation.
//
// Reference: tinyemu-2019-12-21/virtio.c:1219-1233 (virtio_net_set_carrier)
// Note: The C implementation has this disabled with #if 0
func (n *Net) SetCarrier(carrierState bool) {
	// Currently disabled in C code (#if 0)
	// If enabled, it would update config_space[6] and notify
}

// SetDebug sets debug flags for the network device.
func (n *Net) SetDebug(flags int) {
	n.dev.SetDebug(flags)
}
