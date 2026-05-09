package machine

import (
	"github.com/jtolio/tinyemu-go/cpu"
	"github.com/jtolio/tinyemu-go/mem"
	"github.com/jtolio/tinyemu-go/virtio"
)

// Board is the generic machine interface implemented by all
// architecture-specific board implementations (RISC-V virt, PC, etc.).
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
	GetVirtIOAddr() uint64
	GetVirtIOIRQ() *mem.IRQSignal
	AddVirtIODevice(dev *virtio.Device) (int, error)
	Console() *virtio.Console
}
