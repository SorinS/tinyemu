package x86

import "testing"

// TestPFErrorCode_Write verifies the W/R bit (1) is set on a write fault.
func TestPFErrorCode_Write(t *testing.T) {
	c := newTestCPU(t)
	c.SetCR(0, CR0_PE|CR0_PG)
	c.SetCR(3, 0x10000) // page directory address
	c.SetCR(4, 0)

	// PDE at 0 = not present
	c.writePhys32(0x10000, 0)

	defer func() {
		r := recover()
		pf, ok := r.(pageFaultError)
		if !ok {
			t.Fatalf("expected pageFaultError, got %T %v", r, r)
		}
		if pf.errorCode&0x02 == 0 {
			t.Errorf("error code 0x%X missing W/R bit", pf.errorCode)
		}
		if pf.errorCode&0x10 != 0 {
			t.Errorf("error code 0x%X has I/D bit set on data write", pf.errorCode)
		}
	}()
	c.writeMem32(0x1000, 0xDEADBEEF)
}

// TestPFErrorCode_Fetch verifies the I/D bit (4) is set when a code fetch
// causes the fault.
func TestPFErrorCode_Fetch(t *testing.T) {
	c := newTestCPU(t)
	c.SetCR(0, CR0_PE|CR0_PG)
	c.SetCR(3, 0x10000)
	c.SetCR(4, 0)

	c.writePhys32(0x10000, 0) // PDE not present

	defer func() {
		r := recover()
		pf, ok := r.(pageFaultError)
		if !ok {
			t.Fatalf("expected pageFaultError, got %T %v", r, r)
		}
		if pf.errorCode&0x10 == 0 {
			t.Errorf("error code 0x%X missing I/D bit on fetch fault", pf.errorCode)
		}
		if pf.errorCode&0x02 != 0 {
			t.Errorf("error code 0x%X has W/R bit set on read fetch", pf.errorCode)
		}
	}()
	c.fetchMem8(0x2000)
}

// TestPFErrorCode_Present verifies bit 0 (P) is set when a permission check
// fails on a present page.
func TestPFErrorCode_Present(t *testing.T) {
	c := newTestCPU(t)
	c.SetCR(0, CR0_PE|CR0_PG|CR0_WP) // WP forces supervisor write check
	c.SetCR(3, 0x10000)
	c.SetCR(4, 0)
	c.cpl = 0

	// PDE: present, RO (no W bit).
	c.writePhys32(0x10000, 0x11000|0x01) // present, read-only
	// PTE for address 0x1000: present, RO.
	c.writePhys32(0x11000+1*4, 0x2000|0x01) // present, read-only

	defer func() {
		r := recover()
		pf, ok := r.(pageFaultError)
		if !ok {
			t.Fatalf("expected pageFaultError, got %T %v", r, r)
		}
		if pf.errorCode&0x01 == 0 {
			t.Errorf("error code 0x%X missing P bit on permission fault", pf.errorCode)
		}
		if pf.errorCode&0x02 == 0 {
			t.Errorf("error code 0x%X missing W/R bit on write fault", pf.errorCode)
		}
	}()
	c.writeMem32(0x1000, 0xDEADBEEF)
}
