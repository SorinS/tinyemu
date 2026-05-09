// Package temubox implements a friendly human Go wrapper around
// the AI-generated transliterated github.com/jtolio/tinyemu-go.
package temubox

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/jtolio/tinyemu-go/devices"
	"github.com/jtolio/tinyemu-go/machine"
	"github.com/jtolio/tinyemu-go/virtio"
)

type Config struct {
	RAM        int
	ConsoleOut io.Writer
	ConsoleIn  io.Reader
	BIOS       []byte
	RootFS     []byte
	Kernel     []byte
	Cmdline    string
}

type Emulator struct {
	cfg Config
}

func NewEmulator(cfg Config) (*Emulator, error) {
	if cfg.RAM <= 0 {
		cfg.RAM = 256 * 1024 * 1024
	}
	if cfg.ConsoleOut == nil {
		cfg.ConsoleOut = dummyWriter{}
	}
	if cfg.ConsoleIn == nil {
		cfg.ConsoleIn = dummyReader{}
	}
	if cfg.Cmdline == "" {
		cfg.Cmdline = "console=hvc0 root=/dev/vda virtio_net.napi_tx=0 rw"
	}
	return &Emulator{
		cfg: cfg,
	}, nil
}

func (e *Emulator) Run(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	m, err := machine.NewBoard("riscv64", machine.Config{
		RAMSize: uint64(e.cfg.RAM),
		MaxXLEN: 64,
		Console: &virtio.CharacterDevice{Writer: e.cfg.ConsoleOut, Reader: dummyReader{}},
	})
	if err != nil {
		return err
	}
	defer m.Close()

	nbInput := startNonblockingReader(ctx, e.cfg.ConsoleIn)

	root := devices.NewMemoryBlockDeviceFromData(e.cfg.RootFS)
	defer root.Close()

	virtRoot, err := virtio.NewBlockDevice(m.MemMap(), m.GetVirtIOAddr(), m.GetVirtIOIRQ(), root)
	if err != nil {
		return err
	}
	_, err = m.AddVirtIODevice(virtRoot.Device())
	if err != nil {
		return err
	}

	var nics []*virtio.EthernetDevice
	if es := newNetDevice(); es != nil {
		virtNet, err := virtio.NewNet(m.MemMap(), m.GetVirtIOAddr(), m.GetVirtIOIRQ(), es)
		if err != nil {
			return err
		}
		nics = append(nics, es)
		_, err = m.AddVirtIODevice(virtNet.Device())
		if err != nil {
			return err
		}
		if es.DeviceSetCarrier != nil {
			es.DeviceSetCarrier(true)
		}
	}

	err = m.LoadBIOS(e.cfg.BIOS, e.cfg.Kernel, nil, e.cfg.Cmdline)
	if err != nil {
		return err
	}

	const maxExecCycle = 500000
	const maxSleepTime = 10 * time.Millisecond

	cpu := m.GetCPU()
	for {
		if ctx.Err() != nil {
			return nil
		}
		if m.IsShutdownRequested() {
			if exitCode := m.GetShutdownExitCode(); exitCode != 0 {
				return fmt.Errorf("shutdown exit nonzero: %d", exitCode)
			}
			return nil
		}
		m.CheckTimer()
		m.PollDevices()
		for _, es := range nics {
			netPoll(es)
		}

		if virtConsole := m.Console(); virtConsole != nil && virtConsole.CanWriteData() {
			writeLen := virtConsole.GetWriteLen()
			if writeLen > 0 {
				var mem [4096]byte
				buf := mem[:min(len(mem), writeLen)]
				n, err := nbInput.Read(buf)
				if err != nil && errors.Is(err, io.EOF) {
					return err
				}
				if n > 0 {
					virtConsole.WriteData(buf[:n])
				}
			}
		}

		if cpu.IsPowerDown() {
			if cpu.HasPendingInterrupt() {
				cpu.SetPowerDown(false)

			} else {
				time.Sleep(maxSleepTime)
				continue
			}
		}

		cpu.Run(maxExecCycle)
	}
}
