# go-asm — the assembly language server

`go-asm` is a Language Server for assembly, built on tinyemu-go's own
assembler, disassembler, and CPU cores. Beyond the usual editor niceties it can
**run your code**: assemble the buffer, execute it in the emulator, and show the
register/flag state each line produced as inline virtual text.

It speaks LSP over stdin/stdout with no dependencies beyond the standard library
and the in-repo packages, and targets Neovim's built-in client.

- Server: [`lsp/`](../lsp) (package `main`, binary `bin/go-asm`)
- x86 assembler/disassembler: [`asm/`](../asm)
- RISC-V assembler/disassembler: [`asm/riscv/`](../asm/riscv)
- Execution backend: [`asm/emu/`](../asm/emu)
- Neovim glue: [`lsp/nvim/asm-live.lua`](../lsp/nvim/asm-live.lua)

## Supported architectures

The server detects each buffer's ISA and routes every feature accordingly.

| ISA | Selected by | Backend | Registers |
|-----|-------------|---------|-----------|
| **x86-64** | default | `cpu/x86_64` (long mode, paging off) | `rax…r15` |
| **x86-32** | a `BITS 32` directive | `cpu/x86` (flat protected mode) | `eax…edi` |
| **RISC-V RV64I+M+A+Zicsr** | a RISC-V `arch:` directive, else a mnemonic heuristic | `cpu/riscv` (RV64, paging off) | `zero, ra, sp, …` (ABI) |

Architecture detection (`emu.DetectArch`):
1. An explicit comment directive wins — `; arch: riscv64` or `; arch: x86`.
2. Otherwise a count of ISA-distinctive mnemonics decides (e.g. `addi`/`jal`
   vs `mov`/`push`).
3. Ties and empty buffers default to x86. For x86, a `BITS 32`/`BITS 64`
   directive then picks the sub-mode (default 64).

## Features

### Diagnostics
Every line is assembled live, in the buffer's ISA. An **unknown mnemonic** is an
error; a real instruction the encoder doesn't yet reach is a softer *hint*, so
valid code is never marked red over a coverage gap. Branch/jump targets resolve
against the buffer's labels, so `je done` / `blt a0, a1, loop` are not flagged.

### Hover
Shows the instruction's **encoded bytes**, the **canonical disassembly** of
those bytes (decoded back via the disassembler — a cross-check: a decode that
disagrees with what you wrote is a visible red flag), and, for x86, the matching
operand forms from NASM's table. Hover is mode-aware: in a `BITS 32` buffer it
encodes and decodes in 32-bit (`mov eax, ebx`, not `rax`).

### Completion
Instruction mnemonics by prefix. For x86, each item carries its operand forms as
documentation.

### Signature help
As you type operands (x86), a popup lists the instruction's forms
(`ADD r/m32, r32` …) with the operand under the cursor highlighted. RISC-V
operands are positional, so signature help is x86-only.

### Live emulation — inline register/flag state
The differentiator. The custom `asm/run` request assembles the buffer, runs it
in a minimal flat sandbox, and returns the register/flag changes attributable to
each source line. The editor paints them as inline virtual text:

```
  10   xor   eax, eax        ; ZF=1
  11   mov   rax, rbx        ; rax=0x0
  12   add   rax, [rbx+8]    ; rax=0x…
```

- **Run buffer** runs the whole program; **run to cursor** stops just before the
  cursor line. **Breakpoints** are wired through the backend.
- The run reports how it ended: `completed` (a balanced `ret`), `reached-line`,
  `max-steps` (an unbroken loop hits the step cap), `fault` (a guest exception —
  there is no IDT/trap vectoring, so faults are reported, not delivered), or
  `ran-outside-program`.
- It runs on an explicit command/keymap only — never on edit — so a runaway loop
  can't churn.

The sandbox starts from power-on register values (x86: all zero except
`rdx=0x600`; RISC-V: all zero). Set up inputs explicitly. Loops show the **last**
iteration's state per line.

## The `asm/run` request (custom method)

Not part of standard LSP. Neovim triggers it via a keymap.

**Request** `asm/run`:
```json
{
  "textDocument": { "uri": "file:///x.asm" },
  "line": 11,            // run-to-cursor line (0-based); -1 = whole program
  "breakpoints": [20]    // optional: stop before any of these lines
}
```

**Result**:
```json
{
  "arch": "x86", "bits": 64,
  "stop": "completed", "stopLine": -1, "steps": 4,
  "lines": [
    { "line": 0, "text": "rax=0x5" },
    { "line": 1, "text": "rax=0x8" }
  ]
}
```

`text` is the pre-formatted inline annotation; the editor just renders it.

## Build

```sh
make go-asm          # → bin/go-asm
# or
go build -o bin/go-asm ./lsp
```

## Neovim

No plugin required — the built-in client launches it. Attach per filetype:

```lua
vim.api.nvim_create_autocmd("FileType", {
  pattern = { "asm", "nasm" },
  callback = function(args)
    vim.lsp.start({
      name = "go-asm",
      cmd = { vim.fn.expand("~/Dev/Go.Code/tinyemu-go.git/bin/go-asm") },
      root_dir = vim.fs.dirname(args.file),
    })
  end,
})
```

For the inline-emulation keymaps, drop `lsp/nvim/asm-live.lua` on your
`runtimepath` and bind it on attach:

```lua
vim.api.nvim_create_autocmd("LspAttach", {
  callback = function(args)
    local c = vim.lsp.get_client_by_id(args.data.client_id)
    if c and c.name == "go-asm" then
      local live = require("asm-live")
      vim.keymap.set("n", "<leader>rr", live.run,           { buffer = args.buf, desc = "asm: run buffer" })
      vim.keymap.set("n", "<leader>rc", live.run_to_cursor, { buffer = args.buf, desc = "asm: run to cursor" })
      vim.keymap.set("n", "<leader>rx", live.clear,         { buffer = args.buf, desc = "asm: clear state" })
    end
  end,
})
```

(If `.asm` files don't get a filetype:
`vim.filetype.add({ extension = { asm = "asm", nasm = "nasm" } })`.)

## Architecture

```
lsp/            LSP server (stdio, JSON-RPC framing, dispatch, arch routing)
  ├─ x86 features  → asm        (Assemble/Disassemble/Table, mode-aware)
  └─ riscv features→ asm/riscv  (Assemble/Disassemble/Mnemonics/labels)
asm/            x86/x86-64 assembler — data-driven from NASM's insns.dat
                (vendored, //go:embed); disasm via golang.org/x/arch
asm/riscv/      RISC-V assembler/disassembler — compact hand-written table
asm/emu/        execution backend: DetectArch → run in cpu/x86_64 | cpu/x86 |
                cpu/riscv, collapse the per-step trace into per-line state
cpu/*           the CPU cores the emulator already ships
```

Why two assemblers, not one generalized table? x86 encoding is large and
irregular, so `asm` is **data-driven** from NASM's `insns.dat` (the complete
table) with the macro layers reimplemented in Go. RISC-V encoding is small and
regular (six fixed-field formats), so `asm/riscv` is a **hand-written table** —
the maintainable choice for that ISA. They share only the LSP shell.

## Validation

Correctness is checked differentially against external assemblers, plus
round-trips and real-editor smoke tests:

- **x86**: byte-exact against `nasm` (`-f bin`), in both `BITS 64` and
  `BITS 32`; disassembly via `golang.org/x/arch`; assemble→disassemble→
  re-assemble round-trips.
- **RISC-V**: byte-exact against `llvm-mc` (`--triple=riscv64 --mattr=+m,+a
  --show-encoding`); full label programs matched against `llvm-mc`'s ELF
  `.text` (real label resolution); assemble→disassemble→re-assemble round-trips.
- **End-to-end**: headless Neovim drives the real server — attach, diagnostics,
  hover, `asm/run` — and asserts the inline virtual text. (This caught a
  `"diagnostics": null` vs `[]` bug the Go unit tests missed.)

Build oracles: `nasm` (Homebrew), `llvm-mc` (Homebrew LLVM at
`/opt/homebrew/opt/llvm/bin`), `nvim`.

## Limitations / roadmap

- x86 assembler coverage grows with the encoder; SIMD/AVX/EVEX forms are not all
  reached yet (flagged as hints, not errors).
- RISC-V covers RV64I + M + A + Zicsr + basic privileged; F/D (floating point)
  and C (compressed) are not yet implemented.
- The sandbox has no MMU/trap setup — it runs flat, position-fixed code; guest
  faults are reported, not handled.
- Planned: breakpoint UI in Neovim, an "evaluate selection" command, a
  full-register panel/float, and validating `cpu/riscv` against an execution
  golden model (Spike or the official SAIL model).
