; Hello-world payload for BareMetal.
;
; BareMetal's exokernel loads a single user payload at linear address
; 0x1E0000 and jumps to it once init completes. The payload uses the
; kernel API via fixed function-pointer slots in the kernel header
; (see api/libBareMetal.asm in the BareMetal source for the table).
;
; Build:
;   nasm bin/baremetal/hello.asm -o bin/baremetal/hello.bin
;
; Run via run_baremetal.sh:
;   ./run_baremetal.sh "" bin/baremetal/hello.bin
;
; Expected output (after the kernel's own "[ BareMetal ] ... system
; ready" banner):
;   Hello from a BareMetal payload!

BITS 64
DEFAULT ABS
ORG 0x1E0000

; Kernel-exported pointers used by this payload. Addresses come from
; the BareMetal kernel header — the 8-byte slot at each address holds
; a pointer to the function, so call sites use `call [slot]`.
b_output equ 0x100018  ; (rsi=str, rcx=len) — write to serial console
b_system equ 0x100040  ; (rcx=function, rax/rdx=args) — misc syscalls
TIMECOUNTER equ 0x00   ; b_system(TIMECOUNTER, ...) — nanoseconds since boot

start:
    ; Print "Hello from a BareMetal payload!\n" once.
    mov rsi, msg
    mov rcx, msg_len
    call [b_output]

    ; Quietly idle so the kernel's printed banner doesn't scroll away.
    ; A real payload might poll b_input, drive a network protocol, or
    ; exit back to the kernel via int3 — we just halt.
.halt:
    hlt
    jmp .halt

msg:     db "Hello from a BareMetal payload!", 0x0D, 0x0A
msg_len  equ $ - msg
