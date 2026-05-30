package pc

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// These tests are the "passing tests = working boot" guarantee. They
// spawn the actual temu binary against staged kernel/initrd images and
// FAIL when the kernel can't reach an expected boot checkpoint inside a
// wall-clock budget. They exercise the same code path users invoke, so
// nothing differs between "the test setup" and "what the user runs".
//
// They skip — not fail — when the binary or image isn't built/staged.
// A fresh clone can `go test ./...` without first running the iso
// scripts; CI nodes (or anyone who has run `bin/extract-…`) get real
// coverage. They also honour `testing.Short()` and `TINYEMU_SKIP_BOOT_TESTS=1`.

const (
	bootBudgetTinyBanner   = 60 * time.Second
	bootBudgetTinyInitrd   = 90 * time.Second
	bootBudgetAlpineBanner = 120 * time.Second
	// Alpine all the way through switch_root + OpenRC. The full path
	// is dominated by initramfs unpack (~190 kernel-sec) plus the
	// driver-load / package-install steps. 12 minutes gives ample
	// headroom on a slow host; well-running hosts finish in ~8.
	bootBudgetAlpineUserspace = 12 * time.Minute
)

// repoRoot returns the absolute repository root, computed from this
// test file's location.
func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	// machine/pc → ../..
	return filepath.Clean(filepath.Join(wd, "..", ".."))
}

// temuBinary returns the path to the host's temu binary; skips the test
// if it isn't built.
func temuBinary(t *testing.T) string {
	t.Helper()
	root := repoRoot(t)
	name := "temu." + runtime.GOOS + "-" + runtime.GOARCH + ".bin"
	bin := filepath.Join(root, "bin", name)
	if _, err := os.Stat(bin); err != nil {
		t.Skipf("temu binary not built (%s); run `go build -o %s ./cmd/temu`", bin, bin)
	}
	return bin
}

func requireFile(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Skipf("required test image not staged: %s", path)
	}
}

func skipIfBootTestsDisabled(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("-short: skipping boot smoke test")
	}
	if os.Getenv("TINYEMU_SKIP_BOOT_TESTS") == "1" {
		t.Skip("TINYEMU_SKIP_BOOT_TESTS=1: skipping boot smoke test")
	}
}

// runTemuExpect spawns the temu binary with `args`, captures stdout +
// stderr, and waits until either `want` appears in the combined output
// or `budget` elapses. Returns the captured output (so the test can log
// the tail on failure) and a success bool.
func runTemuExpect(t *testing.T, args []string, want string, budget time.Duration) (string, bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), budget)
	defer cancel()
	bin := temuBinary(t)
	cmd := exec.CommandContext(ctx, bin, args...)
	// Don't inherit a TTY: temu refuses to use raw mode on a
	// non-TTY and our pipe IS a non-TTY. stdin closed → guest sees EOF.
	cmd.Stdin = nil

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("StdoutPipe: %v", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		t.Fatalf("StderrPipe: %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("temu start: %v", err)
	}

	// Drain both streams into a shared buffer, polling for `want`.
	var mu = bytes.Buffer{}
	done := make(chan struct{}, 2)
	drain := func(r io.Reader) {
		defer func() { done <- struct{}{} }()
		buf := make([]byte, 4096)
		for {
			n, _ := r.Read(buf)
			if n > 0 {
				mu.Write(buf[:n])
			}
			if n == 0 {
				return
			}
		}
	}
	go drain(stdout)
	go drain(stderr)

	found := false
	deadline := time.Now().Add(budget)
	for time.Now().Before(deadline) {
		if strings.Contains(mu.String(), want) {
			found = true
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	// Tear the process down regardless. Kill, then wait so resources
	// are released; ignore "killed" exit error.
	_ = cmd.Process.Kill()
	cmd.Wait()
	<-done
	<-done
	return mu.String(), found
}

func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return "…" + s[len(s)-n:]
}

// TestBoot64_TinyCore_ReachesKernelBanner is the fastest x86_64 boot
// regression guard. TinyCorePure64's tiny ELF kernel exercises long-mode
// entry, IDT setup, the UART path, and the basic interrupt plumbing.
// "Linux version" appearing means: bzimage64 / vmlinux64 loader entered
// long mode correctly, the early paging is right, the IDT is installed,
// and ttyS0 is wired. If any one regresses, this test fails.
func TestBoot64_TinyCore_ReachesKernelBanner(t *testing.T) {
	skipIfBootTestsDisabled(t)
	root := repoRoot(t)
	kernel := filepath.Join(root, "bin/tinycore64/vmlinux64")
	requireFile(t, kernel)
	args := []string{
		"-machine", "x86_64", "-m", "128", "-kernel", kernel, "-append",
		"console=ttyS0,115200 loglevel=8 earlyprintk=ttyS0,115200 " +
			"noapic nolapic acpi=off pci=noacpi nosmp nokaslr tsc=reliable",
	}
	out, ok := runTemuExpect(t, args, "Linux version", bootBudgetTinyBanner)
	if !ok {
		t.Fatalf("tinycore kernel did not print \"Linux version\" within %v.\nLast output:\n%s",
			bootBudgetTinyBanner, tail(out, 1500))
	}
}

// TestBoot64_TinyCore_ReachesInitramfs takes the next step: the kernel
// has printed its banner AND unpacked an initramfs. The marker is the
// kernel's "Unpacking initramfs" / "Freeing initrd memory" pair. This
// proves the page allocator, copy_to_kernel paths, and gunzip work.
func TestBoot64_TinyCore_ReachesInitramfs(t *testing.T) {
	skipIfBootTestsDisabled(t)
	root := repoRoot(t)
	kernel := filepath.Join(root, "bin/tinycore64/vmlinux64")
	initrd := filepath.Join(root, "bin/tinycore64/corepure64.gz")
	requireFile(t, kernel)
	requireFile(t, initrd)
	args := []string{
		"-machine", "x86_64", "-m", "128", "-kernel", kernel, "-initrd", initrd, "-append",
		"console=ttyS0,115200 loglevel=8 earlyprintk=ttyS0,115200 " +
			"noapic nolapic acpi=off pci=noacpi nosmp nokaslr tsc=reliable",
	}
	// We look for either marker — "Unpacking initramfs..." or the
	// final "Freeing initrd memory" announcement.
	for _, marker := range []string{"Unpacking initramfs", "Freeing initrd memory"} {
		out, ok := runTemuExpect(t, args, marker, bootBudgetTinyInitrd)
		if ok {
			return
		}
		t.Logf("did not see %q (continuing); tail:\n%s", marker, tail(out, 400))
	}
	t.Fatalf("tinycore did not unpack initramfs within %v", bootBudgetTinyInitrd)
}

// TestBoot64_Alpine_ReachesKernelBanner is the Alpine-side guard. Alpine
// uses a real bzImage that goes through the bzImage64 loader, the
// 32-bit-to-long-mode transition (the path where the 0xEA-LMA bug
// lived), and the PCI/virtio init plumbing. Reaching the banner means
// the full long-mode entry path is intact.
func TestBoot64_Alpine_ReachesKernelBanner(t *testing.T) {
	skipIfBootTestsDisabled(t)
	root := repoRoot(t)
	kernel := filepath.Join(root, "bin/alpine64/vmlinuz")
	initrd := filepath.Join(root, "bin/alpine64/initrd")
	requireFile(t, kernel)
	requireFile(t, initrd)
	args := []string{
		"-machine", "x86_64", "-m", "512", "-kernel", kernel, "-initrd", initrd, "-append",
		"console=ttyS0,115200 loglevel=8 noapic nolapic acpi=off pci=noacpi nosmp " +
			"nokaslr tsc=reliable libata.force=disable ide=disable",
	}
	out, ok := runTemuExpect(t, args, "Linux version", bootBudgetAlpineBanner)
	if !ok {
		t.Fatalf("alpine kernel did not print \"Linux version\" within %v.\nLast output:\n%s",
			bootBudgetAlpineBanner, tail(out, 1500))
	}
}

// TestBoot64_Alpine_ReachesUserspace is the "alpine actually boots"
// guard. It uses the patched nonlplug initrd — the same one that
// run64_iso.sh now defaults to — and waits for an OpenRC marker
// that proves switch_root succeeded and userspace init is alive.
//
// This is the test that should have existed when the "Mounting boot
// media" hang regressed silently. With this in place, any future
// regression in the post-banner path (virtio data plane, ELF loader,
// userspace exec, x87/SSE, syscall surface) fails CI within minutes
// instead of being discovered by someone trying to boot interactively.
//
// We grep for "OpenRC" rather than a shell prompt because the
// runTemuExpect helper feeds stdin=closed, which means busybox's
// getty/login never produces an interactive prompt. OpenRC's
// startup message is the latest deterministic marker we can match.
func TestBoot64_Alpine_ReachesUserspace(t *testing.T) {
	skipIfBootTestsDisabled(t)
	root := repoRoot(t)
	kernel := filepath.Join(root, "bin/alpine64/vmlinuz")
	initrd := filepath.Join(root, "bin/alpine64/initrd.nonlplug")
	iso := filepath.Join(root, "bin/alpine/alpine-standard-3.23.4-x86_64.iso")
	requireFile(t, kernel)
	requireFile(t, initrd)
	requireFile(t, iso)
	args := []string{
		"-machine", "x86_64", "-m", "512",
		"-kernel", kernel, "-initrd", initrd,
		"-drive", iso, "-ro",
		"-net-user",
		"-append",
		"console=ttyS0,115200 noapic nolapic acpi=off pci=noacpi nosmp " +
			"nokaslr tsc=reliable libata.force=disable ide=disable " +
			"alpine_dev=vda:iso9660 usbdelay=1 " +
			"modules=virtio_pci,virtio_blk,virtio_net,loop,squashfs " +
			"module.sig_enforce=0 " +
			"modprobe.blacklist=ata_piix,pata_acpi,usb-storage,usbhid",
	}
	out, ok := runTemuExpect(t, args, "OpenRC", bootBudgetAlpineUserspace)
	if !ok {
		t.Fatalf("alpine did not reach OpenRC within %v — boot regressed somewhere "+
			"between kernel banner and switch_root.\nLast output:\n%s",
			bootBudgetAlpineUserspace, tail(out, 2500))
	}
}
