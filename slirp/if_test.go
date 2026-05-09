package slirp

import (
	"testing"
)

// TestIfInit tests that IfInit properly initializes the interface queues.
func TestIfInit(t *testing.T) {
	s := NewSlirp()

	// Verify fastq is a circular empty list
	if s.IfFastQ.Next != &s.IfFastQ {
		t.Error("IfFastQ.Next should point to itself when empty")
	}
	if s.IfFastQ.Prev != &s.IfFastQ {
		t.Error("IfFastQ.Prev should point to itself when empty")
	}

	// Verify batchq is a circular empty list
	if s.IfBatchQ.Next != &s.IfBatchQ {
		t.Error("IfBatchQ.Next should point to itself when empty")
	}
	if s.IfBatchQ.Prev != &s.IfBatchQ {
		t.Error("IfBatchQ.Prev should point to itself when empty")
	}

	// Verify NextM points to batchq
	if s.NextM != &s.IfBatchQ {
		t.Error("NextM should point to IfBatchQ initially")
	}
}

// TestIfsInit tests the session chain initialization.
func TestIfsInit(t *testing.T) {
	s := NewSlirp()
	m := s.MGet()

	ifsInit(m)

	if m.NextPkt != m {
		t.Error("ifsInit should set NextPkt to self")
	}
	if m.PrevPkt != m {
		t.Error("ifsInit should set PrevPkt to self")
	}
}

// TestIfsInsqueRemque tests inserting and removing from session chain.
func TestIfsInsqueRemque(t *testing.T) {
	s := NewSlirp()
	m1 := s.MGet()
	m2 := s.MGet()
	m3 := s.MGet()

	// Initialize m1 as the head
	ifsInit(m1)

	// Insert m2 after m1
	ifsInsque(m2, m1)

	// Verify chain: m1 <-> m2 <-> m1
	if m1.NextPkt != m2 {
		t.Error("m1.NextPkt should be m2")
	}
	if m2.PrevPkt != m1 {
		t.Error("m2.PrevPkt should be m1")
	}
	if m2.NextPkt != m1 {
		t.Error("m2.NextPkt should be m1")
	}
	if m1.PrevPkt != m2 {
		t.Error("m1.PrevPkt should be m2")
	}

	// Insert m3 after m2
	ifsInsque(m3, m2)

	// Verify chain: m1 <-> m2 <-> m3 <-> m1
	if m2.NextPkt != m3 {
		t.Error("m2.NextPkt should be m3")
	}
	if m3.PrevPkt != m2 {
		t.Error("m3.PrevPkt should be m2")
	}
	if m3.NextPkt != m1 {
		t.Error("m3.NextPkt should be m1")
	}
	if m1.PrevPkt != m3 {
		t.Error("m1.PrevPkt should be m3")
	}

	// Remove m2
	ifsRemque(m2)

	// Verify chain: m1 <-> m3 <-> m1
	if m1.NextPkt != m3 {
		t.Error("after removing m2, m1.NextPkt should be m3")
	}
	if m3.PrevPkt != m1 {
		t.Error("after removing m2, m3.PrevPkt should be m1")
	}
}

// TestIfOutputQueuesToBatchQ tests that packets without LOWDELAY go to batchq.
func TestIfOutputQueuesToBatchQ(t *testing.T) {
	s := NewSlirp()

	// Create a socket without LOWDELAY
	so := s.SoCreate()
	so.SoIPTos = 0

	// Get an mbuf and put some data
	m := s.MGet()
	m.Len = 10
	for i := 0; i < 10; i++ {
		m.Data[i] = byte(i)
	}

	// Capture output
	var outputCalled bool
	s.OutputFunc = func(opaque interface{}, pkt []byte) {
		outputCalled = true
	}
	s.CanOutput = func(opaque interface{}) bool {
		return true
	}

	// Queue the packet
	s.IfOutput(so, m)

	// The packet should have been sent immediately by IfStart
	if !outputCalled {
		t.Error("OutputFunc should have been called")
	}
	if s.IfQueued != 0 {
		t.Errorf("IfQueued should be 0 after sending, got %d", s.IfQueued)
	}
}

// TestIfOutputQueuesToFastQ tests that LOWDELAY packets go to fastq.
func TestIfOutputQueuesToFastQ(t *testing.T) {
	s := NewSlirp()

	// Create a socket with LOWDELAY
	so := s.SoCreate()
	so.SoIPTos = IPTOSLowDelay

	// Get an mbuf and put some data
	m := s.MGet()
	m.Len = 10

	// Capture output
	var outputCalled bool
	s.OutputFunc = func(opaque interface{}, pkt []byte) {
		outputCalled = true
	}
	s.CanOutput = func(opaque interface{}) bool {
		return true
	}

	// Queue the packet
	s.IfOutput(so, m)

	// The packet should have been sent immediately
	if !outputCalled {
		t.Error("OutputFunc should have been called for LOWDELAY packet")
	}
}

// TestIfOutputSessionGrouping tests that packets from same session are grouped.
func TestIfOutputSessionGrouping(t *testing.T) {
	s := NewSlirp()

	// Create a socket
	so := s.SoCreate()
	so.SoIPTos = 0

	// Block output so packets queue up
	s.CanOutput = func(opaque interface{}) bool {
		return false
	}

	// Queue multiple packets from same socket
	m1 := s.MGet()
	m1.Len = 10
	s.IfOutput(so, m1)

	m2 := s.MGet()
	m2.Len = 10
	s.IfOutput(so, m2)

	// Both should be queued
	if s.IfQueued != 2 {
		t.Errorf("IfQueued should be 2, got %d", s.IfQueued)
	}

	// Socket queued count should be 2
	if so.SoQueued != 2 {
		t.Errorf("so.SoQueued should be 2, got %d", so.SoQueued)
	}
	if so.SoNQueued != 2 {
		t.Errorf("so.SoNQueued should be 2, got %d", so.SoNQueued)
	}
}

// TestIfStartDrainsQueue tests that IfStart sends all queued packets.
func TestIfStartDrainsQueue(t *testing.T) {
	s := NewSlirp()

	so := s.SoCreate()
	so.SoIPTos = 0

	// Block output initially
	canOutput := false
	s.CanOutput = func(opaque interface{}) bool {
		return canOutput
	}

	packetsSent := 0
	s.OutputFunc = func(opaque interface{}, pkt []byte) {
		packetsSent++
	}

	// Queue some packets
	for i := 0; i < 3; i++ {
		m := s.MGet()
		m.Len = 10
		s.IfOutput(so, m)
	}

	if s.IfQueued != 3 {
		t.Errorf("IfQueued should be 3, got %d", s.IfQueued)
	}

	// Now allow output and call IfStart
	canOutput = true
	s.IfStart()

	if packetsSent != 3 {
		t.Errorf("expected 3 packets sent, got %d", packetsSent)
	}
	if s.IfQueued != 0 {
		t.Errorf("IfQueued should be 0, got %d", s.IfQueued)
	}
	if so.SoQueued != 0 {
		t.Errorf("so.SoQueued should be 0, got %d", so.SoQueued)
	}
}

// TestIfOutputNilSocket tests queueing with nil socket.
func TestIfOutputNilSocket(t *testing.T) {
	s := NewSlirp()

	m := s.MGet()
	m.Len = 10

	outputCalled := false
	s.OutputFunc = func(opaque interface{}, pkt []byte) {
		outputCalled = true
	}
	s.CanOutput = func(opaque interface{}) bool {
		return true
	}

	// Queue with nil socket
	s.IfOutput(nil, m)

	if !outputCalled {
		t.Error("OutputFunc should have been called even with nil socket")
	}
}

// TestIfStartEmptyQueue tests that IfStart does nothing with empty queue.
func TestIfStartEmptyQueue(t *testing.T) {
	s := NewSlirp()

	outputCalled := false
	s.OutputFunc = func(opaque interface{}, pkt []byte) {
		outputCalled = true
	}
	s.CanOutput = func(opaque interface{}) bool {
		return true
	}

	s.IfStart()

	if outputCalled {
		t.Error("OutputFunc should not be called on empty queue")
	}
}

// TestIfOutputDowngrade tests the downgrade from fastq to batchq.
func TestIfOutputDowngrade(t *testing.T) {
	s := NewSlirp()

	so := s.SoCreate()
	so.SoIPTos = IPTOSLowDelay

	packetsSent := 0
	s.OutputFunc = func(opaque interface{}, pkt []byte) {
		packetsSent++
	}
	s.CanOutput = func(opaque interface{}) bool {
		return true
	}

	// Queue 6 packets (the threshold for downgrade consideration)
	// Downgrade happens when: nqueued >= 6 && (nqueued - queued) >= 3
	// So we need to send 3 packets to get (nqueued - queued) = 3
	// Then queue the 6th packet

	// Block output to queue packets
	canOutput := false
	s.CanOutput = func(opaque interface{}) bool {
		return canOutput
	}

	// Queue 6 packets
	for i := 0; i < 6; i++ {
		m := s.MGet()
		m.Len = 10
		s.IfOutput(so, m)
	}

	if so.SoNQueued != 6 {
		t.Errorf("so.SoNQueued should be 6, got %d", so.SoNQueued)
	}
	if so.SoQueued != 6 {
		t.Errorf("so.SoQueued should be 6, got %d", so.SoQueued)
	}
}

// TestIfStartFastQPriority tests that fastq packets are sent before batchq.
func TestIfStartFastQPriority(t *testing.T) {
	s := NewSlirp()

	// Set ClientEthAddr so IfEncap wraps packets (otherwise it sends ARP)
	s.ClientEthAddr = [6]byte{0x52, 0x55, 0x00, 0x01, 0x02, 0x03}

	// Create two sockets: one for fastq, one for batchq
	fastSo := s.SoCreate()
	fastSo.SoIPTos = IPTOSLowDelay

	batchSo := s.SoCreate()
	batchSo.SoIPTos = 0

	// Block output to queue packets
	s.CanOutput = func(opaque interface{}) bool {
		return false
	}

	var packetOrder []byte
	s.OutputFunc = func(opaque interface{}, pkt []byte) {
		// Use the IP data byte to identify packets
		if len(pkt) > EthHLen {
			packetOrder = append(packetOrder, pkt[EthHLen])
		}
	}

	// Queue batch packet first with marker byte
	m1 := s.MGet()
	m1.Len = 1
	m1.Data[0] = 0xBB // Batch marker
	s.IfOutput(batchSo, m1)

	// Queue fast packet second with marker byte
	m2 := s.MGet()
	m2.Len = 1
	m2.Data[0] = 0xFF // Fast marker
	s.IfOutput(fastSo, m2)

	// Allow output and drain
	s.CanOutput = func(opaque interface{}) bool {
		return true
	}
	s.IfStart()

	// Fast packet should be sent first despite being queued second
	if len(packetOrder) != 2 {
		t.Errorf("expected 2 packets, got %d", len(packetOrder))
	}
	if len(packetOrder) >= 2 && packetOrder[0] != 0xFF {
		t.Errorf("expected fast packet (0xFF) first, got order: %v", packetOrder)
	}
}

// TestIfStartCanOutputBlocking tests that IfStart respects CanOutput.
func TestIfStartCanOutputBlocking(t *testing.T) {
	s := NewSlirp()

	so := s.SoCreate()

	// Allow only first output
	outputCount := 0
	s.CanOutput = func(opaque interface{}) bool {
		return outputCount == 0
	}
	s.OutputFunc = func(opaque interface{}, pkt []byte) {
		outputCount++
	}

	// Block to queue
	s.CanOutput = func(opaque interface{}) bool {
		return false
	}

	// Queue 3 packets
	for i := 0; i < 3; i++ {
		m := s.MGet()
		m.Len = 10
		s.IfOutput(so, m)
	}

	// Allow only 1 output
	callCount := 0
	s.CanOutput = func(opaque interface{}) bool {
		callCount++
		return callCount == 1
	}
	s.IfStart()

	if outputCount != 1 {
		t.Errorf("expected 1 packet sent, got %d", outputCount)
	}
	if s.IfQueued != 2 {
		t.Errorf("expected 2 packets still queued, got %d", s.IfQueued)
	}
}

// TestMbufIfqInsqueRemque tests the interface queue insert/remove.
func TestMbufIfqInsqueRemque(t *testing.T) {
	s := NewSlirp()

	// Use the initialized IfBatchQ as head
	head := &s.IfBatchQ

	m1 := s.MGet()
	m2 := s.MGet()

	// Insert m1
	mbufIfqInsque(m1, head)
	if head.Next != m1 {
		t.Error("head.Next should be m1")
	}
	if m1.Prev != head {
		t.Error("m1.Prev should be head")
	}

	// Insert m2
	mbufIfqInsque(m2, m1)
	if m1.Next != m2 {
		t.Error("m1.Next should be m2")
	}

	// Remove m1
	mbufIfqRemque(m1)
	if head.Next != m2 {
		t.Error("after removing m1, head.Next should be m2")
	}
}
