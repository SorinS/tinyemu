package slirp

import (
	"net"
)

// Slirp represents a slirp networking instance.
// Reference: tinyemu-2019-12-21/slirp/slirp.h:168-219
type Slirp struct {
	// Virtual network configuration
	VNetworkAddr    net.IP // network address
	VNetworkMask    net.IP // network mask
	VHostAddr       net.IP // host address (gateway)
	VDHCPStartAddr  net.IP // DHCP start address
	VNameserverAddr net.IP // nameserver address

	// ARP cache for guest IP addresses
	ClientEthAddr  [6]byte
	ClientIPAddr   net.IP
	ClientHostname string

	// Configuration
	Restricted bool

	// Mbuf states
	MFreeList   Mbuf // free list head
	MUsedList   Mbuf // used list head
	MbufAlloced int  // number of mbufs allocated

	// Interface states
	IfQueued int   // number of packets queued
	IfFastQ  Mbuf  // fast queue (for interactive data)
	IfBatchQ Mbuf  // queue for non-interactive data
	NextM    *Mbuf // pointer to next mbuf to output

	// IP states
	IPID uint16 // ip packet counter, for IDs
	// IP fragment reassembly queue
	// Reference: tinyemu-2019-12-21/slirp/slirp.h:178
	IPQ IPQ // IP reassembly queue head

	// BOOTP/DHCP states
	// Reference: tinyemu-2019-12-21/slirp/slirp.h:200-202
	BootpClients  [NBBootpClients]BOOTPClient
	BootpFilename string

	// TCP states
	TCB       Socket // TCP socket list head
	TCPLastSo *Socket
	TCPIss    uint32 // tcp initial send seq #
	TCPNow    uint32 // for RFC 1323 timestamps

	// Timer states for slirp_select_poll
	// Reference: tinyemu-2019-12-21/slirp/slirp.c:39-41
	timeFastTimo uint32 // time for tcp fasttimo
	doSlowTimo   bool   // flag to enable slow timeout
	lastSlowTimo uint32 // last time slow timeout was run

	// UDP states
	UDB       Socket // UDP socket list head
	UDPLastSo *Socket

	// Exec list for port forwarding
	// Reference: tinyemu-2019-12-21/slirp/slirp.h:184
	ExecList *ExList

	// Output callback - called to send packets to the virtual network interface
	// Reference: tinyemu-2019-12-21/slirp/libslirp.h:31
	OutputFunc func(opaque interface{}, pkt []byte)

	// CanOutput callback - returns true if output is currently possible
	// Reference: tinyemu-2019-12-21/slirp/if.c:165
	CanOutput func(opaque interface{}) bool

	// Opaque user data
	Opaque interface{}

	// Tracer for debug output. If set, trace messages will be logged.
	Tracer Tracer
}

// NewSlirp creates a new Slirp instance with default configuration.
// Reference: tinyemu-2019-12-21/slirp/slirp.c:189-226 (slirp_init)
func NewSlirp() *Slirp {
	s := &Slirp{
		VNetworkAddr:    net.IPv4(10, 0, 2, 0),
		VNetworkMask:    net.IPv4(255, 255, 255, 0),
		VHostAddr:       net.IPv4(10, 0, 2, 2),
		VDHCPStartAddr:  net.IPv4(10, 0, 2, 15),
		VNameserverAddr: net.IPv4(10, 0, 2, 3),
		Tracer:          DefaultTracer,
	}

	// Initialize mbuf lists
	// Reference: tinyemu-2019-12-21/slirp/slirp.c:205 (m_init)
	s.MInit()

	// Initialize interface queues
	// Reference: tinyemu-2019-12-21/slirp/slirp.c:201 (if_init)
	s.IfInit()

	// Initialize IP layer (fragment reassembly queue)
	// Reference: tinyemu-2019-12-21/slirp/ip_input.c:59-64 (ip_init)
	s.ipInit()

	// Initialize TCP socket list and state
	// Reference: tinyemu-2019-12-21/slirp/tcp_subr.c:51-56 (tcp_init)
	s.TCPIss = 1 // tcp initial send seq
	s.TCB.Next = &s.TCB
	s.TCB.Prev = &s.TCB
	s.TCPLastSo = &s.TCB

	// Initialize UDP socket list
	// Reference: tinyemu-2019-12-21/slirp/udp.c:47-51 (udp_init)
	s.UDB.Next = &s.UDB
	s.UDB.Prev = &s.UDB
	s.UDPLastSo = &s.UDB

	return s
}

// LoopbackAddr is the loopback address (127.0.0.1).
var LoopbackAddr = net.IPv4(127, 0, 0, 1)

// Ethernet constants
// Reference: tinyemu-2019-12-21/slirp/slirp.c:537-544
const (
	EthALen = 6  // Ethernet address length
	EthHLen = 14 // Ethernet header length

	EthPIP  = 0x0800 // IP protocol
	EthPARP = 0x0806 // ARP protocol

	ARPOpRequest = 1 // ARP request
	ARPOpReply   = 2 // ARP reply
)

// SpecialEthAddr is the special Ethernet address used by slirp.
// Reference: tinyemu-2019-12-21/slirp/slirp.c:30-31
var SpecialEthAddr = [6]byte{0x52, 0x55, 0x00, 0x00, 0x00, 0x00}

// ZeroEthAddr is all zeros.
// Reference: tinyemu-2019-12-21/slirp/slirp.c:34
var ZeroEthAddr = [6]byte{0, 0, 0, 0, 0, 0}

// IfEncap encapsulates an IP packet in an Ethernet frame and outputs it.
// If the client Ethernet address is unknown, sends an ARP request instead.
// Reference: tinyemu-2019-12-21/slirp/slirp.c:663-711
func (s *Slirp) IfEncap(ipData []byte) {
	if len(ipData)+EthHLen > 1600 {
		return
	}

	if s.OutputFunc == nil {
		return
	}

	// Check if client Ethernet address is known
	if s.ClientEthAddr == ZeroEthAddr {
		// Send ARP request
		// Reference: tinyemu-2019-12-21/slirp/slirp.c:672-701
		arpReq := make([]byte, EthHLen+28) // 14 byte eth header + 28 byte ARP

		// Ethernet header
		// Destination: broadcast
		for i := 0; i < EthALen; i++ {
			arpReq[i] = 0xff
		}
		// Source: special_ethaddr with vhost_addr embedded
		copy(arpReq[EthALen:EthALen+2], SpecialEthAddr[:2])
		vhost := s.VHostAddr.To4()
		if vhost != nil {
			copy(arpReq[EthALen+2:EthALen+6], vhost)
		}
		// EtherType: ARP
		arpReq[12] = byte((EthPARP >> 8) & 0xff)
		arpReq[13] = byte(EthPARP & 0xff)

		// ARP header
		arp := arpReq[EthHLen:]
		// Hardware type: Ethernet (1)
		arp[0] = 0
		arp[1] = 1
		// Protocol type: IP
		arp[2] = byte((EthPIP >> 8) & 0xff)
		arp[3] = byte(EthPIP & 0xff)
		// Hardware address length
		arp[4] = EthALen
		// Protocol address length
		arp[5] = 4
		// Operation: request
		arp[6] = byte(ARPOpRequest >> 8)
		arp[7] = byte(ARPOpRequest)

		// Sender hardware address
		copy(arp[8:8+2], SpecialEthAddr[:2])
		if vhost != nil {
			copy(arp[8+2:8+6], vhost)
		}
		// Sender protocol address
		if vhost != nil {
			copy(arp[14:18], vhost)
		}
		// Target hardware address (zeros)
		for i := 18; i < 24; i++ {
			arp[i] = 0
		}
		// Target protocol address (destination IP from IP header)
		if len(ipData) >= 20 {
			copy(arp[24:28], ipData[16:20]) // IP destination address
			s.ClientIPAddr = net.IP(ipData[16:20]).To4()
		}

		s.tracePacket("OUTPUT", arpReq)
		s.OutputFunc(s.Opaque, arpReq)
	} else {
		// Wrap IP packet in Ethernet frame
		// Reference: tinyemu-2019-12-21/slirp/slirp.c:702-709
		buf := make([]byte, len(ipData)+EthHLen)

		// Ethernet header
		// Destination: client MAC
		copy(buf[0:EthALen], s.ClientEthAddr[:])
		// Source: special_ethaddr with vhost_addr embedded
		copy(buf[EthALen:EthALen+2], SpecialEthAddr[:2])
		vhost := s.VHostAddr.To4()
		if vhost != nil {
			copy(buf[EthALen+2:EthALen+6], vhost)
		}
		// EtherType: IP
		buf[12] = byte((EthPIP >> 8) & 0xff)
		buf[13] = byte(EthPIP & 0xff)

		// Copy IP data
		copy(buf[EthHLen:], ipData)

		s.tracePacket("OUTPUT", buf)
		s.OutputFunc(s.Opaque, buf)
	}
}

// AddExec adds an entry to the exec list for port forwarding.
// Reference: tinyemu-2019-12-21/slirp/slirp.c:756-771
func (s *Slirp) AddExec(doPty int, args string, guestAddr net.IP, guestPort int) int {
	// If no guest address, calculate default (10.0.2.4)
	if guestAddr == nil || guestAddr.Equal(net.IPv4zero) {
		// guest_addr = vnetwork_addr | (htonl(0x0204) & ~vnetwork_mask)
		vnet := ipToUint32(s.VNetworkAddr)
		vmask := ipToUint32(s.VNetworkMask)
		addr := vnet | (0x04020000 & ^vmask) // 0x0204 in network byte order is 0x04020000
		guestAddr = uint32ToIP(addr)
	}

	// Validate the address is in the virtual network and not host/nameserver
	guestAddrInt := ipToUint32(guestAddr)
	vnetAddr := ipToUint32(s.VNetworkAddr)
	vnetMask := ipToUint32(s.VNetworkMask)
	vhostAddr := ipToUint32(s.VHostAddr)
	vnsAddr := ipToUint32(s.VNameserverAddr)

	if (guestAddrInt & vnetMask) != vnetAddr {
		return -1
	}
	if guestAddrInt == vhostAddr {
		return -1
	}
	if guestAddrInt == vnsAddr {
		return -1
	}

	return s.addExecInternal(doPty, args, guestAddr, uint16(guestPort))
}

// addExecInternal adds an exec entry to the list.
// Reference: tinyemu-2019-12-21/slirp/misc.c:40-60
func (s *Slirp) addExecInternal(doPty int, args string, addr net.IP, port uint16) int {
	// First, check if the port is "bound"
	// Reference: tinyemu-2019-12-21/slirp/misc.c:46-50
	for tmp := s.ExecList; tmp != nil; tmp = tmp.ExNext {
		if port == tmp.ExFPort && addr.Equal(tmp.ExAddr) {
			return -1
		}
	}

	ex := &ExList{
		ExFPort: port,
		ExAddr:  addr,
		ExPty:   doPty,
		ExExec:  args,
		ExNext:  s.ExecList,
	}
	s.ExecList = ex
	return 0
}

// FindCtlSocket finds a TCP socket by guest address and port.
// Reference: tinyemu-2019-12-21/slirp/slirp.c:785-796
func (s *Slirp) FindCtlSocket(guestAddr net.IP, guestPort int) *Socket {
	for so := s.TCB.Next; so != &s.TCB; so = so.Next {
		if so.SoFAddr.Equal(guestAddr) && int(so.SoFPort) == guestPort {
			return so
		}
	}
	return nil
}

// SocketCanRecv checks how much data can be received on a control socket.
// Returns 0 if the socket cannot receive, otherwise returns the available space.
// Reference: tinyemu-2019-12-21/slirp/slirp.c:798-813
func (s *Slirp) SocketCanRecv(guestAddr net.IP, guestPort int) int {
	so := s.FindCtlSocket(guestAddr, guestPort)
	if so == nil || (so.SoState&SSNoFDRef) != 0 {
		return 0
	}

	// Check connection state - CONN_CANFRCV
	// Reference: tinyemu-2019-12-21/slirp/socket.h:79
	// #define CONN_CANFRCV(so) (((so)->so_state & (SS_FCONNECTED|SS_FCANTRCVMORE)) == SS_FCONNECTED)
	connState := so.SoState & (SSIsFConnected | SSFCantRcvMore)
	if connState != SSIsFConnected {
		return 0
	}

	// Check if send buffer is at least half full
	// Reference: tinyemu-2019-12-21/slirp/sbuf.h:12 (sbspace)
	sndCC := so.SoSnd.SbCC
	sndDataLen := so.SoSnd.SbDataLen
	if sndDataLen == 0 {
		sndDataLen = 8192 // default buffer size
	}
	if sndCC >= sndDataLen/2 {
		return 0
	}

	// Return available space
	return sndDataLen - sndCC
}

// SocketRecv receives data on a control socket.
// Reference: tinyemu-2019-12-21/slirp/slirp.c:815-828
func (s *Slirp) SocketRecv(guestAddr net.IP, guestPort int, buf []byte) {
	so := s.FindCtlSocket(guestAddr, guestPort)
	if so == nil {
		return
	}

	// Append data to send buffer
	// Reference: tinyemu-2019-12-21/slirp/socket.c:soreadbuf
	so.SoSnd.SbAppendBytes(buf)

	// Trigger TCP output
	tp := SoToTCPCB(so)
	if tp != nil {
		tp.TCPOutput()
	}
}

// AddHostfwd adds a host port forwarding rule.
// is_udp: 0 for TCP, non-zero for UDP
// Returns 0 on success, -1 on error.
// Reference: tinyemu-2019-12-21/slirp/slirp.c:738-754
func (s *Slirp) AddHostfwd(isUDP bool, hostAddr net.IP, hostPort int, guestAddr net.IP, guestPort int) int {
	// If no guest address, use DHCP start address
	if guestAddr == nil || guestAddr.Equal(net.IPv4zero) {
		guestAddr = s.VDHCPStartAddr
	}

	if isUDP {
		so := s.UDPListen(hostAddr, uint16(hostPort), guestAddr, uint16(guestPort), SSHostFwd)
		if so == nil {
			return -1
		}
	} else {
		so := s.TCPListen(hostAddr, uint16(hostPort), guestAddr, uint16(guestPort), SSHostFwd)
		if so == nil {
			return -1
		}
	}
	return 0
}

// RemoveHostfwd removes a host port forwarding rule.
// Returns 0 if found and removed, -1 if not found.
// Reference: tinyemu-2019-12-21/slirp/slirp.c:714-736
func (s *Slirp) RemoveHostfwd(isUDP bool, hostAddr net.IP, hostPort int) int {
	var head *Socket
	if isUDP {
		head = &s.UDB
	} else {
		head = &s.TCB
	}

	for so := head.Next; so != head; so = so.Next {
		if (so.SoState & SSHostFwd) == 0 {
			continue
		}

		// Get the local address of the socket
		// In Go, we compare using the stored addresses
		if so.SoFAddr.Equal(hostAddr) && int(so.SoFPort) == hostPort {
			// Close the system socket
			if conn, ok := so.Extra.(interface{ Close() error }); ok && conn != nil {
				conn.Close()
			}
			so.SoFree()
			return 0
		}
	}

	return -1
}

// TCPListen creates a TCP listening socket for port forwarding.
// Returns the socket on success, nil on error.
// Reference: tinyemu-2019-12-21/slirp/socket.c:579-649
// NOTE: Go's net.ListenTCP handles socket creation, SO_REUSEADDR, bind, and listen.
// The C version also sets SO_OOBINLINE which Go's net package does not expose;
// this should not affect normal TCP operation.
func (s *Slirp) TCPListen(haddr net.IP, hport uint16, laddr net.IP, lport uint16, flags int) *Socket {
	// Reference: tinyemu-2019-12-21/slirp/socket.c:595-596
	so := s.SoCreate()
	if so == nil {
		return nil
	}

	// Create TCP protocol control block
	// Reference: tinyemu-2019-12-21/slirp/socket.c:601
	tp := &TCPCB{
		TSocket: so,
	}
	so.SoTCPCB = tp

	// Insert into TCP list
	// Reference: tinyemu-2019-12-21/slirp/socket.c:605
	so.Next = s.TCB.Next
	so.Prev = &s.TCB
	s.TCB.Next.Prev = so
	s.TCB.Next = so

	s.traceSocketCreate(so, "TCP-listen")

	// SS_FACCEPTONCE sockets must time out
	// Reference: tinyemu-2019-12-21/slirp/socket.c:610-611
	if flags&SSFAcceptOnce != 0 {
		tp.TTimer[TCPTKeep] = TCPTVKeepInit * 2
	}

	// Reference: tinyemu-2019-12-21/slirp/socket.c:613-616
	so.SoState &= SSPersistentMask
	so.SoState |= SSFAcceptConn | flags
	so.SoLPort = lport
	so.SoLAddr = laddr

	// Create TCP socket and bind
	// Reference: tinyemu-2019-12-21/slirp/socket.c:618-637
	addr := &net.TCPAddr{
		IP:   haddr,
		Port: int(hport),
	}

	listener, err := net.ListenTCP("tcp4", addr)
	if err != nil {
		so.SoFree()
		return nil
	}

	// Store the listener
	so.Extra = listener
	so.S = 0 // Indicate socket is valid

	// Get the actual bound address
	// Reference: tinyemu-2019-12-21/slirp/socket.c:640-645
	localAddr := listener.Addr().(*net.TCPAddr)
	so.SoFPort = uint16(localAddr.Port)

	if localAddr.IP.Equal(net.IPv4zero) || localAddr.IP.Equal(LoopbackAddr) {
		so.SoFAddr = s.VHostAddr
	} else {
		so.SoFAddr = localAddr.IP
	}

	// Reference: tinyemu-2019-12-21/slirp/socket.c:647
	return so
}

// SlirpSend sends data on a socket.
// This is a wrapper around the system send call.
// Reference: tinyemu-2019-12-21/slirp/slirp.c:773-782
func SlirpSend(so *Socket, buf []byte, flags int) int {
	if so == nil || so.S == -1 {
		return -1
	}

	// In Go, we use the connection stored in Extra
	if conn, ok := so.Extra.(net.Conn); ok && conn != nil {
		n, err := conn.Write(buf)
		if err != nil {
			return -1
		}
		return n
	}

	return -1
}

// ARPInput processes an incoming ARP packet.
// Reference: tinyemu-2019-12-21/slirp/slirp.c:570-626
func (s *Slirp) ARPInput(pkt []byte) {
	if len(pkt) < EthHLen+28 { // 14 byte eth header + 28 byte ARP minimum
		return
	}

	// Extract ethernet source address from eth header
	// eh->h_source is at bytes 6-11
	ethSource := pkt[EthALen : EthALen+EthALen]

	// ARP header starts after ethernet header
	arp := pkt[EthHLen:]

	// ar_op is at offset 6-7 (network byte order)
	arOp := int(arp[6])<<8 | int(arp[7])

	switch arOp {
	case ARPOpRequest:
		// ar_tip is at offset 24-27 (target IP address)
		arTip := uint32(arp[24])<<24 | uint32(arp[25])<<16 | uint32(arp[26])<<8 | uint32(arp[27])

		// Check if target is in our virtual network
		vnetMask := ipToUint32(s.VNetworkMask)
		vnetAddr := ipToUint32(s.VNetworkAddr)

		if (arTip & vnetMask) == vnetAddr {
			// Check if it's for nameserver or host address
			vnsAddr := ipToUint32(s.VNameserverAddr)
			vhostAddr := ipToUint32(s.VHostAddr)

			isOk := false
			if arTip == vnsAddr || arTip == vhostAddr {
				isOk = true
			} else {
				// Check exec list
				for ex := s.ExecList; ex != nil; ex = ex.ExNext {
					if ipToUint32(ex.ExAddr) == arTip {
						isOk = true
						break
					}
				}
			}

			if !isOk {
				return
			}

			// Save client ethernet address
			// Reference: tinyemu-2019-12-21/slirp/slirp.c:596
			copy(s.ClientEthAddr[:], ethSource)

			// Build ARP reply
			// Reference: tinyemu-2019-12-21/slirp/slirp.c:594-613
			arpReply := make([]byte, 64) // min 64 bytes for ethernet

			// Ethernet header
			// Destination: sender's MAC (from original packet)
			copy(arpReply[0:EthALen], pkt[EthALen:EthALen+EthALen])
			// Source: special_ethaddr with target IP embedded
			copy(arpReply[EthALen:EthALen+2], SpecialEthAddr[:2])
			arpReply[EthALen+2] = arp[24] // ar_tip bytes
			arpReply[EthALen+3] = arp[25]
			arpReply[EthALen+4] = arp[26]
			arpReply[EthALen+5] = arp[27]
			// EtherType: ARP
			arpReply[12] = byte((EthPARP >> 8) & 0xff)
			arpReply[13] = byte(EthPARP & 0xff)

			// ARP header
			rarp := arpReply[EthHLen:]
			// ar_hrd = 1 (Ethernet)
			rarp[0] = 0
			rarp[1] = 1
			// ar_pro = ETH_P_IP
			rarp[2] = byte((EthPIP >> 8) & 0xff)
			rarp[3] = byte(EthPIP & 0xff)
			// ar_hln = 6
			rarp[4] = EthALen
			// ar_pln = 4
			rarp[5] = 4
			// ar_op = ARPOP_REPLY
			rarp[6] = byte(ARPOpReply >> 8)
			rarp[7] = byte(ARPOpReply)
			// ar_sha = our MAC (same as eth source)
			copy(rarp[8:8+EthALen], arpReply[EthALen:EthALen+EthALen])
			// ar_sip = target IP (what they were asking about)
			copy(rarp[14:18], arp[24:28])
			// ar_tha = their MAC
			copy(rarp[18:18+EthALen], arp[8:8+EthALen])
			// ar_tip = their IP
			copy(rarp[24:28], arp[14:18])

			if s.OutputFunc != nil {
				s.tracePacket("OUTPUT", arpReply)
				s.OutputFunc(s.Opaque, arpReply)
			}
		}

	case ARPOpReply:
		// Reply to our ARP request for client MAC address
		// Reference: tinyemu-2019-12-21/slirp/slirp.c:616-621

		// ar_sip is at offset 14-17 (sender IP)
		arSip := uint32(arp[14])<<24 | uint32(arp[15])<<16 | uint32(arp[16])<<8 | uint32(arp[17])

		if s.ClientEthAddr == ZeroEthAddr && arSip == ipToUint32(s.ClientIPAddr) {
			// ar_sha is at offset 8-13 (sender hardware address)
			copy(s.ClientEthAddr[:], arp[8:8+EthALen])
		}
	}
}

// Input processes an incoming Ethernet packet from the guest.
// This is the main entry point for guest-to-host packets.
// Reference: tinyemu-2019-12-21/slirp/slirp.c:628-660
func (s *Slirp) Input(pkt []byte) {
	s.tracePacket("INPUT", pkt)

	if len(pkt) < EthHLen {
		return
	}

	// Get protocol from ethernet header (bytes 12-13)
	proto := int(pkt[12])<<8 | int(pkt[13])

	switch proto {
	case EthPARP:
		s.ARPInput(pkt)

	case EthPIP:
		m := s.MGet()
		if m == nil {
			return
		}

		// Calculate IP data length (skip ethernet header)
		ipLen := len(pkt) - EthHLen
		if ipLen <= 0 {
			m.Free()
			return
		}

		// Allocate space for IP packet
		// Reference: tinyemu-2019-12-21/slirp/slirp.c:646-648
		if m.FreeRoom() < ipLen {
			m.Inc(ipLen)
		}

		// Copy IP packet (after ethernet header) to mbuf
		// Reference: tinyemu-2019-12-21/slirp/slirp.c:649-653
		m.Len = ipLen
		copy(m.Data[:ipLen], pkt[EthHLen:])

		s.IPInput(m)
	}
}

// TCPInput is implemented in tcp_input.go
// Reference: tinyemu-2019-12-21/slirp/tcp_input.c
