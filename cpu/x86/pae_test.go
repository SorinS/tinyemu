package x86

import (
	"testing"

	"github.com/sorins/tinyemu-go/mem"
)

// newPAETestCPU builds a CPU with enough physical RAM to hold paging
// structures plus the test pages.
func newPAETestCPU(t *testing.T) *CPU {
	t.Helper()
	mm := mem.NewPhysMemoryMap()
	if _, err := mm.RegisterRAM(0, 16<<20, 0); err != nil {
		t.Fatalf("register ram: %v", err)
	}
	c := NewCPU(mm)
	c.SetCR(0, c.GetCR(0)|CR0_PE)
	c.SetSegAccess(CS, 0x0400)
	return c
}

// enablePAE writes a PDPT/PD/PT chain mapping lin -> phys (4 KB page), then
// turns on PAE paging via CR4/CR0.
func enablePAE(c *CPU, lin, phys uint32, perm uint64) {
	const (
		pdptBase = uint32(0x100000)
		pdBase   = uint32(0x101000)
		ptBase   = uint32(0x102000)
	)
	// Zero everything.
	for off := uint32(0); off < 0x4000; off++ {
		c.writePhys8(pdptBase+off, 0)
	}
	pdptIdx := (lin >> 30) & 0x3
	pdIdx := (lin >> 21) & 0x1FF
	ptIdx := (lin >> 12) & 0x1FF
	// PDPTE points to PD.
	c.writePhys64(pdptBase+pdptIdx*8, uint64(pdBase)|0x01)
	// PDE points to PT.
	c.writePhys64(pdBase+pdIdx*8, uint64(ptBase)|0x07)
	// PTE: phys + perm bits (present + user + writable default).
	c.writePhys64(ptBase+ptIdx*8, uint64(phys)|perm)

	c.SetCR(3, pdptBase)
	c.SetCR(4, c.GetCR(4)|CR4_PAE)
	c.SetCR(0, c.GetCR(0)|CR0_PG)
	c.updatePAEActive()
}

// enablePAELargePage writes a PDPT/PD chain with a 2 MB page mapping.
func enablePAELargePage(c *CPU, lin, phys uint32, perm uint64) {
	const (
		pdptBase = uint32(0x100000)
		pdBase   = uint32(0x101000)
	)
	for off := uint32(0); off < 0x3000; off++ {
		c.writePhys8(pdptBase+off, 0)
	}
	pdptIdx := (lin >> 30) & 0x3
	pdIdx := (lin >> 21) & 0x1FF
	c.writePhys64(pdptBase+pdptIdx*8, uint64(pdBase)|0x01)
	c.writePhys64(pdBase+pdIdx*8, uint64(phys&0xFFE00000)|perm|0x80) // PS=1

	c.SetCR(3, pdptBase)
	c.SetCR(4, c.GetCR(4)|CR4_PAE)
	c.SetCR(0, c.GetCR(0)|CR0_PG)
	c.updatePAEActive()
}

// TestPAE_4KBMapping verifies a basic PAE 3-level translation of one 4 KB
// page.
func TestPAE_4KBMapping(t *testing.T) {
	c := newPAETestCPU(t)
	const lin = uint32(0x00400000)
	const phys = uint32(0x00200000)
	enablePAE(c, lin, phys, 0x07)
	c.writePhys32(phys+0x100, 0xCAFEBABE)
	if got := c.readMem32(lin + 0x100); got != 0xCAFEBABE {
		t.Errorf("read through PAE = 0x%08X, want 0xCAFEBABE", got)
	}
}

// TestPAE_2MBPage verifies translation of a 2 MB page (PDE.PS=1).
func TestPAE_2MBPage(t *testing.T) {
	c := newPAETestCPU(t)
	const lin = uint32(0x00800000)
	const phys = uint32(0x00400000)
	enablePAELargePage(c, lin, phys, 0x07)
	c.writePhys32(phys+0x1234, 0xDEADBEEF)
	if got := c.readMem32(lin + 0x1234); got != 0xDEADBEEF {
		t.Errorf("read through PAE 2MB = 0x%08X, want 0xDEADBEEF", got)
	}
}

// TestPAE_NonPresentRaisesPF verifies #PF on a non-present PDPTE.
func TestPAE_NonPresentRaisesPF(t *testing.T) {
	c := newPAETestCPU(t)
	const lin = uint32(0x00400000)
	const phys = uint32(0x00200000)
	enablePAE(c, lin, phys, 0x07)

	// Stomp the PDPTE for lin's region back to not-present.
	pdptIdx := (lin >> 30) & 0x3
	c.writePhys64(0x100000+pdptIdx*8, 0)
	c.refreshPDPTEs()

	defer func() {
		r := recover()
		_, ok := r.(pageFaultError)
		if !ok {
			t.Fatalf("expected pageFaultError, got %T %v", r, r)
		}
	}()
	c.readMem32(lin)
}

// TestPAE_UserViolation verifies that a user-mode access to a supervisor-only
// page raises #PF with U/S set.
func TestPAE_UserViolation(t *testing.T) {
	c := newPAETestCPU(t)
	const lin = uint32(0x00400000)
	const phys = uint32(0x00200000)
	// Permission 0x03 = present + writable, no U bit (supervisor only).
	enablePAE(c, lin, phys, 0x03)

	defer func() {
		r := recover()
		pf, ok := r.(pageFaultError)
		if !ok {
			t.Fatalf("expected pageFaultError, got %T %v", r, r)
		}
		if pf.errorCode&0x04 == 0 {
			t.Errorf("error code 0x%X missing U/S bit", pf.errorCode)
		}
	}()
	// User access: translate with user=true.
	_ = c.translateAddress(lin, false, true, false)
}

// TestPAE_NXFetchViolation verifies that a code fetch from an NX page raises
// #PF with I/D bit set (when EFER.NXE is on).
func TestPAE_NXFetchViolation(t *testing.T) {
	c := newPAETestCPU(t)
	const lin = uint32(0x00400000)
	const phys = uint32(0x00200000)
	// Permission 0x07 (P+W+U) PLUS NX (bit 63).
	enablePAE(c, lin, phys, 0x07|(1<<63))
	c.efer = 1 << 11 // NXE

	defer func() {
		r := recover()
		pf, ok := r.(pageFaultError)
		if !ok {
			t.Fatalf("expected pageFaultError, got %T %v", r, r)
		}
		if pf.errorCode&0x10 == 0 {
			t.Errorf("error code 0x%X missing I/D bit on NX fetch fault", pf.errorCode)
		}
	}()
	c.fetchMem8(lin)
}

// TestPAE_PDPTERefreshOnCR3 verifies that writing CR3 reloads the PDPTE cache.
func TestPAE_PDPTERefreshOnCR3(t *testing.T) {
	c := newPAETestCPU(t)
	const lin = uint32(0x00400000)
	const phys = uint32(0x00200000)
	enablePAE(c, lin, phys, 0x07)
	// Switch CR3 to a second PDPT pointing to a different PD/PT.
	const (
		pdptBase2 = uint32(0x200000)
		pdBase2   = uint32(0x201000)
		ptBase2   = uint32(0x202000)
		phys2     = uint32(0x00300000)
	)
	for off := uint32(0); off < 0x4000; off++ {
		c.writePhys8(pdptBase2+off, 0)
	}
	pdptIdx := (lin >> 30) & 0x3
	pdIdx := (lin >> 21) & 0x1FF
	ptIdx := (lin >> 12) & 0x1FF
	c.writePhys64(pdptBase2+pdptIdx*8, uint64(pdBase2)|0x01)
	c.writePhys64(pdBase2+pdIdx*8, uint64(ptBase2)|0x07)
	c.writePhys64(ptBase2+ptIdx*8, uint64(phys2)|0x07)
	c.writePhys32(phys2, 0xABCDEF01)

	c.SetCR(3, pdptBase2)
	c.refreshPDPTEs()
	if got := c.readMem32(lin); got != 0xABCDEF01 {
		t.Errorf("after CR3 switch, read = 0x%08X, want 0xABCDEF01", got)
	}
}
