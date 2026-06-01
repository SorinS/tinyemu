package pc

import "testing"

// TestPS2_SelfTestReturns55 — sending command 0xAA must make the next
// data-port read return 0x55 (controller self-test passed). BareMetal's
// ps2_init bails to ps2_init_error on anything else, which silently
// skips the rest of HID init; the guest still POSTs but real keyboard
// probes downstream won't get to run.
func TestPS2_SelfTestReturns55(t *testing.T) {
	p := NewPS2Controller()
	p.writeCommand(ps2CmdSelfTest)
	if status := p.readStatus(); status&ps2StatusOBF == 0 {
		t.Fatalf("status OBF clear after self-test command — guest's "+
			"`bt ax, 0 ; jc` loop would spin forever (status=%#x)", status)
	}
	if v := p.readData(); v != 0x55 {
		t.Fatalf("self-test result = %#x, want 0x55", v)
	}
	if status := p.readStatus(); status&ps2StatusOBF != 0 {
		t.Errorf("status OBF still set after draining the response (status=%#x)", status)
	}
}

// TestPS2_CCBRoundtrip — read-modify-write the Controller
// Configuration Byte. BareMetal does exactly this in ps2_init to
// flip the per-port-IRQ and translation bits.
func TestPS2_CCBRoundtrip(t *testing.T) {
	p := NewPS2Controller()
	p.writeCommand(ps2CmdReadCCB)
	original := p.readData()

	// Modify and write back.
	newVal := original ^ 0x55
	p.writeCommand(ps2CmdWriteCCB)
	p.writeData(newVal)

	// Read back: must reflect the write.
	p.writeCommand(ps2CmdReadCCB)
	if got := p.readData(); got != newVal {
		t.Errorf("CCB round-trip: wrote %#x, read back %#x", newVal, got)
	}
}

// TestPS2_StatusOBFTracksQueue verifies the load-bearing invariant that
// makes ps2_flush terminate: status bit 0 is set iff there's a byte
// queued for port 0x60. Draining the queue must clear it.
//
// BareMetal's ps2_flush is `in al,0x60; in al,0x64; bt ax,0; jc start`.
// If OBF doesn't clear after the 0x60 read, the routine spins forever
// — which is exactly the symptom we saw before adding this stub.
func TestPS2_StatusOBFTracksQueue(t *testing.T) {
	p := NewPS2Controller()
	if p.readStatus()&ps2StatusOBF != 0 {
		t.Fatalf("fresh controller has OBF set; want clear")
	}
	// Queue a byte via self-test.
	p.writeCommand(ps2CmdSelfTest)
	if p.readStatus()&ps2StatusOBF == 0 {
		t.Fatalf("OBF clear with a byte queued; want set")
	}
	_ = p.readData()
	if p.readStatus()&ps2StatusOBF != 0 {
		t.Fatalf("OBF set after draining queue; ps2_flush would hang")
	}
}

// TestPS2_EmptyDataReturnsFF — reading 0x60 with an empty queue
// returns 0xFF (matches what most real KBCs do when nothing's pending).
// Guests use this in a "drain leftover bytes" loop; returning 0x00
// would also be acceptable, but 0xFF lets BIOS probes that test for
// 0x00 to distinguish "real silence" from "missing controller".
func TestPS2_EmptyDataReturnsFF(t *testing.T) {
	p := NewPS2Controller()
	if v := p.readData(); v != 0xFF {
		t.Errorf("empty data read = %#x, want 0xFF", v)
	}
}

// TestPS2_EnableDisableAreNoops — the kbd/aux enable/disable commands
// must not push any response byte. If we accidentally pushed one,
// ps2_init's `call ps2_send_cmd ; call ps2_send_cmd` sequence would
// leave one byte queued and the next ps2_read_data would consume it
// instead of the self-test result.
func TestPS2_EnableDisableAreNoops(t *testing.T) {
	p := NewPS2Controller()
	for _, cmd := range []byte{
		ps2CmdDisableKbd, ps2CmdEnableKbd,
		ps2CmdDisableAux, ps2CmdEnableAux,
	} {
		p.writeCommand(cmd)
		if p.readStatus()&ps2StatusOBF != 0 {
			t.Errorf("command %#x pushed a stray output byte (OBF set)", cmd)
		}
	}
}
