package pc

import "testing"

// TestSMIHandshake models SeaBIOS's SMM-relocation handshake
// (fw/smm.c smm_relocate_and_restore): it writes 0x01 to the SMI status
// port (0xB3), raises an SMI via the command port (0xB2), and spins until
// the status reads back 0x00. temu has no SMM, so a command-port write
// must synchronously clear the status — otherwise SeaBIOS hangs forever
// polling 0xB3 (it attempts SMM because it found the PIIX4 ACPI PM).
func TestSMIHandshake(t *testing.T) {
	p, err := New(Config{RAMSize: 16 * 1024 * 1024})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer p.Close()

	p.io.Write8(0xB3, 0x01)
	if got := p.io.Read8(0xB3); got != 0x01 {
		t.Errorf("SMI status after writing 0x01 = %#x, want 0x01", got)
	}
	// Writing the SMI command port stands in for the SMM handler running:
	// it clears the status, so SeaBIOS's wait loop terminates.
	p.io.Write8(0xB2, 0x00)
	if got := p.io.Read8(0xB3); got != 0x00 {
		t.Errorf("SMI status after command = %#x, want 0x00 (SeaBIOS would spin otherwise)", got)
	}
}
