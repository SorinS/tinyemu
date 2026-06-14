# go-asm — the assembly language server

`go-asm` is a Language Server for assembly, built on tinyemu-go's own
assembler, disassembler, and CPU cores. Beyond the usual editor niceties it can
**run and debug your code**: assemble the buffer, execute it in the emulator,
show the register/flag state inline, and single-step it like a debugger.

It speaks LSP over stdin/stdout with no dependencies beyond the standard library
and the in-repo packages, and targets Neovim's built-in client.

- Server: [`lsp/`](../lsp) (package `main`, binary `go-asm`)
- x86 assembler/disassembler: [`asm/`](../asm)
- RISC-V assembler/disassembler: [`asm/riscv/`](../asm/riscv)
- Execution backend: [`asm/emu/`](../asm/emu)
- Neovim module: [`lsp/nvim/go_asm.lua`](../lsp/nvim/go_asm.lua)

## Install

```sh
make install-go-asm          # → ~/.local/bin/go-asm  (override with PREFIX=…)
# or
make go-asm                  # → bin/go-asm
go build -o ~/.local/bin/go-asm ./lsp
```

`go-asm` is a self-contained binary (the instruction table is embedded), so it
runs from anywhere on your `PATH`.

## Neovim setup

Copy the module onto your config and require it:

```sh
cp lsp/nvim/go_asm.lua ~/.config/nvim/lua/go_asm.lua
```

```lua
-- in init.lua
require("go_asm")
```

That's all — the module does filetype detection (`.asm`/`.nasm`), starts the
server (`cmd = { "go-asm" }`, resolved from `PATH`), and binds the keymaps on
attach. (If you keep the file in the repo instead, add its directory to
`package.path` and `require("go_asm")`.)

### Keymaps

All under `<leader>` (Space on NvChad), active in an attached asm buffer:

| Key | Action |
|-----|--------|
| `<leader>rr` | **Run** the whole buffer (inline state on every line) |
| `<leader>rc` | **Run to cursor** (stop just before the cursor line) |
| `<leader>rg` | **Registers** — toggle a float with the full register file |
| `<leader>rx` | **Clear** the inline overlay |
| `<leader>rs` | **Step** one instruction (arms the session on first press) |
| `<leader>ro` | **Step over** — run a call to its return, not into it |
| `<leader>rS` | **Step back** — time-travel one instruction (exact replay) |
| `<leader>rR` | **Restart** the session at the entry |
| `<leader>rn` | **Continue** to the next breakpoint / end |
| `<leader>rb` | Toggle a **breakpoint** on the cursor line |
| `<leader>rB` | **Conditional breakpoint** — prompt for `reg op value` |
| `<leader>rm` | **Memory** — prompt for an address/register, hex-dump it |
| `<leader>rq` | **Quit** the debug session |
| `K` | Hover — encoded bytes + canonical decode (+ x86 forms) |

The register/memory floats (`<leader>rg` / `<leader>rm`) toggle on their own key
and also close when you move the cursor, or with `q` / `<Esc>` if you focus in.

## Two workflows

### Live run (fast inspection)
`<leader>rr` runs the whole program and annotates each executed line with the
registers/flags it changed; a `⇒` line under the last instruction shows the
final non-zero register file. `<leader>rc` does the same but stops just before
the cursor line — good for "what is the state at this point?". A status line
reports how it ended: `completed` (a balanced `ret`), `halted` (`hlt`/`wfi`),
`reached-line`, `max-steps` (an unbroken loop hit the cap), `fault` (a guest
exception — there is no IDT/trap vectoring, so faults are reported, not
delivered), or `ran-outside-program`.

```
  10   xor   eax, eax        ; ZF=1
  11   mov   rax, rbx        ; rax=0x0
  12   add   rax, [rbx+8]    ; rax=0x…
       ⇒  rax=0x1a6d  rbx=0x2ac2
```

### Stepping debugger (interactive)
The server holds a **live CPU session** per buffer, so you drive it instruction
by instruction:

1. `<leader>rb` on a line or two to set breakpoints (a `●` appears in the sign
   column).
2. `<leader>rs` to step — the first press arms the session at the entry; each
   press executes one instruction. The current line is highlighted, an arrow
   shows the step count, and the register file is shown under it.
3. `<leader>rn` to continue to the next breakpoint (or a clean end / fault).
4. `<leader>rq` to end the session.

The cursor follows the current instruction, so you can watch a loop iterate and
see registers change in place.

Niceties:
- **Step over** (`<leader>ro`) runs a call to its return instead of diving in.
- **Step back / restart** (`<leader>rS` / `<leader>rR`) — execution is
  deterministic, so the session replays exactly from the entry; you can walk
  backward through a run or re-arm at the start.
- **Conditional breakpoints** (`<leader>rB`) prompt for `reg op value` (e.g.
  `rcx == 0`, `rax > 0x100`); continue stops on that line only when it holds. A
  `◆` marks the line.
- **Memory inspector** (`<leader>rm`) prompts for an address — a number or a
  register name (resolved from the current state, e.g. `rsp`) — and shows a hex
  dump.

## Supported architectures

The server detects each buffer's ISA and routes every feature accordingly.

| ISA | Selected by | Backend | Registers |
|-----|-------------|---------|-----------|
| **x86-64** | default | `cpu/x86_64` (long mode, paging off) | `rax…r15` |
| **x86-32** | a `BITS 32` directive | `cpu/x86` (flat protected mode) | `eax…edi` |
| **RISC-V RV64GC (IMAFDC+Zicsr)** | a RISC-V `arch:` directive, else a mnemonic heuristic | `cpu/riscv` (RV64, FP on) | `zero…t6` + `ft0…ft11` (FP) |

Architecture detection (`emu.DetectArch`):
1. An explicit comment directive wins — `; arch: riscv64` or `; arch: x86`.
2. Otherwise a count of ISA-distinctive mnemonics decides (`addi`/`jal` vs
   `mov`/`push`).
3. Ties and empty buffers default to x86. For x86, a `BITS 32`/`BITS 64`
   directive picks the sub-mode (default 64).

The sandbox starts from power-on register values (x86: all zero except
`rdx=0x600`; RISC-V: all zero). Set up inputs explicitly; loops show the **last**
iteration's state per line.

## Editor features

- **Diagnostics** — each line is assembled live in the buffer's ISA. An unknown
  mnemonic is an error; a real instruction the encoder doesn't yet reach is a
  softer hint. Branch/jump targets resolve against the buffer's labels, so
  `je done` / `blt a0, a1, loop` are not flagged.
- **Hover** — encoded bytes + the canonical disassembly of those bytes (a
  cross-check: a decode that disagrees with what you wrote is a visible flag) +,
  for x86, the operand forms from NASM's table. Mode-aware (`mov eax, ebx` in a
  `BITS 32` buffer).
- **Completion** — mnemonics by prefix; x86 items carry their operand forms.
- **Signature help** — x86 operand forms with the active operand highlighted
  (RISC-V operands are positional, so this is x86-only).

## Protocol (custom requests)

Not part of standard LSP; the editor triggers them via keymaps.

`asm/run` — one-shot run. Params `{ textDocument, line, breakpoints? }`
(`line: -1` = whole program, `>=0` = run-to-cursor). Returns:

```json
{
  "arch": "x86", "bits": 64,
  "stop": "halted", "stopLine": -1, "steps": 146,
  "lines": [ { "line": 10, "text": "ZF=1", "regs": [ … ] }, … ],
  "final": [ { "name": "rax", "value": 6765, "hex": "0x1a6d" }, … ]
}
```

Each register carries an exact `hex` string (render that, not `value` — JSON
numbers lose precision above 2^53) and, for RISC-V FP registers, a `float`
interpretation (`{"name":"fa0","hex":"0xffffffff40400000","float":"3"}`). The
RISC-V register view is the 32 GPRs followed by `f0–f31`.

`asm/debug/start`, `asm/debug/step`, `asm/debug/stepover`, `asm/debug/stepback`,
`asm/debug/restart`, `asm/debug/continue`
(`{ …, breakpoints: [int], conditions: [{line,reg,op,value}] }`),
`asm/debug/memory` (`{ addr, count }` → `{ addr, bytes: [int] }`), and
`asm/debug/stop` — drive the per-document live session. The stepping calls
return a **DebugState**:

```json
{
  "arch": "x86", "bits": 64,
  "line": 11,                 // line about to execute, -1 if ended
  "regs":    [ {"name":"rax","value":1}, … ],
  "changed": [ {"name":"rax","value":1} ],   // what the last step changed
  "flags":   [ {"name":"ZF","value":0}, … ], // x86 only
  "stop": "",                 // "" while paused, else halted/completed/fault/…
  "steps": 5
}
```

## Architecture

```
lsp/            LSP server (stdio JSON-RPC, dispatch, arch routing, debug sessions)
  ├─ x86 features  → asm        (Assemble/Disassemble/Table, mode-aware)
  └─ riscv features→ asm/riscv  (Assemble/Disassemble/Mnemonics/labels)
asm/            x86/x86-64 assembler — data-driven from NASM's insns.dat
                (vendored, //go:embed); disasm via golang.org/x/arch
asm/riscv/      RISC-V assembler/disassembler — compact hand-written table
asm/emu/        execution backend: DetectArch → buildSandbox → run in
                cpu/x86_64 | cpu/x86 | cpu/riscv. Run() one-shots it;
                Session (Step/Continue/State) drives it for the debugger.
cpu/*           the CPU cores the emulator already ships
```

Why two assemblers, not one generalized table? x86 encoding is large and
irregular, so `asm` is **data-driven** from NASM's `insns.dat` (the complete
table) with the macro layers reimplemented in Go. RISC-V encoding is small and
regular (six fixed-field formats), so `asm/riscv` is a **hand-written table** —
the maintainable choice for that ISA. They share only the LSP shell.

## Validation

- **x86**: byte-exact against `nasm` (`-f bin`) in both `BITS 64` and `BITS 32`;
  disassembly via `golang.org/x/arch`; assemble→disassemble→re-assemble
  round-trips.
- **RISC-V**: byte-exact against `llvm-mc` (`--triple=riscv64 --mattr=+m,+a`);
  full label programs matched against `llvm-mc`'s ELF `.text`; round-trips. The
  CPU core (`cpu/riscv`) is itself differentially tested against **Spike**
  (`make test-riscv-spike`).
- **End-to-end**: headless Neovim drives the real server — attach, diagnostics,
  hover, `asm/run`, and the full step/breakpoint/continue debug flow — asserting
  the inline state. (This caught a `"diagnostics": null` vs `[]` bug the Go unit
  tests missed.)

Build oracles: `nasm` (Homebrew), `llvm-mc` (Homebrew LLVM at
`/opt/homebrew/opt/llvm/bin`), `spike` + `riscv64-unknown-elf-gcc`, `nvim`.

## Limitations / roadmap

- x86 assembler coverage grows with the encoder; SIMD/AVX/EVEX forms are not all
  reached yet (flagged as hints, not errors).
- RISC-V covers RV64I + M + A + Zicsr + basic privileged; F/D and C are not yet
  implemented in the assembler.
- The sandbox runs flat, position-fixed code with no MMU/trap setup; guest
  faults are reported, not handled.
- Possible next steps: a watch/evaluate-selection command, conditional
  breakpoints, and an "evaluate expression" prompt.
