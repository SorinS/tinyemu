/* Fibonacci(20). Expected after EBREAK: a0 = 6765
   Standard iterative two-register schedule. */

    .text
    .globl _start
_start:
    li    a0, 0          # fib(0)
    li    a1, 1          # fib(1)
    li    t0, 20         # counter
1:
    beqz  t0, 2f
    add   a2, a0, a1     # next = a0 + a1
    mv    a0, a1
    mv    a1, a2
    addi  t0, t0, -1
    j     1b
2:
    ebreak
