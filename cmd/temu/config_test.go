package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadConfigBasic(t *testing.T) {
	// Create a temporary config file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "test.cfg")

	configContent := `{
		"version": 1,
		"machine": "riscv64",
		"memory_size": 256,
		"bios": "bios.bin",
		"kernel": "vmlinux",
		"cmdline": "console=hvc0 root=/dev/vda rw"
	}`

	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	// Check values
	if cfg.Version != 1 {
		t.Errorf("expected Version 1, got %d", cfg.Version)
	}
	if cfg.Machine != "riscv64" {
		t.Errorf("expected Machine riscv64, got %s", cfg.Machine)
	}
	if cfg.MemorySize != 256<<20 {
		t.Errorf("expected MemorySize %d, got %d", 256<<20, cfg.MemorySize)
	}
	if cfg.CmdLine != "console=hvc0 root=/dev/vda rw" {
		t.Errorf("unexpected cmdline: %s", cfg.CmdLine)
	}

	// Check relative paths were resolved
	expectedBios := filepath.Join(tmpDir, "bios.bin")
	if cfg.BIOS != expectedBios {
		t.Errorf("expected BIOS path %s, got %s", expectedBios, cfg.BIOS)
	}

	expectedKernel := filepath.Join(tmpDir, "vmlinux")
	if cfg.Kernel != expectedKernel {
		t.Errorf("expected Kernel path %s, got %s", expectedKernel, cfg.Kernel)
	}
}

func TestLoadConfigRV32(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "test.cfg")

	configContent := `{
		"version": 1,
		"machine": "riscv32",
		"memory_size": 64
	}`

	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if cfg.Machine != "riscv32" {
		t.Errorf("expected Machine riscv32, got %s", cfg.Machine)
	}
}

func TestLoadConfigWithDrives(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "test.cfg")

	configContent := `{
		"version": 1,
		"machine": "riscv64",
		"memory_size": 128,
		"drive0": { "file": "disk0.img", "device": "virtio-blk" },
		"drive1": { "file": "disk1.img" }
	}`

	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if len(cfg.Drives) != 2 {
		t.Fatalf("expected 2 drives, got %d", len(cfg.Drives))
	}

	if cfg.Drives[0].Device != "virtio-blk" {
		t.Errorf("expected drive0 device virtio-blk, got %s", cfg.Drives[0].Device)
	}

	expectedPath := filepath.Join(tmpDir, "disk0.img")
	if cfg.Drives[0].File != expectedPath {
		t.Errorf("expected drive0 file %s, got %s", expectedPath, cfg.Drives[0].File)
	}
}

func TestLoadConfigWithFS(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "test.cfg")

	configContent := `{
		"version": 1,
		"machine": "riscv64",
		"memory_size": 128,
		"fs0": { "file": "/path/to/rootfs", "tag": "/dev/root" },
		"fs1": { "file": "/path/to/data" }
	}`

	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if len(cfg.Filesystems) != 2 {
		t.Fatalf("expected 2 filesystems, got %d", len(cfg.Filesystems))
	}

	if cfg.Filesystems[0].Tag != "/dev/root" {
		t.Errorf("expected fs0 tag /dev/root, got %s", cfg.Filesystems[0].Tag)
	}

	// fs1 should have auto-generated tag
	if cfg.Filesystems[1].Tag != "/dev/root1" {
		t.Errorf("expected fs1 tag /dev/root1, got %s", cfg.Filesystems[1].Tag)
	}
}

func TestLoadConfigWithNetwork(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "test.cfg")

	configContent := `{
		"version": 1,
		"machine": "riscv64",
		"memory_size": 128,
		"eth0": { "driver": "tap", "ifname": "tap0" }
	}`

	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if len(cfg.Networks) != 1 {
		t.Fatalf("expected 1 network, got %d", len(cfg.Networks))
	}

	if cfg.Networks[0].Driver != "tap" {
		t.Errorf("expected network driver tap, got %s", cfg.Networks[0].Driver)
	}
	if cfg.Networks[0].IFName != "tap0" {
		t.Errorf("expected ifname tap0, got %s", cfg.Networks[0].IFName)
	}
}

func TestLoadConfigInvalidVersion(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "test.cfg")

	configContent := `{
		"version": 99,
		"machine": "riscv64",
		"memory_size": 128
	}`

	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	_, err := LoadConfig(configPath)
	if err == nil {
		t.Error("expected error for invalid version")
	}
}

func TestLoadConfigInvalidMachine(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "test.cfg")

	configContent := `{
		"version": 1,
		"machine": "invalid_machine",
		"memory_size": 128
	}`

	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	_, err := LoadConfig(configPath)
	if err == nil {
		t.Error("expected error for invalid machine type")
	}
}

func TestLoadConfigInvalidJSON(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "test.cfg")

	configContent := `{ invalid json }`

	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	_, err := LoadConfig(configPath)
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

// TestLoadConfigInvalidAccel verifies that invalid accel values are rejected.
// Reference: tinyemu-2019-12-21/machine.c:376-387
func TestLoadConfigInvalidAccel(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "test.cfg")

	configContent := `{
		"version": 1,
		"machine": "riscv64",
		"memory_size": 128,
		"accel": "invalid"
	}`

	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	_, err := LoadConfig(configPath)
	if err == nil {
		t.Error("expected error for invalid accel value")
	}
	if err != nil && !strings.Contains(err.Error(), "accel") {
		t.Errorf("expected error about accel, got: %v", err)
	}
}

// TestLoadConfigValidAccel verifies that valid accel values are accepted.
// Reference: tinyemu-2019-12-21/machine.c:376-387
func TestLoadConfigValidAccel(t *testing.T) {
	tmpDir := t.TempDir()

	testCases := []struct {
		name  string
		accel string
	}{
		{"none", "none"},
		{"auto", "auto"},
		{"empty", ""},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			configPath := filepath.Join(tmpDir, "test_"+tc.name+".cfg")
			var configContent string
			if tc.accel == "" {
				configContent = `{
					"version": 1,
					"machine": "riscv64",
					"memory_size": 128
				}`
			} else {
				configContent = fmt.Sprintf(`{
					"version": 1,
					"machine": "riscv64",
					"memory_size": 128,
					"accel": %q
				}`, tc.accel)
			}

			if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
				t.Fatalf("failed to write config file: %v", err)
			}

			_, err := LoadConfig(configPath)
			if err != nil {
				t.Errorf("LoadConfig failed for accel=%q: %v", tc.accel, err)
			}
		})
	}
}

// TestLoadConfigMaxEthDevice verifies that only eth0 is supported.
// Reference: tinyemu-2019-12-21/machine.h:45 - MAX_ETH_DEVICE = 1
func TestLoadConfigMaxEthDevice(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "test.cfg")

	// This config only has eth0, which should work
	configContent := `{
		"version": 1,
		"machine": "riscv64",
		"memory_size": 128,
		"eth0": { "driver": "tap", "ifname": "tap0" }
	}`

	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	// Should have exactly 1 network interface
	if len(cfg.Networks) != 1 {
		t.Errorf("expected 1 network interface, got %d", len(cfg.Networks))
	}
}

// TestResolvePathURL verifies that URL paths are not modified.
// Reference: tinyemu-2019-12-21/machine.c:431-432
func TestResolvePathURL(t *testing.T) {
	cfg := &VMConfig{configDir: "/some/dir"}

	testCases := []struct {
		name     string
		input    string
		expected string
	}{
		{"http_url", "http://example.com/file.bin", "http://example.com/file.bin"},
		{"https_url", "https://example.com/file.bin", "https://example.com/file.bin"},
		{"ftp_url", "ftp://example.com/file.bin", "ftp://example.com/file.bin"},
		{"file_url", "file:///path/to/file", "file:///path/to/file"},
		{"relative_path", "bios.bin", "/some/dir/bios.bin"},
		{"absolute_path", "/absolute/path/file.bin", "/absolute/path/file.bin"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := cfg.resolvePath(tc.input)
			if result != tc.expected {
				t.Errorf("resolvePath(%q) = %q, want %q", tc.input, result, tc.expected)
			}
		})
	}
}

func TestSubstCmdLineTZ(t *testing.T) {
	// Test TZ substitution
	cmdline := "foo ${TZ} bar"
	result := substCmdLine(cmdline)

	// Check that ${TZ} was replaced
	if strings.Contains(result, "${TZ}") {
		t.Error("${TZ} was not substituted")
	}

	// Check that result contains UTC (the TZ format)
	if !strings.Contains(result, "UTC") {
		t.Errorf("expected UTC in result, got %s", result)
	}

	// Check format is correct: UTC[+-]HH:MM
	if !strings.Contains(result, "foo UTC") {
		t.Errorf("unexpected result format: %s", result)
	}
}

func TestSubstCmdLineNoTZ(t *testing.T) {
	// Test that non-TZ cmdline is unchanged
	cmdline := "console=hvc0 root=/dev/vda"
	result := substCmdLine(cmdline)

	if result != cmdline {
		t.Errorf("expected %s, got %s", cmdline, result)
	}
}

func TestSubstCmdLineEmpty(t *testing.T) {
	result := substCmdLine("")
	if result != "" {
		t.Errorf("expected empty string, got %s", result)
	}
}

// TestSubstCmdLineUnknownVar verifies unknown variables are silently removed.
// Reference: tinyemu-2019-12-21/machine.c:129-172
func TestSubstCmdLineUnknownVar(t *testing.T) {
	// Unknown variable with closing brace - should output nothing for var
	result := substCmdLine("foo ${UNKNOWN} bar")
	if result != "foo  bar" {
		t.Errorf("expected 'foo  bar', got %q", result)
	}
}

// TestSubstCmdLineUnclosedBrace verifies C-matching behavior for unclosed braces.
// In C, ${TZ without closing brace still substitutes the timezone.
// Reference: tinyemu-2019-12-21/machine.c:137-147
func TestSubstCmdLineUnclosedBrace(t *testing.T) {
	// TZ without closing brace - should still substitute
	result := substCmdLine("foo ${TZ")
	if !strings.HasPrefix(result, "foo UTC") {
		t.Errorf("expected 'foo UTC...', got %q", result)
	}
	// Should NOT contain literal ${TZ
	if strings.Contains(result, "${TZ") {
		t.Errorf("${TZ should have been substituted, got %q", result)
	}

	// Unknown var without closing brace - should output nothing for var
	result2 := substCmdLine("foo ${UNKNOWN")
	if result2 != "foo " {
		t.Errorf("expected 'foo ', got %q", result2)
	}
}

func TestGetTimezoneString(t *testing.T) {
	tz := getTimezoneString()

	// Should start with UTC
	if !strings.HasPrefix(tz, "UTC") {
		t.Errorf("expected timezone to start with UTC, got %s", tz)
	}

	// Should have format UTC[+-]HH:MM
	if len(tz) < 9 { // UTC+00:00 is 9 chars
		t.Errorf("timezone string too short: %s", tz)
	}

	// Verify sign convention matches POSIX TZ format:
	// - East of UTC (positive offset) gets '-' sign
	// - West of UTC (negative offset) gets '+' sign
	// Reference: tinyemu-2019-12-21/machine.c:149-164
	_, offset := time.Now().Zone()
	if offset > 0 {
		// East of UTC - should have '-' sign in POSIX TZ format
		if tz[3] != '-' {
			t.Errorf("east of UTC should use '-' sign (POSIX TZ format), got %s", tz)
		}
	} else if offset < 0 {
		// West of UTC - should have '+' sign in POSIX TZ format
		if tz[3] != '+' {
			t.Errorf("west of UTC should use '+' sign (POSIX TZ format), got %s", tz)
		}
	}
}
