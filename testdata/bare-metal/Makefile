# Makefile for bare-metal VirtIO tests
#
# Reference: testdata/riscv-tests/isa/ (similar test binaries)
#
# Usage:
#   make all       - Build all tests
#   make clean     - Remove built binaries
#   make rebuild   - Clean and build all tests

AS = riscv64-linux-gnu-as
LD = riscv64-linux-gnu-ld

ASFLAGS = -march=rv64imac_zicsr
LDFLAGS = -T link.ld

# Test binaries
TESTS = hello_virtio_console read_virtio_block virtio_interrupt_test

.PHONY: all clean rebuild

all: $(TESTS)

clean:
	rm -f $(TESTS) *.o

rebuild: clean all

# Pattern rule for building tests
%: %.S link.ld
	$(AS) $(ASFLAGS) -o $@.o $<
	$(LD) $(LDFLAGS) -o $@ $@.o
	rm -f $@.o

# Explicit dependencies
hello_virtio_console: hello_virtio_console.S link.ld
read_virtio_block: read_virtio_block.S link.ld
virtio_interrupt_test: virtio_interrupt_test.S link.ld
