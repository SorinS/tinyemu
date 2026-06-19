package pc

// Regression for machine/pc/vmlinux64.go. Build a minimal x86_64
// ELF in memory with two PT_LOAD segments, hand it to
// loadVMLinux64, and verify:
//   - each segment's contents land at its paddr
//   - the BSS portion of each segment is zero
//   - boot_params has the right header magic + cmdline pointer
//   - the CPU is in long mode with paging on, CS.L=1, RSI=boot_params,
//     RIP = ELF e_entry.

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/sorins/tinyemu-go/cpu/x86_64"
)

// minimalELF64 builds a stripped ELF64 with N PT_LOAD entries.
// Each loadSegment describes one segment: where it lives (paddr +
// vaddr), how many bytes from the file, how many in memory (the
// remainder is BSS), and the data to put in the file portion.
type loadSegment struct {
	paddr  uint64
	vaddr  uint64
	filesz uint64
	memsz  uint64
	data   []byte
}

func minimalELF64(entry uint64, segs []loadSegment) []byte {
	const (
		ehdrSize = 64
		phdrSize = 56
	)
	phoff := uint64(ehdrSize)
	dataOff := phoff + uint64(len(segs))*phdrSize

	var out bytes.Buffer
	// ELF header.
	out.Write([]byte{0x7F, 'E', 'L', 'F', 2, 1, 1, 0}) // ident
	out.Write(make([]byte, 8))                         // padding
	binary.Write(&out, binary.LittleEndian, uint16(2)) // e_type=EXEC
	binary.Write(&out, binary.LittleEndian, uint16(0x3E)) // e_machine=X86_64
	binary.Write(&out, binary.LittleEndian, uint32(1)) // e_version
	binary.Write(&out, binary.LittleEndian, entry)
	binary.Write(&out, binary.LittleEndian, phoff)
	binary.Write(&out, binary.LittleEndian, uint64(0)) // e_shoff
	binary.Write(&out, binary.LittleEndian, uint32(0)) // e_flags
	binary.Write(&out, binary.LittleEndian, uint16(ehdrSize))
	binary.Write(&out, binary.LittleEndian, uint16(phdrSize))
	binary.Write(&out, binary.LittleEndian, uint16(len(segs)))
	binary.Write(&out, binary.LittleEndian, uint16(0)) // shentsize
	binary.Write(&out, binary.LittleEndian, uint16(0)) // shnum
	binary.Write(&out, binary.LittleEndian, uint16(0)) // shstrndx

	// Program headers — point each segment at sequential file offsets.
	curOff := dataOff
	for _, s := range segs {
		binary.Write(&out, binary.LittleEndian, uint32(1))      // p_type=LOAD
		binary.Write(&out, binary.LittleEndian, uint32(7))      // p_flags=RWE
		binary.Write(&out, binary.LittleEndian, curOff)         // p_offset
		binary.Write(&out, binary.LittleEndian, s.vaddr)        // p_vaddr
		binary.Write(&out, binary.LittleEndian, s.paddr)        // p_paddr
		binary.Write(&out, binary.LittleEndian, s.filesz)       // p_filesz
		binary.Write(&out, binary.LittleEndian, s.memsz)        // p_memsz
		binary.Write(&out, binary.LittleEndian, uint64(0x1000)) // p_align
		curOff += s.filesz
	}
	// Segment payloads, in the same order.
	for _, s := range segs {
		out.Write(s.data[:s.filesz])
	}
	return out.Bytes()
}

func TestLoadVMLinux64_LoadsSegmentsAndZerosBSS(t *testing.T) {
	cfg := Config{RAMSize: 64 * 1024 * 1024, MachineType: "x86_64"}
	pcMachine, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer pcMachine.Close()

	// Two segments at known paddrs with filesz < memsz so we can
	// observe the BSS zeroing.
	seg1Data := make([]byte, 0x100)
	for i := range seg1Data {
		seg1Data[i] = byte(0xA0 + (i & 0xF))
	}
	seg2Data := []byte{0xDE, 0xAD, 0xBE, 0xEF}
	elfBytes := minimalELF64(
		0x01001234, // entry
		[]loadSegment{
			{paddr: 0x01000000, vaddr: 0xffffffff81000000,
				filesz: uint64(len(seg1Data)), memsz: uint64(len(seg1Data)) + 0x40,
				data: seg1Data},
			{paddr: 0x02000000, vaddr: 0xffffffff82000000,
				filesz: uint64(len(seg2Data)), memsz: 0x100,
				data: seg2Data},
		},
	)

	if err := pcMachine.loadVMLinux64(elfBytes, nil, "console=ttyS0 test=1"); err != nil {
		t.Fatalf("loadVMLinux64: %v", err)
	}

	mm := pcMachine.memMap

	// Segment 1 content is in place.
	for i, want := range seg1Data {
		got, _ := mm.Read8(0x01000000 + uint64(i))
		if got != want {
			t.Errorf("seg1[%d] = %#x, want %#x", i, got, want)
			break
		}
	}
	// Segment 1 BSS is zero.
	for i := uint64(len(seg1Data)); i < uint64(len(seg1Data))+0x40; i++ {
		got, _ := mm.Read8(0x01000000 + i)
		if got != 0 {
			t.Errorf("seg1 BSS @ %#x = %#x, want 0", i, got)
			break
		}
	}
	// Segment 2 content + BSS.
	for i, want := range seg2Data {
		got, _ := mm.Read8(0x02000000 + uint64(i))
		if got != want {
			t.Errorf("seg2[%d] = %#x, want %#x", i, got, want)
			break
		}
	}
	for i := uint64(len(seg2Data)); i < 0x100; i++ {
		got, _ := mm.Read8(0x02000000 + i)
		if got != 0 {
			t.Errorf("seg2 BSS @ %#x = %#x", i, got)
			break
		}
	}

	// boot_params at 0x90000: boot_flag = 0xAA55 at offset 0x1FE.
	bootFlag, _ := mm.Read16(0x90000 + 0x1FE)
	if bootFlag != 0xAA55 {
		t.Errorf("boot_flag = %#x, want 0xAA55", bootFlag)
	}
	// "HdrS" at 0x202.
	hdrS, _ := mm.Read32(0x90000 + 0x202)
	if hdrS != 0x53726448 {
		t.Errorf("header = %#x, want 'HdrS'", hdrS)
	}
	// cmdline pointer at 0x228, contents reachable.
	cmdlinePtr, _ := mm.Read32(0x90000 + 0x228)
	if cmdlinePtr == 0 {
		t.Errorf("cmd_line_ptr is zero")
	}
	for i, ch := range []byte("console=ttyS0 test=1") {
		got, _ := mm.Read8(uint64(cmdlinePtr) + uint64(i))
		if got != ch {
			t.Errorf("cmdline[%d] = %q, want %q", i, got, ch)
			break
		}
	}

	// CPU state. Cast through cpu/x86_64 to read its specific fields.
	cpu64 := pcMachine.cpu.(*x86_64.CPU)
	if cpu64.GetRIP() != 0x01001234 {
		t.Errorf("RIP = %#x, want 0x01001234", cpu64.GetRIP())
	}
	if cpu64.GetReg64(x86_64.RSI) != 0x90000 {
		t.Errorf("RSI = %#x, want boot_params 0x90000", cpu64.GetReg64(x86_64.RSI))
	}
	if cpu64.GetEFER()&x86_64.EFER_LMA == 0 {
		t.Errorf("EFER.LMA clear after vmlinux64 setup")
	}
	if cpu64.GetCR(0)&x86_64.CR0_PG == 0 {
		t.Errorf("CR0.PG clear after vmlinux64 setup")
	}
	if cpu64.GetSegAccess(x86_64.CS)&(1<<9) == 0 {
		t.Errorf("CS.L not set — kernel won't enter 64-bit mode")
	}
}

// Reject obvious non-ELF input.
func TestLoadVMLinux64_RejectsNonELF(t *testing.T) {
	cfg := Config{RAMSize: 64 * 1024 * 1024, MachineType: "x86_64"}
	pcMachine, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer pcMachine.Close()
	if err := pcMachine.loadVMLinux64([]byte("MZ\x90\x00not-an-elf"), nil, ""); err == nil {
		t.Errorf("expected error from non-ELF input")
	}
}

// 32-bit ELF must be rejected (i386 path's job).
func TestLoadVMLinux64_RejectsELF32(t *testing.T) {
	cfg := Config{RAMSize: 64 * 1024 * 1024, MachineType: "x86_64"}
	pcMachine, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer pcMachine.Close()
	// Just an ELF magic + class byte = 1 (ELF32). Enough to fail the
	// class check.
	bad := []byte{0x7F, 'E', 'L', 'F', 1, 1, 1, 0}
	bad = append(bad, make([]byte, 60)...)
	if err := pcMachine.loadVMLinux64(bad, nil, ""); err == nil {
		t.Errorf("expected error for ELFCLASS32")
	}
}
