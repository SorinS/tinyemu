# Assembly examples

Small assembly programs for each of the CPU backends in this tree. Each
file has a header comment listing the expected register state after the
final halt instruction — pair them with `run_asm.sh` (x86/x86_64) or
your own RISC-V harness to verify the emulator end-to-end.

## Layout

```
examples/x86/      — 32-bit programs (cpu/x86)            NASM syntax (.asm)
examples/x86_64/   — 64-bit long-mode programs (cpu/x86_64) NASM syntax (.asm)
examples/riscv64/  — 64-bit RISC-V programs (cpu/riscv)   GNU AS syntax (.S)
```

## Running x86 / x86_64

```sh
./run_asm.sh x86_64 examples/x86_64/factorial.asm   # → RAX = 0x375F00 = 10!
./run_asm.sh x86    examples/x86/fibonacci.asm      # → EAX = 6765
```

The runner prints the final register state on stderr. Each `.asm`
file's header comment lists the expected values.

Requires NASM (`brew install nasm`).

## Running RISC-V

`run_asm.sh` doesn't support `riscv64` yet, but the source files are
ready for any GNU-AS-compatible assembler. To assemble manually:

```sh
# Pick one of:
riscv64-unknown-elf-as   examples/riscv64/factorial.S -o factorial.o
riscv64-linux-gnu-as     examples/riscv64/factorial.S -o factorial.o
clang -target riscv64    -c examples/riscv64/factorial.S -o factorial.o
```

Each program ends with `ebreak`, which traps to the supervisor — the
test harness (or a runner you build) should stop on that vector and
dump the register file. The expected values are in the header comment.

On macOS, the GNU toolchain isn't in Homebrew core — use Nix
(`nix-shell -p pkgsCross.riscv64-embedded.buildPackages.binutils`) or
Docker (`docker run -v "$PWD":/work riscv64/ubuntu as ...`).

## Programs

| Topic              | x86                       | x86_64                          | RISC-V                |
|--------------------|---------------------------|---------------------------------|-----------------------|
| Hello / immediate  | hello                     | hello                           | hello                 |
| Arithmetic         | multiply, divide          | multiply, divide, mul128        | add                   |
| Loops              | factorial, fibonacci      | factorial, fibonacci            | factorial, fibonacci  |
| Algorithms         | gcd, is_prime             | gcd                             | gcd                   |
| Sorting / search   | bubble_sort, max_array    | minmax                          |                       |
| Aggregation        | sum_array                 |                                 | sum_array             |
| Control flow       | branches, call_ret        | branches, call_ret, cmov        | branches, call_ret    |
| Stack / recursion  | stack                     | stack, recursion, swap          |                       |
| String ops         | memcpy, string_length, reverse_string | memcpy, string_length, rep_stos | memcpy, string_length |
| Shifts / bits      | shifts, bit_count         | shifts, bit_count, bittest, bsf_bsr, popcnt | |
| Extension          |                           | movsx_movzx                     |                       |
| Addressing         | lea                       | lea, r8_r15                     |                       |
| Atomics            |                           | cmpxchg                         |                       |
| SIMD               |                           | xmm_add                         |                       |
| System / probes    |                           | cpuid                           |                       |

## Adding new examples

1. Drop a `.asm` or `.S` file into the matching subdir.
2. Header comment: one sentence describing the program, one or two
   lines listing the expected register/memory state after the final
   halt instruction.
3. Halt convention:
   - x86/x86_64: end with `hlt` (the runner stops on `IsPowerDown`).
   - RISC-V: end with `ebreak`.
4. Keep the program self-contained — no external symbols, no static
   relocations the bin-format output doesn't carry.

The aim is "readable as documentation": someone learning the ISA
should be able to skim a file and understand what each instruction
contributes.
