#!/bin/bash
# Post-image script to compress kernel with xz and copy OpenSBI
# Reference: docs/image-build-plan-1769311563.md

BINARIES_DIR="$1"

# Compress kernel if not already compressed
if [ -f "$BINARIES_DIR/Image" ] && [ ! -f "$BINARIES_DIR/Image.xz" ]; then
    echo "Compressing kernel with xz..."
    xz -k -9 "$BINARIES_DIR/Image"
    echo "Created Image.xz ($(du -h "$BINARIES_DIR/Image.xz" | cut -f1))"
fi

# Copy OpenSBI firmware if built
# OpenSBI fw_jump.bin is used as BIOS for TinyEMU with modern kernels
OPENSBI_FW="$BINARIES_DIR/fw_jump.bin"
if [ -f "$OPENSBI_FW" ]; then
    echo "OpenSBI firmware found: $(du -h "$OPENSBI_FW" | cut -f1)"
else
    echo "Warning: OpenSBI fw_jump.bin not found in $BINARIES_DIR"
fi
