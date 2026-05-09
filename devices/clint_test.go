package devices

import (
	"testing"

	"github.com/jtolio/tinyemu-go/mem"
)

// mockInterruptController tracks MIP set/reset calls
type mockInterruptController struct {
	mip    uint32
	cycles uint64
}

func (m *mockInterruptController) SetMIP(mask uint32) {
	m.mip |= mask
}

func (m *mockInterruptController) ResetMIP(mask uint32) {
	m.mip &^= mask
}

func (m *mockInterruptController) GetMIP() uint32 {
	return m.mip
}

func (m *mockInterruptController) GetCycles() uint64 {
	return m.cycles
}

func TestCLINTNew(t *testing.T) {
	intCtrl := &mockInterruptController{}
	clint := NewCLINT(intCtrl)

	if clint == nil {
		t.Fatal("NewCLINT returned nil")
	}

	// Initial mtimecmp should be max value (timer disabled)
	if clint.GetMtimecmp() != 0xFFFFFFFFFFFFFFFF {
		t.Errorf("initial mtimecmp = 0x%x, want 0xFFFFFFFFFFFFFFFF", clint.GetMtimecmp())
	}

	// Initial msip should be 0
	if clint.GetMsip() != 0 {
		t.Errorf("initial msip = %d, want 0", clint.GetMsip())
	}
}

func TestCLINTMsip(t *testing.T) {
	intCtrl := &mockInterruptController{}
	clint := NewCLINT(intCtrl)

	// Set MSIP
	clint.SetMsip(1)
	if clint.GetMsip() != 1 {
		t.Errorf("msip = %d, want 1", clint.GetMsip())
	}
	if intCtrl.mip&MipMSIP == 0 {
		t.Error("MIP MSIP bit should be set")
	}

	// Clear MSIP
	clint.SetMsip(0)
	if clint.GetMsip() != 0 {
		t.Errorf("msip = %d, want 0", clint.GetMsip())
	}
	if intCtrl.mip&MipMSIP != 0 {
		t.Error("MIP MSIP bit should be clear")
	}

	// Only bit 0 matters
	clint.SetMsip(0xFF)
	if clint.GetMsip() != 1 {
		t.Errorf("msip = %d, want 1 (masked)", clint.GetMsip())
	}
}

func TestCLINTMtimecmp(t *testing.T) {
	intCtrl := &mockInterruptController{}
	clint := NewCLINT(intCtrl)

	// Set a timer interrupt pending first
	intCtrl.SetMIP(MipMTIP)

	// Setting mtimecmp should clear MTIP
	clint.SetMtimecmp(1000)
	if intCtrl.mip&MipMTIP != 0 {
		t.Error("Setting mtimecmp should clear MTIP")
	}

	if clint.GetMtimecmp() != 1000 {
		t.Errorf("mtimecmp = %d, want 1000", clint.GetMtimecmp())
	}

	// Test 64-bit value
	clint.SetMtimecmp(0x123456789ABCDEF0)
	if clint.GetMtimecmp() != 0x123456789ABCDEF0 {
		t.Errorf("mtimecmp = 0x%x, want 0x123456789ABCDEF0", clint.GetMtimecmp())
	}
}

func TestCLINTMtime(t *testing.T) {
	intCtrl := &mockInterruptController{}
	clint := NewCLINT(intCtrl)

	// mtime should be based on instruction counter / RTCFreqDiv
	intCtrl.cycles = 0
	time1 := clint.GetMtime()
	if time1 != 0 {
		t.Errorf("mtime at cycles=0: got %d, want 0", time1)
	}

	// Advance cycles by 160 (should give mtime=10 at RTCFreqDiv=16)
	intCtrl.cycles = 160
	time2 := clint.GetMtime()
	if time2 != 10 {
		t.Errorf("mtime at cycles=160: got %d, want 10", time2)
	}

	// Verify mtime increases with cycles
	if time2 <= time1 {
		t.Errorf("mtime not incrementing: time1=%d, time2=%d", time1, time2)
	}
}

func TestCLINTCheckTimer(t *testing.T) {
	intCtrl := &mockInterruptController{}
	clint := NewCLINT(intCtrl)

	// Set mtimecmp to 10
	clint.SetMtimecmp(10)

	// With cycles=0, mtime=0, so no interrupt yet
	intCtrl.cycles = 0
	clint.CheckTimer()
	if intCtrl.mip&MipMTIP != 0 {
		t.Error("CheckTimer should not set MTIP when mtime < mtimecmp")
	}

	// Advance cycles so mtime >= mtimecmp (160 cycles -> mtime=10)
	intCtrl.cycles = 160

	// Check should set MTIP
	clint.CheckTimer()
	if intCtrl.mip&MipMTIP == 0 {
		t.Error("CheckTimer should set MTIP when mtime >= mtimecmp")
	}
}

func TestCLINTReadWrite(t *testing.T) {
	intCtrl := &mockInterruptController{}
	clint := NewCLINT(intCtrl)

	// Test MSIP register via Read/Write
	clint.Write(nil, CLINTMsipOffset, 1, 2)
	val := clint.Read(nil, CLINTMsipOffset, 2)
	if val != 1 {
		t.Errorf("MSIP read = %d, want 1", val)
	}

	// Test mtimecmp via Read/Write (low word)
	clint.Write(nil, CLINTMtimecmpOffset, 0x12345678, 2)
	val = clint.Read(nil, CLINTMtimecmpOffset, 2)
	if val != 0x12345678 {
		t.Errorf("mtimecmp low = 0x%x, want 0x12345678", val)
	}

	// Test mtimecmp (high word)
	clint.Write(nil, CLINTMtimecmpOffset+4, 0xABCDEF00, 2)
	val = clint.Read(nil, CLINTMtimecmpOffset+4, 2)
	if val != 0xABCDEF00 {
		t.Errorf("mtimecmp high = 0x%x, want 0xABCDEF00", val)
	}

	// Verify combined mtimecmp
	expected := uint64(0xABCDEF0012345678)
	if clint.GetMtimecmp() != expected {
		t.Errorf("mtimecmp = 0x%x, want 0x%x", clint.GetMtimecmp(), expected)
	}

	// Test mtime read (can't easily test value, just that it works)
	_ = clint.Read(nil, CLINTMtimeOffset, 2)
	_ = clint.Read(nil, CLINTMtimeOffset+4, 2)
}

func TestCLINTRegister(t *testing.T) {
	intCtrl := &mockInterruptController{}
	clint := NewCLINT(intCtrl)
	memMap := mem.NewPhysMemoryMap()

	pr, err := clint.Register(memMap)
	if err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	if pr.Addr != CLINTBaseAddr {
		t.Errorf("addr = 0x%x, want 0x%x", pr.Addr, CLINTBaseAddr)
	}
	if pr.Size != CLINTSize {
		t.Errorf("size = 0x%x, want 0x%x", pr.Size, CLINTSize)
	}
	if pr.IsRAM {
		t.Error("CLINT should not be RAM")
	}
}

func TestCLINTRegisterAt(t *testing.T) {
	intCtrl := &mockInterruptController{}
	clint := NewCLINT(intCtrl)
	memMap := mem.NewPhysMemoryMap()

	customAddr := uint64(0x10000000)
	pr, err := clint.RegisterAt(memMap, customAddr)
	if err != nil {
		t.Fatalf("RegisterAt failed: %v", err)
	}

	if pr.Addr != customAddr {
		t.Errorf("addr = 0x%x, want 0x%x", pr.Addr, customAddr)
	}
}

func TestCLINTMemMapAccess(t *testing.T) {
	intCtrl := &mockInterruptController{}
	clint := NewCLINT(intCtrl)
	memMap := mem.NewPhysMemoryMap()

	_, err := clint.Register(memMap)
	if err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	// Write MSIP via memory map
	err = memMap.Write32(CLINTBaseAddr+CLINTMsipOffset, 1)
	if err != nil {
		t.Fatalf("Write32 failed: %v", err)
	}

	// Read back via memory map
	val, err := memMap.Read32(CLINTBaseAddr + CLINTMsipOffset)
	if err != nil {
		t.Fatalf("Read32 failed: %v", err)
	}
	if val != 1 {
		t.Errorf("msip = %d, want 1", val)
	}

	// Verify interrupt was set
	if intCtrl.mip&MipMSIP == 0 {
		t.Error("MSIP interrupt should be set")
	}

	// Write mtimecmp via memory map (64-bit as two 32-bit writes)
	err = memMap.Write32(CLINTBaseAddr+CLINTMtimecmpOffset, 0xDEADBEEF)
	if err != nil {
		t.Fatalf("Write32 mtimecmp low failed: %v", err)
	}
	err = memMap.Write32(CLINTBaseAddr+CLINTMtimecmpOffset+4, 0xCAFEBABE)
	if err != nil {
		t.Fatalf("Write32 mtimecmp high failed: %v", err)
	}

	expected := uint64(0xCAFEBABEDEADBEEF)
	if clint.GetMtimecmp() != expected {
		t.Errorf("mtimecmp = 0x%x, want 0x%x", clint.GetMtimecmp(), expected)
	}
}

func TestCLINTSetTimerFrequency(t *testing.T) {
	intCtrl := &mockInterruptController{}
	clint := NewCLINT(intCtrl)

	// Set a different frequency (this is used for FDT, not for mtime calculation)
	clint.SetTimerFrequency(1_000_000) // 1 MHz

	// mtime is now based on instruction counter, not wall clock
	// Verify mtime still works correctly after setting frequency
	intCtrl.cycles = 320 // Should give mtime=20 at RTCFreqDiv=16
	mtime := clint.GetMtime()
	if mtime != 20 {
		t.Errorf("mtime at cycles=320: got %d, want 20", mtime)
	}
}

func TestCLINTNilInterruptController(t *testing.T) {
	// CLINT should work even without an interrupt controller
	clint := NewCLINT(nil)

	// These should not panic
	clint.SetMsip(1)
	clint.SetMtimecmp(0)
	clint.CheckTimer()

	// Values should still be stored
	if clint.GetMsip() != 1 {
		t.Errorf("msip = %d, want 1", clint.GetMsip())
	}
	if clint.GetMtimecmp() != 0 {
		t.Errorf("mtimecmp = %d, want 0", clint.GetMtimecmp())
	}
}

func TestCLINTUnusedOffset(t *testing.T) {
	intCtrl := &mockInterruptController{}
	clint := NewCLINT(intCtrl)

	// Reading from unused offset should return 0
	val := clint.Read(nil, 0x1000, 2)
	if val != 0 {
		t.Errorf("unused offset read = %d, want 0", val)
	}

	// Writing to unused offset should not panic
	clint.Write(nil, 0x1000, 0x12345678, 2) // Should be a no-op
}

// mockTimeSource provides a controllable time source for testing
type mockTimeSource struct {
	rtcTime uint64
}

func (m *mockTimeSource) GetRTCTime() uint64 {
	return m.rtcTime
}

func TestCLINTTimeSource(t *testing.T) {
	intCtrl := &mockInterruptController{}
	clint := NewCLINT(intCtrl)
	timeSource := &mockTimeSource{}

	// Initially uses instruction counter
	intCtrl.cycles = 320 // mtime = 320/16 = 20
	if mtime := clint.GetMtime(); mtime != 20 {
		t.Errorf("without TimeSource: mtime = %d, want 20", mtime)
	}

	// Set up time source
	clint.SetTimeSource(timeSource)

	// Now should use time source instead of cycles
	timeSource.rtcTime = 12345
	if mtime := clint.GetMtime(); mtime != 12345 {
		t.Errorf("with TimeSource: mtime = %d, want 12345", mtime)
	}

	// Instruction counter should be ignored when time source is set
	intCtrl.cycles = 99999
	if mtime := clint.GetMtime(); mtime != 12345 {
		t.Errorf("with TimeSource: mtime = %d, want 12345 (cycles should be ignored)", mtime)
	}
}

func TestCLINTTimeSourceCheckTimer(t *testing.T) {
	intCtrl := &mockInterruptController{}
	clint := NewCLINT(intCtrl)
	timeSource := &mockTimeSource{}

	clint.SetTimeSource(timeSource)
	clint.SetMtimecmp(1000)

	// Time < mtimecmp: no interrupt
	timeSource.rtcTime = 500
	clint.CheckTimer()
	if intCtrl.mip&MipMTIP != 0 {
		t.Error("CheckTimer should not set MTIP when rtcTime < mtimecmp")
	}

	// Time >= mtimecmp: interrupt fires
	timeSource.rtcTime = 1000
	clint.CheckTimer()
	if intCtrl.mip&MipMTIP == 0 {
		t.Error("CheckTimer should set MTIP when rtcTime >= mtimecmp")
	}
}

func TestCLINTConstants(t *testing.T) {
	// Verify CLINT constants match RISC-V spec
	if CLINTMsipOffset != 0x0000 {
		t.Errorf("CLINTMsipOffset = 0x%x, want 0x0000", CLINTMsipOffset)
	}
	if CLINTMtimecmpOffset != 0x4000 {
		t.Errorf("CLINTMtimecmpOffset = 0x%x, want 0x4000", CLINTMtimecmpOffset)
	}
	if CLINTMtimeOffset != 0xBFF8 {
		t.Errorf("CLINTMtimeOffset = 0x%x, want 0xBFF8", CLINTMtimeOffset)
	}
	if CLINTBaseAddr != 0x02000000 {
		t.Errorf("CLINTBaseAddr = 0x%x, want 0x02000000", CLINTBaseAddr)
	}

	// Verify MIP bit positions
	if MipMSIP != 1<<3 {
		t.Errorf("MipMSIP = 0x%x, want 0x%x", MipMSIP, 1<<3)
	}
	if MipMTIP != 1<<7 {
		t.Errorf("MipMTIP = 0x%x, want 0x%x", MipMTIP, 1<<7)
	}
}
