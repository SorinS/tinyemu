package pc

import (
	"bytes"
	"testing"
)

func newTestUART(t *testing.T) (*UART16550, *PIC8259, *IOPortDispatcher) {
	t.Helper()
	cpu := newPICTestCPU(t)
	pic := NewPIC8259(cpu, 0x20)
	initPIC(pic, 0x20, 0x00)
	u := NewUART16550(pic, 4, &bytes.Buffer{})
	io := NewIOPortDispatcher()
	u.Register(io)
	return u, pic, io
}

// TestUARTTransmitWritesOutput verifies a write to port 0x3F8 reaches the
// underlying io.Writer.
func TestUARTTransmitWritesOutput(t *testing.T) {
	cpu := newPICTestCPU(t)
	pic := NewPIC8259(cpu, 0x20)
	initPIC(pic, 0x20, 0x00)
	var buf bytes.Buffer
	u := NewUART16550(pic, 4, &buf)
	io := NewIOPortDispatcher()
	u.Register(io)
	io.Write8(0x3F8, 'A')
	io.Write8(0x3F8, 'Z')
	if got := buf.String(); got != "AZ" {
		t.Errorf("buf = %q, want %q", got, "AZ")
	}
}

// TestUARTPushSetsLSRDataReady: Push() should make the LSR Data Ready bit go
// high.
func TestUARTPushSetsLSRDataReady(t *testing.T) {
	u, _, io := newTestUART(t)
	if io.Read8(0x3FD)&lsrDR != 0 {
		t.Errorf("DR set before any input")
	}
	u.Push([]byte("X"))
	if io.Read8(0x3FD)&lsrDR == 0 {
		t.Errorf("DR not set after Push")
	}
}

// TestUARTReadPopsFIFO: reading port 0x3F8 returns the next FIFO byte and
// clears LSR.DR once empty.
func TestUARTReadPopsFIFO(t *testing.T) {
	u, _, io := newTestUART(t)
	u.Push([]byte("Hi"))
	if got := io.Read8(0x3F8); got != 'H' {
		t.Errorf("first read = %c, want H", got)
	}
	if io.Read8(0x3FD)&lsrDR == 0 {
		t.Errorf("DR cleared too early (one byte left)")
	}
	if got := io.Read8(0x3F8); got != 'i' {
		t.Errorf("second read = %c, want i", got)
	}
	if io.Read8(0x3FD)&lsrDR != 0 {
		t.Errorf("DR still set after FIFO empty")
	}
}

// TestUARTRxIRQ: when IER.RxData is enabled, Push raises IRQ4; reading drains
// the FIFO and lowers it.
func TestUARTRxIRQ(t *testing.T) {
	u, pic, io := newTestUART(t)
	cpu := pic.cpu
	io.Write8(0x3F9, ierRxData) // enable RX data interrupt
	if cpu.GetINTR() != 0 {
		t.Fatalf("INTR set before any input")
	}
	u.Push([]byte("X"))
	if cpu.GetINTR() == 0 {
		t.Errorf("INTR not asserted after Push with IER.RxData set")
	}
	io.Read8(0x3F8) // drain
	if cpu.GetINTR() != 0 {
		t.Errorf("INTR still asserted after FIFO drained")
	}
}

// TestUARTRxIRQ_MaskedNoIRQ: with IER.RxData clear, Push must not raise IRQ.
func TestUARTRxIRQ_MaskedNoIRQ(t *testing.T) {
	u, pic, _ := newTestUART(t)
	cpu := pic.cpu
	u.Push([]byte("X"))
	if cpu.GetINTR() != 0 {
		t.Errorf("INTR asserted while IER.RxData was clear")
	}
}

// TestUARTIIRReceivedData: when bytes are pending and IER.RxData is on, IIR
// reports "Received Data Available" with bit 0 = 0.
func TestUARTIIRReceivedData(t *testing.T) {
	u, _, io := newTestUART(t)
	io.Write8(0x3F9, ierRxData)
	u.Push([]byte("X"))
	iir := io.Read8(0x3FA)
	if iir&0x01 != 0 {
		t.Errorf("IIR bit 0 = 1 (no int) but data is pending: 0x%02X", iir)
	}
	if iir&0x0E != 0x04 {
		t.Errorf("IIR priority = 0x%X, want 0x4 (received data)", iir&0x0E)
	}
}

// TestUARTPushIsThreadSafe ensures concurrent Push calls don't corrupt the
// FIFO. We don't measure exact ordering but require all bytes to be readable.
func TestUARTPushIsThreadSafe(t *testing.T) {
	u, _, io := newTestUART(t)
	io.Write8(0x3F9, ierRxData)
	done := make(chan struct{})
	const N = 100
	go func() {
		for i := 0; i < N; i++ {
			u.Push([]byte{'a'})
		}
		close(done)
	}()
	for i := 0; i < N; i++ {
		u.Push([]byte{'b'})
	}
	<-done

	got := 0
	for io.Read8(0x3FD)&lsrDR != 0 {
		_ = io.Read8(0x3F8)
		got++
	}
	if got != 2*N {
		t.Errorf("read %d bytes back, want %d", got, 2*N)
	}
}
