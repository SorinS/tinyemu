package slirp

import (
	"bytes"
	"encoding/binary"
	"net"
	"strings"
	"testing"
)

// TestTracerInterface tests that the Tracer interface works.
func TestTracerInterface(t *testing.T) {
	var buf bytes.Buffer
	tracer := &WriterTracer{Writer: &buf, Prefix: "[TEST] "}

	tracer.Trace("hello %s", "world")

	output := buf.String()
	if !strings.Contains(output, "[TEST] hello world") {
		t.Errorf("Trace output = %q, want to contain %q", output, "[TEST] hello world")
	}
}

// TestSlirpTracing tests that Slirp traces packets correctly.
func TestSlirpTracing(t *testing.T) {
	var buf bytes.Buffer
	tracer := &WriterTracer{Writer: &buf, Prefix: "[SLIRP] "}

	s := NewSlirp()
	s.Tracer = tracer

	// Set up output capture
	copy(s.ClientEthAddr[:], []byte{0x52, 0x54, 0x00, 0x12, 0x34, 0x56})
	s.OutputFunc = func(opaque interface{}, pkt []byte) {}

	// Build a simple ARP request packet
	frame := make([]byte, EthHLen+28)
	// Ethernet header
	copy(frame[0:6], []byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff}) // broadcast
	copy(frame[6:12], s.ClientEthAddr[:])                        // source
	binary.BigEndian.PutUint16(frame[12:14], EthPARP)

	// ARP header
	arp := frame[EthHLen:]
	binary.BigEndian.PutUint16(arp[0:2], 1)      // hardware type
	binary.BigEndian.PutUint16(arp[2:4], EthPIP) // protocol type
	arp[4] = 6                                   // hw addr len
	arp[5] = 4                                   // proto addr len
	binary.BigEndian.PutUint16(arp[6:8], ARPOpRequest)
	copy(arp[14:18], net.IPv4(10, 0, 2, 15).To4()) // sender IP
	copy(arp[24:28], s.VHostAddr.To4())            // target IP

	// Input the packet
	s.Input(frame)

	output := buf.String()
	if !strings.Contains(output, "INPUT") {
		t.Errorf("Trace output missing INPUT, got: %q", output)
	}
	if !strings.Contains(output, "ARP") {
		t.Errorf("Trace output missing ARP, got: %q", output)
	}
	if !strings.Contains(output, "OUTPUT") {
		t.Errorf("Trace output missing OUTPUT (ARP reply), got: %q", output)
	}
}

// TestTCPTracingOutput tests TCP output tracing.
func TestTCPTracingOutput(t *testing.T) {
	var buf bytes.Buffer
	tracer := &WriterTracer{Writer: &buf, Prefix: "[SLIRP] "}

	h := newTestHelper()
	h.slirp.Tracer = tracer

	// Configure exec forwarding for an address
	execAddr := net.IPv4(10, 0, 2, 100)
	execPort := 8080
	h.slirp.AddExec(3, "test-handler", execAddr, execPort)

	// Build TCP SYN packet
	synSrcPort := uint16(12345)
	tcpPayload := make([]byte, 20)
	binary.BigEndian.PutUint16(tcpPayload[0:2], synSrcPort)
	binary.BigEndian.PutUint16(tcpPayload[2:4], uint16(execPort))
	binary.BigEndian.PutUint32(tcpPayload[4:8], 1000) // seq
	tcpPayload[12] = (5 << 4)                         // data offset
	tcpPayload[13] = THSyn                            // flags

	// Compute checksum
	pseudoLen := 12 + len(tcpPayload)
	pseudo := make([]byte, pseudoLen)
	copy(pseudo[0:4], h.guestIP.To4())
	copy(pseudo[4:8], execAddr.To4())
	pseudo[9] = IPProtoTCP
	binary.BigEndian.PutUint16(pseudo[10:12], uint16(len(tcpPayload)))
	copy(pseudo[12:], tcpPayload)
	cksum := CksumData(pseudo)
	binary.BigEndian.PutUint16(tcpPayload[16:18], cksum)

	ipPayload := h.buildIPPacket(IPProtoTCP, h.guestIP, execAddr, tcpPayload)
	execMAC := [6]byte{0x52, 0x55}
	copy(execMAC[2:], execAddr.To4())
	frame := h.buildEthFrame(execMAC, EthPIP, ipPayload)

	// Send the SYN packet
	h.slirp.Input(frame)

	output := buf.String()
	// Should see INPUT and OUTPUT traces
	if !strings.Contains(output, "INPUT") {
		t.Errorf("Trace output missing INPUT, got: %q", output)
	}
	if !strings.Contains(output, "TCPInput") {
		t.Errorf("Trace output missing TCPInput, got: %q", output)
	}
	if !strings.Contains(output, "TCPOutput") {
		t.Errorf("Trace output missing TCPOutput, got: %q", output)
	}
	// Note: TCPCtl is only called when the connection completes (ACK received),
	// not during the initial SYN. It would be traced if we completed the handshake.
	if !strings.Contains(output, "SYN") {
		t.Errorf("Trace output missing SYN flag, got: %q", output)
	}
	if !strings.Contains(output, "socket create") {
		t.Errorf("Trace output missing socket create, got: %q", output)
	}
	// Verify SYN-ACK output
	if !strings.Contains(output, "[SYN,ACK]") {
		t.Errorf("Trace output missing [SYN,ACK], got: %q", output)
	}
}

// TestFormatTCPFlags tests the TCP flag formatting.
func TestFormatTCPFlags(t *testing.T) {
	tests := []struct {
		flags    uint8
		expected string
	}{
		{0, "[]"},
		{THSyn, "[SYN]"},
		{THAck, "[ACK]"},
		{THSyn | THAck, "[SYN,ACK]"},
		{THFin | THAck, "[FIN,ACK]"},
		{THRst, "[RST]"},
		{THPush | THAck, "[PSH,ACK]"},
		{THUrg, "[URG]"},
		{THFin | THSyn | THRst | THPush | THAck | THUrg, "[FIN,SYN,RST,PSH,ACK,URG]"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := formatTCPFlags(tt.flags)
			if result != tt.expected {
				t.Errorf("formatTCPFlags(0x%02x) = %q, want %q", tt.flags, result, tt.expected)
			}
		})
	}
}

// TestTCPStateName tests the TCP state name function.
func TestTCPStateName(t *testing.T) {
	tests := []struct {
		state    int16
		expected string
	}{
		{TCPSClosed, "CLOSED"},
		{TCPSListen, "LISTEN"},
		{TCPSSynSent, "SYN_SENT"},
		{TCPSSynReceived, "SYN_RECEIVED"},
		{TCPSEstablished, "ESTABLISHED"},
		{TCPSCloseWait, "CLOSE_WAIT"},
		{TCPSFinWait1, "FIN_WAIT_1"},
		{TCPSClosing, "CLOSING"},
		{TCPSLastAck, "LAST_ACK"},
		{TCPSFinWait2, "FIN_WAIT_2"},
		{TCPSTimeWait, "TIME_WAIT"},
		{-1, "UNKNOWN(-1)"},
		{99, "UNKNOWN(99)"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := tcpStateName(tt.state)
			if result != tt.expected {
				t.Errorf("tcpStateName(%d) = %q, want %q", tt.state, result, tt.expected)
			}
		})
	}
}

// TestNoTracingByDefault tests that tracing doesn't happen without a tracer.
func TestNoTracingByDefault(t *testing.T) {
	s := NewSlirp()

	// With no tracer, these should not panic
	s.tracePacket("test", []byte{1, 2, 3})
	s.traceTCPInput(net.IPv4(1, 2, 3, 4), net.IPv4(5, 6, 7, 8), 80, 8080, THSyn, 100, 0, 0)
	s.traceTCPOutput(net.IPv4(1, 2, 3, 4), net.IPv4(5, 6, 7, 8), 80, 8080, THSyn|THAck, 100, 101, 0)
	s.traceTCPCtl(&Socket{}, 1, "test")
	s.traceTCPState(&Socket{}, 0, 1, "test")
	s.traceSocketCreate(&Socket{}, "TCP")
	s.traceSocketDestroy(&Socket{}, "TCP")
}
