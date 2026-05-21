# Assembly examples

Small NASM programs runnable via `./run_asm.sh`. Pair each with a sanity
check using the registers/memory the program leaves in known states.

## Layout

```
examples/x86/      — 32-bit programs (cpu/x86)
examples/x86_64/   — 64-bit long-mode programs (cpu/x86_64)
```

## Running

```sh
./run_asm.sh x86_64 examples/x86_64/factorial.asm     # → RAX = 0x375F00 = 10!
./run_asm.sh x86    examples/x86/fibonacci.asm        # → EAX = 6765
```

The runner prints the final register state on stderr. Each .asm file's
header comment lists the expected values.

## Categories

| Topic            | x86 file              | x86_64 file               |
|------------------|-----------------------|---------------------------|
| Basic arithmetic | hello, multiply, divide | hello, multiply, divide |
| Loops            | factorial, fibonacci  | factorial, fibonacci      |
| Algorithms       | gcd                   | gcd                       |
| Control flow     | branches, call_ret    | branches, call_ret, cmov  |
| Stack            | stack                 | stack, recursion, swap    |
| String ops       | memcpy, string_length | memcpy, string_length     |
| Shifts/bits      | shifts, bit_count     | shifts, bit_count, bittest |
| Addressing       | lea                   | lea, r8_r15               |
| Atomics          |                       | cmpxchg                   |
| SIMD             |                       | xmm_add                   |
| Min/max          |                       | minmax                    |

Add new programs as plain `.asm` files in the matching subdir — the
runner will pick them up by path.
