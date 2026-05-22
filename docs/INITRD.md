# Patched-Initrd Variants

How and why we patch Alpine's initramfs to speed up boots and work around
emulator quirks. Two scripts produce the artifacts:

- `scripts/extract_alpine.sh`   — 32-bit, ISO at `bin/alpine.iso` (used by
                                  `run_alpine_iso.sh` against `cpu/x86`).
- `scripts/extract_alpine64.sh` — 64-bit, ISO at `bin/alpine64/alpine-…iso`
                                  (used by `run64_iso.sh` against `cpu/x86_64`).

Both are idempotent — re-run safely; nothing changes unless the upstream
initrd or the script itself is newer than the output.

---

## Why patch the initrd at all?

When the emulator's MMU is software-only, every memory access costs ~50–200 ns
versus ~0.3 ns on silicon. That blows up two things in particular:

1. **`modloop` RSA-SHA verify** over the ~28 MB squashfs `modules.sqfs` —
   takes minutes wall-clock on darwin-arm64 to verify a signature we don't
   need (we trust our own ISO).
2. **`hwdrivers` udev cold-plug** — fires a modprobe storm across every PCI
   device, most of which aren't present, all of which fail-and-retry.

Neither step is required to reach a shell. They are *features* on real
hardware and *cost centres* under emulation.

Beyond speed, the 64-bit variant has a correctness reason too: `nlplug-findfs`
hangs indefinitely on `cpu/x86_64`. We patch around it (see "nonlplug
family" below) so boot can proceed at all.

---

## What's inside an Alpine initramfs?

Plain old `gzip-of-cpio` — no squashfs, no overlay, no signatures. The kernel's
`init/initramfs.c` understands cpio (newc format) optionally compressed with
gzip / xz / lz4 / zstd. Alpine ships gzip.

Unpacked, the interesting bits:

```
/init                  — small POSIX-sh script run by the kernel as PID 1
/sbin/nlplug-findfs    — discovers boot media via netlink/uevent
/lib/                  — busybox + a handful of utilities (mount, awk, …)
/etc/runlevels/        — empty in the initrd, but `/init` populates `$sysroot`
                         with OpenRC service symlinks before pivot
```

The orchestration of the whole early boot lives in `/init`. It runs
sysinit-style stages — load modules, mount boot media (via
`nlplug-findfs`), mount the rootfs (`alpine_dev=…` from kernel cmdline),
populate `/sysroot`, then `exec switch_root /sysroot /sbin/init` to hand
control to OpenRC.

This means: **edit `/init` in a copy of the initrd, repack, and you can
change boot behavior without rebuilding Alpine.**

---

## The trick in three steps

For any variant:

```sh
# 1. Decompress + unpack into a tmp dir
gunzip -c initrd | (cd "$tmp" && cpio -id)

# 2. Patch /init in place (awk does the actual work — see below)
awk '…' "$tmp/init" > "$tmp/init.new" && mv "$tmp/init.new" "$tmp/init"

# 3. Repack
(cd "$tmp" && find . | cpio -o -H newc) | gzip > "$out"
```

That's the entire build. The cpio newc format and gzip wrapper are not
arbitrary — the kernel rejects anything else for an initramfs. `cpio -o -H
newc` works identically under macOS (bsdcpio) and Linux (GNU cpio).

The `find` invocation must preserve all file modes and ownerships, which
`cpio -o -H newc` does (newc stores uid/gid/mode in the header). If you
ever switch to `tar` here, hardlinks inside busybox-utils break — don't.

---

## Variant A — "kill OpenRC services post-pivot"

OpenRC discovers what services to run by reading `/etc/runlevels/<level>/`
for symlinks. If the symlink isn't there, the service doesn't run.

The trick: just before `/init` does `exec switch_root "$sysroot"
/sbin/init`, inject `rm -f "$sysroot"/etc/runlevels/sysinit/modloop` and
similar lines. The initrd has already mounted the future rootfs at
`$sysroot`, so removing the symlinks there means the *real* `/sbin/init`
never finds them.

`extract_alpine.sh` does this with one awk rule:

```awk
/^exec switch_root/ {
    n = split(skip, paths, " ")
    for (i = 1; i <= n; i++) {
        print "rm -f \"$sysroot\"" paths[i]
    }
}
{ print }
```

`skip` is a space-separated list of paths passed via `awk -v`. Each path is
emitted as a `rm -f` line *before* the `exec switch_root` line is re-printed.
The `{ print }` at the end always echoes the current line, so the original
`/init` is preserved verbatim, with extra `rm` calls injected.

| Variant       | Removed services                                             | Approx savings |
|---------------|--------------------------------------------------------------|----------------|
| `nohw`        | `sysinit/hwdrivers`                                          | ~55 s          |
| `nomodloop`   | `sysinit/modloop`                                            | ~110 s         |
| `fast`        | `sysinit/hwdrivers` + `sysinit/modloop`                      | ~165 s         |
| `superfast`   | `fast` + `boot/syslog` + `boot/bootmisc` + `default/firstboot` | ~165 s + a few |

The timings are from `cpu/x86`-against-32-bit-Alpine on darwin-arm64; they
shift on different hosts and ISAs, but the ratio holds. Wall-clock savings
matter more than absolute numbers — `nomodloop` is the single biggest win,
which is why it's in every "fast" variant.

`superfast` is **not** for benchmarking — it cuts userspace init aggressively
and the resulting system isn't quite "real Alpine" anymore (no syslog, no
firstboot). Use `fast` if you want a meaningful boot-time measurement.

---

## Variant B — "bypass `nlplug-findfs`"

`nlplug-findfs` discovers the boot medium by listening to kernel netlink
uevents (`AF_NETLINK`/`NETLINK_KOBJECT_UEVENT`) and polling `/sys`. It hangs
indefinitely on `cpu/x86_64` (still unrooted — the parent shell blocks on
`futex(FUTEX_WAIT, val=-1)` waiting for a wake that never arrives; likely an
emulator atomic-op or memory-ordering interaction with musl pthread
internals).

Since we *know* the boot device — `alpine_dev=vda:iso9660` on the kernel
cmdline — we can replace the whole `nlplug-findfs` invocation block with a
direct `mount`:

```awk
/^ebegin "Mounting boot media"$/ {
    print "ebegin \"Mounting boot media (nlplug bypass)\""
    print "mkdir -p /media/cdrom"
    print "mount -r -t iso9660 /dev/vda /media/cdrom"
    skip = 1; next
}
skip && /^eend / { print; skip = 0; next }
!skip { print }
```

A two-state awk script: when we see the `ebegin "Mounting boot media"`
banner, emit a replacement block and set `skip=1`. While skipping, drop
every line until we see the matching `eend`, then re-emit `eend` and clear
the flag. Outside the skip region we pass lines through unchanged.

Side effects of the bypass:
- **modloop verification** is disabled (nlplug normally verifies it via
  `/.modloop.SHA256.sig`). For our purposes, fine; trust the ISO.
- **apkovl pickup** doesn't happen (the configured-system feature Alpine
  uses for diskless installs).

For just-get-to-a-shell, both are acceptable.

`nonlplug` outputs `bin/alpine64/initrd.nonlplug` and is **only built for
64-bit** — `cpu/x86` doesn't need the workaround.

---

## Combining both: `nonlplug-fast`

The two awk rules touch different lines (the boot-media block vs the
`switch_root` line), so they compose with no conflict. `build_nonlplug_fast`
runs both rules in one awk pass:

```awk
/^ebegin "Mounting boot media"$/ { …emit nlplug bypass…; skip=1; next }
skip && /^eend /                 { print; skip=0; next }
/^exec switch_root/              { …emit rm -f lines…; }
!skip { print }
```

Result: `bin/alpine64/initrd.nonlplug-fast` — nlplug bypass + `superfast`
service-killing in a single initrd. The path we recommend on 64-bit when
you don't need full-fidelity OpenRC startup.

---

## Idempotency

Every `build_*` function starts with:

```sh
if [ -f "$out" ] && [ ! "$INITRD" -nt "$out" ] && [ ! "$0" -nt "$out" ]; then
    return
fi
```

Translation: skip if the output already exists *and* the upstream initrd
isn't newer *and* the script itself isn't newer. Touching the build script
invalidates every variant — useful when you've changed an awk rule.

The first run after a fresh `git pull` will likely rebuild everything;
subsequent runs are no-ops.

---

## What doesn't this approach scale to?

- **Replacing binaries.** The patches are pure text-edits. To swap `/init`
  for a completely different script, just `mv`; to swap `/sbin/nlplug-findfs`
  for a custom binary, drop it in place. Both work, but neither is what we
  do — text edits keep `git diff` reviewable.
- **Different distros.** Buildroot/Yocto initramfs are structured
  similarly (gzip-cpio of a flat filesystem with an `/init` script). The
  same trick works; only the script-paths-to-edit change. Buildroot's
  default is `/init -> /sbin/init`, no userspace bring-up in initramfs;
  there's nothing useful to elide.
- **Image-based initrds (mkinitcpio dracut etc.)** Same gzip-cpio layout
  but with much more elaborate `/init` (multi-stage hooks, modules
  config). Possible but messier.

For Alpine's minimal busybox-based initrd, the text-edit approach hits the
sweet spot: one awk command per change, no build system, no kernel rebuild.

---

## File map

```
scripts/extract_alpine.sh           — 32-bit, builds bin/alpine/initrd*
scripts/extract_alpine64.sh         — 64-bit, builds bin/alpine64/initrd*
bin/alpine/initrd                   — upstream, unmodified
bin/alpine/initrd.nohw              — nohw variant
bin/alpine/initrd.nomodloop         — nomodloop variant
bin/alpine/initrd.fast              — fast variant
bin/alpine/initrd.superfast         — superfast variant
bin/alpine64/initrd                 — upstream, unmodified
bin/alpine64/initrd.nohw            — nohw variant
bin/alpine64/initrd.nomodloop       — nomodloop variant
bin/alpine64/initrd.fast            — fast variant
bin/alpine64/initrd.superfast       — superfast variant
bin/alpine64/initrd.nonlplug        — nlplug bypass only
bin/alpine64/initrd.nonlplug-fast   — nlplug bypass + superfast
run_alpine_iso.sh                   — 32-bit launcher, picks variant via arg
run64_iso.sh                        — 64-bit launcher, picks variant via arg
```

## Adding a new variant

Five lines in `extract_alpine64.sh`:

```sh
build_variant "$OUT/initrd.myvariant" \
    /etc/runlevels/sysinit/some-service \
    /etc/runlevels/boot/another-service
```

…and add the name to the case-statement in `run64_iso.sh`. That's it.

For a structural change (something more than removing service symlinks),
clone `build_nonlplug` as a template — awk rule pair (matcher + skip-end),
state machine in the script body.
