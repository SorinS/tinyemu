// Command disasm is an objdump-style multi-ISA disassembler built on the
// tinyemu-go decoders. It reads raw machine code (a file or stdin) or an ELF
// .text section and prints one instruction per line — address, bytes, and the
// decoded mnemonic — for AArch64, RISC-V, or x86. The AArch64 and RISC-V
// decoders are the project's own (cross-checked against llvm); x86 delegates to
// golang.org/x/arch.
package main

import (
	"bufio"
	"debug/elf"
	"encoding/binary"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/jtolio/tinyemu-go/asm"
	a64 "github.com/jtolio/tinyemu-go/asm/arm64"
	rv "github.com/jtolio/tinyemu-go/asm/riscv"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "disasm:", err)
		os.Exit(1)
	}
}

func run(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("disasm", flag.ContinueOnError)
	arch := fs.String("arch", "arm64", "ISA: arm64, riscv64, x86_64, x86")
	base := fs.Uint64("base", 0, "load/origin address for raw input")
	asELF := fs.Bool("elf", false, "read .text from an ELF file instead of raw bytes")
	fs.SetOutput(out)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *asELF {
		code, addr, err := readELFText(fs.Arg(0))
		if err != nil {
			return err
		}
		return Disassemble(out, *arch, code, addr)
	}
	code, err := readBytes(fs.Arg(0))
	if err != nil {
		return err
	}
	return Disassemble(out, *arch, code, *base)
}

// readBytes reads raw input from a file path, or stdin when the path is empty
// or "-".
func readBytes(path string) ([]byte, error) {
	if path == "" || path == "-" {
		return io.ReadAll(os.Stdin)
	}
	return os.ReadFile(path)
}

// Disassemble writes an objdump-style listing of code (loaded at origin) for
// the given architecture.
func Disassemble(out io.Writer, arch string, code []byte, origin uint64) error {
	bw := bufio.NewWriter(out)
	defer bw.Flush()
	emit := func(addr uint64, b []byte, text string) {
		fmt.Fprintf(bw, "%8x:\t%-24s\t%s\n", addr, hexBytes(b), text)
	}
	switch arch {
	case "arm64", "aarch64":
		for off := 0; off+4 <= len(code); off += 4 {
			b := code[off : off+4]
			text, err := a64.DisassembleBytes(b)
			if err != nil {
				text = fmt.Sprintf(".inst 0x%08x", binary.LittleEndian.Uint32(b))
			}
			emit(origin+uint64(off), b, text)
		}
	case "riscv64", "riscv":
		for off := 0; off < len(code); {
			text, n, err := rv.Disassemble(code[off:])
			if err != nil || n == 0 {
				n = 2
				if off+2 > len(code) {
					break
				}
				text = fmt.Sprintf(".short 0x%04x", binary.LittleEndian.Uint16(code[off:]))
			}
			emit(origin+uint64(off), code[off:off+n], text)
			off += n
		}
	case "x86_64", "amd64", "x86", "i386":
		bits := 64
		if arch == "x86" || arch == "i386" {
			bits = 32
		}
		for off := 0; off < len(code); {
			text, n, err := asm.Disassemble(code[off:], bits)
			if err != nil || n == 0 {
				n = 1
				text = fmt.Sprintf(".byte 0x%02x", code[off])
			}
			emit(origin+uint64(off), code[off:off+n], text)
			off += n
		}
	default:
		return fmt.Errorf("unknown -arch %q (want arm64, riscv64, x86_64, or x86)", arch)
	}
	return nil
}

func hexBytes(b []byte) string {
	parts := make([]string, len(b))
	for i, x := range b {
		parts[i] = fmt.Sprintf("%02x", x)
	}
	return strings.Join(parts, " ")
}

// readELFText returns the .text section bytes and its virtual address.
func readELFText(path string) ([]byte, uint64, error) {
	f, err := elf.Open(path)
	if err != nil {
		return nil, 0, err
	}
	defer f.Close()
	sec := f.Section(".text")
	if sec == nil {
		return nil, 0, fmt.Errorf("%s: no .text section", path)
	}
	data, err := sec.Data()
	if err != nil {
		return nil, 0, err
	}
	return data, sec.Addr, nil
}
