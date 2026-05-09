# TinyEMU-Go Image Build Plan

## Current State Analysis

### Existing Setup
- **Kernel**: 3.8MB (uncompressed Linux kernel)
- **BBL**: 53KB (Berkeley Boot Loader)
- **Root FS**: 4MB (ext2, busybox-based)
- **Total**: ~7.8MB

### Networking Stack
The emulator has a complete slirp (user-mode NAT) implementation:
- **DHCP server** at 10.0.2.2 (gateway)
- **DNS server** at 10.0.2.3 (forwards to host's DNS)
- Guest gets IP 10.0.2.15 via DHCP
- Full TCP/UDP/ICMP support

### Current Limitations
- No `/etc/resolv.conf` in rootfs
- No curl or TLS libraries
- No DHCP client configured to run at boot
- Uses static busybox (no dynamic networking tools)

---

## Design Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Minimal image base | Buildroot | Best size control, Alpine too large with curl |
| Larger image base | Ubuntu minimal (prebuilt) | Avoid cross-compile complexity |
| TLS library | OpenSSL | 500KB larger but better compatibility |
| Init system | Match distro default | Buildroot: busybox init, Ubuntu: systemd |
| Kernel | Shared between both | Simplicity, one build |
| Build artifacts | `images/` directory | Keep configs in version control |

---

## Phase 1: Buildroot Minimal Image (<10MB)

### Target Specifications
| Component | Size Target |
|-----------|-------------|
| Kernel | ~2.5MB (optimized, shared) |
| Root FS | ~7MB |
| BBL | ~50KB |
| **Total** | **<10MB** |

### Directory Structure

```
images/
├── buildroot/
│   ├── README.md                    # Build instructions
│   ├── configs/
│   │   └── tinyemu_riscv64_defconfig  # Buildroot defconfig
│   ├── board/
│   │   └── tinyemu/
│   │       ├── linux.config         # Kernel config fragment
│   │       ├── busybox.config       # Busybox config
│   │       ├── post-build.sh        # Post-build customization
│   │       └── rootfs-overlay/      # Files to add to rootfs
│   │           └── etc/
│   │               ├── init.d/
│   │               │   └── S40network
│   │               └── udhcpc.script
│   └── build.sh                     # Convenience build script
└── output/                          # Build outputs (gitignored)
    ├── kernel-riscv64.bin
    ├── root-minimal.bin
    └── root-ubuntu.bin
```

### Buildroot Configuration

**`configs/tinyemu_riscv64_defconfig`**:
```
# Architecture
BR2_riscv=y
BR2_RISCV_64=y
BR2_RISCV_ABI_LP64D=y
BR2_RISCV_ISA_RVC=y

# Toolchain (musl for size)
BR2_TOOLCHAIN_BUILDROOT_MUSL=y
BR2_TOOLCHAIN_BUILDROOT_CXX=n

# Kernel
BR2_LINUX_KERNEL=y
BR2_LINUX_KERNEL_CUSTOM_VERSION=y
BR2_LINUX_KERNEL_CUSTOM_VERSION_VALUE="6.6"
BR2_LINUX_KERNEL_USE_CUSTOM_CONFIG=y
BR2_LINUX_KERNEL_CUSTOM_CONFIG_FILE="$(BR2_EXTERNAL_TINYEMU_PATH)/board/tinyemu/linux.config"
BR2_LINUX_KERNEL_IMAGE_TARGET="Image"

# System
BR2_TARGET_GENERIC_HOSTNAME="tinyemu"
BR2_TARGET_GENERIC_ISSUE="TinyEMU Minimal Linux"
BR2_INIT_BUSYBOX=y
BR2_ROOTFS_DEVICE_CREATION_DYNAMIC_DEVTMPFS=y

# Root filesystem
BR2_TARGET_ROOTFS_EXT2=y
BR2_TARGET_ROOTFS_EXT2_4=y
BR2_TARGET_ROOTFS_EXT2_SIZE="8M"
BR2_TARGET_ROOTFS_EXT2_INODES=1024
BR2_ROOTFS_POST_BUILD_SCRIPT="$(BR2_EXTERNAL_TINYEMU_PATH)/board/tinyemu/post-build.sh"
BR2_ROOTFS_OVERLAY="$(BR2_EXTERNAL_TINYEMU_PATH)/board/tinyemu/rootfs-overlay"

# Packages
BR2_PACKAGE_BUSYBOX_CONFIG="$(BR2_EXTERNAL_TINYEMU_PATH)/board/tinyemu/busybox.config"
BR2_PACKAGE_CURL=y
BR2_PACKAGE_LIBCURL_CURL=y
BR2_PACKAGE_LIBCURL_VERBOSE=n
BR2_PACKAGE_OPENSSL=y
BR2_PACKAGE_CA_CERTIFICATES=y

# Disable unnecessary features
BR2_PACKAGE_BUSYBOX_SHOW_OTHERS=y
BR2_OPTIMIZE_S=y
BR2_STRIP_strip=y
```

### Kernel Config

**`board/tinyemu/linux.config`** (fragment to apply on top of defconfig):
```
# Size optimization
CONFIG_CC_OPTIMIZE_FOR_SIZE=y
CONFIG_EMBEDDED=y
CONFIG_EXPERT=y

# VirtIO (required for TinyEMU)
CONFIG_VIRTIO_MENU=y
CONFIG_VIRTIO_PCI=y
CONFIG_VIRTIO_BLK=y
CONFIG_VIRTIO_NET=y
CONFIG_VIRTIO_CONSOLE=y
CONFIG_HVC_DRIVER=y
CONFIG_HVC_RISCV_SBI=y

# Networking
CONFIG_NET=y
CONFIG_PACKET=y
CONFIG_UNIX=y
CONFIG_INET=y
CONFIG_IP_PNP=n
CONFIG_IP_PNP_DHCP=n

# Filesystems
CONFIG_EXT4_FS=y
CONFIG_PROC_FS=y
CONFIG_SYSFS=y
CONFIG_TMPFS=y
CONFIG_DEVTMPFS=y
CONFIG_DEVTMPFS_MOUNT=y

# Disable unnecessary subsystems
CONFIG_SOUND=n
CONFIG_USB_SUPPORT=n
CONFIG_WIRELESS=n
CONFIG_WLAN=n
CONFIG_BT=n
CONFIG_INPUT_MOUSE=n
CONFIG_INPUT_KEYBOARD=n
CONFIG_VGA_CONSOLE=n
CONFIG_FRAMEBUFFER_CONSOLE=n
CONFIG_DRM=n
CONFIG_FB=n
CONFIG_LOGO=n
CONFIG_NFS_FS=n
CONFIG_CIFS=n
CONFIG_NETWORK_FILESYSTEMS=n
CONFIG_PCMCIA=n
CONFIG_PCCARD=n
CONFIG_ATA=n
CONFIG_SCSI=n
CONFIG_MD=n
CONFIG_CRYPTO_USER=n
CONFIG_SECURITY=n
CONFIG_DEBUG_KERNEL=n
CONFIG_PRINTK_TIME=n
```

### Network Init Script

**`board/tinyemu/rootfs-overlay/etc/init.d/S40network`**:
```sh
#!/bin/sh

DAEMON="udhcpc"
IFACE="eth0"

start() {
    printf "Starting network: "
    ip link set "$IFACE" up
    udhcpc -i "$IFACE" -s /etc/udhcpc.script -p /var/run/udhcpc.pid -q -n
    [ $? = 0 ] && echo "OK" || echo "FAIL"
}

stop() {
    printf "Stopping network: "
    [ -f /var/run/udhcpc.pid ] && kill $(cat /var/run/udhcpc.pid) 2>/dev/null
    ip link set "$IFACE" down
    echo "OK"
}

case "$1" in
    start)
        start
        ;;
    stop)
        stop
        ;;
    restart)
        stop
        start
        ;;
    *)
        echo "Usage: $0 {start|stop|restart}"
        exit 1
esac
```

**`board/tinyemu/rootfs-overlay/etc/udhcpc.script`**:
```sh
#!/bin/sh
# Minimal udhcpc script for slirp networking

[ -z "$1" ] && exit 1

case "$1" in
    deconfig)
        ip addr flush dev "$interface"
        ip link set "$interface" up
        ;;
    bound|renew)
        ip addr flush dev "$interface"
        ip addr add "$ip/${mask:-24}" dev "$interface"

        if [ -n "$router" ]; then
            ip route add default via "$router" dev "$interface"
        fi

        # Write DNS servers
        : > /etc/resolv.conf
        for ns in $dns; do
            echo "nameserver $ns" >> /etc/resolv.conf
        done
        ;;
esac
```

### Build Script

**`images/buildroot/build.sh`**:
```bash
#!/bin/bash
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BUILDROOT_VERSION="2024.02.1"
BUILDROOT_DIR="$SCRIPT_DIR/buildroot-$BUILDROOT_VERSION"
OUTPUT_DIR="$SCRIPT_DIR/../output"

# Download buildroot if needed
if [ ! -d "$BUILDROOT_DIR" ]; then
    echo "Downloading Buildroot $BUILDROOT_VERSION..."
    curl -L "https://buildroot.org/downloads/buildroot-$BUILDROOT_VERSION.tar.gz" | tar xz -C "$SCRIPT_DIR"
fi

# Build
cd "$BUILDROOT_DIR"
make BR2_EXTERNAL="$SCRIPT_DIR" tinyemu_riscv64_defconfig
make -j$(nproc)

# Copy outputs
mkdir -p "$OUTPUT_DIR"
cp output/images/Image "$OUTPUT_DIR/kernel-riscv64.bin"
cp output/images/rootfs.ext4 "$OUTPUT_DIR/root-minimal.bin"

echo "Build complete!"
echo "  Kernel: $OUTPUT_DIR/kernel-riscv64.bin"
echo "  RootFS: $OUTPUT_DIR/root-minimal.bin"
```

---

## Phase 2: Ubuntu Minimal Image (<40MB)

### Approach

Use the **kernel built in Phase 1** with a **prebuilt Ubuntu minimal rootfs**.

Ubuntu provides official minimal base images:
- https://cloud-images.ubuntu.com/minimal/releases/
- https://cdimage.ubuntu.com/ubuntu-base/

### Target Specifications
| Component | Size Target |
|-----------|-------------|
| Kernel | ~2.5MB (shared from Phase 1) |
| Root FS | ~35MB |
| BBL | ~50KB |
| **Total** | **<40MB** |

### Build Steps

**`images/ubuntu/build.sh`**:
```bash
#!/bin/bash
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
OUTPUT_DIR="$SCRIPT_DIR/../output"
WORK_DIR="$SCRIPT_DIR/work"
ROOTFS_SIZE="40M"

# Ubuntu base image URL (noble = 24.04 LTS)
UBUNTU_VERSION="noble"
UBUNTU_BASE_URL="https://cdimage.ubuntu.com/ubuntu-base/releases/$UBUNTU_VERSION/release"
UBUNTU_TARBALL="ubuntu-base-24.04-base-riscv64.tar.gz"

mkdir -p "$WORK_DIR" "$OUTPUT_DIR"

# Download Ubuntu base if needed
if [ ! -f "$WORK_DIR/$UBUNTU_TARBALL" ]; then
    echo "Downloading Ubuntu base..."
    curl -L "$UBUNTU_BASE_URL/$UBUNTU_TARBALL" -o "$WORK_DIR/$UBUNTU_TARBALL"
fi

# Create rootfs directory
ROOTFS="$WORK_DIR/rootfs"
rm -rf "$ROOTFS"
mkdir -p "$ROOTFS"

# Extract base system
echo "Extracting Ubuntu base..."
sudo tar xzf "$WORK_DIR/$UBUNTU_TARBALL" -C "$ROOTFS"

# Configure the system
echo "Configuring system..."
sudo chroot "$ROOTFS" /bin/bash <<'EOF'
set -e

# Set up basic networking config for DHCP
cat > /etc/netplan/01-netcfg.yaml <<'NETPLAN'
network:
  version: 2
  ethernets:
    eth0:
      dhcp4: true
NETPLAN

# Set hostname
echo "tinyemu-ubuntu" > /etc/hostname

# Set root password
echo 'root:root' | chpasswd

# Enable serial console
mkdir -p /etc/systemd/system/serial-getty@hvc0.service.d
cat > /etc/systemd/system/serial-getty@hvc0.service.d/override.conf <<'GETTY'
[Service]
ExecStart=
ExecStart=-/sbin/agetty -o '-p -- \\u' --noclear --keep-baud hvc0 115200,38400,9600 $TERM
GETTY
systemctl enable serial-getty@hvc0.service

# Create fstab
cat > /etc/fstab <<'FSTAB'
/dev/vda    /       ext4    defaults    0 1
FSTAB

# Install curl and CA certificates
apt-get update
apt-get install -y --no-install-recommends curl ca-certificates

# Clean up to reduce size
apt-get clean
rm -rf /var/lib/apt/lists/*
rm -rf /var/cache/apt/*
rm -rf /usr/share/doc/*
rm -rf /usr/share/man/*
rm -rf /usr/share/info/*
rm -rf /usr/share/locale/*
rm -rf /var/log/*
rm -rf /tmp/*
EOF

# Create ext4 image
echo "Creating disk image..."
dd if=/dev/zero of="$OUTPUT_DIR/root-ubuntu.bin" bs=1M count=40
sudo mkfs.ext4 -d "$ROOTFS" "$OUTPUT_DIR/root-ubuntu.bin"

# Clean up
sudo rm -rf "$ROOTFS"

echo "Build complete!"
echo "  RootFS: $OUTPUT_DIR/root-ubuntu.bin"
echo ""
echo "Use with kernel from buildroot build:"
echo "  Kernel: $OUTPUT_DIR/kernel-riscv64.bin"
```

### Alternative: QEMU for chroot

If `chroot` fails for RISC-V binaries, use QEMU user-mode emulation:

```bash
# Install QEMU user-mode
sudo apt-get install qemu-user-static binfmt-support

# Register RISC-V handler
sudo update-binfmts --enable qemu-riscv64

# Now chroot will work transparently
sudo chroot "$ROOTFS" /bin/bash
```

---

## Configuration Files

### Minimal Image Config

**`testdata/boot/minimal.cfg`**:
```
{
    version: 1,
    machine: "riscv64",
    memory_size: 128,
    bios: "bbl64.bin",
    kernel: "../images/output/kernel-riscv64.bin",
    cmdline: "console=hvc0 root=/dev/vda rw",
    drive0: { file: "../images/output/root-minimal.bin" },
    eth0: { driver: "user" },
}
```

### Ubuntu Image Config

**`testdata/boot/ubuntu.cfg`**:
```
{
    version: 1,
    machine: "riscv64",
    memory_size: 256,
    bios: "bbl64.bin",
    kernel: "../images/output/kernel-riscv64.bin",
    cmdline: "console=hvc0 root=/dev/vda rw",
    drive0: { file: "../images/output/root-ubuntu.bin" },
    eth0: { driver: "user" },
}
```

---

## Implementation Checklist

### Phase 1: Buildroot Minimal
- [ ] Create `images/buildroot/` directory structure
- [ ] Write buildroot defconfig
- [ ] Write kernel config fragment
- [ ] Write busybox config (minimal networking)
- [ ] Create rootfs overlay (network scripts)
- [ ] Write build script
- [ ] Test build
- [ ] Test boot in emulator
- [ ] Test DHCP works (get IP from slirp)
- [ ] Test DNS works (`nslookup example.com`)
- [ ] Test curl works (`curl https://example.com`)
- [ ] Verify total size <10MB

### Phase 2: Ubuntu Minimal
- [ ] Create `images/ubuntu/` directory structure
- [ ] Write build script
- [ ] Download Ubuntu base tarball
- [ ] Configure with QEMU user-mode if needed
- [ ] Install curl + ca-certificates
- [ ] Configure netplan for DHCP
- [ ] Configure serial console
- [ ] Clean up to minimize size
- [ ] Test boot in emulator (with Phase 1 kernel)
- [ ] Test networking + curl
- [ ] Verify total size <40MB

---

## Size Budget

### Phase 1: Buildroot (<10MB)

| Component | Estimated | Notes |
|-----------|-----------|-------|
| Kernel (Image) | 2.5MB | Stripped, minimal drivers |
| musl libc | 0.6MB | |
| busybox | 0.5MB | Minimal config |
| libcurl | 0.4MB | |
| OpenSSL | 2.0MB | libssl + libcrypto |
| CA certificates | 0.2MB | |
| Filesystem overhead | 0.5MB | ext4 metadata |
| BBL | 0.05MB | |
| **Total** | **~6.8MB** | **Margin: 3.2MB** |

### Phase 2: Ubuntu (<40MB)

| Component | Estimated | Notes |
|-----------|-----------|-------|
| Kernel (shared) | 2.5MB | From Phase 1 |
| Ubuntu base | 25MB | After cleanup |
| curl + deps | 2MB | |
| CA certificates | 0.2MB | |
| Filesystem overhead | 2MB | ext4 metadata |
| BBL | 0.05MB | |
| **Total** | **~32MB** | **Margin: 8MB** |
