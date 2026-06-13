package x86_64

import "testing"

// TestTranslateNonCanonicalMarker: Translate refuses a non-canonical linear
// address and marks it NonCanonical, so fault delivery routes it to #GP.
func TestTranslateNonCanonicalMarker(t *testing.T) {
	c, _ := longModeCPU(t)
	const nonCanon = uint64(0x0000800000000000) // bit 47 set, bits 63:48 clear
	_, perr := c.Translate(nonCanon, false, false, false)
	if perr == nil {
		t.Fatalf("Translate accepted a non-canonical address")
	}
	if !perr.NonCanonical {
		t.Errorf("PageFaultError.NonCanonical = false, want true")
	}
}

// TestNonCanonicalDeliversGP: a data access to a non-canonical address must
// deliver #GP (vector 13), not #PF (vector 14). Paging is enabled with a
// 2 MB identity map, and both #GP and #PF gates are installed at distinct
// handlers so the delivered vector is observable.
func TestNonCanonicalDeliversGP(t *testing.T) {
	c, mm := longModeCPU(t)

	// 2 MB identity map for low RAM: PML4 -> PDPT -> PD(PS=1).
	const pml4 = uint64(0x10000)
	const pdpt = uint64(0x11000)
	const pd = uint64(0x12000)
	_ = mm.Write64(pml4, pdpt|0x03)
	_ = mm.Write64(pdpt, pd|0x03)
	_ = mm.Write64(pd, 0x80|0x03) // 2 MB page, base 0, P|RW|PS
	c.cr[3] = pml4
	c.SetCR64(0, c.cr[0]|CR0_PG)

	const idtBase = uint64(0x4000)
	const gpHandler = uint64(0x90000)
	const pfHandler = uint64(0x91000)
	c.segBase[IDTR] = idtBase
	c.segLimit[IDTR] = 0x1000 - 1
	installIDTGate(t, mm, idtBase, 13, 0x0008, gpHandler, 0, 0x8E)
	installIDTGate(t, mm, idtBase, 14, 0x0008, pfHandler, 0, 0x8E)
	c.reg64[RSP] = 0x8000

	c.SetReg64(RBX, 0x0000800000000000) // non-canonical operand address
	// 48 8B 03 = mov rax, [rbx]
	if err := runInsn(t, c, mm, []byte{0x48, 0x8B, 0x03}); err != nil {
		t.Fatalf("Step: %v", err)
	}
	if c.GetRIP() == pfHandler {
		t.Fatalf("non-canonical access delivered #PF (vec 14); want #GP (vec 13)")
	}
	if c.GetRIP() != gpHandler {
		t.Errorf("RIP = %#x after non-canonical access, want #GP handler %#x", c.GetRIP(), gpHandler)
	}
}
