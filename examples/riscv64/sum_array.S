/* Sum of array [1,2,3,...,10] stored in memory.
   Expected after EBREAK: a0 = 55  */

    .text
    .globl _start
_start:
    la    t0, array
    li    t1, 10         # count
    li    a0, 0          # sum
1:
    beqz  t1, 2f
    lw    t2, 0(t0)
    add   a0, a0, t2
    addi  t0, t0, 4
    addi  t1, t1, -1
    j     1b
2:
    ebreak

    .data
    .align 2
array:
    .word 1, 2, 3, 4, 5, 6, 7, 8, 9, 10
