# Emulator performance optimization plan

The Alpine x86 boot currently takes ~10 wall-minutes to reach `localhost login:` on Apple Silicon. Profile-style reasoning (no measurement yet — that's step 0) suggests we're spending most of those cycles on per-access bookkeeping that *should* be O(1) but currently isn't. The plan below is ordered by expected wins; each step is small enough to land + verify against the test suite before moving on.

Target: cut boot-to-login wall time by **5–10×** without changing semantics. Stretch goal: real-time on a Mac.

## Step 0 — Measurement infrastructure

Before optimizing anything, establish a baseline and a way to compare. **Don't skip this.** Without it we'll spend hours on changes that look good but don't help, and miss easy wins because we don't realize they're easy.

- Add `TINYEMU_X86_PROFILE=1` that turns on `runtime/pprof` CPU profiling around the boot. Dump to `/tmp/temu.prof`. Look at it with `go tool pprof -top` and `go tool pprof -web`.
- Add cycle-rate metric: every 5 wall seconds, print `cycles/sec` (or expose via a periodic `[perf]` log line). Currently we have `cpu.GetCycles()` — just diff that against time.
- Pick a fixed boot phase as the benchmark: time-to-`Welcome to Alpine` from `temu` launch. Record current value in a `BENCH.md`. Update on every commit.

**Expected impact**: doesn't speed anything up, but every later step needs this to validate.

---

## Step 1 — `mem.PhysMemoryMap.GetRange` is a linear scan on every access

`GetRange` (mem/physmem.go:120) walks every range on every read/write/fetch:

```go
for i := range m.ranges {
    pr := &m.ranges[i]
    if pr.Size > 0 && paddr >= pr.Addr && paddr < pr.Addr+pr.Size { return pr }
}
```

For a typical PC machine the slice has ~10 entries (RAM, BIOS, BIOS-high, VGA, virtio-mmio for each device, …). That's 10 comparisons per memory access, called millions of times per second. The RAM range catches >99% of accesses but sits anywhere in the slice depending on registration order.

**Fix**: cache the last-hit `*PhysMemoryRange` in the map. On a fresh access, check the cache first; on miss, fall back to the linear scan and update the cache.

```go
type PhysMemoryMap struct {
    ranges []PhysMemoryRange
    last   *PhysMemoryRange // last successful GetRange
}

func (m *PhysMemoryMap) GetRange(paddr uint64) *PhysMemoryRange {
    if pr := m.last; pr != nil && pr.Size > 0 && paddr >= pr.Addr && paddr < pr.Addr+pr.Size {
        return pr
    }
    // ... existing linear scan ...
    if pr != nil { m.last = pr }
    return pr
}
```

Locality of memory access (instruction fetch from the same page, stack pushes near ESP) means the cache hit-rate should be >95%.

**Expected impact**: 10–30% speedup. Trivial change, high confidence.

**Risk**: zero — semantically a pure cache.

---

## Step 2 — TLB is already there but the wrappers re-walk too much

`cpu/x86/tlb.go` exists and is correct (direct-mapped, 64K entries, 4 KB granularity). But the hot wrappers don't always hit the fast path optimally:

- `translateAddress` is called *every* mem access. The TLB lookup is two map-keyed compares (idx, linTag) + four bool checks. Inlinable — make sure it actually inlines.
- The cross-page split functions I added in `8e7e2b5` / `19ee6fe` call `readMem8` byte-by-byte for the (rare) cross-page case. For the **hot** case (same page) they do nothing extra. Verify that branch predicts well.

**Sub-step 2a**: profile to confirm TLB hit-rate is >99%. If not, investigate why (over-aggressive invalidation? perm mismatch causing re-walks?).

**Sub-step 2b**: inline `tlb.lookup` into `translateAddress` — Go's mid-stack inliner *should* do this already but verify in the assembly (`go build -gcflags='-m=2'`).

**Sub-step 2c**: split `translateAddress` into a fetch-fast-path (`translateAddressForFetch`) and data-fast-path. Each can skip its irrelevant permission checks.

**Expected impact**: 20–40% speedup. Higher confidence after profile.

---

## Step 3 — Instruction prefetch buffer

Currently `fetch8` / `fetch16` / `fetch32` each do their own `translateAddress` + `readPhys` per call. For a 7-byte instruction we do 7 translations + 7 phys reads where 1 translation + 1 phys read would suffice (when the instruction doesn't cross a page).

**Fix**: a small instruction-stream cache. After decode-start, fetch 16 bytes once (or 32 — one cache line); subsequent `fetch*` pull from the buffer until exhausted or the instruction crosses a page.

```go
type CPU struct {
    // ...
    ifBuf      [16]byte   // instruction fetch buffer
    ifBufLip   uint32     // LIP corresponding to ifBuf[0]
    ifBufValid uint8      // how many valid bytes
}

func (c *CPU) fetch8() uint8 {
    lip := c.GetLIP()
    if c.ifBufValid > 0 && lip >= c.ifBufLip && lip < c.ifBufLip + uint32(c.ifBufValid) {
        b := c.ifBuf[lip - c.ifBufLip]
        c.eip++
        c.maskEIP()
        return b
    }
    return c.fetch8Slow()
}
```

Invalidate `ifBufValid = 0` on any control-flow transfer (jumps, calls, returns, interrupt delivery, segment loads). Hot-path stays tight.

**Expected impact**: 15–30% speedup on instruction-heavy workloads (most of the kernel).

**Risk**: bugs around the cross-page edge and invalidation. Need tests for: indirect branches, page-faults on instruction fetch, and the 15-byte instruction case.

---

## Step 4 — Eliminate `physWatchHook` overhead on the hot path

Every `readPhys32`/`writePhys32` calls `physWatchHook`/`physReadWatchHook`, which immediately returns when `physWatchLo == 0`:

```go
func (c *CPU) physWatchHook(addr, val uint32, size int) {
    if physWatchLo == 0 && physWatchHi == 0 { return }
    // ...
}
```

That's a function call + 2 compares + return on every access. Even after Go inlines it (which it should — both are trivial), it's not free.

**Fix**: gate at compile time via a build tag, OR (lighter) hoist the global check to an atomic boolean that's `false` by default. A `func (c *CPU) writePhys32` should look like:

```go
func (c *CPU) writePhys32(addr uint32, val uint32) {
    if c.physWatchActive { c.physWatchHook(addr, val, 4) }  // single bool check, cache-hot
    c.memMap.Write32(uint64(addr&c.a20Mask), val)
}
```

`physWatchActive` is set to `physWatchLo != 0 || physWatchHi != 0` at init. The branch predictor handles `false`-most-of-the-time perfectly.

**Expected impact**: 5–10% speedup.

**Risk**: zero — purely structural.

---

## Step 5 — Batch `REP MOVS` / `REP STOS` when no page boundary is crossed

Currently the string ops loop one element at a time (`cpu/x86/strings.go` `stos`, `movs`). For long copies — typical: kernel `clear_page` (4 KB STOSD), `memcpy` (>1 KB MOVSD) — that's thousands of `writeMem32` calls when one big `copy(dst, src)` would do.

**Fix**: in `executeString`, detect when:
- ECX is large enough that batching pays
- The source range fits in one page
- The dest range fits in one page
- DF=0 (forward; the common case)
- No segment override that complicates things

…and then do a single `mem.Copy(destPhys, srcPhys, count*size)` (or `mem.Fill` for STOS) on the underlying byte slices via `GetRAMPtr`. Decrement ECX appropriately and fall back to the loop for the tail.

**Expected impact**: 5–15% speedup overall, much more on memcpy-heavy workloads (e.g., `cp -a` of /firmware).

**Risk**: medium. Need careful handling of:
- Faults mid-batch (the SDM-correct behavior is "ECX reflects how far we got")
- Self-modifying writes (`REP STOS` over its own .text — pathological but possible)
- Cross-page (just fall back to the loop)

Cover with a unit test that copies across a page boundary and a STOS that fills exactly one page.

---

## Step 6 — Function-pointer dispatch instead of giant `switch`

`exec.go`'s main switch and 0F sub-switch each have hundreds of cases. Go's compiler turns dense switches into jump tables but sparse ones into binary searches. Verify with `go build -gcflags='-S'` whether the 0F switch is a jump table.

If it's not, replace with `[256]func(c *CPU) error` populated at init. Each handler is a `func(c *CPU)`. Hot-path dispatch becomes a single load + indirect call.

**Expected impact**: 5–15% on instruction throughput.

**Risk**: medium-low. Big rewrite, easy to introduce regressions. Land behind a build tag and benchmark both before/after.

---

## Step 7 — Reduce allocations on the hot path

`parseModRM` returns a struct by value — that's fine, no allocation. But check via `go build -gcflags='-m'` that none of:
- `c.handleInterruptCore`
- `c.executeString`
- Anything called from each `Step`

…allocates on every invocation. Each escape to heap is a GC tick eventually. The fix is usually to use a `*ModRMResult` field on the CPU struct (reusable) instead of returning by value.

**Expected impact**: 2–8%, mostly removes GC-induced pauses.

**Risk**: low.

---

## Step 8 — Bigger-page granularity for known RAM

Currently every 4 KB page in physical RAM is its own TLB-able mapping. The kernel maps everything at PSE-2MB granularity for the direct map. We could:

- Detect when CR4.PSE is on and a PDE has PS=1, and cache the 2 MB mapping
- The TLB entry covers a 2 MB region; a lookup masks linear by `~0x1FFFFF`

Linux uses 2 MB-mapped low memory for the kernel direct map and ~4 MB-mapped initial mapping. With this in place, kernel→kernel TLB hits stay hot across the whole 2 MB at once.

**Expected impact**: 5–20% on kernel-heavy phases (early boot, page-allocator activity).

**Risk**: medium. Need to make sure we still re-walk on permission downgrade and invalidate properly on `INVLPG`/CR3 reload.

---

## Step 9 — Compile-time disable expensive diagnostics

`physSentinelAddr`, `pfDebug`, `cpuidTrace`, `intDebug`, `espTrace`, `userPFDebug`, the half-dozen env-gated tracers we accumulated during debugging — each adds an `if globalBool { … }` to the hot path even when off.

**Fix**: behind a `tinyemu_diag` build tag. Default build has zero diagnostic overhead; `-tags=tinyemu_diag` build re-enables everything for debugging sessions.

**Expected impact**: 5–10% on default builds.

**Risk**: zero. Just shuffles code behind build constraints.

---

## Step 10 — Translation block cache (advanced, last resort)

The big traditional win is a "translation block" or "basic block" cache: decode an instruction once, cache its handler+operand specialization (e.g. a closure that does `EAX += [EBX+EDI*4+0x10]`), and execute the closure on subsequent visits. QEMU's TCG does this. Bochs doesn't and is slower.

This is a **lot** of work — basic blocks, invalidation on self-modifying code, recovery on faults mid-block. Only attempt after Steps 1–9 have plateaued.

**Expected impact**: 2–5× speedup, but high effort and lots of risk.

---

## Suggested order

1. **Step 0** (measurement infra) — half a day.
2. **Step 1** (`GetRange` cache) — one hour.
3. **Step 4** (hot-path diag check) — one hour.
4. Re-measure. If the boot is already 2× faster, that's solid progress and the next steps are easier to evaluate.
5. **Step 2** (TLB inlining + split) — half a day.
6. **Step 3** (instruction prefetch buffer) — one day.
7. Re-measure. We should be 3–5× faster by here.
8. **Step 5** (REP MOVS/STOS batching) — half a day.
9. **Step 7** (allocation audit) — half a day.
10. **Step 9** (diagnostic build tag) — one hour.
11. **Step 6** (function-pointer dispatch) — one day, only if profile says the dispatch dominates.
12. **Step 8** (2 MB TLB) — one day, only if profile says the TLB miss rate is high.
13. **Step 10** (block cache) — multi-week effort. Don't take this on without a clear win signal.

After each step, run the full test suite (`PATH=/opt/homebrew/bin:$PATH go test ./...`) AND boot Alpine to login — slowness rarely correlates with correctness, but the speedup ratios will tell us where we still have headroom.

## Cross-cutting

- **Always Profile First**: any "I bet X is slow" hypothesis has a ~50% chance of being wrong. `go tool pprof` will tell you.
- **Bench Before/After**: every change should report its measured impact in the commit message. If the speedup is <2%, the complexity isn't worth it.
- **Keep Correctness Tests Green**: the page-cross tests (and the rest of the suite) catch most regressions. Add new tests for any optimization that changes a hot-path invariant.
