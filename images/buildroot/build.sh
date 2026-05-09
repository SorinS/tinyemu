#!/bin/bash
#
# Build script for TinyEMU minimal Linux image using Buildroot
# Reference: docs/image-build-plan-1769311563.md
#
# Usage: ./build.sh [clean]
#   clean - remove buildroot directory and start fresh

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BUILDROOT_VERSION="2025.02.10"
BUILDROOT_URL="https://buildroot.org/downloads/buildroot-${BUILDROOT_VERSION}.tar.gz"
BUILDROOT_DIR="$SCRIPT_DIR/buildroot-${BUILDROOT_VERSION}"
OUTPUT_DIR="$SCRIPT_DIR/../output"
DEFCONFIG="tinyemu_riscv64_defconfig"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

info() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

error() {
    echo -e "${RED}[ERROR]${NC} $1"
    exit 1
}

# Handle clean argument
if [ "$1" = "clean" ]; then
    info "Cleaning build directory..."
    rm -rf "$BUILDROOT_DIR"
    rm -rf "$OUTPUT_DIR"/*.bin
    info "Clean complete"
    exit 0
fi

# Download buildroot if not present
if [ ! -d "$BUILDROOT_DIR" ]; then
    info "Downloading Buildroot ${BUILDROOT_VERSION}..."
    TARBALL="$SCRIPT_DIR/buildroot-${BUILDROOT_VERSION}.tar.gz"

    if [ ! -f "$TARBALL" ]; then
        curl -L "$BUILDROOT_URL" -o "$TARBALL" || error "Failed to download buildroot"
    fi

    info "Extracting buildroot..."
    tar xzf "$TARBALL" -C "$SCRIPT_DIR" || error "Failed to extract buildroot"

    # Clean up tarball to save space
    rm -f "$TARBALL"
fi

# Enter buildroot directory
cd "$BUILDROOT_DIR"

# Configure with our external tree
info "Configuring buildroot with TinyEMU defconfig..."
make BR2_EXTERNAL="$SCRIPT_DIR" "$DEFCONFIG" || error "Failed to configure buildroot"

# Build
info "Building (this will take a while on first run)..."
NPROC=$(nproc 2>/dev/null || echo 4)
make -j"$NPROC" || error "Build failed"

# Create output directory
mkdir -p "$OUTPUT_DIR"

# Copy outputs
info "Copying build outputs..."

# Prefer compressed kernel if available (only copy one)
if [ -f "output/images/Image.xz" ]; then
    cp "output/images/Image.xz" "$OUTPUT_DIR/kernel-riscv64.bin.xz"
    rm -f "$OUTPUT_DIR/kernel-riscv64.bin"  # Remove old uncompressed if exists
    info "  Kernel: $OUTPUT_DIR/kernel-riscv64.bin.xz (compressed)"
elif [ -f "output/images/Image" ]; then
    cp "output/images/Image" "$OUTPUT_DIR/kernel-riscv64.bin"
    rm -f "$OUTPUT_DIR/kernel-riscv64.bin.xz"  # Remove old compressed if exists
    info "  Kernel: $OUTPUT_DIR/kernel-riscv64.bin"
else
    warn "Kernel image not found"
fi

if [ -f "output/images/rootfs.ext2.xz" ]; then
    cp "output/images/rootfs.ext2.xz" "$OUTPUT_DIR/root-minimal.bin.xz"
    info "  RootFS: $OUTPUT_DIR/root-minimal.bin.xz (compressed)"
elif [ -f "output/images/rootfs.ext4" ]; then
    cp "output/images/rootfs.ext4" "$OUTPUT_DIR/root-minimal.bin"
    info "  RootFS: $OUTPUT_DIR/root-minimal.bin"
elif [ -f "output/images/rootfs.ext2" ]; then
    cp "output/images/rootfs.ext2" "$OUTPUT_DIR/root-minimal.bin"
    info "  RootFS: $OUTPUT_DIR/root-minimal.bin"
else
    warn "RootFS image not found"
fi

# Copy OpenSBI firmware (required for 6.x kernels)
if [ -f "output/images/fw_jump.bin" ]; then
    cp "output/images/fw_jump.bin" "$OUTPUT_DIR/fw_jump.bin"
    info "  OpenSBI: $OUTPUT_DIR/fw_jump.bin"
else
    warn "OpenSBI fw_jump.bin not found - enable BR2_TARGET_OPENSBI in config"
fi

# Report sizes
info "Build complete! Image sizes:"
echo ""

TOTAL_SIZE=0
for img in "$OUTPUT_DIR"/kernel-*.bin "$OUTPUT_DIR"/kernel-*.bin.xz "$OUTPUT_DIR"/root-*.bin "$OUTPUT_DIR"/root-*.bin.xz; do
    if [ -f "$img" ]; then
        SIZE=$(stat -c%s "$img" 2>/dev/null || stat -f%z "$img" 2>/dev/null)
        SIZE_MB=$(echo "scale=2; $SIZE / 1048576" | bc)
        TOTAL_SIZE=$((TOTAL_SIZE + SIZE))
        printf "  %-30s %6s MB\n" "$(basename "$img")" "$SIZE_MB"
    fi
done

if [ $TOTAL_SIZE -gt 0 ]; then
    TOTAL_MB=$(echo "scale=2; $TOTAL_SIZE / 1048576" | bc)
    echo "  ----------------------------------------"
    printf "  %-30s %6s MB\n" "TOTAL" "$TOTAL_MB"
    echo ""

    # Check against target
    TARGET_MB=10
    if [ "$(echo "$TOTAL_MB < $TARGET_MB" | bc)" -eq 1 ]; then
        info "Size target met: ${TOTAL_MB}MB < ${TARGET_MB}MB"
    else
        warn "Size target exceeded: ${TOTAL_MB}MB >= ${TARGET_MB}MB"
    fi
fi

echo ""
info "To run: go run ./cmd/temu images/output/minimal.cfg"
