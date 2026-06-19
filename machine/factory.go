package machine

import (
	"fmt"

	"github.com/sorins/tinyemu-go/machine/pc"
)

// NewBoard creates a new machine board of the given architecture type.
// Supported types: "riscv64", "riscv32", "x86", "x86_64".
func NewBoard(machineType string, cfg Config) (Board, error) {
	switch machineType {
	case "riscv32":
		cfg.MaxXLEN = 32
		return New(cfg)
	case "riscv64":
		cfg.MaxXLEN = 64
		return New(cfg)
	case "x86", "x86_64":
		pcCfg := pc.Config{
			RAMSize:     cfg.RAMSize,
			Console:     cfg.Console,
			MachineType: machineType,
			EnableAPIC:  cfg.EnableAPIC,
		}
		return pc.New(pcCfg)
	default:
		return nil, fmt.Errorf("unsupported machine type: %s", machineType)
	}
}
