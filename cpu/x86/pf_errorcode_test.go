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

// TestPFErrorCode_UserBitFromCPL verifies the U/S bit (2) is set on a fault
// taken while CPL=3 — and cleared while CPL=0. This caused init/modprobe
// page-fault oopses early in this session because Linux's do_page_fault
// dispatches based on the U/S bit: a non-present user-page-fault with U/S=0
// looks like a kernel BUG and is not demand-paged. Boot couldn't reach a
// shell until the bit was wired through to translateAddress.
func TestPFErrorCode_UserBitFromCPL(t *testing.T) {
	c := newTestCPU(t)
	c.SetCR(0, CR0_PE|CR0_PG)
	c.SetCR(3, 0x10000)
	c.SetCR(4, 0)
	c.writePhys32(0x10000, 0) // PDE not present

	// CPL=3 access ⇒ U/S=1.
	c.cpl = 3
	func() {
		defer func() {
			r := recover()
			pf, ok := r.(pageFaultError)
			if !ok {
				t.Fatalf("expected pageFaultError, got %T %v", r, r)
			}
			if pf.errorCode&0x04 == 0 {
				t.Errorf("CPL=3: error code 0x%X missing U/S bit", pf.errorCode)
			}
		}()
		c.readMem32(0x1000)
	}()

	// CPL=0 access ⇒ U/S=0.
	c.cpl = 0
	func() {
		defer func() {
			r := recover()
			pf, ok := r.(pageFaultError)
			if !ok {
				t.Fatalf("expected pageFaultError, got %T %v", r, r)
			}
			if pf.errorCode&0x04 != 0 {
				t.Errorf("CPL=0: error code 0x%X should not have U/S bit", pf.errorCode)
			}
		}()
		c.readMem32(0x1000)
	}()
}

// TestSupervisorReadIgnoresCPL verifies that readMemSV* always accesses with
// user=false, even when CPL=3. This is what makes IDT/GDT/TSS reads behave
// as implicit supervisor accesses (Intel SDM Vol. 3 §6.12 / §7.2.3) so that
// delivering an interrupt from user mode does not double-fault on the
// kernel's supervisor-only IDT page.
func TestSupervisorReadIgnoresCPL(t *testing.T) {
	c := newTestCPU(t)
	c.SetCR(0, CR0_PE|CR0_PG)
	c.SetCR(3, 0x10000)
	c.SetCR(4, 0)
	// PDE: present, user-accessible (U=1), so user reads of the page reach
	// the PTE — but the PTE itself is supervisor-only.
	c.writePhys32(0x10000, 0x11000|0x07) // P|RW|U at PDE
	c.writePhys32(0x11000+1*4, 0x2000|0x03) // P|RW only (no U) at PTE
	c.writePhys32(0x2000, 0xCAFEF00D)

	// User-mode access at CPL=3 must fault with U/S bit.
	c.cpl = 3
	func() {
		defer func() {
			pf, ok := recover().(pageFaultError)
			if !ok {
				t.Fatalf("expected pageFaultError on user read")
			}
			if pf.errorCode&0x04 == 0 {
				t.Errorf("user read: error 0x%X missing U/S bit", pf.errorCode)
			}
		}()
		_ = c.readMem32(0x1000)
	}()

	// Supervisor read while CPL=3 must succeed (no fault, no U/S bit).
	c.cpl = 3
	got := c.readMemSV32(0x1000)
	if got != 0xCAFEF00D {
		t.Errorf("supervisor read got 0x%08X, want 0xCAFEF00D", got)
	}
}

// TestPFErrorCode_UserFetch verifies the U/S bit is set on an instruction
// fetch fault taken at CPL=3 — this is the exact signature ("supervisor
// instruction fetch in user mode" in dmesg) that blocked early userspace.
func TestPFErrorCode_UserFetch(t *testing.T) {
	c := newTestCPU(t)
	c.SetCR(0, CR0_PE|CR0_PG)
	c.SetCR(3, 0x10000)
	c.SetCR(4, 0)
	c.writePhys32(0x10000, 0)
	c.cpl = 3

	defer func() {
		r := recover()
		pf, ok := r.(pageFaultError)
		if !ok {
			t.Fatalf("expected pageFaultError, got %T %v", r, r)
		}
		if pf.errorCode&0x14 != 0x14 {
			t.Errorf("error code 0x%X must have both U/S and I/D set for user fetch fault", pf.errorCode)
		}
	}()
	c.fetchMem8(0x1000)
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
