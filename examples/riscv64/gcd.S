/* Euclid's GCD: gcd(48, 18) = 6. Expected after EBREAK: a0 = 6  */

    .text
    .globl _start
_start:
    li    a0, 48
    li    a1, 18
1:
    beqz  a1, 2f
    remu  t0, a0, a1     # t0 = a0 % a1
    mv    a0, a1
    mv    a1, t0
    j     1b
2:
    ebreak
