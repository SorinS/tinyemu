// Package elfloader provides ELF file loading functionality for RISC-V binaries.
// This package has no dependencies on other tinyemu-go packages, allowing it
// to be imported by both cpu and test packages without circular imports.
package elfloader

import (
	"bytes"
	"debug/elf"
	"fmt"
	"io"
	"os"
)

// Info contains information extracted from an ELF file.
type Info struct {
	// Entry point address
	EntryPoint uint64

	// Symbol addresses (nil if symbol not found)
	BeginSignature *uint64
	EndSignature   *uint64
	ToHostAddr     *uint64

	// Segments to load
	Segments []Segment
}

// Segment represents a loadable segment from an ELF file.
type Segment struct {
	VAddr uint64 // Virtual address to load at
	Data  []byte // Segment data
}

// Load loads an ELF file from the given path and extracts relevant information.
func Load(path string) (*Info, error) {
	f, err := elf.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open ELF file: %w", err)
	}
	defer f.Close()

	return parseELF(f)
}

// LoadFromReader loads an ELF from an io.ReaderAt.
func LoadFromReader(r io.ReaderAt) (*Info, error) {
	f, err := elf.NewFile(r)
	if err != nil {
		return nil, fmt.Errorf("failed to parse ELF: %w", err)
	}
	defer f.Close()

	return parseELF(f)
}

// LoadFromBytes loads an ELF from a byte slice.
func LoadFromBytes(data []byte) (*Info, error) {
	return LoadFromReader(bytes.NewReader(data))
}

// parseELF extracts information from a parsed ELF file.
func parseELF(f *elf.File) (*Info, error) {
	info := &Info{
		EntryPoint: f.Entry,
	}

	// Load program segments
	for _, prog := range f.Progs {
		if prog.Type != elf.PT_LOAD {
			continue
		}

		data := make([]byte, prog.Memsz)
		if prog.Filesz > 0 {
			_, err := prog.ReadAt(data[:prog.Filesz], 0)
			if err != nil && err != io.EOF {
				return nil, fmt.Errorf("failed to read segment: %w", err)
			}
		}
		// Remaining bytes (Memsz - Filesz) are already zero

		info.Segments = append(info.Segments, Segment{
			VAddr: prog.Vaddr,
			Data:  data,
		})
	}

	// Find symbols
	symbols, err := f.Symbols()
	if err == nil {
		for _, sym := range symbols {
			switch sym.Name {
			case "begin_signature":
				addr := sym.Value
				info.BeginSignature = &addr
			case "end_signature":
				addr := sym.Value
				info.EndSignature = &addr
			case "tohost":
				addr := sym.Value
				info.ToHostAddr = &addr
			}
		}
	}

	return info, nil
}

// String returns a human-readable representation of Info.
func (e *Info) String() string {
	s := fmt.Sprintf("Entry: 0x%x\n", e.EntryPoint)
	s += fmt.Sprintf("Segments: %d\n", len(e.Segments))
	for i, seg := range e.Segments {
		s += fmt.Sprintf("  [%d] VAddr=0x%x Size=%d\n", i, seg.VAddr, len(seg.Data))
	}
	if e.BeginSignature != nil {
		s += fmt.Sprintf("begin_signature: 0x%x\n", *e.BeginSignature)
	}
	if e.EndSignature != nil {
		s += fmt.Sprintf("end_signature: 0x%x\n", *e.EndSignature)
	}
	if e.ToHostAddr != nil {
		s += fmt.Sprintf("tohost: 0x%x\n", *e.ToHostAddr)
	}
	return s
}

// LoadFromTempFile loads an ELF from a byte slice by writing to a temp file.
// Deprecated: Use LoadFromBytes which is more efficient.
func LoadFromTempFile(data []byte) (*Info, error) {
	f, err := os.CreateTemp("", "elf-*.bin")
	if err != nil {
		return nil, err
	}
	defer os.Remove(f.Name())
	defer f.Close()

	if _, err := f.Write(data); err != nil {
		return nil, err
	}
	if _, err := f.Seek(0, 0); err != nil {
		return nil, err
	}

	return LoadFromReader(f)
}
