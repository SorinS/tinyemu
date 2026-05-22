/* Conditional branches: pick max of three values.
   Inputs: 17, 42, 7. Expected after EBREAK: a0 = 42 */

    .text
    .globl _start
_start:
    li    t0, 17
    li    t1, 42
    li    t2, 7

    # max(t0, t1) -> a0
    mv    a0, t0
    blt   t1, a0, 1f     # if t1 < a0 skip
    mv    a0, t1
1:
    # max(a0, t2)
    blt   t2, a0, 2f
    mv    a0, t2
2:
    ebreak
