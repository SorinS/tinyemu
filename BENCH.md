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
| 2026-05-18 | 105517d | Step 3        | 501s         | ~21.5M     | Instruction prefetch buffer. +5.7% over previous; total 7.4% vs baseline. Modloop verify (openssl) ~33% faster on its own. |
| 2026-05-18 | a25b892 | Step 3 refine | 386s         | ~25.3M     | 32-byte buffer; bulk-copy RAM in fillFetchBuffer; eipBPActive bool gate (skip per-Step map lookup). +23% over Step 3; total **28.8% vs baseline**. Modloop verify 210s → 110s (~48% faster than baseline). |
| 2026-05-19 | 7dc7704 | async stdin   | 386s         | ~28.1M     | Stdin polling moved to a poll(2)-blocking goroutine; main loop drains a channel. The 28% syscall slice in profiles was averaged over HLT-idle phases (post-boot waits) — boot critical path is CPU-bound (modloop / apk) and saw no measurable improvement. Real-world gain is **post-boot idle responsiveness + ~28% less CPU during guest HLT**. cycles/sec metric rose because the rate samples include less syscall blocking time. Use a workload that includes idle waiting to see the win. |
| 2026-05-19 | d9e53bc | (lazy flags reverted) | 390s   | ~27.7M     | Attempted lazy flag computation (defer OF/SF/ZF/AF/PF; keep CF eager). Tests passed but boot got stuck in an infinite loop (cycles/sec spiked to 75B = tight no-progress loop, indicating a stale flag read somewhere I didn't audit — likely handleInterrupt's eflags push or another direct `c.eflags & EFLAGS_XX` read). Reverted. Recovering would need a full audit of every eflags-bit read in `cpu/x86/*.go` + a debug switch — deferred. **Optimization round stops here at 28.8% vs original baseline.** |
