package images

import (
	"bytes"
	_ "embed"
	"fmt"
	"io"

	"github.com/ulikunitz/xz"
)

//go:embed output/fw_jump.bin
var BIOS []byte

//go:embed output/kernel-riscv64.bin.xz
var KernelRiscv64_XZ []byte

//go:embed output/root-minimal.bin.xz
var RootFSMinimal_XZ []byte

func XZDecompress(data []byte) ([]byte, error) {
	r, err := xz.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("failed to create xz reader: %w", err)
	}
	decompressed, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("failed to decompress: %w", err)
	}
	return decompressed, nil
}

func MustXZDecompress(data []byte) []byte {
	data, err := XZDecompress(data)
	if err != nil {
		panic(err)
	}
	return data
}
