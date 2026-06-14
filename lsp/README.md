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

## Build

```sh
go build -o bin/go-asm ./lsp
```

## Neovim

No plugin required — Neovim's built-in client can launch it. Point `cmd` at the
binary you built:

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

Open a `.asm`/`.nasm` file and you'll get diagnostics as you type, `K` for
hover, and completion. (If `.asm` files don't get a filetype, add
`vim.filetype.add({ extension = { asm = "asm", nasm = "nasm" } })`.)

## Live emulation (inline register/flag state)

The reason this lives in an emulator repo: the server can **run your code**.
The custom `asm/run` request assembles the buffer, executes it in the
tinyemu-go x86-64 CPU (flat long mode, a small sandbox — code at 1 MiB, a
stack seeded with a sentinel return address, power-on registers), and returns
the register/flag changes for each source line. The editor paints those as
inline virtual text — a live view of what the code actually does.

Drop [`nvim/asm-live.lua`](nvim/asm-live.lua) on your `runtimepath` (or copy it
into `~/.config/nvim/lua/`) and add keymaps when the client attaches:

```lua
vim.api.nvim_create_autocmd("LspAttach", {
  callback = function(args)
    local c = vim.lsp.get_client_by_id(args.data.client_id)
    if c and c.name == "go-asm" then
      local live = require("asm-live")
      vim.keymap.set("n", "<leader>rr", live.run,            { buffer = args.buf, desc = "asm: run buffer" })
      vim.keymap.set("n", "<leader>rc", live.run_to_cursor,  { buffer = args.buf, desc = "asm: run to cursor" })
      vim.keymap.set("n", "<leader>rx", live.clear,          { buffer = args.buf, desc = "asm: clear state" })
    end
  end,
})
```

Open `example.asm`, press `<leader>rr`, and each executed line gets an
annotation like `rax=0x5  ZF=1`. `<leader>rc` runs only up to the cursor line
(marked `◀ cursor`). The status line reports how the run ended: `completed`
(a balanced `ret`), `reached-line`, `max-steps` (an unbroken loop), `fault`
(a guest exception — there's no IDT, so faults are reported, not vectored), or
`ran-outside-program`.

A `BITS 32` directive runs the program in the 32-bit i386 core instead (8
GPRs, `eax…edi`); without a directive it's x86-64. The inline view adapts
automatically (`eax=0x5`).

Notes:
- Registers start at their power-on values — all zero except `rdx=0x600` — so
  `mov rax, rbx` early on shows `rax=0x0`. Set up inputs explicitly.
- It runs on an explicit keymap, never on edit, so a runaway loop can't churn.
- Loops show the **last** iteration's state per line.

## Roadmap

Next: breakpoints (the `breakpoints` field of `asm/run` is already wired
through the backend), an "evaluate selection" command, and a full-register
floating window / panel for the line under the cursor.
