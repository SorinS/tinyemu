# x86 Emulator Tests

This directory contains integration tests for the x86 CPU emulator.

## Quick Start

```bash
# Run all x86 assembly tests (requires NASM)
make test-x86-asm

# Run test386 CPU test suite milestones
make test-x86-test386

# Run everything including the full test386 attempt
make test-x86-asm test-x86-test386
```

## Writing a New Assembly Test

Assembly tests are written in NASM syntax and executed directly on the emulator.
They are useful for verifying specific instructions or instruction sequences.

### 1. Write the NASM source

Add a new test function to `asm_test.go` (or a new `*_test.go` file).
The source should use `bits 16` and avoid `org` directives — the binary is
loaded at the start of the CS segment and execution begins at IP=0.

```go
func TestAsmMyFeature(t *testing.T) {
	src := `bits 16
	mov ax, 0x1234
	mov bx, 0x5678
	add ax, bx
	hlt
`
	bin := assemble(t, src)
	defer os.Remove(bin)

	r := newAsmRunner(t)
	r.load(t, bin, 0xF0000)
	r.setStart(0xF000, 0x0000)

	if err := r.run(t, 100); err != nil {
		t.Fatalf("execution failed: %v", err)
	}
	if v := r.cpu.GetReg16(x86.AX); v != 0x68AC {
		t.Errorf("AX = 0x%04X, want 0x68AC", v)
	}
}
```

### 2. Key helpers

| Helper | Purpose |
|--------|---------|
| `newAsmRunner(t)` | Creates a CPU with 1MB RAM in PC reset state |
| `assemble(t, src)` | Runs `nasm -f bin` on the source string |
| `r.load(t, bin, physAddr)` | Copies the flat binary to physical memory |
| `r.setStart(cs, ip)` | Sets initial CS:IP |
| `r.run(t, maxSteps)` | Executes until HLT, error, or step limit |

### 3. Assertions

Use the `cpu/x86` register accessors to verify state:

```go
r.cpu.GetReg32(x86.EAX)   // 32-bit
r.cpu.GetReg16(x86.AX)    // 16-bit
r.cpu.GetReg8(x86.AL)     // 8-bit
r.cpu.GetEIP()
r.cpu.GetSeg(x86.CS)
r.cpu.IsProtectedMode()
```

## test386 Integration

[test386](https://github.com/barotto/test386.asm) is a comprehensive 80386 CPU
test suite that runs as a BIOS replacement. It communicates via POST codes written
to I/O port `0x190`.

The test binary is built from `bin/test386_asm.git/` and loaded at physical
`0xF0000`. The Go tests capture POST codes and assert milestones:

| Test | Assertion |
|------|-----------|
| `TestTest386MilestonePost00` | Must reach POST 0x00 (Initialization) |
| `TestTest386MilestonePost01` | Must reach POST 0x01 (Real-mode data movement) |
| `TestTest386FullRun` | Must reach POST 0xFF (success) — skipped until ready |

As the emulator gains more opcodes, advance the milestone tests to match.

### Rebuilding test386

```bash
cd bin/test386_asm.git
make   # requires nasm
```

### Debugging test386 progress

Run with verbose logging to see POST codes and the last error:

```bash
go test -v -run TestTest386Progress ./test/x86/...
```

## Architecture Notes

- `asm_test.go` — NASM assembly test framework and sample tests
- `test386_test.go` — test386 BIOS-test runner and milestones
- The tests live in package `x86_test` so they exercise the public API
