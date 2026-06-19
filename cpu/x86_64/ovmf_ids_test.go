package x86_64

import (
	"testing"

	"github.com/sorins/tinyemu-go/mem"
)

// Tests for the CPU-identification values OVMF (and other firmware) reads
// early in SEC/PEI. Each was wrong (returned 0) and silently diverged the
// firmware until matched to a real CPU / the QEMU vCPU. Found via a
// temu-vs-QEMU register-state diff; see project_uefi_ovmf_bringup memory.

// CPUID leaf 1 EBX[15:8] = CLFLUSH line size in 8-byte units (8 → 64 bytes).
func TestCPUID_Leaf1_EBX_CLFlushSize(t *testing.T) {
	_, ebx, _, _ := runCPUID(t, 1)
	if got := (ebx >> 8) & 0xFF; got != 8 {
		t.Errorf("CPUID.1 EBX CLFLUSH line size = %d (8-byte units), want 8; EBX=%#x", got, ebx)
	}
}

// Max extended leaf must reach 0x80000008 so the address-size leaf below
// is queryable.
func TestCPUID_MaxExtendedLeaf(t *testing.T) {
	eax, _, _, _ := runCPUID(t, 0x80000000)
	if eax < 0x80000008 {
		t.Errorf("CPUID.80000000 EAX (max ext leaf) = %#x, want >= 0x80000008", eax)
	}
}

// Leaf 0x80000008 EAX: physical-address bits (7:0) and linear-address
// bits (15:8). 40/48 matches the QEMU default vCPU; firmware sizes its
// GCD memory space and page tables from this, so it must be non-zero.
func TestCPUID_AddressSizes(t *testing.T) {
	eax, _, _, _ := runCPUID(t, 0x80000008)
	if phys := eax & 0xFF; phys != 40 {
		t.Errorf("phys-address bits = %d, want 40 (EAX=%#x)", phys, eax)
	}
	if lin := (eax >> 8) & 0xFF; lin != 48 {
		t.Errorf("linear-address bits = %d, want 48 (EAX=%#x)", lin, eax)
	}
}

// runRDMSR executes RDMSR with ECX=msr and returns EDX:EAX.
func runRDMSR(t *testing.T, msr uint32) (eax, edx uint32) {
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
	c.SetReg32(ECX, msr)
	const code uint64 = 0x1000
	_ = mm.Write8(code, 0x0F) // 0F 32 = rdmsr
	_ = mm.Write8(code+1, 0x32)
	_ = mm.Write8(code+2, 0xF4)
	c.SetRIP(code)
	if err := c.Step(); err != nil {
		t.Fatalf("step RDMSR: %v", err)
	}
	return c.GetReg32(EAX), c.GetReg32(EDX)
}

// IA32_APIC_BASE (0x1B): we don't model a local APIC, but the BSP bit (8)
// must be set — this is the bootstrap processor (QEMU reports 0x100).
func TestRDMSR_APICBase_BSPBit(t *testing.T) {
	eax, edx := runRDMSR(t, 0x1B)
	if eax&0x100 == 0 {
		t.Errorf("IA32_APIC_BASE EAX = %#x, want BSP bit (0x100) set", eax)
	}
	if edx != 0 {
		t.Errorf("IA32_APIC_BASE EDX = %#x, want 0", edx)
	}
}
