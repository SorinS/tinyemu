package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/sorins/tinyemu-go/devices"
	"github.com/ulikunitz/xz"
)

// TestOpenBlockDeviceReadWrite tests opening a block device in read-write mode.
// Reference: tinyemu-2019-12-21/temu.c:307-347 (block_device_init)
func TestOpenBlockDeviceReadWrite(t *testing.T) {
	tmpDir := t.TempDir()
	diskPath := filepath.Join(tmpDir, "disk.img")

	// Create a test disk file
	diskData := make([]byte, 8192) // 16 sectors
	diskData[0] = 0xAA
	diskData[512] = 0xBB
	if err := os.WriteFile(diskPath, diskData, 0644); err != nil {
		t.Fatalf("failed to create disk file: %v", err)
	}

	// Open in read-write mode
	bd, err := openBlockDevice(diskPath, devices.ModeReadWrite)
	if err != nil {
		t.Fatalf("openBlockDevice failed: %v", err)
	}
	defer bd.Close()

	// Verify sector count
	if bd.GetSectorCount() != 16 {
		t.Errorf("expected 16 sectors, got %d", bd.GetSectorCount())
	}

	// Read first sector
	buf := make([]byte, 512)
	if _, err := bd.ReadSectors(0, buf, 1); err != nil {
		t.Fatalf("ReadSectors failed: %v", err)
	}
	if buf[0] != 0xAA {
		t.Errorf("expected first byte 0xAA, got 0x%02X", buf[0])
	}

	// Write to second sector
	writeBuf := make([]byte, 512)
	writeBuf[0] = 0xCC
	if _, err := bd.WriteSectors(1, writeBuf, 1); err != nil {
		t.Fatalf("WriteSectors failed: %v", err)
	}

	// Read back to verify
	if _, err := bd.ReadSectors(1, buf, 1); err != nil {
		t.Fatalf("ReadSectors failed: %v", err)
	}
	if buf[0] != 0xCC {
		t.Errorf("expected written byte 0xCC, got 0x%02X", buf[0])
	}
}

// TestOpenBlockDeviceReadOnly tests opening a block device in read-only mode.
// Reference: tinyemu-2019-12-21/temu.c:310 (BF_MODE_RO)
func TestOpenBlockDeviceReadOnly(t *testing.T) {
	tmpDir := t.TempDir()
	diskPath := filepath.Join(tmpDir, "disk.img")

	// Create a test disk file
	diskData := make([]byte, 4096) // 8 sectors
	diskData[0] = 0xAA
	if err := os.WriteFile(diskPath, diskData, 0644); err != nil {
		t.Fatalf("failed to create disk file: %v", err)
	}

	// Open in read-only mode
	bd, err := openBlockDevice(diskPath, devices.ModeReadOnly)
	if err != nil {
		t.Fatalf("openBlockDevice failed: %v", err)
	}
	defer bd.Close()

	// Reading should work
	buf := make([]byte, 512)
	if _, err := bd.ReadSectors(0, buf, 1); err != nil {
		t.Fatalf("ReadSectors failed: %v", err)
	}
	if buf[0] != 0xAA {
		t.Errorf("expected first byte 0xAA, got 0x%02X", buf[0])
	}

	// Writing should fail
	writeBuf := make([]byte, 512)
	writeBuf[0] = 0xBB
	_, err = bd.WriteSectors(0, writeBuf, 1)
	if err == nil {
		t.Error("expected write to fail in read-only mode")
	}
}

// TestOpenBlockDeviceSnapshot tests opening a block device in snapshot mode.
// Reference: tinyemu-2019-12-21/temu.c:337-340 (BF_MODE_SNAPSHOT)
func TestOpenBlockDeviceSnapshot(t *testing.T) {
	tmpDir := t.TempDir()
	diskPath := filepath.Join(tmpDir, "disk.img")

	// Create a test disk file
	diskData := make([]byte, 4096) // 8 sectors
	diskData[0] = 0xAA
	diskData[512] = 0xBB
	if err := os.WriteFile(diskPath, diskData, 0644); err != nil {
		t.Fatalf("failed to create disk file: %v", err)
	}

	// Open in snapshot mode
	bd, err := openBlockDevice(diskPath, devices.ModeSnapshot)
	if err != nil {
		t.Fatalf("openBlockDevice failed: %v", err)
	}
	defer bd.Close()

	// Reading original data should work
	buf := make([]byte, 512)
	if _, err := bd.ReadSectors(0, buf, 1); err != nil {
		t.Fatalf("ReadSectors failed: %v", err)
	}
	if buf[0] != 0xAA {
		t.Errorf("expected first byte 0xAA, got 0x%02X", buf[0])
	}

	// Writing should succeed (copy-on-write)
	writeBuf := make([]byte, 512)
	writeBuf[0] = 0xCC
	if _, err := bd.WriteSectors(0, writeBuf, 1); err != nil {
		t.Fatalf("WriteSectors failed: %v", err)
	}

	// Reading should return the written data
	if _, err := bd.ReadSectors(0, buf, 1); err != nil {
		t.Fatalf("ReadSectors failed: %v", err)
	}
	if buf[0] != 0xCC {
		t.Errorf("expected written byte 0xCC, got 0x%02X", buf[0])
	}

	// Second sector should still have original data
	if _, err := bd.ReadSectors(1, buf, 1); err != nil {
		t.Fatalf("ReadSectors failed: %v", err)
	}
	if buf[0] != 0xBB {
		t.Errorf("expected original byte 0xBB, got 0x%02X", buf[0])
	}

	// Close and reopen - the write should not have been persisted
	bd.Close()

	// Verify file was not modified
	fileData, err := os.ReadFile(diskPath)
	if err != nil {
		t.Fatalf("failed to read disk file: %v", err)
	}
	if fileData[0] != 0xAA {
		t.Errorf("snapshot mode modified the original file: expected 0xAA, got 0x%02X", fileData[0])
	}
}

// TestOpenBlockDeviceNotFound tests opening a non-existent block device.
func TestOpenBlockDeviceNotFound(t *testing.T) {
	_, err := openBlockDevice("/nonexistent/disk.img", devices.ModeReadOnly)
	if err == nil {
		t.Error("expected error for non-existent file")
	}
}

// TestDriveModeFlags tests that --rw and --ro flags work correctly.
// Reference: tinyemu-2019-12-21/temu.c:659,673-677
func TestDriveModeFlags(t *testing.T) {
	// This is a compile-time check that the flags exist
	// We can't easily test the actual flag parsing without running main()

	// Verify the flag variables exist and have correct defaults
	if driveRW == nil || driveRO == nil {
		t.Error("drive mode flags not initialized")
	}

	// Default values should be false
	if *driveRW {
		t.Error("driveRW should default to false")
	}
	if *driveRO {
		t.Error("driveRO should default to false")
	}
}

// TestStringSlice tests the stringSlice flag type used for repeatable flags.
func TestStringSlice(t *testing.T) {
	var s stringSlice

	// Initially empty
	if s.String() != "" {
		t.Errorf("expected empty string, got %q", s.String())
	}

	// Add values
	if err := s.Set("value1"); err != nil {
		t.Fatalf("Set failed: %v", err)
	}
	if err := s.Set("value2"); err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	// Check String representation
	if s.String() != "value1,value2" {
		t.Errorf("expected 'value1,value2', got %q", s.String())
	}

	// Check slice contents
	if len(s) != 2 {
		t.Fatalf("expected 2 elements, got %d", len(s))
	}
	if s[0] != "value1" || s[1] != "value2" {
		t.Errorf("unexpected slice contents: %v", s)
	}
}

// TestBuildConfigFromCLI tests building config from CLI arguments.
func TestBuildConfigFromCLI(t *testing.T) {
	// Save original flag values
	origBiosPath := *biosPath
	origKernelPath := *kernelPath
	origMachineType := *machineType
	defer func() {
		*biosPath = origBiosPath
		*kernelPath = origKernelPath
		*machineType = origMachineType
	}()

	// Test: no BIOS or kernel should fail
	*biosPath = ""
	*kernelPath = ""
	*machineType = "riscv64"
	_, err := buildConfigFromCLI()
	if err == nil {
		t.Error("expected error when no BIOS or kernel specified")
	}

	// Test: invalid machine type should fail
	*biosPath = "bios.bin"
	*machineType = "invalid_machine"
	_, err = buildConfigFromCLI()
	if err == nil {
		t.Error("expected error for invalid machine type")
	}

	// Test: valid config with BIOS
	*machineType = "riscv64"
	*biosPath = "bios.bin"
	*kernelPath = ""
	cfg, err := buildConfigFromCLI()
	if err != nil {
		t.Fatalf("buildConfigFromCLI failed: %v", err)
	}
	if cfg.Machine != "riscv64" {
		t.Errorf("expected machine riscv64, got %s", cfg.Machine)
	}
	if cfg.MemorySize != 256<<20 {
		t.Errorf("expected default memory size %d, got %d", 256<<20, cfg.MemorySize)
	}

	// Test: valid config with kernel only
	*biosPath = ""
	*kernelPath = "vmlinux"
	cfg, err = buildConfigFromCLI()
	if err != nil {
		t.Fatalf("buildConfigFromCLI failed: %v", err)
	}

	// Test: riscv32 machine type
	*machineType = "riscv32"
	cfg, err = buildConfigFromCLI()
	if err != nil {
		t.Fatalf("buildConfigFromCLI failed: %v", err)
	}
	if cfg.Machine != "riscv32" {
		t.Errorf("expected machine riscv32, got %s", cfg.Machine)
	}
}

// TestApplyCLIOverrides tests applying CLI overrides to config.
func TestApplyCLIOverrides(t *testing.T) {
	// Save original flag values
	origRamSize := *ramSizeMB
	origBiosPath := *biosPath
	origKernelPath := *kernelPath
	origInitrdPath := *initrdPath
	origAppendCmd := *appendCmd
	origNetUser := *netUser
	origDriveFiles := driveFiles
	origP9Shares := p9Shares
	defer func() {
		*ramSizeMB = origRamSize
		*biosPath = origBiosPath
		*kernelPath = origKernelPath
		*initrdPath = origInitrdPath
		*appendCmd = origAppendCmd
		*netUser = origNetUser
		driveFiles = origDriveFiles
		p9Shares = origP9Shares
	}()

	// Test: memory override
	cfg := &VMConfig{MemorySize: 128 << 20}
	*ramSizeMB = 512
	*biosPath = ""
	*kernelPath = ""
	*initrdPath = ""
	*appendCmd = ""
	*netUser = false
	driveFiles = nil
	p9Shares = nil
	applyCLIOverrides(cfg)
	if cfg.MemorySize != 512<<20 {
		t.Errorf("expected memory %d, got %d", 512<<20, cfg.MemorySize)
	}

	// Test: BIOS/kernel/initrd override
	cfg = &VMConfig{
		BIOS:   "old_bios.bin",
		Kernel: "old_kernel",
		Initrd: "old_initrd",
	}
	*ramSizeMB = 0
	*biosPath = "new_bios.bin"
	*kernelPath = "new_kernel"
	*initrdPath = "new_initrd"
	applyCLIOverrides(cfg)
	if cfg.BIOS != "new_bios.bin" {
		t.Errorf("expected BIOS 'new_bios.bin', got %q", cfg.BIOS)
	}
	if cfg.Kernel != "new_kernel" {
		t.Errorf("expected Kernel 'new_kernel', got %q", cfg.Kernel)
	}
	if cfg.Initrd != "new_initrd" {
		t.Errorf("expected Initrd 'new_initrd', got %q", cfg.Initrd)
	}

	// Test: cmdline append
	cfg = &VMConfig{CmdLine: "console=hvc0"}
	*appendCmd = "root=/dev/vda"
	*biosPath = ""
	*kernelPath = ""
	*initrdPath = ""
	applyCLIOverrides(cfg)
	if cfg.CmdLine != "console=hvc0 root=/dev/vda" {
		t.Errorf("expected combined cmdline, got %q", cfg.CmdLine)
	}

	// Test: cmdline set (empty original)
	cfg = &VMConfig{}
	applyCLIOverrides(cfg)
	if cfg.CmdLine != "root=/dev/vda" {
		t.Errorf("expected cmdline 'root=/dev/vda', got %q", cfg.CmdLine)
	}

	// Test: drive files
	cfg = &VMConfig{
		Drives: []DriveConfig{{File: "existing.img", Device: "virtio-blk"}},
	}
	*appendCmd = ""
	driveFiles = stringSlice{"new1.img", "new2.img"}
	applyCLIOverrides(cfg)
	if len(cfg.Drives) != 3 {
		t.Fatalf("expected 3 drives, got %d", len(cfg.Drives))
	}
	if cfg.Drives[1].File != "new1.img" {
		t.Errorf("expected second drive 'new1.img', got %q", cfg.Drives[1].File)
	}

	// Test: 9P shares with tags
	cfg = &VMConfig{}
	driveFiles = nil
	p9Shares = stringSlice{"/path1:tag1", "/path2"}
	applyCLIOverrides(cfg)
	if len(cfg.Filesystems) != 2 {
		t.Fatalf("expected 2 filesystems, got %d", len(cfg.Filesystems))
	}
	if cfg.Filesystems[0].File != "/path1" || cfg.Filesystems[0].Tag != "tag1" {
		t.Errorf("unexpected first filesystem: %+v", cfg.Filesystems[0])
	}
	if cfg.Filesystems[1].Tag != "/dev/root1" {
		t.Errorf("expected auto-generated tag '/dev/root1', got %q", cfg.Filesystems[1].Tag)
	}

	// Test: user networking
	cfg = &VMConfig{}
	p9Shares = nil
	*netUser = true
	applyCLIOverrides(cfg)
	if len(cfg.Networks) != 1 {
		t.Fatalf("expected 1 network, got %d", len(cfg.Networks))
	}
	if cfg.Networks[0].Driver != "user" {
		t.Errorf("expected driver 'user', got %q", cfg.Networks[0].Driver)
	}

	// Test: user networking not added if network already configured
	cfg = &VMConfig{
		Networks: []NetworkConfig{{Driver: "tap", IFName: "tap0"}},
	}
	applyCLIOverrides(cfg)
	if len(cfg.Networks) != 1 {
		t.Errorf("expected 1 network (not added), got %d", len(cfg.Networks))
	}
}

// TestLoadImageFileXZ tests loading xz-compressed image files (kernel, bios, initrd).
func TestLoadImageFileXZ(t *testing.T) {
	tmpDir := t.TempDir()

	// Create test data
	originalData := []byte("This is a test kernel image with some padding...")
	for len(originalData) < 1024 {
		originalData = append(originalData, byte(len(originalData)%256))
	}

	// Compress it with xz
	var compressed bytes.Buffer
	w, err := xz.NewWriter(&compressed)
	if err != nil {
		t.Fatalf("failed to create xz writer: %v", err)
	}
	w.Write(originalData)
	w.Close()

	// Write compressed file
	xzPath := filepath.Join(tmpDir, "kernel.bin.xz")
	if err := os.WriteFile(xzPath, compressed.Bytes(), 0644); err != nil {
		t.Fatalf("failed to write xz file: %v", err)
	}

	// Load and decompress
	data, err := loadImageFile(xzPath)
	if err != nil {
		t.Fatalf("loadImageFile failed: %v", err)
	}

	// Verify data matches
	if !bytes.Equal(data, originalData) {
		t.Errorf("decompressed data doesn't match original: got %d bytes, want %d bytes", len(data), len(originalData))
	}
}

// TestLoadImageFileUncompressed tests loading regular (non-compressed) image files.
func TestLoadImageFileUncompressed(t *testing.T) {
	tmpDir := t.TempDir()

	// Create test data
	originalData := []byte("This is an uncompressed kernel image")

	// Write uncompressed file
	path := filepath.Join(tmpDir, "kernel.bin")
	if err := os.WriteFile(path, originalData, 0644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}

	// Load file
	data, err := loadImageFile(path)
	if err != nil {
		t.Fatalf("loadImageFile failed: %v", err)
	}

	// Verify data matches
	if !bytes.Equal(data, originalData) {
		t.Errorf("data doesn't match original")
	}
}

// TestOpenBlockDeviceXZ tests opening an xz-compressed block device image.
func TestOpenBlockDeviceXZ(t *testing.T) {
	tmpDir := t.TempDir()

	// Create test disk data (must be sector-aligned)
	diskData := make([]byte, 4096) // 8 sectors
	diskData[0] = 0xAA
	diskData[512] = 0xBB
	diskData[1024] = 0xCC

	// Compress it with xz
	var compressed bytes.Buffer
	w, err := xz.NewWriter(&compressed)
	if err != nil {
		t.Fatalf("failed to create xz writer: %v", err)
	}
	if _, err := w.Write(diskData); err != nil {
		t.Fatalf("failed to write xz data: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("failed to close xz writer: %v", err)
	}

	// Write compressed file
	xzPath := filepath.Join(tmpDir, "disk.img.xz")
	if err := os.WriteFile(xzPath, compressed.Bytes(), 0644); err != nil {
		t.Fatalf("failed to write xz file: %v", err)
	}

	// Open xz-compressed image (default mode allows writes to memory)
	bd, err := openBlockDevice(xzPath, devices.ModeSnapshot)
	if err != nil {
		t.Fatalf("openBlockDevice failed: %v", err)
	}
	defer bd.Close()

	// Verify sector count
	if bd.GetSectorCount() != 8 {
		t.Errorf("expected 8 sectors, got %d", bd.GetSectorCount())
	}

	// Read and verify original data
	buf := make([]byte, 512)
	if _, err := bd.ReadSectors(0, buf, 1); err != nil {
		t.Fatalf("ReadSectors failed: %v", err)
	}
	if buf[0] != 0xAA {
		t.Errorf("expected first byte 0xAA, got 0x%02X", buf[0])
	}

	if _, err := bd.ReadSectors(1, buf, 1); err != nil {
		t.Fatalf("ReadSectors failed: %v", err)
	}
	if buf[0] != 0xBB {
		t.Errorf("expected second sector byte 0xBB, got 0x%02X", buf[0])
	}

	// Write should work (in-memory)
	writeBuf := make([]byte, 512)
	writeBuf[0] = 0xDD
	if _, err := bd.WriteSectors(0, writeBuf, 1); err != nil {
		t.Fatalf("WriteSectors failed: %v", err)
	}

	// Read back the write
	if _, err := bd.ReadSectors(0, buf, 1); err != nil {
		t.Fatalf("ReadSectors failed: %v", err)
	}
	if buf[0] != 0xDD {
		t.Errorf("expected written byte 0xDD, got 0x%02X", buf[0])
	}
}

// TestOpenBlockDeviceXZReadOnly tests opening an xz-compressed image in read-only mode.
func TestOpenBlockDeviceXZReadOnly(t *testing.T) {
	tmpDir := t.TempDir()

	// Create and compress test data
	diskData := make([]byte, 2048) // 4 sectors
	diskData[0] = 0xEE

	var compressed bytes.Buffer
	w, err := xz.NewWriter(&compressed)
	if err != nil {
		t.Fatalf("failed to create xz writer: %v", err)
	}
	w.Write(diskData)
	w.Close()

	xzPath := filepath.Join(tmpDir, "readonly.img.xz")
	os.WriteFile(xzPath, compressed.Bytes(), 0644)

	// Open in read-only mode
	bd, err := openBlockDevice(xzPath, devices.ModeReadOnly)
	if err != nil {
		t.Fatalf("openBlockDevice failed: %v", err)
	}
	defer bd.Close()

	// Read should work
	buf := make([]byte, 512)
	if _, err := bd.ReadSectors(0, buf, 1); err != nil {
		t.Fatalf("ReadSectors failed: %v", err)
	}
	if buf[0] != 0xEE {
		t.Errorf("expected byte 0xEE, got 0x%02X", buf[0])
	}

	// Write should fail
	writeBuf := make([]byte, 512)
	_, err = bd.WriteSectors(0, writeBuf, 1)
	if err == nil {
		t.Error("expected write to fail in read-only mode")
	}
}

// TestNewCLIFlags tests that all new CLI flags are properly defined.
func TestNewCLIFlags(t *testing.T) {
	// Verify all new flags exist and have expected defaults
	if biosPath == nil {
		t.Error("biosPath flag not initialized")
	}
	if kernelPath == nil {
		t.Error("kernelPath flag not initialized")
	}
	if initrdPath == nil {
		t.Error("initrdPath flag not initialized")
	}
	if machineType == nil {
		t.Error("machineType flag not initialized")
	}
	if smpCount == nil {
		t.Error("smpCount flag not initialized")
	}
	if netUser == nil {
		t.Error("netUser flag not initialized")
	}

	// Check defaults
	if *machineType != "riscv64" {
		t.Errorf("expected default machineType 'riscv64', got %q", *machineType)
	}
	if *smpCount != 1 {
		t.Errorf("expected default smpCount 1, got %d", *smpCount)
	}
}
