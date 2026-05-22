/* Function call convention: caller passes argument in a0, callee
   returns 2*x in a0. Demonstrates ra/sp via call+ret.
   Call double(21) -> a0 = 42  */

    .text
    .globl _start
_start:
    la    sp, _stack_top
    li    a0, 21
    call  double_it      # jal ra, double_it (PC + 4 -> ra)
    ebreak               # final a0 = 42

double_it:
    slli  a0, a0, 1      # a0 = a0 << 1 = a0 * 2
    ret                  # jalr x0, 0(ra)

    .data
    .align 8
_stack:
    .skip 256
_stack_top:
