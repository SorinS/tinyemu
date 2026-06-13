# asm-lsp — a Language Server for NASM/Intel x86-64 assembly

A Language Server backed by the tinyemu-go [`asm`](../asm) assembler. It speaks
LSP over stdin/stdout (no dependencies beyond the standard library) and gives an
editor:

- **Diagnostics** — each line is assembled live. An *unknown mnemonic* is
  flagged as an error; a real instruction the encoder doesn't yet reach is a
  softer hint, so valid code is never marked red on a coverage gap.
- **Hover** — the instruction's encoded bytes (`48 89 d8`, 3 bytes) and the
  matching operand forms from NASM's instruction table.
- **Completion** — instruction mnemonics by prefix.

The assembler underneath matches nasm byte-for-byte on ~85% of the integer ISA;
coverage grows as the encoder does, and the LSP follows automatically.

## Build

```sh
go build -o bin/asm-lsp ./lsp
```

## Neovim

No plugin required — Neovim's built-in client can launch it. Point `cmd` at the
binary you built:

```lua
vim.api.nvim_create_autocmd("FileType", {
  pattern = { "asm", "nasm" },
  callback = function(args)
    vim.lsp.start({
      name = "asm-lsp",
      cmd = { vim.fn.expand("~/Dev/Go.Code/tinyemu-go.git/bin/asm-lsp") },
      root_dir = vim.fs.dirname(args.file),
    })
  end,
})
```

Open a `.asm`/`.nasm` file and you'll get diagnostics as you type, `K` for
hover, and completion. (If `.asm` files don't get a filetype, add
`vim.filetype.add({ extension = { asm = "asm", nasm = "nasm" } })`.)

## Roadmap

The standard LSP surface is in place. The differentiator — and the reason this
lives in an emulator repo — is **execution-backed features**: "run to cursor",
inline register/flag state, and "evaluate selection", using the tinyemu-go CPU
as the backend. That's the next step.
