# Test Data

This directory contains test data for the TinyEMU RISC-V emulator.

## Directory Structure

- `riscv-tests/` - RISC-V ISA compliance tests
  - `isa/` - ISA test ELF binaries and reference outputs
- `boot/` - Boot images for integration testing
- `softfp/` - Soft float test vectors

## Usage

Test data in this directory is embedded into tests using Go's `embed` package.
See `testdata/embed.go` for the embed patterns.

## Adding Test Data

When adding new test data:
1. Place files in the appropriate subdirectory
2. Update the corresponding README.md with source and version info
3. Ensure embed patterns in `testdata/embed.go` cover the new files
