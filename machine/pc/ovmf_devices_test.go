package pc

import "testing"

// PIIX4 ACPI/PM function at 00:01.3 — OVMF's PlatformPei probes it right
// after the host bridge; a missing device (all-ones reads) diverged its
// platform init. See project_uefi_ovmf_bringup memory.
func TestPIIX4_PM_Present(t *testing.T) {
	p, err := New(Config{RAMSize: 64 << 20, MachineType: "x86_64"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer p.Close()

	// vendor/device dword at 00:01.3 offset 0.
	p.io.Write32(0xCF8, pciCmd(0, 1, 3, 0))
	vd := p.io.Read32(0xCFC)
	if want := uint32(0x7113<<16 | 0x8086); vd != want {
		t.Fatalf("00:01.3 vendor/device = %#08x, want %#08x (8086:7113)", vd, want)
	}

	// PMBA (reg 0x40) must be programmable: write a base, bit 0 stays 1.
	p.io.Write32(0xCF8, pciCmd(0, 1, 3, 0x40))
	p.io.Write32(0xCFC, 0xB000)
	p.io.Write32(0xCF8, pciCmd(0, 1, 3, 0x40))
	if pmba := p.io.Read32(0xCFC); pmba != 0xB001 {
		t.Errorf("PMBA after writing 0xB000 = %#x, want 0xB001 (base + I/O bit)", pmba)
	}

	// PMREGMISC (reg 0x80) PMIOSE bit must be settable.
	p.io.Write32(0xCF8, pciCmd(0, 1, 3, 0x80))
	p.io.Write8(0xCFC, 0x01)
	p.io.Write32(0xCF8, pciCmd(0, 1, 3, 0x80))
	if v := p.io.Read32(0xCFC) & 0x1; v != 1 {
		t.Errorf("PMREGMISC PMIOSE bit didn't stick")
	}
}

// fw_cfg well-known selectors OVMF reads directly (not via file_dir).
func TestFwCfgWellKnownSelectors(t *testing.T) {
	f := newFWCfg()
	if d := f.dataForSelector(0x0005); len(d) != 2 || d[0] != 1 || d[1] != 0 {
		t.Errorf("nb_cpus (0x5) = %v, want [1 0]", d)
	}
	if d := f.dataForSelector(0x000F); len(d) != 2 || d[0] != 1 {
		t.Errorf("max_cpus (0xF) = %v, want [1 0]", d)
	}
	if d := f.dataForSelector(0x000E); len(d) != 2 { // boot_menu present (disabled)
		t.Errorf("boot_menu (0xE) = %v, want 2 bytes", d)
	}
}
