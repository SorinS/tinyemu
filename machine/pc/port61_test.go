package pc

import "testing"

// TestPort61RefreshToggle verifies the System Control Port B (0x61) read
// toggles bit 4 (the PIT refresh bit) on every read. Linux's PIT calibration
// loop polls this port and spins forever if it never changes.
func TestPort61RefreshToggle(t *testing.T) {
	p, err := New(Config{RAMSize: 4 * 1024 * 1024})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer p.Close()

	v1 := uint8(p.io.Read8(0x61))
	v2 := uint8(p.io.Read8(0x61))
	if (v1 ^ v2)&0x10 == 0 {
		t.Errorf("port 0x61 reads %02X then %02X — bit 4 did not toggle", v1, v2)
	}
	v3 := uint8(p.io.Read8(0x61))
	if (v2^v3)&0x10 == 0 {
		t.Errorf("port 0x61 reads %02X then %02X (third read) — bit 4 did not toggle", v2, v3)
	}
}
