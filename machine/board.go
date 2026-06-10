package machine

import (
	"github.com/jtolio/tinyemu-go/cpu"
	"github.com/jtolio/tinyemu-go/devices"
	"github.com/jtolio/tinyemu-go/mem"
	"github.com/jtolio/tinyemu-go/virtio"
)

// BlockDeviceAttacher is the optional capability implemented by boards that
// can attach a raw block device through native (non-VirtIO) hardware. On a
// PC board this becomes an IDE/ATA disk at the primary channel; on an ARM
// virt board it might become virtio-blk-mmio. cmd/temu does an interface
// assertion to decide which path to take.
//
// Boards that only support VirtIO drives (the RISC-V virt board, today)
// simply don't implement this interface; the generic VirtIO path is then
// used instead.
type BlockDeviceAttacher interface {
	AttachBlockDevice(bd devices.BlockDevice) error
}

// CDROMAttacher is the analogue for CD-ROM media. On a PC board this
// becomes an ATAPI device on the IDE channel; on virtio-equipped boards
// it could be a virtio-blk with the readonly bit set, or virtio-scsi.
// Optional capability; boards that don't implement it can't mount ISOs.
type CDROMAttacher interface {
	AttachCDROM(bd devices.BlockDevice) error
}

// NetAttacher is implemented by boards that attach an EthernetDevice to
// the guest via native means (virtio-net-pci on PC). Boards that only
// support VirtIO-MMIO simply don't implement this; cmd/temu falls back
// to AddVirtIODevice in that case.
type NetAttacher interface {
	AttachNet(es *virtio.EthernetDevice) error
}

// VirtioMMIOAttacher is the optional capability for boards that expose the
// generic VirtIO-MMIO attach path: a fixed per-device slot address + IRQ
// line, with the device registered via AddVirtIODevice. The RISC-V virt
// board uses this; the PC board prefers native hardware (ATA) and
// virtio-pci, so this is not forced onto every Board (advisor report #2).
// cmd/temu type-asserts it for the fallback path.
type VirtioMMIOAttacher interface {
	GetVirtIOAddr() uint64
	GetVirtIOIRQ() *mem.IRQSignal
	AddVirtIODevice(dev *virtio.Device) (int, error)
}

// Board is the generic machine interface implemented by all
// architecture-specific board implementations (RISC-V virt, PC, etc.).
// VirtIO-MMIO attach lives in the optional VirtioMMIOAttacher interface;
// Console() stays here as the machine's primary console accessor.
type Board interface {
	Run(maxCycles int) error
	LoadBIOS(bios, kernel, initrd []byte, cmdline string) error
	GetCPU() cpu.Core
	MemMap() *mem.PhysMemoryMap
	Close()
	IsShutdownRequested() bool
	GetShutdownExitCode() int
	CheckTimer()
	PollDevices()
	GetSleepDuration(delay int) int
	Console() *virtio.Console
}
