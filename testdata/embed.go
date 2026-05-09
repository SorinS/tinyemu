// Package testdata provides embedded test data for the TinyEMU emulator.
package testdata

import (
	"embed"
)

// ISATests contains RISC-V ISA compliance test binaries and reference outputs.
// Test files are expected to be placed in riscv-tests/isa/.
//
//go:embed riscv-tests/isa/*
var ISATests embed.FS

// BootImages contains boot images for integration testing.
// Expected files: bbl64.bin, kernel-riscv64.bin, root-riscv64.bin
//
//go:embed boot/*
var BootImages embed.FS

// SoftFPTests contains soft float test vectors.
// Test vectors are stored as JSON files.
//
//go:embed softfp/*
var SoftFPTests embed.FS

// All embeds all testdata for tests that need access to everything.
//
//go:embed all:riscv-tests all:boot all:softfp
var All embed.FS
