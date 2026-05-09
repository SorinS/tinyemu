---
id: tinyemu-go-8cs
status: closed
deps: []
links: []
created: 2026-01-16T21:53:54.82141494-05:00
type: bug
priority: 1
---
# FMIN/FMAX fails compliance test case 20

The rv64uf-p-fmin and rv64ud-p-fmin compliance tests both fail at test case 20.

## Symptoms
```
go test -v -run "TestRISCVCompliance/rv64uf/fmin" ./cpu/
    compliance_test.go:160: test case 20 failed (tohost=0x29)

go test -v -run "TestRISCVCompliance/rv64ud/fmin" ./cpu/
    compliance_test.go:160: test case 20 failed (tohost=0x29)
```

## Investigation needed
1. Disassemble the test binary to find test case 20
2. Check what FMIN/FMAX behavior is being tested
3. Compare against RISC-V spec and TinyEMU C implementation

## Likely causes
- NaN handling in FMIN/FMAX (IEEE 754-2008 minNum/maxNum semantics)
- Signaling NaN vs quiet NaN behavior
- -0.0 vs +0.0 comparison

## Files to check
- softfp/softfp.go (FMIN/FMAX implementation)
- cpu/fp.go (floating-point instruction execution)
- tinyemu-2019-12-21/softfp.c (reference implementation)


