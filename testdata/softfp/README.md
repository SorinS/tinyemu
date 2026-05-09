# Soft Float Test Vectors

This directory contains test vectors for soft float operations.

## Purpose

Test vectors verify that the softfp package correctly implements IEEE 754
floating-point operations, including:

- Basic arithmetic (add, sub, mul, div, sqrt)
- Fused multiply-add (fma)
- Comparisons (eq, lt, le)
- Conversions (float-int, int-float, float-float)
- Special value handling (NaN, infinity, subnormals)
- Rounding mode behavior

## Vector Format

Test vectors are stored as JSON files:

```json
{
  "operation": "fadd",
  "precision": "f32",
  "vectors": [
    {
      "a": "0x3f800000",
      "b": "0x40000000",
      "rm": 0,
      "result": "0x40400000",
      "flags": 0
    }
  ]
}
```

Fields:
- `a`, `b`, `c`: Input operands as hex strings
- `rm`: Rounding mode (0=RNE, 1=RTZ, 2=RDN, 3=RUP, 4=RMM)
- `result`: Expected result as hex string
- `flags`: Expected exception flags (NV=16, DZ=8, OF=4, UF=2, NX=1)

## Sources

Test vectors can be generated from:
1. Berkeley TestFloat: http://www.jhauser.us/arithmetic/TestFloat.html
2. RISC-V compliance tests
3. Hand-crafted edge cases

## Generating Vectors

```bash
# Using TestFloat
testfloat_gen f32_add | ./convert_vectors.py > f32_add.json
```
