package slirp

import (
	"net"

	"golang.org/x/sys/unix"

	"github.com/jtolio/tinyemu-go/virtio"
)

// slirpDeviceState holds state for a Slirp-backed network device.
// This wraps a Slirp instance to implement the EthernetDevice callbacks.
type slirpDeviceState struct {
	slirp *Slirp
	es    *virtio.EthernetDevice
}

// NewEthernetDevice creates a new EthernetDevice backed by Slirp userspace networking.
// This provides network connectivity without requiring root access or TUN/TAP devices.
//
// The network configuration matches C TinyEMU's defaults:
//   - Network: 10.0.2.0/24
//   - Gateway/Host: 10.0.2.2
//   - DNS: 10.0.2.3
//   - Guest DHCP start: 10.0.2.15
//
// Reference: tinyemu-2019-12-21/temu.c:497-530 (slirp_open)
func NewEthernetDevice() *virtio.EthernetDevice {
	return NewEthernetDeviceWithConfig(SlirpConfig{})
}

// SlirpConfig holds configuration for creating a Slirp-backed network device.
type SlirpConfig struct {
	// Network is the virtual network address (default: 10.0.2.0)
	Network net.IP
	// Netmask is the virtual network mask (default: 255.255.255.0)
	Netmask net.IP
	// Host is the virtual host/gateway address (default: 10.0.2.2)
	Host net.IP
	// DHCPStart is the DHCP start address (default: 10.0.2.15)
	DHCPStart net.IP
	// DNS is the virtual DNS server address (default: 10.0.2.3)
	DNS net.IP
	// Restricted limits network access if true
	Restricted bool
}

// NewEthernetDeviceWithConfig creates a new EthernetDevice backed by Slirp
// with the given configuration.
//
// Reference: tinyemu-2019-12-21/temu.c:497-530 (slirp_open)
// Reference: tinyemu-2019-12-21/slirp/slirp.c:189-226 (slirp_init)
func NewEthernetDeviceWithConfig(cfg SlirpConfig) *virtio.EthernetDevice {
	// Create the Slirp instance
	// Reference: tinyemu-2019-12-21/slirp/slirp.c:189-226
	slirp := NewSlirp()

	// Apply configuration if provided
	// Reference: tinyemu-2019-12-21/temu.c:500-504
	if cfg.Network != nil {
		slirp.VNetworkAddr = cfg.Network.To4()
	}
	if cfg.Netmask != nil {
		slirp.VNetworkMask = cfg.Netmask.To4()
	}
	if cfg.Host != nil {
		slirp.VHostAddr = cfg.Host.To4()
	}
	if cfg.DHCPStart != nil {
		slirp.VDHCPStartAddr = cfg.DHCPStart.To4()
	}
	if cfg.DNS != nil {
		slirp.VNameserverAddr = cfg.DNS.To4()
	}
	slirp.Restricted = cfg.Restricted

	// Create the EthernetDevice
	// Reference: tinyemu-2019-12-21/temu.c:513-527
	es := &virtio.EthernetDevice{
		// Default MAC address: 02:00:00:00:00:01
		// Reference: tinyemu-2019-12-21/temu.c:518-523
		MACAddr: [6]byte{0x02, 0x00, 0x00, 0x00, 0x00, 0x01},
	}

	// Create the state wrapper
	state := &slirpDeviceState{
		slirp: slirp,
		es:    es,
	}

	// Set up the write_packet callback: guest -> network
	// Reference: tinyemu-2019-12-21/temu.c:462-467 (slirp_write_packet)
	es.WritePacket = func(buf []byte) {
		// Forward packet to slirp (wraps slirp_input)
		// Reference: tinyemu-2019-12-21/slirp/slirp.c:628-660
		slirp.Input(buf)
	}

	// Store state in Opaque for access from callbacks
	es.Opaque = state
	slirp.Opaque = state

	// Set up Slirp's output callback: network -> guest
	// Reference: tinyemu-2019-12-21/temu.c:475-479 (slirp_output)
	slirp.OutputFunc = func(opaque interface{}, pkt []byte) {
		s := opaque.(*slirpDeviceState)
		if s.es.DeviceWritePacket != nil {
			s.es.DeviceWritePacket(pkt)
		}
	}

	// Set up Slirp's can_output callback
	// Reference: tinyemu-2019-12-21/temu.c:469-473 (slirp_can_output)
	slirp.CanOutput = func(opaque interface{}) bool {
		s := opaque.(*slirpDeviceState)
		if s.es.DeviceCanWritePacket != nil {
			return s.es.DeviceCanWritePacket()
		}
		return false
	}

	return es
}

// GetSlirp returns the underlying Slirp instance from an EthernetDevice
// created by NewEthernetDevice. Returns nil if the device is not Slirp-backed.
func GetSlirp(es *virtio.EthernetDevice) *Slirp {
	if es == nil || es.Opaque == nil {
		return nil
	}
	if state, ok := es.Opaque.(*slirpDeviceState); ok {
		return state.slirp
	}
	return nil
}

// Poll processes pending I/O for a Slirp-backed EthernetDevice.
// This should be called periodically from the main emulation loop.
//
// In the C implementation, this is handled by slirp_select_fill and
// slirp_select_poll using file descriptor sets and select(). In Go,
// we use a polling approach with non-blocking sockets.
//
// Reference: tinyemu-2019-12-21/slirp/slirp.c:239-355 (slirp_select_fill)
// Reference: tinyemu-2019-12-21/slirp/slirp.c:357-534 (slirp_select_poll)
func Poll(es *virtio.EthernetDevice) {
	slirp := GetSlirp(es)
	if slirp == nil {
		return
	}

	// Update current time for TCP timers
	// Reference: tinyemu-2019-12-21/slirp/slirp.c:368
	curtime := GetTimeMs()

	// Update timer flags based on current socket state
	// Reference: tinyemu-2019-12-21/slirp/slirp.c:254-272 (slirp_select_fill)
	slirp.updateTimerFlags(curtime)

	// Process TCP fast timeout (every 200ms)
	// Reference: tinyemu-2019-12-21/slirp/slirp.c:374-377
	slirp.processTimers(curtime)

	// Check for incoming TCP connections on listening sockets
	// Reference: tinyemu-2019-12-21/slirp/slirp.c:412-419
	slirp.checkTCPListeners()

	// Process TCP socket I/O
	// Reference: tinyemu-2019-12-21/slirp/slirp.c:391-502
	slirp.processTCPSockets()

	// Process UDP socket I/O
	// Reference: tinyemu-2019-12-21/slirp/slirp.c:509-516
	slirp.processUDPSockets()

	// Process pending output
	// Reference: tinyemu-2019-12-21/slirp/slirp.c:522-524
	if slirp.IfQueued > 0 {
		slirp.IfStart()
	}
}

// updateTimerFlags updates the timer control flags based on socket state.
// This mirrors the timer-related logic in C's slirp_select_fill.
// Reference: tinyemu-2019-12-21/slirp/slirp.c:254-272
func (s *Slirp) updateTimerFlags(curtime uint32) {
	// Reset doSlowTimo and check if we need slow timeouts
	// Reference: tinyemu-2019-12-21/slirp/slirp.c:254
	s.doSlowTimo = false

	// Need slow timeout if there are TCP connections active or IP fragments pending
	// Reference: tinyemu-2019-12-21/slirp/slirp.c:261-262
	// C code: do_slowtimo |= ((tcb.so_next != &tcb) || (&ipq.ip_link != ipq.ip_link.next))
	if s.TCB.Next != &s.TCB || s.IPQ.IPLink.Next != &s.IPQ.IPLink {
		s.doSlowTimo = true
	}

	// Check each TCP socket for delayed ACK flag
	// Reference: tinyemu-2019-12-21/slirp/slirp.c:264-272
	for so := s.TCB.Next; so != &s.TCB; so = so.Next {
		// Check if we need a tcp_fasttimo
		// Reference: tinyemu-2019-12-21/slirp/slirp.c:271-272
		tp := SoToTCPCB(so)
		if tp != nil && s.timeFastTimo == 0 && (tp.TFlags&TFDelAck) != 0 {
			s.timeFastTimo = curtime // Flag when we want a fasttimo
		}
	}

	// Check UDP sockets for expiring connections and detach expired ones
	// Reference: tinyemu-2019-12-21/slirp/slirp.c:329-335
	for so := s.UDB.Next; so != &s.UDB; {
		soNext := so.Next
		if so.SoExpire != 0 {
			if so.SoExpire <= curtime {
				// Socket has expired - detach it
				// Reference: tinyemu-2019-12-21/slirp/slirp.c:330-332
				s.UDPDetach(so)
			} else {
				// Socket has not expired yet - let it expire later
				// Reference: tinyemu-2019-12-21/slirp/slirp.c:333-334
				s.doSlowTimo = true
			}
		}
		so = soNext
	}
}

// processTimers handles TCP timers (fast and slow timeouts).
// Reference: tinyemu-2019-12-21/slirp/slirp.c:374-382
func (s *Slirp) processTimers(curtime uint32) {
	// Fast timeout: every 200ms (RFC 1122)
	// Reference: tinyemu-2019-12-21/slirp/slirp.c:374-377
	if s.timeFastTimo != 0 && (curtime-s.timeFastTimo) >= 2 {
		s.TCPFasttimo()
		s.timeFastTimo = 0
	}

	// Slow timeout: every 500ms
	// Reference: tinyemu-2019-12-21/slirp/slirp.c:378-382
	if s.doSlowTimo && (curtime-s.lastSlowTimo) >= 499 {
		s.IPSlowtimo()
		s.TCPSlowtimo()
		s.lastSlowTimo = curtime
	}
}

// processTCPSockets handles I/O on TCP sockets with active connections.
// This polls each TCP socket for readable/writable state and processes data.
// Reference: tinyemu-2019-12-21/slirp/slirp.c:391-502
func (s *Slirp) processTCPSockets() {
	// Build a list of sockets to process (to avoid modifying list while iterating)
	var sockets []*Socket
	for so := s.TCB.Next; so != &s.TCB; so = so.Next {
		sockets = append(sockets, so)
	}

	for _, so := range sockets {
		// Skip sockets without valid fd
		// Reference: tinyemu-2019-12-21/slirp/slirp.c:399-400
		if (so.SoState&SSNoFDRef) != 0 || so.S < 0 {
			continue
		}

		// Skip listener sockets (handled by checkTCPListeners)
		// Reference: tinyemu-2019-12-21/slirp/slirp.c:416-419
		if (so.SoState & SSFAcceptConn) != 0 {
			continue
		}

		// Handle sockets that are still connecting
		// Reference: tinyemu-2019-12-21/slirp/slirp.c:434-456
		if (so.SoState & SSIsFConnecting) != 0 {
			s.handleConnectingSocket(so)
			continue
		}

		// Check for OOB (urgent) data first
		// Reference: tinyemu-2019-12-21/slirp/slirp.c:407-408
		// The C code uses select() with xfds to detect OOB. We use Poll with POLLPRI.
		hasOOB := false
		if so.S >= 0 {
			pollFds := []unix.PollFd{{Fd: int32(so.S), Events: unix.POLLPRI}}
			n, _ := unix.Poll(pollFds, 0) // timeout=0 for non-blocking check
			if n > 0 && (pollFds[0].Revents&unix.POLLPRI) != 0 {
				hasOOB = true
			}
		}

		// Try to read from socket if we can receive and have buffer space
		// Reference: tinyemu-2019-12-21/slirp/slirp.c:412-425
		// CONN_CANFRCV: (so_state & (SS_ISFCONNECTED|SS_FCANTRCVMORE)) == SS_ISFCONNECTED
		canReceive := (so.SoState & (SSIsFConnected | SSFCantRcvMore)) == SSIsFConnected
		hasBufferSpace := so.SoSnd.SbCC < (so.SoSnd.SbDataLen / 2)
		if canReceive && hasBufferSpace {
			if hasOOB {
				// OOB data present - use SoRecvOOB to read and mark as urgent
				// Reference: tinyemu-2019-12-21/slirp/slirp.c:407-408
				so.SoRecvOOB()
			} else {
				// Normal read
				ret := so.SoRead()
				// Output it if we read something
				// Reference: tinyemu-2019-12-21/slirp/slirp.c:423-424
				if ret > 0 {
					tp := SoToTCPCB(so)
					if tp != nil {
						tp.TCPOutput()
					}
				}
			}
		}

		// Try to write to socket if we are connected and have data to send
		// Reference: tinyemu-2019-12-21/slirp/slirp.c:430-464
		// CONN_CANFSEND: (so_state & (SS_ISFCONNECTED|SS_FCANTSENDMORE)) == SS_ISFCONNECTED
		canSend := (so.SoState & (SSIsFConnected | SSFCantSendMore)) == SSIsFConnected
		hasDataToSend := so.SoRcv.SbCC > 0
		if canSend && hasDataToSend {
			so.SoWrite()
		}
	}
}

// handleConnectingSocket handles a socket that is still in the connecting state.
// When the connection completes (socket becomes writable), it transitions to connected.
// Reference: tinyemu-2019-12-21/slirp/slirp.c:434-456
func (s *Slirp) handleConnectingSocket(so *Socket) {
	// First, poll for writability to see if the connection has completed.
	// In C, select() is used with the write fd_set to detect this.
	// A non-blocking connect socket becomes writable when the connection completes.
	// Reference: tinyemu-2019-12-21/slirp/slirp.c:315-320 (slirp_select_fill adds connecting sockets to write set)
	pollFds := []unix.PollFd{{Fd: int32(so.S), Events: unix.POLLOUT}}
	n, err := unix.Poll(pollFds, 0) // timeout=0 for non-blocking check
	if err != nil || n <= 0 {
		// Not yet writable, connection still in progress
		return
	}
	if (pollFds[0].Revents & (unix.POLLERR | unix.POLLHUP)) != 0 {
		// Connection failed
		so.SoState &= SSPersistentMask
		so.SoState |= SSNoFDRef
		return
	}
	if (pollFds[0].Revents & unix.POLLOUT) == 0 {
		// Not writable yet
		return
	}

	// Socket is writable - verify connection completed with a zero-byte send
	// Reference: tinyemu-2019-12-21/slirp/slirp.c:438
	// In C: ret = send(so->s, (const void *)&ret, 0, 0)
	ret := s.slirpSend(so, nil, 0)
	if ret < 0 {
		// Connection failed
		so.SoState &= SSPersistentMask
		so.SoState |= SSNoFDRef
		return
	}

	// Connection completed - clear connecting flag
	// Reference: tinyemu-2019-12-21/slirp/slirp.c:449
	so.SoState &= ^SSIsFConnecting

	// Continue tcp_input to process the connection
	// Reference: tinyemu-2019-12-21/slirp/slirp.c:454
	// We must pass the socket so tcpInputImpl can set up the template and send SYN-ACK
	s.tcpInputContinue(so)
}

// processUDPSockets handles I/O on UDP sockets.
// Reference: tinyemu-2019-12-21/slirp/slirp.c:509-516
func (s *Slirp) processUDPSockets() {
	// Build a list of sockets to process (to avoid modifying list while iterating)
	var sockets []*Socket
	for so := s.UDB.Next; so != &s.UDB; so = so.Next {
		sockets = append(sockets, so)
	}

	for _, so := range sockets {
		// Skip sockets without valid fd
		if so.S < 0 {
			continue
		}

		// Check if socket is connected and can receive
		// The so_queued <= 4 check is from slirp_select_fill (lines 347-350)
		// which limits the number of packets queued by this session to 4.
		// Reference: tinyemu-2019-12-21/slirp/slirp.c:347-350, 513-515
		if (so.SoState&SSIsFConnected) != 0 && so.SoQueued <= 4 {
			// Try to receive from UDP socket
			so.SoRecvFrom()
		}
	}
}
