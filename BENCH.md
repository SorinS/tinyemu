# Boot benchmarks

Tracking emulator boot wall-time and CPU throughput as we work through
`docs/Optimization.md`. Update on every commit that touches the hot
path.

## How to measure

Run either guest with the perf-print env var:

```
TINYEMU_X86_PERF=1 ./run_iso.sh alpine
TINYEMU_X86_PERF=1 ./run_iso.sh tinycore
```

Every 5 wall-seconds the emulator emits a `[perf]` line on stderr:

```
[perf] 12345678 cycles/sec  total=N  elapsed=Ts
```

For a profile (optional), add `TINYEMU_X86_PROFILE=1`. Output goes to
`/tmp/temu.prof`; analyse with:

```
go tool pprof -top /tmp/temu.prof
go tool pprof -web /tmp/temu.prof
```

## Headline metric

**Time from temu launch to `localhost login:` (Alpine)** — the primary
target. Stretch goal: real-time on Apple Silicon.

## Baseline (pre-optimization, 2026-05-18)

Apple M-series, darwin/arm64, Go default build.

| Phase                   | Wall time | Avg cycles/sec |
|-------------------------|-----------|----------------|
| Alpine → `localhost login:` | 541s (9:01) | ~19.8M |
| TinyCore → autologin    | TBD       | TBD            |

Notable phases observed:
- Kernel boot → userspace: ~160s wall
- `apk` install of 27 packages: ~35s wall
- `Verifying modloop` (single openssl pass over modloop.squashfs): **~110s wall** (single biggest contiguous segment)
- Coldplug (`Loading hardware drivers`): ~85s wall

## History

| Date       | Commit  | Step          | Alpine→login | cycles/sec | Notes |
|------------|---------|---------------|--------------|------------|-------|
| 2026-05-18 | b6af7ae | Step 0 setup  | 541s         | ~19.8M     | baseline; measurement infra only |
| 2026-05-18 | 3f94278 | Steps 1 + 4   | 531s         | ~20.8M     | GetRange cache + physWatch gate; ~1.85% — below expected. Pivot to profile-driven next. |
| 2026-05-18 | HEAD    | Step 3        | 501s         | ~21.5M     | Instruction prefetch buffer. +5.7% over previous; total 7.4% vs baseline. Modloop verify (openssl) ~33% faster on its own. |
