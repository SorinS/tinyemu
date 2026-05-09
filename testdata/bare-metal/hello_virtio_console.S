/* hello_virtio_console.S - Test VirtIO console TX output.
 *
 * This test initializes a VirtIO console device and sends "Hello World"
 * through the TX queue (queue 1).
 *
 * Reference: virtio/console.go (Console implementation)
 * Reference: virtio/mmio.go (MMIO transport)
 * Reference: tinyemu-2019-12-21/virtio.c lines 1348-1360 (virtio_console_init)
 *
 * Pass: Device accepts output and updates used ring
 * Fail: Magic value wrong, device not found, or queue not processed
 */

/* VirtIO MMIO register offsets - Reference: virtio.c lines 37-64 */
.equ VIRTIO_MMIO_MAGIC,         0x000
.equ VIRTIO_MMIO_VERSION,       0x004
.equ VIRTIO_MMIO_DEVICE_ID,     0x008
.equ VIRTIO_MMIO_VENDOR_ID,     0x00c
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
.equ VIRTIO_MMIO_CONFIG,        0x100

/* VirtIO constants */
.equ VIRTIO_MAGIC,       0x74726976  /* "virt" in little endian */
.equ VIRTIO_VERSION,     2           /* Modern virtio */
.equ VIRTIO_DEV_CONSOLE, 3           /* Console device ID */

/* VirtIO status bits - Reference: virtio spec */
.equ VIRTIO_STATUS_ACK,         0x01
.equ VIRTIO_STATUS_DRIVER,      0x02
.equ VIRTIO_STATUS_DRIVER_OK,   0x04
.equ VIRTIO_STATUS_FEATURES_OK, 0x08

/* Console queue indices - Reference: virtio/console.go lines 14-18 */
.equ CONSOLE_QUEUE_TX, 1

/* TinyEMU addresses - Reference: machine/machine.go */
.equ VIRTIO_BASE,   0x40010000

/*
 * Entry point and test framework
 */
.section .text.init, "ax"
.global _start

_start:
    /* Jump past trap vector to reset_vector */
    j reset_vector

.align 4
trap_vector:
    /* On any trap, fail the test */
    csrr t5, mcause
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

    /* Jump to test main */
    j test_main

/*
 * test_pass - Signal test success
 * Write 1 to tohost (exit code = 0)
 */
test_pass:
    li t0, 1
    la t1, tohost
    sd t0, 0(t1)
1:  j 1b

/*
 * test_fail - Signal test failure
 * Write (gp << 1) | 1 to tohost
 */
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
    /* Test 1: Check VirtIO magic value */
    li gp, 1
    li t0, VIRTIO_BASE
    lw t1, VIRTIO_MMIO_MAGIC(t0)
    li t2, VIRTIO_MAGIC
    bne t1, t2, test_fail

    /* Test 2: Check VirtIO version */
    li gp, 2
    lw t1, VIRTIO_MMIO_VERSION(t0)
    li t2, VIRTIO_VERSION
    bne t1, t2, test_fail

    /* Test 3: Check device ID (should be console = 3) */
    li gp, 3
    lw t1, VIRTIO_MMIO_DEVICE_ID(t0)
    li t2, VIRTIO_DEV_CONSOLE
    bne t1, t2, test_fail

    /* Initialize VirtIO device - Reference: virtio.c virtio_init sequence */

    /* Step 1: Reset device (write 0 to status) */
    li gp, 4
    sw zero, VIRTIO_MMIO_STATUS(t0)

    /* Step 2: Set ACKNOWLEDGE status bit */
    li t1, VIRTIO_STATUS_ACK
    sw t1, VIRTIO_MMIO_STATUS(t0)

    /* Step 3: Set DRIVER status bit */
    li t1, VIRTIO_STATUS_ACK | VIRTIO_STATUS_DRIVER
    sw t1, VIRTIO_MMIO_STATUS(t0)

    /* Step 4: Read and accept device features */
    sw zero, VIRTIO_MMIO_DRV_FEAT_SEL(t0)
    lw t1, VIRTIO_MMIO_DEV_FEATURES(t0)
    sw t1, VIRTIO_MMIO_DRV_FEATURES(t0)

    /* Step 5: Set FEATURES_OK status bit */
    li t1, VIRTIO_STATUS_ACK | VIRTIO_STATUS_DRIVER | VIRTIO_STATUS_FEATURES_OK
    sw t1, VIRTIO_MMIO_STATUS(t0)

    /* Verify FEATURES_OK is still set */
    li gp, 5
    lw t1, VIRTIO_MMIO_STATUS(t0)
    andi t1, t1, VIRTIO_STATUS_FEATURES_OK
    beqz t1, test_fail

    /* Step 6: Set up TX queue (queue 1) */
    li gp, 6
    li t1, CONSOLE_QUEUE_TX
    sw t1, VIRTIO_MMIO_QUEUE_SEL(t0)

    /* Check maximum queue size */
    lw t1, VIRTIO_MMIO_QUEUE_NUM_MAX(t0)
    beqz t1, test_fail

    /* Set queue size to 16 (power of 2) */
    li t1, 16
    sw t1, VIRTIO_MMIO_QUEUE_NUM(t0)

    /* Set up descriptor table address */
    la t1, desc_table
    sw t1, VIRTIO_MMIO_QUEUE_DESC_LO(t0)
    srli t2, t1, 32
    sw t2, VIRTIO_MMIO_QUEUE_DESC_HI(t0)

    /* Set up available ring address */
    la t1, avail_ring
    sw t1, VIRTIO_MMIO_QUEUE_AVAIL_LO(t0)
    srli t2, t1, 32
    sw t2, VIRTIO_MMIO_QUEUE_AVAIL_HI(t0)

    /* Set up used ring address */
    la t1, used_ring
    sw t1, VIRTIO_MMIO_QUEUE_USED_LO(t0)
    srli t2, t1, 32
    sw t2, VIRTIO_MMIO_QUEUE_USED_HI(t0)

    /* Mark queue as ready */
    li t1, 1
    sw t1, VIRTIO_MMIO_QUEUE_READY(t0)

    /* Step 7: Set DRIVER_OK status bit - device is now live */
    li t1, VIRTIO_STATUS_ACK | VIRTIO_STATUS_DRIVER | VIRTIO_STATUS_FEATURES_OK | VIRTIO_STATUS_DRIVER_OK
    sw t1, VIRTIO_MMIO_STATUS(t0)

    /* Now send "Hello World\n" through console TX */
    li gp, 7

    /* Set up descriptor 0: points to message buffer
     * Descriptor format (16 bytes):
     *   +0: addr (64-bit)
     *   +8: len (32-bit)
     *   +12: flags (16-bit)
     *   +14: next (16-bit)
     */
    la t1, desc_table

    /* desc[0].addr = message buffer address */
    la t2, message
    sd t2, 0(t1)

    /* desc[0].len = message length (12 bytes for "Hello World\n") */
    li t2, 12
    sw t2, 8(t1)

    /* desc[0].flags = 0 (device reads from buffer, no chaining) */
    sh zero, 12(t1)

    /* desc[0].next = 0 (unused) */
    sh zero, 14(t1)

    /* Add descriptor 0 to available ring
     * Available ring format:
     *   +0: flags (16-bit)
     *   +2: idx (16-bit) - next slot for driver to fill
     *   +4: ring[N] (16-bit each) - descriptor indices
     */
    la t1, avail_ring
    /* avail.flags = 0 */
    sh zero, 0(t1)
    /* avail.ring[0] = 0 (descriptor index) */
    sh zero, 4(t1)
    /* Memory barrier before updating idx */
    fence
    /* avail.idx = 1 (one descriptor added) */
    li t2, 1
    sh t2, 2(t1)

    /* Memory barrier to ensure writes are visible to device */
    fence

    /* Notify the device (write queue index to notify register) */
    li t0, VIRTIO_BASE
    li t1, CONSOLE_QUEUE_TX
    sw t1, VIRTIO_MMIO_QUEUE_NOTIFY(t0)

    /* Wait for device to process the descriptor */
    li gp, 8
    li t3, 100000    /* Timeout counter */
wait_used:
    addi t3, t3, -1
    beqz t3, test_fail

    /* Check used.idx
     * Used ring format:
     *   +0: flags (16-bit)
     *   +2: idx (16-bit) - next slot device will fill
     *   +4: ring[N] (8 bytes each) - {id: u32, len: u32}
     */
    la t1, used_ring
    lh t2, 2(t1)
    beqz t2, wait_used

    /* Success! used.idx is now 1 */
    li gp, 9

    /* Verify the used ring entry */
    /* used.ring[0].id should be 0 (our descriptor index) */
    lw t2, 4(t1)
    bnez t2, test_fail

    /* Test passed! */
    j test_pass

/*
 * Data section
 */
.section .data
.align 8
message:
    .ascii "Hello World\n"

/*
 * BSS section - VirtIO queue structures
 * Must be properly aligned for VirtIO
 */
.section .bss
.align 12  /* 4KB aligned for VirtIO */

/* Descriptor table: 16 entries * 16 bytes = 256 bytes */
.global desc_table
desc_table:
    .space 256

/* Available ring: 2 + 2 + 16*2 + 2 = 38 bytes, round up to 64 */
.align 4
.global avail_ring
avail_ring:
    .space 64

/* Used ring: 2 + 2 + 16*8 + 2 = 134 bytes, round up to 256 */
.align 4
.global used_ring
used_ring:
    .space 256

/*
 * tohost/fromhost for HTIF protocol
 */
.section .tohost, "aw"
.align 6
.global tohost
.global fromhost
tohost:   .dword 0
fromhost: .dword 0
