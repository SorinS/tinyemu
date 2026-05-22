/* Compute strlen of a null-terminated string.
   String: "tinyemu-go" -> 10 chars.
   Expected after EBREAK: a0 = 10  */

    .text
    .globl _start
_start:
    la    t0, str        # ptr
    li    a0, 0          # length
1:
    lb    t1, 0(t0)
    beqz  t1, 2f
    addi  a0, a0, 1
    addi  t0, t0, 1
    j     1b
2:
    ebreak

    .data
str:
    .asciz "tinyemu-go"
