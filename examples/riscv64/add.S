/* Basic arithmetic. a0 = 7 + 35 = 42. a1 = 100 - 17 = 83.
   Expected after EBREAK: a0 = 42, a1 = 83  */

    .text
    .globl _start
_start:
    li    t0, 7
    li    t1, 35
    add   a0, t0, t1

    li    t0, 100
    li    t1, 17
    sub   a1, t0, t1
    ebreak
