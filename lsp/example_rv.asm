# asm-lsp RISC-V demo. Open with the server attached, then <leader>rr to run.
# The ISA is auto-detected (RISC-V mnemonics); inline state shows a0=0x… etc.

  li   a0, 0          # sum
  li   a1, 5          # count
loop:
  addi a0, a0, 1      # a0 += 1   (watch a0 climb on <leader>rc per line)
  blt  a0, a1, loop   # loop while a0 < 5
  mv   a2, a0         # a2 = 5
  ret
