package x86_64

// Port-I/O regression. Without IN/OUT the kernel can't talk to the
// UART (so no boot console), can't program the PIC (so no IRQs
// other than spurious), can't program the PIT (so no timer). The
// missing opcodes manifest as a silent kernel that just runs to
// HLT and never produces output.

import (
	"testing"

	"github.com/sorins/tinyemu-go/mem"
)

func newCPUWithIO(t *testing.T, reads map[uint16]uint32, writes *map[uint16]uint32) *CPU {
	t.Helper()
	mm := mem.NewPhysMemoryMap()
	t.Cleanup(mm.Close)
	if _, err := mm.RegisterRAM(0, 1<<20, 0); err != nil {
		t.Fatalf("RegisterRAM: %v", err)
	}
	c := NewCPU(mm)
	c.SetCR64(0, CR0_PE)
	c.SetEFER(EFER_LME | EFER_LMA)
	c.SetSegAccess(CS, csLBit)
	c.SetSegBase(CS, 0)
	w := *writes
	c.SetIOHandlers(
		func(port uint16) uint8 { return uint8(reads[port]) },
		func(port uint16, v uint8) { w[port] = uint32(v); *writes = w },
		func(port uint16) uint16 { return uint16(reads[port]) },
		func(port uint16, v uint16) { w[port] = uint32(v); *writes = w },
		func(port uint16) uint32 { return reads[port] },
		func(port uint16, v uint32) { w[port] = v; *writes = w },
	)
	return c
}

func TestPort_OUT_AL_to_DX(t *testing.T) {
	writes := map[uint16]uint32{}
	c := newCPUWithIO(t, nil, &writes)
	c.SetReg64(RAX, 0x42)
	c.SetReg64(RDX, 0x3F8)
	// EE  out dx, al
	// F4  hlt
	const base uint64 = 0x1000
	c.memMap.Write8(base, 0xEE)
	c.memMap.Write8(base+1, 0xF4)
	c.SetRIP(base)
	if err := c.Run(10); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if writes[0x3F8] != 0x42 {
		t.Errorf("OUT DX,AL didn't reach port 0x3F8 with 0x42; writes=%v", writes)
	}
}

func TestPort_OUT_imm8(t *testing.T) {
	writes := map[uint16]uint32{}
	c := newCPUWithIO(t, nil, &writes)
	c.SetReg64(RAX, 0xAB)
	// E6 21   out 0x21, al   (PIC1 IMR)
	// F4
	const base uint64 = 0x1000
	c.memMap.Write8(base, 0xE6)
	c.memMap.Write8(base+1, 0x21)
	c.memMap.Write8(base+2, 0xF4)
	c.SetRIP(base)
	if err := c.Run(10); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if writes[0x21] != 0xAB {
		t.Errorf("OUT 0x21, AL didn't write; writes=%v", writes)
	}
}

func TestPort_IN_AL_from_DX(t *testing.T) {
	reads := map[uint16]uint32{0x3FD: 0x60} // LSR THR-empty + transmitter-idle bits
	writes := map[uint16]uint32{}
	c := newCPUWithIO(t, reads, &writes)
	c.SetReg64(RDX, 0x3FD)
	// EC      in al, dx
	// F4
	const base uint64 = 0x1000
	c.memMap.Write8(base, 0xEC)
	c.memMap.Write8(base+1, 0xF4)
	c.SetRIP(base)
	if err := c.Run(10); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if c.GetReg8(AL) != 0x60 {
		t.Errorf("IN AL, DX = %#x, want 0x60", c.GetReg8(AL))
	}
}

func TestPort_OUT_EAX_32bit(t *testing.T) {
	writes := map[uint16]uint32{}
	c := newCPUWithIO(t, nil, &writes)
	c.SetReg64(RAX, 0xDEADBEEF)
	c.SetReg64(RDX, 0xCF8)
	// EF   out dx, eax  (PCI config address)
	// F4
	const base uint64 = 0x1000
	c.memMap.Write8(base, 0xEF)
	c.memMap.Write8(base+1, 0xF4)
	c.SetRIP(base)
	if err := c.Run(10); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if writes[0xCF8] != 0xDEADBEEF {
		t.Errorf("OUT DX, EAX didn't write 32 bits; got %#x", writes[0xCF8])
	}
}
