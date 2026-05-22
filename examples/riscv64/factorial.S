/* Iterative factorial of 10.
   Expected after EBREAK: a0 = 3628800  */

    .text
    .globl _start
_start:
    li    a0, 1          # accumulator
    li    t0, 10         # counter
1:
    beqz  t0, 2f
    mul   a0, a0, t0
    addi  t0, t0, -1
    j     1b
2:
    ebreak
