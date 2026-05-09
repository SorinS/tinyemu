#!/bin/bash
#
# Buildroot post-build script for TinyEMU
# Called by buildroot after building all packages, before creating rootfs image
# Reference: docs/image-build-plan-1769311563.md
#
# Arguments:
#   $1 = target rootfs directory (e.g., output/target)
#
# Environment:
#   BR2_CONFIG = path to .config
#   HOST_DIR = path to host tools
#   STAGING_DIR = path to staging directory
#   TARGET_DIR = path to target directory (same as $1)
#   BUILD_DIR = path to build directory
#   BINARIES_DIR = path to images directory

TARGET_DIR="$1"

# Ensure /etc/resolv.conf exists (will be populated by DHCP)
touch "$TARGET_DIR/etc/resolv.conf"

# Create /var/run for pid files
mkdir -p "$TARGET_DIR/var/run"

# Ensure init scripts are executable
chmod +x "$TARGET_DIR/etc/init.d/"* 2>/dev/null || true
chmod +x "$TARGET_DIR/etc/udhcpc.script" 2>/dev/null || true

# Set root password to empty (passwordless login)
# This modifies /etc/shadow
if [ -f "$TARGET_DIR/etc/shadow" ]; then
    sed -i 's|^root:[^:]*:|root::|' "$TARGET_DIR/etc/shadow"
fi

# Always configure inittab for auto-login (no login prompt)
# This is essential for testing and typical emulator use
# Only spawn shell on hvc0 (VirtIO console) to avoid competition
cat > "$TARGET_DIR/etc/inittab" << 'EOF'
# TinyEMU inittab - auto-login enabled for hvc0 console
::sysinit:/bin/mount -t proc proc /proc
::sysinit:/bin/mount -o remount,rw /
::sysinit:/bin/mkdir -p /dev/pts /dev/shm
::sysinit:/bin/mount -a
::sysinit:/bin/hostname -F /etc/hostname
::sysinit:/etc/init.d/rcS

# Auto-login on hvc0 only (VirtIO console)
# Using 'respawn' ensures shell restarts if it exits
# -n = no login prompt, -l = run /bin/sh as login shell
hvc0::respawn:/sbin/getty -n -l /bin/sh hvc0 0 vt100

::shutdown:/etc/init.d/rcK
::shutdown:/sbin/swapoff -a
::shutdown:/bin/umount -a -r
EOF

# Create fstab if not present
if [ ! -f "$TARGET_DIR/etc/fstab" ]; then
    cat > "$TARGET_DIR/etc/fstab" << 'EOF'
# TinyEMU fstab
/dev/vda        /               ext4    defaults,noatime    0 1
proc            /proc           proc    defaults            0 0
sysfs           /sys            sysfs   defaults            0 0
devpts          /dev/pts        devpts  defaults,gid=5,mode=620 0 0
tmpfs           /tmp            tmpfs   defaults            0 0
tmpfs           /var/run        tmpfs   defaults            0 0
EOF
fi

echo "Post-build script completed for TinyEMU"
