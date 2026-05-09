/* read_virtio_block.S - Test VirtIO block device read operation.
 *
 * This test initializes a VirtIO block device and reads sector 0.
 * The test verifies that the read completes successfully and checks
 * that the read data has a specific signature (first 4 bytes = 0xDEADBEEF).
 *
 * Reference: virtio/block.go (Block device implementation)
 * Reference: virtio/mmio.go (MMIO transport)
 * Reference: tinyemu-2019-12-21/virtio.c lines 1117-1132 (virtio_block_init)
 *
 * Pass: Block read completes with status OK and expected data
 * Fail: Magic wrong, device not found, read fails, or data mismatch
 *
 * The test harness must provide a block device with sector 0 containing:
 *   - Bytes 0-3: 0xDEADBEEF (little-endian: EF BE AD DE)
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
.equ VIRTIO_DEV_BLOCK,   2           /* Block device ID */

/* VirtIO status bits - Reference: virtio spec */
.equ VIRTIO_STATUS_ACK,         0x01
.equ VIRTIO_STATUS_DRIVER,      0x02
.equ VIRTIO_STATUS_DRIVER_OK,   0x04
.equ VIRTIO_STATUS_FEATURES_OK, 0x08

/* VirtIO descriptor flags - Reference: virtio/mmio.go lines 74-80 */
.equ VRING_DESC_F_NEXT,  1
.equ VRING_DESC_F_WRITE, 2

/* VirtIO block request types - Reference: virtio/block.go lines 15-21 */
.equ VIRTIO_BLK_T_IN,    0  /* Read request */

/* VirtIO block status codes - Reference: virtio/block.go lines 23-27 */
.equ VIRTIO_BLK_S_OK,    0  /* Success */

/* Sector size */
.equ SECTOR_SIZE, 512

/* TinyEMU addresses - Reference: machine/machine.go */
.equ VIRTIO_BASE,   0x40010000

/* Expected data pattern in sector 0 (for verification) */
.equ EXPECTED_MAGIC, 0xDEADBEEF

/*
 * Entry point and test framework
 */
.section .text.init, "ax"
.global _start

_start:
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

    /* Test 3: Check device ID (should be block = 2) */
    li gp, 3
    lw t1, VIRTIO_MMIO_DEVICE_ID(t0)
    li t2, VIRTIO_DEV_BLOCK
    bne t1, t2, test_fail

    /* Initialize VirtIO device */
    li gp, 4
    sw zero, VIRTIO_MMIO_STATUS(t0)

    /* Set ACKNOWLEDGE */
    li t1, VIRTIO_STATUS_ACK
    sw t1, VIRTIO_MMIO_STATUS(t0)

    /* Set DRIVER */
    li t1, VIRTIO_STATUS_ACK | VIRTIO_STATUS_DRIVER
    sw t1, VIRTIO_MMIO_STATUS(t0)

    /* Accept features */
    sw zero, VIRTIO_MMIO_DRV_FEAT_SEL(t0)
    lw t1, VIRTIO_MMIO_DEV_FEATURES(t0)
    sw t1, VIRTIO_MMIO_DRV_FEATURES(t0)

    /* Set FEATURES_OK */
    li t1, VIRTIO_STATUS_ACK | VIRTIO_STATUS_DRIVER | VIRTIO_STATUS_FEATURES_OK
    sw t1, VIRTIO_MMIO_STATUS(t0)

    /* Verify FEATURES_OK is still set */
    li gp, 5
    lw t1, VIRTIO_MMIO_STATUS(t0)
    andi t1, t1, VIRTIO_STATUS_FEATURES_OK
    beqz t1, test_fail

    /* Set up queue 0 (request queue) */
    li gp, 6
    sw zero, VIRTIO_MMIO_QUEUE_SEL(t0)

    /* Check maximum queue size */
    lw t1, VIRTIO_MMIO_QUEUE_NUM_MAX(t0)
    beqz t1, test_fail

    /* Set queue size to 16 */
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

    /* Set DRIVER_OK - device is now live */
    li t1, VIRTIO_STATUS_ACK | VIRTIO_STATUS_DRIVER | VIRTIO_STATUS_FEATURES_OK | VIRTIO_STATUS_DRIVER_OK
    sw t1, VIRTIO_MMIO_STATUS(t0)

    /* Prepare block read request
     *
     * VirtIO block request format (Reference: virtio/block.go lines 64-74):
     *   Request header (16 bytes, device reads):
     *     +0: type (32-bit) = VIRTIO_BLK_T_IN
     *     +4: ioprio (32-bit) = 0
     *     +8: sector (64-bit) = 0
     *   Data buffer (512 bytes, device writes)
     *   Status (1 byte, device writes)
     *
     * We need 3 descriptors chained together:
     *   desc[0]: request header (read by device)
     *   desc[1]: data buffer (written by device)
     *   desc[2]: status byte (written by device)
     */
    li gp, 7

    /* Initialize request header */
    la t1, req_header
    li t2, VIRTIO_BLK_T_IN
    sw t2, 0(t1)        /* type = IN (read) */
    sw zero, 4(t1)      /* ioprio = 0 */
    sd zero, 8(t1)      /* sector = 0 */

    /* Set up descriptor 0: request header (16 bytes, device reads) */
    la t1, desc_table
    la t2, req_header
    sd t2, 0(t1)        /* addr */
    li t2, 16
    sw t2, 8(t1)        /* len */
    li t2, VRING_DESC_F_NEXT
    sh t2, 12(t1)       /* flags = NEXT (continues to desc 1) */
    li t2, 1
    sh t2, 14(t1)       /* next = 1 */

    /* Set up descriptor 1: data buffer (512 bytes, device writes) */
    addi t1, t1, 16     /* Move to desc[1] */
    la t2, data_buffer
    sd t2, 0(t1)        /* addr */
    li t2, SECTOR_SIZE
    sw t2, 8(t1)        /* len = 512 */
    li t2, VRING_DESC_F_WRITE | VRING_DESC_F_NEXT
    sh t2, 12(t1)       /* flags = WRITE | NEXT */
    li t2, 2
    sh t2, 14(t1)       /* next = 2 */

    /* Set up descriptor 2: status byte (1 byte, device writes) */
    addi t1, t1, 16     /* Move to desc[2] */
    la t2, status_byte
    sd t2, 0(t1)        /* addr */
    li t2, 1
    sw t2, 8(t1)        /* len = 1 */
    li t2, VRING_DESC_F_WRITE
    sh t2, 12(t1)       /* flags = WRITE (no NEXT) */
    sh zero, 14(t1)     /* next = 0 (unused) */

    /* Add descriptor chain to available ring */
    la t1, avail_ring
    sh zero, 0(t1)      /* flags = 0 */
    sh zero, 4(t1)      /* ring[0] = 0 (start of chain) */
    fence
    li t2, 1
    sh t2, 2(t1)        /* idx = 1 */

    fence

    /* Notify the device */
    li t0, VIRTIO_BASE
    sw zero, VIRTIO_MMIO_QUEUE_NOTIFY(t0)

    /* Wait for device to process the request */
    li gp, 8
    li t3, 100000       /* Timeout counter */
wait_used:
    addi t3, t3, -1
    beqz t3, test_fail

    la t1, used_ring
    lh t2, 2(t1)        /* used.idx */
    beqz t2, wait_used

    /* Check status byte */
    li gp, 9
    la t1, status_byte
    lb t2, 0(t1)
    li t3, VIRTIO_BLK_S_OK
    bne t2, t3, test_fail

    /* Verify used ring entry */
    li gp, 10
    la t1, used_ring
    lw t2, 4(t1)        /* used.ring[0].id should be 0 */
    bnez t2, test_fail

    /* Verify read data: first 4 bytes should be EXPECTED_MAGIC */
    /* Note: Use lwu (load word unsigned) to avoid sign-extension issues
     * since EXPECTED_MAGIC (0xDEADBEEF) has bit 31 set */
    li gp, 11
    la t1, data_buffer
    lwu t2, 0(t1)
    li t3, EXPECTED_MAGIC
    bne t2, t3, test_fail

    /* Test passed! */
    j test_pass

/*
 * Data section
 */
.section .data
.align 8
req_header:
    .space 16

/*
 * BSS section
 */
.section .bss
.align 12  /* 4KB aligned for VirtIO */

/* Descriptor table: 16 entries * 16 bytes = 256 bytes */
.global desc_table
desc_table:
    .space 256

/* Available ring */
.align 4
.global avail_ring
avail_ring:
    .space 64

/* Used ring */
.align 4
.global used_ring
used_ring:
    .space 256

/* Data buffer for sector read (512 bytes) */
.align 8
.global data_buffer
data_buffer:
    .space 512

/* Status byte */
.align 1
.global status_byte
status_byte:
    .space 1

/*
 * tohost/fromhost for HTIF protocol
 */
.section .tohost, "aw"
.align 6
.global tohost
.global fromhost
tohost:   .dword 0
fromhost: .dword 0
