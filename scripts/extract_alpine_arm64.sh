#!/bin/sh
# Fetch/build the arm64 "virt" boot assets into bin/alpine-arm64/:
#   Image                   flat arm64 Linux kernel (Alpine aarch64 virt, the
#                           EFI-zboot vmlinuz decompressed to a raw Image)
#   initramfs-virt          Alpine's own initramfs (for the full-init boot)
#   busybox-initramfs.gz    a minimal busybox-only initramfs that boots straight
#                           to a shell (no distro init, no boot media needed)
#
# Run with run_alpine-arm64.sh (full Alpine) or run_arm64.sh (busybox). Idempotent: skips downloads that are already present.
set -e

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
DIR="$ROOT/bin/alpine-arm64"
ALPINE_VER="${ALPINE_VER:-v3.21}"
ARCH=aarch64
CDN="https://dl-cdn.alpinelinux.org/alpine/$ALPINE_VER"
NETBOOT="$CDN/releases/$ARCH/netboot"
mkdir -p "$DIR"

# --- kernel (vmlinuz-virt) + initramfs ---
[ -f "$DIR/vmlinuz-virt" ] || curl -fsSL "$NETBOOT/vmlinuz-virt" -o "$DIR/vmlinuz-virt"
[ -f "$DIR/initramfs-virt" ] || curl -fsSL "$NETBOOT/initramfs-virt" -o "$DIR/initramfs-virt"

# --- decompress the EFI-zboot vmlinuz to a flat Image ---
# The arm64 vmlinuz is a "zboot" PE wrapper: a header ("MZ"+"zimg") with a gzip
# payload (offset @0x08, size @0x0c). Extract it and gunzip; a flat Image has the
# magic "ARMd" at offset 56.
if [ ! -f "$DIR/Image" ]; then
    f="$DIR/vmlinuz-virt"
    poff=$(od -An -tu4 -j8 -N4 "$f" | tr -d ' ')
    psize=$(od -An -tu4 -j12 -N4 "$f" | tr -d ' ')
    dd if="$f" bs=1 skip="$poff" count="$psize" status=none | gunzip > "$DIR/Image"
    echo "[extract_arm64] decompressed kernel -> Image ($(wc -c <"$DIR/Image") bytes)"
fi

# --- minimal busybox-only initramfs ---
if [ ! -f "$DIR/busybox-initramfs.gz" ]; then
    command -v cpio >/dev/null 2>&1 || { echo "need cpio to build the busybox initramfs" >&2; exit 1; }
    pkg=$(curl -s "$CDN/main/$ARCH/" | grep -oE 'busybox-static-[0-9][^"]*\.apk' | head -1)
    [ -n "$pkg" ] || { echo "could not find busybox-static apk" >&2; exit 1; }
    tmp=$(mktemp -d)
    curl -fsSL "$CDN/main/$ARCH/$pkg" -o "$tmp/bb.apk"
    tar -xzf "$tmp/bb.apk" -C "$tmp" 2>/dev/null || true
    bb="$tmp/bin/busybox.static"
    [ -f "$bb" ] || { echo "busybox.static not found in apk" >&2; exit 1; }

    rfs="$tmp/rootfs"
    mkdir -p "$rfs/bin" "$rfs/proc" "$rfs/sys" "$rfs/dev"
    cp "$bb" "$rfs/bin/busybox"; chmod +x "$rfs/bin/busybox"
    ( cd "$rfs/bin" && ln -sf busybox sh )
    cat > "$rfs/init" <<'EOF'
#!/bin/busybox sh
/bin/busybox mount -t proc none /proc 2>/dev/null
/bin/busybox mount -t sysfs none /sys 2>/dev/null
/bin/busybox mount -t devtmpfs none /dev 2>/dev/null
/bin/busybox --install -s /bin 2>/dev/null
echo "===== minimal busybox initramfs on temu arm64-virt ====="
/bin/busybox uname -a
exec /bin/busybox sh
EOF
    chmod +x "$rfs/init"
    ( cd "$rfs" && find . | LC_ALL=C cpio -o -H newc 2>/dev/null | gzip ) > "$DIR/busybox-initramfs.gz"
    rm -rf "$tmp"
    echo "[extract_arm64] built busybox-initramfs.gz ($(wc -c <"$DIR/busybox-initramfs.gz") bytes)"
fi

# --- full Alpine minirootfs as an initramfs (boots to a real Alpine shell with
#     apk, no install media / disk needed) ---
if [ ! -f "$DIR/alpine-rootfs-initramfs.gz" ]; then
    command -v cpio >/dev/null 2>&1 || { echo "need cpio for the Alpine rootfs initramfs" >&2; exit 1; }
    RELEASES="$CDN/releases/$ARCH"
    mr=$(curl -s "$RELEASES/" | grep -oE "alpine-minirootfs-[0-9][^\"]*-$ARCH\.tar\.gz" | sort -u | tail -1)
    [ -n "$mr" ] || { echo "could not find alpine-minirootfs tarball" >&2; exit 1; }
    tmp=$(mktemp -d)
    curl -fsSL "$RELEASES/$mr" -o "$tmp/mr.tar.gz"
    rfs="$tmp/rootfs"; mkdir -p "$rfs"
    tar -xzf "$tmp/mr.tar.gz" -C "$rfs"
    cat > "$rfs/init" <<'EOF'
#!/bin/sh
mount -t proc none /proc 2>/dev/null
mount -t sysfs none /sys 2>/dev/null
mount -t devtmpfs none /dev 2>/dev/null
export PATH=/usr/sbin:/usr/bin:/sbin:/bin HOME=/root TERM=linux
echo; echo "===== Alpine $(cat /etc/alpine-release 2>/dev/null) (aarch64) on temu virt ====="
exec /bin/sh
EOF
    chmod +x "$rfs/init"
    ( cd "$rfs" && find . | LC_ALL=C cpio -o -H newc 2>/dev/null | gzip ) > "$DIR/alpine-rootfs-initramfs.gz"
    rm -rf "$tmp"
    echo "[extract_arm64] built alpine-rootfs-initramfs.gz ($(wc -c <"$DIR/alpine-rootfs-initramfs.gz") bytes)"
fi

echo "[extract_arm64] arm64 assets ready in $DIR"
