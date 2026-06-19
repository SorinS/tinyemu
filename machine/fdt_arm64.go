package machine

// buildARM64FDT generates the flattened device tree for the virt board: a single
// CPU with PSCI, the GICv2, the ARM generic timer, the PL011 UART and the
// populated VirtIO-MMIO slots. It mirrors the QEMU virt layout closely enough
// that a mainline arm64 kernel binds its drivers by compatible string.
func (m *ARM64Machine) buildARM64FDT(dst []byte, initrdStart, initrdEnd uint64, cmdLine string) int {
	const (
		gicPhandle = 1
		clkPhandle = 2
	)
	s := newFDTState()

	s.beginNode("") // root
	s.propU32("#address-cells", 2)
	s.propU32("#size-cells", 2)
	s.propStr("compatible", "linux,dummy-virt")
	s.propStr("model", "linux,dummy-virt")
	s.propU32("interrupt-parent", gicPhandle)

	// chosen
	s.beginNode("chosen")
	s.propStr("bootargs", cmdLine)
	s.propStr("stdout-path", "/pl011@9000000")
	if initrdEnd > initrdStart {
		s.propU64("linux,initrd-start", initrdStart)
		s.propU64("linux,initrd-end", initrdEnd)
	}
	s.endNode()

	// memory
	s.beginNodeNum("memory", a64RAMBase)
	s.propStr("device_type", "memory")
	s.propU64x2("reg", a64RAMBase, m.ramSize)
	s.endNode()

	// UART reference clock (the pl011 driver requires it).
	s.beginNode("apb-pclk")
	s.propStr("compatible", "fixed-clock")
	s.propU32("#clock-cells", 0)
	s.propU32("clock-frequency", 24000000)
	s.propStr("clock-output-names", "clk24mhz")
	s.propU32("phandle", clkPhandle)
	s.endNode()

	// cpus
	s.beginNode("cpus")
	s.propU32("#address-cells", 1)
	s.propU32("#size-cells", 0)
	s.beginNodeNum("cpu", 0)
	s.propStr("device_type", "cpu")
	s.propStr("compatible", "arm,cortex-a57")
	s.propU32("reg", 0)
	s.propStr("enable-method", "psci")
	s.endNode()
	s.endNode()

	// PSCI (power state coordination via HVC).
	s.beginNode("psci")
	s.propStrTab("compatible", "arm,psci-0.2", "arm,psci")
	s.propStr("method", "hvc")
	s.endNode()

	// Generic timer: secure-phys/non-secure-phys(EL1)/virtual/hyp PPIs, all
	// level-high to CPU0 (flags 0x104).
	s.beginNode("timer")
	s.propStr("compatible", "arm,armv8-timer")
	s.propU32Tab("interrupts",
		1, 13, 0x104, // secure physical
		1, 14, 0x104, // non-secure physical (EL1) -> INTID 30
		1, 11, 0x104, // virtual            -> INTID 27
		1, 10, 0x104, // hypervisor
	)
	s.endNode()

	// GICv2: distributor + CPU interface.
	s.beginNodeNum("intc", a64GICDBase)
	s.propStr("compatible", "arm,cortex-a15-gic")
	s.propU32("#interrupt-cells", 3)
	s.propEmpty("interrupt-controller")
	s.propU32("#address-cells", 0)
	s.propU32Tab("reg",
		0, a64GICDBase, 0, 0x10000,
		0, a64GICCBase, 0, 0x10000,
	)
	s.propU32("phandle", gicPhandle)
	s.endNode()

	// PL011 UART on SPI 1 (level-high).
	s.beginNodeNum("pl011", a64UARTBase)
	s.propStrTab("compatible", "arm,pl011", "arm,primecell")
	s.propU64x2("reg", a64UARTBase, 0x1000)
	s.propU32Tab("interrupts", 0, 1, 4) // SPI 1, level-high
	s.propU32Tab("clocks", clkPhandle, clkPhandle)
	s.propStrTab("clock-names", "uartclk", "apb_pclk")
	s.endNode()

	// VirtIO-MMIO slots that are populated. Device i is SPI (16+i), level-high.
	for i := 0; i < m.virtioCount; i++ {
		addr := uint64(a64VirtIOBase + i*a64VirtIOSize)
		s.beginNodeNum("virtio_mmio", addr)
		s.propStr("compatible", "virtio,mmio")
		s.propU64x2("reg", addr, a64VirtIOSize)
		s.propU32Tab("interrupts", 0, uint32(16+i), 4)
		s.endNode()
	}

	s.endNode() // root
	return s.output(dst)
}
