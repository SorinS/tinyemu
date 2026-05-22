/* Copy 16 bytes from src to dst, then load first word of each
   for sanity. Expected after EBREAK: a0 = 0xDEADBEEF, a1 = 0xDEADBEEF  */

    .text
    .globl _start
_start:
    la    t0, src
    la    t1, dst
    li    t2, 16         # bytes to copy
1:
    beqz  t2, 2f
    lb    t3, 0(t0)
    sb    t3, 0(t1)
    addi  t0, t0, 1
    addi  t1, t1, 1
    addi  t2, t2, -1
    j     1b
2:
    la    t0, src
    la    t1, dst
    lw    a0, 0(t0)
    lw    a1, 0(t1)
    ebreak

    .data
    .align 2
src:
    .word 0xDEADBEEF, 0xCAFEBABE, 0xFEEDFACE, 0xBADDCAFE
dst:
    .skip 16
