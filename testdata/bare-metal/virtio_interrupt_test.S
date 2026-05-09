/* virtio_interrupt_test.S - Test VirtIO device interrupt delivery.
 *
 * This test verifies the complete interrupt path:
 *   VirtIO device -> PLIC -> CPU external interrupt
 *
 * Test sequence:
 *   1. Set up VirtIO console device
 *   2. Configure PLIC to accept VirtIO interrupt (IRQ 1)
 *   3. Enable machine external interrupts
 *   4. Submit a TX request to generate an interrupt
 *   5. Handle the interrupt in trap handler
 *   6. Verify interrupt was received and acknowledged
 *
 * Reference: devices/plic.go (PLIC implementation)
 * Reference: virtio/console.go (Console device)
 * Reference: tinyemu-2019-12-21/riscv_machine.c lines 257-301 (plic_read/write)
 *
 * Pass: Interrupt received, claimed, and completed successfully
 * Fail: No interrupt, wrong IRQ, or interrupt handling error
 */

/* VirtIO MMIO register offsets - Reference: virtio.c lines 37-64 */
.equ VIRTIO_MMIO_MAGIC,         0x000
.equ VIRTIO_MMIO_VERSION,       0x004
.equ VIRTIO_MMIO_DEVICE_ID,     0x008
.equ VIRTIO_MMIO_DEV_FEATURES,  0x010
.equ VIRTIO_MMIO_DEV_FEAT_SEL,  0x014
.equ VIRTIO_MMIO_DRV_FEATURES,  0x020
.equ VIRTIO_MMIO_DRV_FEAT_SEL,  0x024
.equ VIRTIO_MMIO_QUEUE_SEL,     0x030
.equ VIRTIO_MMIO_QUEUE_NUM_MAX, 0x034
.equ VIRTIO_MMIO_QUEUE_NUM,     0x038
.equ VIRTIO_MMIO_QUEUE_READY,   0x044
.equ VIRTIO_MMIO_QUEUE_NOTIFY,  0x050
.equ VIRTIO_MMIO_INT_STATUS,    0x060
.equ VIRTIO_MMIO_INT_ACK,       0x064
.equ VIRTIO_MMIO_STATUS,        0x070
.equ VIRTIO_MMIO_QUEUE_DESC_LO, 0x080
.equ VIRTIO_MMIO_QUEUE_DESC_HI, 0x084
.equ VIRTIO_MMIO_QUEUE_AVAIL_LO, 0x090
.equ VIRTIO_MMIO_QUEUE_AVAIL_HI, 0x094
.equ VIRTIO_MMIO_QUEUE_USED_LO, 0x0a0
.equ VIRTIO_MMIO_QUEUE_USED_HI, 0x0a4

/* VirtIO constants */
.equ VIRTIO_MAGIC,       0x74726976
.equ VIRTIO_VERSION,     2
.equ VIRTIO_DEV_CONSOLE, 3

/* VirtIO status bits */
.equ VIRTIO_STATUS_ACK,         0x01
.equ VIRTIO_STATUS_DRIVER,      0x02
.equ VIRTIO_STATUS_DRIVER_OK,   0x04
.equ VIRTIO_STATUS_FEATURES_OK, 0x08

/* TinyEMU addresses - Reference: machine/machine.go, devices/plic.go */
.equ VIRTIO_BASE, 0x40010000
.equ PLIC_BASE,   0x40100000

/* PLIC register addresses - Reference: devices/plic.go */
.equ PLIC_PRIORITY1,  (PLIC_BASE + 0x004)      /* Priority for source 1 */
.equ PLIC_PENDING,    (PLIC_BASE + 0x1000)     /* Pending bits */
.equ PLIC_ENABLE0,    (PLIC_BASE + 0x2000)     /* Enable for context 0 */
.equ PLIC_THRESHOLD0, (PLIC_BASE + 0x200000)   /* Threshold for context 0 */
.equ PLIC_CLAIM0,     (PLIC_BASE + 0x200004)   /* Claim/complete for context 0 */

/* VirtIO IRQ number - Reference: machine/machine.go VirtIOIRQ = 1 */
.equ VIRTIO_IRQ, 1

/* Console queue indices */
.equ CONSOLE_QUEUE_TX, 1

/* CSR bits */
.equ MIE_MEIE, (1 << 11)   /* Machine external interrupt enable */
.equ MSTATUS_MIE, (1 << 3) /* Global machine interrupt enable */

/* mcause value for machine external interrupt */
.equ MCAUSE_MEI, 0x800000000000000b

/*
 * Entry point
 */
.section .text.init, "ax"
.global _start

_start:
    j reset_vector

.align 4
trap_vector:
    /* Save context (minimal - just what we need) */
    csrr t5, mcause

    /* Check if this is a machine external interrupt */
    li t6, MCAUSE_MEI
    bne t5, t6, trap_unexpected

    /* Expected external interrupt - handle it */

    /* Read PLIC claim register to get IRQ number */
    li t0, PLIC_CLAIM0
    lw t1, 0(t0)

    /* Store claimed IRQ for verification */
    la t2, claimed_irq
    sw t1, 0(t2)

    /* Verify it's the VirtIO IRQ */
    li t3, VIRTIO_IRQ
    bne t1, t3, trap_wrong_irq

    /* Acknowledge the VirtIO interrupt */
    li t0, VIRTIO_BASE
    lw t1, VIRTIO_MMIO_INT_STATUS(t0)
    sw t1, VIRTIO_MMIO_INT_ACK(t0)

    /* Complete the interrupt in PLIC */
    li t0, PLIC_CLAIM0
    li t1, VIRTIO_IRQ
    sw t1, 0(t0)

    /* Mark interrupt as handled */
    la t0, interrupt_handled
    li t1, 1
    sw t1, 0(t0)

    /* Return from interrupt */
    mret

trap_unexpected:
    /* Unexpected trap - store mcause and fail */
    la t0, unexpected_mcause
    sd t5, 0(t0)
    li gp, 100
    j test_fail

trap_wrong_irq:
    /* Wrong IRQ number */
    li gp, 101
    j test_fail

reset_vector:
    /* Clear registers */
    li x1, 0
    li x2, 0
    li x3, 0
    li x4, 0
    li x5, 0
    li x6, 0
    li x7, 0
    li x8, 0
    li x9, 0
    li x10, 0
    li x11, 0
    li x12, 0
    li x13, 0
    li x14, 0
    li x15, 0
    li x16, 0
    li x17, 0
    li x18, 0
    li x19, 0
    li x20, 0
    li x21, 0
    li x22, 0
    li x23, 0
    li x24, 0
    li x25, 0
    li x26, 0
    li x27, 0
    li x28, 0
    li x29, 0
    li x30, 0
    li x31, 0

    /* Set up trap vector */
    la t0, trap_vector
    csrw mtvec, t0

    /* Set up stack */
    la sp, __stack_top

    /* Clear BSS */
    la t0, __bss_start
    la t1, __bss_end
1:  bgeu t0, t1, 2f
    sd zero, 0(t0)
    addi t0, t0, 8
    j 1b
2:

    j test_main

test_pass:
    li t0, 1
    la t1, tohost
    sd t0, 0(t1)
1:  j 1b

test_fail:
    slli t0, gp, 1
    ori t0, t0, 1
    la t1, tohost
    sd t0, 0(t1)
1:  j 1b

/*
 * Main test code
 */
.section .text
test_main:
    /* Test 1: Check VirtIO device */
    li gp, 1
    li t0, VIRTIO_BASE
    lw t1, VIRTIO_MMIO_MAGIC(t0)
    li t2, VIRTIO_MAGIC
    bne t1, t2, test_fail

    /* Check device ID */
    li gp, 2
    lw t1, VIRTIO_MMIO_DEVICE_ID(t0)
    li t2, VIRTIO_DEV_CONSOLE
    bne t1, t2, test_fail

    /* Initialize VirtIO device */
    li gp, 3
    sw zero, VIRTIO_MMIO_STATUS(t0)
    li t1, VIRTIO_STATUS_ACK
    sw t1, VIRTIO_MMIO_STATUS(t0)
    li t1, VIRTIO_STATUS_ACK | VIRTIO_STATUS_DRIVER
    sw t1, VIRTIO_MMIO_STATUS(t0)

    /* Accept features */
    sw zero, VIRTIO_MMIO_DRV_FEAT_SEL(t0)
    lw t1, VIRTIO_MMIO_DEV_FEATURES(t0)
    sw t1, VIRTIO_MMIO_DRV_FEATURES(t0)
    li t1, VIRTIO_STATUS_ACK | VIRTIO_STATUS_DRIVER | VIRTIO_STATUS_FEATURES_OK
    sw t1, VIRTIO_MMIO_STATUS(t0)

    /* Set up TX queue */
    li gp, 4
    li t1, CONSOLE_QUEUE_TX
    sw t1, VIRTIO_MMIO_QUEUE_SEL(t0)
    lw t1, VIRTIO_MMIO_QUEUE_NUM_MAX(t0)
    beqz t1, test_fail

    li t1, 16
    sw t1, VIRTIO_MMIO_QUEUE_NUM(t0)

    la t1, desc_table
    sw t1, VIRTIO_MMIO_QUEUE_DESC_LO(t0)
    srli t2, t1, 32
    sw t2, VIRTIO_MMIO_QUEUE_DESC_HI(t0)

    la t1, avail_ring
    sw t1, VIRTIO_MMIO_QUEUE_AVAIL_LO(t0)
    srli t2, t1, 32
    sw t2, VIRTIO_MMIO_QUEUE_AVAIL_HI(t0)

    la t1, used_ring
    sw t1, VIRTIO_MMIO_QUEUE_USED_LO(t0)
    srli t2, t1, 32
    sw t2, VIRTIO_MMIO_QUEUE_USED_HI(t0)

    li t1, 1
    sw t1, VIRTIO_MMIO_QUEUE_READY(t0)
    li t1, VIRTIO_STATUS_ACK | VIRTIO_STATUS_DRIVER | VIRTIO_STATUS_FEATURES_OK | VIRTIO_STATUS_DRIVER_OK
    sw t1, VIRTIO_MMIO_STATUS(t0)

    /* Configure PLIC - Reference: devices/plic.go */
    li gp, 5

    /* Set priority for VirtIO IRQ (source 1) to 1 */
    li t0, PLIC_PRIORITY1
    li t1, 1
    sw t1, 0(t0)

    /* Set threshold to 0 (accept all priorities) */
    li t0, PLIC_THRESHOLD0
    sw zero, 0(t0)

    /* Enable VirtIO IRQ (bit 0 for source 1) */
    li t0, PLIC_ENABLE0
    li t1, (1 << (VIRTIO_IRQ - 1))
    sw t1, 0(t0)

    /* Enable machine external interrupts in mie */
    li gp, 6
    li t0, MIE_MEIE
    csrs mie, t0

    /* Enable global interrupts in mstatus */
    li t0, MSTATUS_MIE
    csrs mstatus, t0

    /* Set up descriptor to send data */
    li gp, 7
    la t1, desc_table
    la t2, message
    sd t2, 0(t1)
    li t2, 5
    sw t2, 8(t1)
    sh zero, 12(t1)
    sh zero, 14(t1)

    /* Add to available ring */
    la t1, avail_ring
    sh zero, 0(t1)
    sh zero, 4(t1)
    fence
    li t2, 1
    sh t2, 2(t1)
    fence

    /* Notify the device - this should trigger an interrupt */
    li gp, 8
    li t0, VIRTIO_BASE
    li t1, CONSOLE_QUEUE_TX
    sw t1, VIRTIO_MMIO_QUEUE_NOTIFY(t0)

    /* Wait for interrupt to be handled */
    li t3, 100000
wait_interrupt:
    addi t3, t3, -1
    beqz t3, test_fail

    la t0, interrupt_handled
    lw t1, 0(t0)
    beqz t1, wait_interrupt

    /* Verify the interrupt was handled correctly */
    li gp, 9
    la t0, claimed_irq
    lw t1, 0(t0)
    li t2, VIRTIO_IRQ
    bne t1, t2, test_fail

    /* Verify the used ring was updated */
    li gp, 10
    la t0, used_ring
    lh t1, 2(t0)
    beqz t1, test_fail

    /* Test passed! */
    j test_pass

/*
 * Data section
 */
.section .data
.align 8
message:
    .ascii "test\n"

/* Variables for interrupt verification */
.align 4
interrupt_handled:
    .word 0
claimed_irq:
    .word 0
unexpected_mcause:
    .dword 0

/*
 * BSS section
 */
.section .bss
.align 12

desc_table:
    .space 256

.align 4
avail_ring:
    .space 64

.align 4
used_ring:
    .space 256

/*
 * tohost/fromhost for HTIF
 */
.section .tohost, "aw"
.align 6
.global tohost
.global fromhost
tohost:   .dword 0
fromhost: .dword 0
