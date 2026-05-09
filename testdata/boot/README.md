# Boot Images

This directory contains boot images for integration testing.

## Expected Contents

- `bbl64.bin` - Berkeley Boot Loader for RV64
- `kernel-riscv64.bin` - Linux kernel image
- `root-riscv64.bin` - Root filesystem image

## Source

Images can be obtained from TinyEMU:
https://bellard.org/tinyemu/

Or built from source using buildroot:
https://github.com/buildroot/buildroot

## Building from Source

### Using Buildroot

```bash
git clone https://github.com/buildroot/buildroot
cd buildroot
make qemu_riscv64_virt_defconfig
make
```

Output will be in `output/images/`.

### TinyEMU Images

Download from: https://bellard.org/tinyemu/diskimage-linux-riscv-2018-09-23.tar.gz

## Image Format

- `bbl64.bin`: Binary bootloader, loads kernel at 0x80200000
- `kernel-riscv64.bin`: Uncompressed Linux kernel
- `root-riscv64.bin`: ext2 filesystem image

## Usage

Images are used by integration tests to verify:
1. Boot process completes successfully
2. Shell prompt is reached
3. Basic commands execute correctly
