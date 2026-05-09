package machine

import (
	"bytes"
	"encoding/binary"
	"testing"
	"time"

	"github.com/jtolio/tinyemu-go/virtio"
)

// TestNewMachineRV64 tests creating a 64-bit RISC-V machine.
func TestNewMachineRV64(t *testing.T) {
	cfg := Config{
		RAMSize: 128 * 1024 * 1024, // 128 MB
		MaxXLEN: 64,
	}

	m, err := New(cfg)
	if err != nil {
		t.Fatalf("failed to create machine: %v", err)
	}
	defer m.Close()

	// Check XLEN
	if m.MaxXLEN() != 64 {
		t.Errorf("expected MaxXLEN 64, got %d", m.MaxXLEN())
	}

	// Check RAM size
	if m.RAMSize() != cfg.RAMSize {
		t.Errorf("expected RAMSize %d, got %d", cfg.RAMSize, m.RAMSize())
	}

	// Check CPU was created
	if m.CPU() == nil {
		t.Error("CPU is nil")
	}

	// Check memory map was created
	if m.MemMap() == nil {
		t.Error("MemMap is nil")
	}

	// Check devices were created
	if m.CLINT() == nil {
		t.Error("CLINT is nil")
	}
	if m.PLIC() == nil {
		t.Error("PLIC is nil")
	}
	if m.HTIF() == nil {
		t.Error("HTIF is nil")
	}

	// Check VirtIO count (should be 0 without console)
	if m.VirtIOCount() != 0 {
		t.Errorf("expected VirtIOCount 0, got %d", m.VirtIOCount())
	}
}

// TestNewMachineRV32 tests creating a 32-bit RISC-V machine.
func TestNewMachineRV32(t *testing.T) {
	cfg := Config{
		RAMSize: 64 * 1024 * 1024, // 64 MB
		MaxXLEN: 32,
	}

	m, err := New(cfg)
	if err != nil {
		t.Fatalf("failed to create machine: %v", err)
	}
	defer m.Close()

	if m.MaxXLEN() != 32 {
		t.Errorf("expected MaxXLEN 32, got %d", m.MaxXLEN())
	}
}

// TestNewMachineRV128 tests creating a 128-bit RISC-V machine.
func TestNewMachineRV128(t *testing.T) {
	cfg := Config{
		RAMSize: 64 * 1024 * 1024, // 64 MB
		MaxXLEN: 128,
	}

	m, err := New(cfg)
	if err != nil {
		t.Fatalf("failed to create machine: %v", err)
	}
	defer m.Close()

	if m.MaxXLEN() != 128 {
		t.Errorf("expected MaxXLEN 128, got %d", m.MaxXLEN())
	}
}

// TestNewMachineInvalidXLEN tests creating a machine with invalid XLEN.
func TestNewMachineInvalidXLEN(t *testing.T) {
	cfg := Config{
		RAMSize: 64 * 1024 * 1024,
		MaxXLEN: 48, // Invalid
	}

	_, err := New(cfg)
	if err == nil {
		t.Error("expected error for invalid XLEN")
	}
}

// TestNewMachineWithConsole tests creating a machine with a console.
func TestNewMachineWithConsole(t *testing.T) {
	var output bytes.Buffer
	console := &virtio.CharacterDevice{
		Writer: &output,
	}

	cfg := Config{
		RAMSize: 128 * 1024 * 1024,
		MaxXLEN: 64,
		Console: console,
	}

	m, err := New(cfg)
	if err != nil {
		t.Fatalf("failed to create machine: %v", err)
	}
	defer m.Close()

	// Check VirtIO console was created
	if m.VirtIOCount() != 1 {
		t.Errorf("expected VirtIOCount 1, got %d", m.VirtIOCount())
	}

	if m.Console() == nil {
		t.Error("Console is nil")
	}
}

// TestMemoryMapSetup tests that memory regions are correctly registered.
func TestMemoryMapSetup(t *testing.T) {
	cfg := Config{
		RAMSize: 128 * 1024 * 1024,
		MaxXLEN: 64,
	}

	m, err := New(cfg)
	if err != nil {
		t.Fatalf("failed to create machine: %v", err)
	}
	defer m.Close()

	memMap := m.MemMap()

	// Test low RAM region
	lowRAM := memMap.GetRange(LowRAMAddr)
	if lowRAM == nil {
		t.Error("low RAM range not found")
	} else {
		if !lowRAM.IsRAM {
			t.Error("low RAM should be marked as RAM")
		}
		if lowRAM.Size != LowRAMSize {
			t.Errorf("low RAM size: expected %d, got %d", LowRAMSize, lowRAM.Size)
		}
	}

	// Test main RAM region
	mainRAM := memMap.GetRange(RAMBaseAddr)
	if mainRAM == nil {
		t.Error("main RAM range not found")
	} else {
		if !mainRAM.IsRAM {
			t.Error("main RAM should be marked as RAM")
		}
		if mainRAM.Size != cfg.RAMSize {
			t.Errorf("main RAM size: expected %d, got %d", cfg.RAMSize, mainRAM.Size)
		}
	}

	// Test CLINT region
	clint := memMap.GetRange(CLINTAddr)
	if clint == nil {
		t.Error("CLINT range not found")
	} else if clint.IsRAM {
		t.Error("CLINT should not be marked as RAM")
	}

	// Test PLIC region
	plic := memMap.GetRange(PLICAddr)
	if plic == nil {
		t.Error("PLIC range not found")
	} else if plic.IsRAM {
		t.Error("PLIC should not be marked as RAM")
	}

	// Test HTIF region
	htif := memMap.GetRange(HTIFAddr)
	if htif == nil {
		t.Error("HTIF range not found")
	} else if htif.IsRAM {
		t.Error("HTIF should not be marked as RAM")
	}
}

// TestLoadBIOS tests loading a BIOS image.
func TestLoadBIOS(t *testing.T) {
	cfg := Config{
		RAMSize: 128 * 1024 * 1024,
		MaxXLEN: 64,
	}

	m, err := New(cfg)
	if err != nil {
		t.Fatalf("failed to create machine: %v", err)
	}
	defer m.Close()

	// Create a simple BIOS image
	biosData := make([]byte, 4096)
	for i := range biosData {
		biosData[i] = byte(i)
	}

	err = m.LoadBIOS(biosData, nil, nil, "console=hvc0")
	if err != nil {
		t.Fatalf("failed to load BIOS: %v", err)
	}

	// Check that BIOS was copied to main RAM
	memMap := m.MemMap()
	mainRAM := memMap.GetRange(RAMBaseAddr)
	if mainRAM == nil {
		t.Fatal("main RAM not found")
	}

	for i := 0; i < len(biosData); i++ {
		if mainRAM.PhysMem[i] != biosData[i] {
			t.Errorf("BIOS data mismatch at offset %d: expected 0x%02x, got 0x%02x",
				i, biosData[i], mainRAM.PhysMem[i])
			break
		}
	}

	// Check that PC was set to boot stub
	if m.CPU().PC != 0x1000 {
		t.Errorf("expected PC 0x1000, got 0x%x", m.CPU().PC)
	}

	// Check that boot stub was written
	lowRAM := memMap.GetRange(LowRAMAddr)
	if lowRAM == nil {
		t.Fatal("low RAM not found")
	}

	// First instruction should be auipc t0
	insn := binary.LittleEndian.Uint32(lowRAM.PhysMem[0x1000:])
	if insn&0x7F != 0x17 { // AUIPC opcode
		t.Errorf("expected AUIPC instruction, got 0x%08x", insn)
	}
}

// TestLoadBIOSWithKernel tests loading BIOS with a kernel image.
func TestLoadBIOSWithKernel(t *testing.T) {
	cfg := Config{
		RAMSize: 128 * 1024 * 1024,
		MaxXLEN: 64,
	}

	m, err := New(cfg)
	if err != nil {
		t.Fatalf("failed to create machine: %v", err)
	}
	defer m.Close()

	// Create BIOS and kernel images
	biosData := make([]byte, 1024*1024)     // 1 MB BIOS
	kernelData := make([]byte, 4*1024*1024) // 4 MB kernel

	// Mark kernel with known pattern
	for i := range kernelData {
		kernelData[i] = 0xAB
	}

	err = m.LoadBIOS(biosData, kernelData, nil, "")
	if err != nil {
		t.Fatalf("failed to load BIOS with kernel: %v", err)
	}

	// Kernel should be at 2MB-aligned offset after BIOS
	memMap := m.MemMap()
	mainRAM := memMap.GetRange(RAMBaseAddr)
	if mainRAM == nil {
		t.Fatal("main RAM not found")
	}

	// Check that kernel was placed at aligned offset
	kernelBase := uint64(2 * 1024 * 1024) // 2 MB alignment for RV64
	if mainRAM.PhysMem[kernelBase] != 0xAB {
		t.Errorf("kernel not found at expected offset 0x%x", kernelBase)
	}
}

// TestLoadBIOSTooLarge tests error handling for oversized BIOS.
func TestLoadBIOSTooLarge(t *testing.T) {
	cfg := Config{
		RAMSize: 1024 * 1024, // 1 MB
		MaxXLEN: 64,
	}

	m, err := New(cfg)
	if err != nil {
		t.Fatalf("failed to create machine: %v", err)
	}
	defer m.Close()

	// Create BIOS larger than RAM
	biosData := make([]byte, 2*1024*1024) // 2 MB

	err = m.LoadBIOS(biosData, nil, nil, "")
	if err != ErrBIOSTooLarge {
		t.Errorf("expected ErrBIOSTooLarge, got %v", err)
	}
}

// TestShutdownHandling tests the shutdown request mechanism.
func TestShutdownHandling(t *testing.T) {
	cfg := Config{
		RAMSize: 64 * 1024 * 1024,
		MaxXLEN: 64,
	}

	m, err := New(cfg)
	if err != nil {
		t.Fatalf("failed to create machine: %v", err)
	}
	defer m.Close()

	// Initially not shutdown
	if m.IsShutdownRequested() {
		t.Error("shutdown should not be requested initially")
	}

	// Simulate HTIF shutdown by writing to tohost
	// The tohost register is at offset 0x8 from the HTIF base address.
	// The HTIF handler triggers on writing the high word (offset + 4).
	memMap := m.MemMap()
	toHostAddr := uint64(HTIFAddr + 8) // tohost register offset
	// Write 1 to tohost (shutdown command)
	if err := memMap.Write32(toHostAddr, 1); err != nil {
		t.Fatalf("failed to write to HTIF: %v", err)
	}
	if err := memMap.Write32(toHostAddr+4, 0); err != nil {
		t.Fatalf("failed to write to HTIF: %v", err)
	}

	// Check shutdown is requested
	if !m.IsShutdownRequested() {
		t.Error("shutdown should be requested after HTIF command")
	}
}

// TestCheckTimer tests the timer checking mechanism.
func TestCheckTimer(t *testing.T) {
	cfg := Config{
		RAMSize: 64 * 1024 * 1024,
		MaxXLEN: 64,
	}

	m, err := New(cfg)
	if err != nil {
		t.Fatalf("failed to create machine: %v", err)
	}
	defer m.Close()

	// Set a very low timer compare value that should trigger immediately
	m.CLINT().SetMtimecmp(0)

	// Check timer
	m.CheckTimer()

	// Timer interrupt should be pending
	mip := m.CPU().GetMIP()
	if mip&(1<<7) == 0 { // MIP_MTIP bit
		t.Error("timer interrupt should be pending")
	}
}

// TestVirtIODeviceAddress tests the VirtIO device address allocation.
func TestVirtIODeviceAddress(t *testing.T) {
	cfg := Config{
		RAMSize: 64 * 1024 * 1024,
		MaxXLEN: 64,
	}

	m, err := New(cfg)
	if err != nil {
		t.Fatalf("failed to create machine: %v", err)
	}
	defer m.Close()

	// First VirtIO address should be base address
	addr1 := m.GetVirtIOAddr()
	if addr1 != VirtIOAddr {
		t.Errorf("expected first VirtIO addr 0x%x, got 0x%x", VirtIOAddr, addr1)
	}

	// Get IRQ
	irq := m.GetVirtIOIRQ()
	if irq == nil {
		t.Error("GetVirtIOIRQ returned nil")
	}
}

// TestGetRTCTime tests the RTC time in both instruction-counter and real-time modes.
// Reference: riscv_machine.c lines 90-97 (rtc_get_time)
func TestGetRTCTime(t *testing.T) {
	// Test instruction-counter mode (deterministic)
	t.Run("instruction-counter mode", func(t *testing.T) {
		cfg := Config{
			RAMSize:          64 * 1024 * 1024,
			MaxXLEN:          64,
			RTCDeterministic: true,
		}

		m, err := New(cfg)
		if err != nil {
			t.Fatalf("failed to create machine: %v", err)
		}
		defer m.Close()

		// Initial RTC time should be 0 (no cycles executed)
		rtc := m.GetRTCTime()
		if rtc != 0 {
			t.Errorf("initial RTC time = %d, want 0", rtc)
		}

		// After executing some cycles, RTC should advance
		// RTCFreqDiv is 16, so we need 16 cycles for 1 RTC tick
		for i := 0; i < 100; i++ {
			m.CPU().Run(1)
		}

		rtc = m.GetRTCTime()
		cycles := m.CPU().GetCycles()
		expected := cycles / 16
		if rtc != expected {
			t.Errorf("RTC time = %d, want cycles(%d)/16 = %d", rtc, cycles, expected)
		}
	})

	// Test real-time mode (default)
	t.Run("real-time mode", func(t *testing.T) {
		cfg := Config{
			RAMSize: 64 * 1024 * 1024,
			MaxXLEN: 64,
		}

		m, err := New(cfg)
		if err != nil {
			t.Fatalf("failed to create machine: %v", err)
		}
		defer m.Close()

		// RTC time should be very small immediately after creation
		rtc1 := m.GetRTCTime()

		// Sleep a bit and check time advances
		time.Sleep(10 * time.Millisecond)
		rtc2 := m.GetRTCTime()

		if rtc2 <= rtc1 {
			t.Errorf("RTC time should advance: rtc1=%d, rtc2=%d", rtc1, rtc2)
		}

		// Should have advanced by roughly 100,000 ticks (10ms at 10MHz)
		// Allow some tolerance for scheduling delays
		delta := rtc2 - rtc1
		if delta < 50_000 || delta > 500_000 {
			t.Errorf("RTC delta = %d, expected ~100,000 for 10ms", delta)
		}
	})
}

// TestRTCRealTimeTimer tests that the timer fires in real-time mode during WFI.
// This is the key test for the WFI wakeup functionality.
// Reference: riscv_machine.c lines 997-1001 (timer check)
func TestRTCRealTimeTimer(t *testing.T) {
	cfg := Config{
		RAMSize: 64 * 1024 * 1024,
		MaxXLEN: 64,
	}

	m, err := New(cfg)
	if err != nil {
		t.Fatalf("failed to create machine: %v", err)
	}
	defer m.Close()

	// Set a timer compare value in the near future (50ms = 500,000 ticks at 10MHz)
	futureTime := m.GetRTCTime() + 500_000
	m.CLINT().SetMtimecmp(futureTime)

	// Timer should not have fired yet
	m.CheckTimer()
	mip := m.CPU().GetMIP()
	if mip&(1<<7) != 0 { // MIP_MTIP bit
		t.Error("timer should not have fired yet")
	}

	// Wait for the timer to expire
	time.Sleep(60 * time.Millisecond)

	// Now check timer - should fire
	m.CheckTimer()
	mip = m.CPU().GetMIP()
	if mip&(1<<7) == 0 { // MIP_MTIP bit
		t.Error("timer should have fired after waiting")
	}
}

// TestGetSleepDuration tests GetSleepDuration matches C behavior.
// Reference: riscv_machine.c:990-1012 (riscv_machine_get_sleep_duration)
func TestGetSleepDuration(t *testing.T) {
	cfg := Config{
		RAMSize:          64 * 1024 * 1024,
		MaxXLEN:          64,
		RTCDeterministic: true, // Use instruction-counter mode for deterministic testing
	}

	m, err := New(cfg)
	if err != nil {
		t.Fatalf("failed to create machine: %v", err)
	}
	defer m.Close()

	// Test case 1: Timer already fired (mtime >= mtimecmp)
	// C: if (delay1 <= 0) { riscv_cpu_set_mip(s, MIP_MTIP); delay = 0; }
	t.Run("timer already fired", func(t *testing.T) {
		m.CLINT().SetMtimecmp(0) // Timer compare in the past
		m.CPU().ResetMIP(1 << 7) // Clear MTIP first
		delay := m.GetSleepDuration(1000)
		if delay != 0 {
			t.Errorf("delay = %d, want 0 (timer already fired)", delay)
		}
		// Should have set MTIP
		if m.CPU().GetMIP()&(1<<7) == 0 {
			t.Error("MTIP should be set when mtime >= mtimecmp")
		}
	})

	// Test case 2: Timer in the future, delay should be min(input, calculated)
	// C: delay1 = delay1 / (RTC_FREQ / 1000); if (delay1 < delay) delay = delay1;
	t.Run("timer in future shorter than input", func(t *testing.T) {
		m.CLINT().SetMtimecmp(0xFFFFFFFFFFFFFFFF) // Far future
		m.CPU().ResetMIP(1 << 7)                  // Clear MTIP

		// With mtime near 0 and mtimecmp at max, delay should be capped by input
		delay := m.GetSleepDuration(100)
		// Since CPU is not powered down, delay should be 0
		if delay != 0 {
			t.Errorf("delay = %d, want 0 (CPU not powered down)", delay)
		}
	})

	// Test case 3: MTIP already set - should skip timer check
	// C: if (!(riscv_cpu_get_mip(s) & MIP_MTIP)) { ... }
	t.Run("MTIP already set", func(t *testing.T) {
		m.CLINT().SetMtimecmp(0xFFFFFFFFFFFFFFFF)
		m.CPU().SetMIP(1 << 7) // Set MTIP

		// With MTIP set and CPU running, delay should be 0
		delay := m.GetSleepDuration(1000)
		if delay != 0 {
			t.Errorf("delay = %d, want 0 (CPU running)", delay)
		}
	})

	// Test case 4: CPU powered down, timer in future
	// C: if (!riscv_cpu_get_power_down(s)) delay = 0;
	t.Run("CPU powered down", func(t *testing.T) {
		m.CPU().ResetMIP(1 << 7) // Clear MTIP
		m.CPU().SetPowerDown(true)
		m.CLINT().SetMtimecmp(0xFFFFFFFFFFFFFFFF) // Far future

		delay := m.GetSleepDuration(500)
		// With CPU powered down and timer far in future,
		// delay should be capped at input (500ms)
		if delay != 500 {
			t.Errorf("delay = %d, want 500 (CPU powered down, timer far future)", delay)
		}

		m.CPU().SetPowerDown(false)
	})
}
