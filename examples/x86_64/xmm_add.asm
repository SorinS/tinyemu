; SSE2: load two 128-bit values into XMM regs, add as packed doubles,
; store result. Expected: low 64 of [result] = 4.5, high 64 = 7.0.
;
;   xmm0 = {1.0, 2.0}
;   xmm1 = {3.5, 5.0}
;   xmm0 = xmm0 + xmm1 = {4.5, 7.0}
bits 64
    movupd xmm0, [rel a]
    movupd xmm1, [rel b]
    addpd  xmm0, xmm1
    movupd [rel result], xmm0
    hlt

align 16
a:      dq __float64__(1.0), __float64__(2.0)
b:      dq __float64__(3.5), __float64__(5.0)
result: dq 0, 0
