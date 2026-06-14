# go-asm — a Language Server for NASM/Intel x86-64 assembly

A Language Server backed by the tinyemu-go [`asm`](../asm) assembler. It speaks
LSP over stdin/stdout (no dependencies beyond the standard library) and gives an
editor:

- **Diagnostics** — each line is assembled live. An *unknown mnemonic* is
  flagged as an error; a real instruction the encoder doesn't yet reach is a
  softer hint, so valid code is never marked red on a coverage gap.
- **Hover** — the instruction's encoded bytes (`48 89 d8`, 3 bytes) and the
  matching operand forms from NASM's instruction table.
- **Completion** — instruction mnemonics by prefix, each carrying its operand
  forms as documentation.
- **Signature help** — as you type operands, a popup lists the instruction's
  forms (`ADD r/m32, r32`…) with the operand under the cursor highlighted.

The assembler underneath matches nasm byte-for-byte on ~85% of the integer ISA;
coverage grows as the encoder does, and the LSP follows automatically.

It also **runs and debugs your code** — assemble, execute in the emulator, show
inline register/flag state, and single-step with breakpoints. Targets x86-64,
32-bit x86 (`BITS 32`), and RISC-V (RV64I+M+A+Zicsr), auto-detected per buffer.

## Quick start

```sh
make install-go-asm        # → ~/.local/bin/go-asm (on PATH)
cp lsp/nvim/go_asm.lua ~/.config/nvim/lua/go_asm.lua
```

```lua
-- init.lua
require("go_asm")
```

Open an `.asm` file; `<leader>rr` runs it, `<leader>rs` steps, `<leader>rb`
toggles a breakpoint, `K` hovers. See **[docs/LSP.md](../docs/LSP.md)** for the
full keymap table, the two workflows (live run / stepping debugger), the
`asm/run` + `asm/debug/*` protocol, architecture, and validation.

Demo files: [`example.asm`](example.asm) (x86-64), [`example_rv.asm`](example_rv.asm) (RISC-V).
