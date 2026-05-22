package slirp

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
)

// Debug tracing infrastructure for SLIRP networking.
// This is a development/debugging tool to trace packet flow through the
// network stack and understand TCP handshake issues.
//
// Enable tracing by setting the Tracer field on a Slirp instance, or by
// setting DefaultTracer for new instances.

// Tracer is an interface for trace output.
type Tracer interface {
	// Trace logs a trace message with the given format and arguments.
	Trace(format string, args ...interface{})
}

// DefaultTracer is the default tracer used for new Slirp instances.
// Set this before creating Slirp instances to enable tracing globally.
// Auto-enabled to stderr if TINYEMU_SLIRP_TRACE=1.
var DefaultTracer Tracer

func init() {
	if os.Getenv("TINYEMU_SLIRP_TRACE") == "1" {
		DefaultTracer = StderrTracer()
		fmt.Fprintln(os.Stderr, "[SLIRP] tracing enabled via TINYEMU_SLIRP_TRACE=1")
	}
}

// WriterTracer is a Tracer that writes to an io.Writer.
type WriterTracer struct {
	Writer io.Writer
	Prefix string
}

// Trace implements Tracer.
func (t *WriterTracer) Trace(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	prefix := t.Prefix
	if prefix == "" {
		prefix = "[SLIRP] "
	}
	fmt.Fprintf(t.Writer, "%s%s\n", prefix, msg)
}

// StderrTracer returns a tracer that writes to stderr.
func StderrTracer() *WriterTracer {
	return &WriterTracer{Writer: os.Stderr, Prefix: "[SLIRP] "}
}

// trace logs a message if tracing is enabled for this Slirp instance.
func (s *Slirp) trace(format string, args ...interface{}) {
	if s.Tracer != nil {
		s.Tracer.Trace(format, args...)
	}
}

// tracePacket logs a packet with direction and decoded header information.
func (s *Slirp) tracePacket(direction string, pkt []byte) {
	if s.Tracer == nil {
		return
	}

	var parts []string
	parts = append(parts, fmt.Sprintf("%s %d bytes", direction, len(pkt)))

	// Try to decode the packet
	if len(pkt) >= EthHLen {
		etherType := binary.BigEndian.Uint16(pkt[12:14])
		switch etherType {
		case EthPARP:
			parts = append(parts, "ARP")
			if len(pkt) >= EthHLen+28 {
				arp := pkt[EthHLen:]
				op := binary.BigEndian.Uint16(arp[6:8])
				switch op {
				case ARPOpRequest:
					parts = append(parts, "REQUEST")
				case ARPOpReply:
					parts = append(parts, "REPLY")
				}
				senderIP := net.IP(arp[14:18])
				targetIP := net.IP(arp[24:28])
				parts = append(parts, fmt.Sprintf("sender=%v target=%v", senderIP, targetIP))
			}
		case EthPIP:
			if len(pkt) >= EthHLen+IPHeaderSize {
				ip := pkt[EthHLen:]
				srcIP := net.IP(ip[12:16])
				dstIP := net.IP(ip[16:20])
				proto := ip[9]
				parts = append(parts, fmt.Sprintf("IP %v -> %v", srcIP, dstIP))

				switch proto {
				case IPProtoICMP:
					parts = append(parts, "ICMP")
					if len(ip) >= IPHeaderSize+4 {
						icmpType := ip[IPHeaderSize]
						icmpCode := ip[IPHeaderSize+1]
						parts = append(parts, fmt.Sprintf("type=%d code=%d", icmpType, icmpCode))
					}
				case IPProtoTCP:
					parts = append(parts, "TCP")
					if len(ip) >= IPHeaderSize+20 {
						tcp := ip[IPHeaderSize:]
						srcPort := binary.BigEndian.Uint16(tcp[0:2])
						dstPort := binary.BigEndian.Uint16(tcp[2:4])
						seq := binary.BigEndian.Uint32(tcp[4:8])
						ack := binary.BigEndian.Uint32(tcp[8:12])
						flags := tcp[13]
						parts = append(parts, fmt.Sprintf(":%d -> :%d", srcPort, dstPort))
						parts = append(parts, fmt.Sprintf("seq=%d ack=%d", seq, ack))
						parts = append(parts, formatTCPFlags(flags))
					}
				case IPProtoUDP:
					parts = append(parts, "UDP")
					if len(ip) >= IPHeaderSize+8 {
						udp := ip[IPHeaderSize:]
						srcPort := binary.BigEndian.Uint16(udp[0:2])
						dstPort := binary.BigEndian.Uint16(udp[2:4])
						parts = append(parts, fmt.Sprintf(":%d -> :%d", srcPort, dstPort))
					}
				default:
					parts = append(parts, fmt.Sprintf("proto=%d", proto))
				}
			}
		default:
			parts = append(parts, fmt.Sprintf("ether=0x%04x", etherType))
		}
	}

	// Add hex dump of first 64 bytes
	hexLen := len(pkt)
	if hexLen > 64 {
		hexLen = 64
	}
	parts = append(parts, fmt.Sprintf("hex=%x", pkt[:hexLen]))

	s.Tracer.Trace("%s", strings.Join(parts, " "))
}

// traceTCPState logs a TCP state transition.
func (s *Slirp) traceTCPState(so *Socket, oldState, newState int16, reason string) {
	if s.Tracer == nil {
		return
	}
	s.Tracer.Trace("TCP state %v:%d -> %v:%d: %s -> %s (%s)",
		so.SoLAddr, so.SoLPort,
		so.SoFAddr, so.SoFPort,
		tcpStateName(oldState), tcpStateName(newState),
		reason)
}

// traceTCPInput logs entry to TCP input processing.
func (s *Slirp) traceTCPInput(srcIP, dstIP net.IP, srcPort, dstPort uint16, flags uint8, seq, ack uint32, dataLen int) {
	if s.Tracer == nil {
		return
	}
	s.Tracer.Trace("TCPInput: %v:%d -> %v:%d %s seq=%d ack=%d datalen=%d",
		srcIP, srcPort, dstIP, dstPort, formatTCPFlags(flags), seq, ack, dataLen)
}

// traceTCPOutput logs TCP output.
func (s *Slirp) traceTCPOutput(srcIP, dstIP net.IP, srcPort, dstPort uint16, flags uint8, seq, ack uint32, dataLen int) {
	if s.Tracer == nil {
		return
	}
	s.Tracer.Trace("TCPOutput: %v:%d -> %v:%d %s seq=%d ack=%d datalen=%d",
		srcIP, srcPort, dstIP, dstPort, formatTCPFlags(flags), seq, ack, dataLen)
}

// traceTCPCtl logs TCPCtl invocation.
func (s *Slirp) traceTCPCtl(so *Socket, result int, execMatch string) {
	if s.Tracer == nil {
		return
	}
	extra := ""
	if execMatch != "" {
		extra = fmt.Sprintf(" exec=%q", execMatch)
	}
	s.Tracer.Trace("TCPCtl: %v:%d -> %v:%d result=%d%s",
		so.SoLAddr, so.SoLPort, so.SoFAddr, so.SoFPort, result, extra)
}

// traceSocketCreate logs socket creation.
func (s *Slirp) traceSocketCreate(so *Socket, proto string) {
	if s.Tracer == nil {
		return
	}
	s.Tracer.Trace("socket create: %s %v:%d -> %v:%d state=0x%x",
		proto, so.SoLAddr, so.SoLPort, so.SoFAddr, so.SoFPort, so.SoState)
}

// traceSocketDestroy logs socket destruction.
func (s *Slirp) traceSocketDestroy(so *Socket, proto string) {
	if s.Tracer == nil {
		return
	}
	s.Tracer.Trace("socket destroy: %s %v:%d -> %v:%d state=0x%x",
		proto, so.SoLAddr, so.SoLPort, so.SoFAddr, so.SoFPort, so.SoState)
}

// formatTCPFlags returns a string representation of TCP flags.
func formatTCPFlags(flags uint8) string {
	var parts []string
	if flags&THFin != 0 {
		parts = append(parts, "FIN")
	}
	if flags&THSyn != 0 {
		parts = append(parts, "SYN")
	}
	if flags&THRst != 0 {
		parts = append(parts, "RST")
	}
	if flags&THPush != 0 {
		parts = append(parts, "PSH")
	}
	if flags&THAck != 0 {
		parts = append(parts, "ACK")
	}
	if flags&THUrg != 0 {
		parts = append(parts, "URG")
	}
	if len(parts) == 0 {
		return "[]"
	}
	return "[" + strings.Join(parts, ",") + "]"
}

// tcpStateName returns the name of a TCP state.
func tcpStateName(state int16) string {
	names := []string{
		"CLOSED",
		"LISTEN",
		"SYN_SENT",
		"SYN_RECEIVED",
		"ESTABLISHED",
		"CLOSE_WAIT",
		"FIN_WAIT_1",
		"CLOSING",
		"LAST_ACK",
		"FIN_WAIT_2",
		"TIME_WAIT",
	}
	if state >= 0 && int(state) < len(names) {
		return names[state]
	}
	return fmt.Sprintf("UNKNOWN(%d)", state)
}
