/* Linker script for bare-metal VirtIO tests.
 * Memory layout matches TinyEMU and riscv-tests:
 *   0x80000000: RAM start (reset vector)
 *   0x80001000: tohost/fromhost symbols (HTIF protocol)
 *
 * Reference: testdata/riscv-tests/isa/ (existing test format)
 */

OUTPUT_ARCH(riscv)
ENTRY(_start)

MEMORY
{
    RAM (rwx) : ORIGIN = 0x80000000, LENGTH = 64M
}

SECTIONS
{
    . = 0x80000000;

    .text.init : {
        *(.text.init)
    } > RAM

    .text : {
        *(.text)
        *(.text.*)
    } > RAM

    .rodata : {
        *(.rodata)
        *(.rodata.*)
    } > RAM

    .data : {
        *(.data)
        *(.data.*)
    } > RAM

    /* tohost/fromhost at fixed address for HTIF protocol (matches riscv-tests) */
    . = 0x80001000;
    .tohost ALIGN(64) : {
        tohost = .;
        . += 8;
        fromhost = .;
        . += 8;
    } > RAM

    . = ALIGN(8);
    __bss_start = .;

    .bss : {
        *(.bss)
        *(.bss.*)
        *(COMMON)
    } > RAM

    __bss_end = .;

    /* Stack at higher memory */
    . = 0x80100000;
    __stack_top = .;
}
