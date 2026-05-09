# TinyEMU Buildroot Image

Minimal Linux image for TinyEMU using Buildroot. Target size: <10MB total.

## Prerequisites

- Linux host with standard build tools
- curl for downloading Buildroot
- ~5GB disk space for build

## Build

```bash
./build.sh
```

This will:
1. Download Buildroot (if not present)
2. Configure with TinyEMU defconfig
3. Build kernel, rootfs, and all packages
4. Copy outputs to `../output/`

## Directory Structure

```
images/buildroot/
├── external.desc           # Buildroot external tree descriptor
├── external.mk             # External package definitions (none)
├── Config.in               # External config options (none)
├── configs/
│   └── tinyemu_riscv64_defconfig  # Main Buildroot config
├── board/tinyemu/
│   ├── linux.config        # Kernel config fragment
│   ├── busybox.config      # Busybox config
│   ├── post-build.sh       # Post-build script
│   └── rootfs-overlay/     # Files copied to rootfs
│       └── etc/
│           ├── init.d/S40network
│           └── udhcpc.script
└── build.sh                # Build script
```

## Output

After build completes:
- `../output/kernel-riscv64.bin` - Linux kernel
- `../output/root-minimal.bin` - Root filesystem (ext4)

## Running

```bash
# From repository root
go run ./cmd/tinyemu testdata/boot/minimal.cfg
```

## Customization

- **Kernel config**: Edit `board/tinyemu/linux.config`
- **Packages**: Edit `configs/tinyemu_riscv64_defconfig`
- **Busybox**: Edit `board/tinyemu/busybox.config`
- **Root files**: Add to `board/tinyemu/rootfs-overlay/`

## Rebuilding

```bash
# Full rebuild
rm -rf buildroot-*
./build.sh

# Incremental (after config changes)
cd buildroot-*/
make
```

## Size Budget

| Component | Target |
|-----------|--------|
| Kernel | ~2.5MB |
| RootFS | ~7MB |
| **Total** | **<10MB** |
