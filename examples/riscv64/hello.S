/* Set a register to a known value and stop.
   Expected after EBREAK: a0 = 42, a1 = -1, a2 = 0x1234abcd  */

    .text
    .globl _start
_start:
    li    a0, 42
    li    a1, -1
    li    a2, 0x1234abcd
    ebreak
