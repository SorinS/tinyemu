# Commit Expectations for TinyEMU-Go

This document outlines the expectations for every commit to this project, derived from lessons learned during the Phase 1-3 implementation.

## 1. Test Coverage

**Every commit must maintain or improve test coverage.**

- Target **75% minimum coverage** for all packages
- **Critical packages** (cpu/, mem/, devices/, etc) should target **80%+**
- Run `go test -cover ./...` before committing

The boot-blocking bug (store access fault at unmapped address 0x10000) could have been caught earlier with better coverage in the memory subsystem. Low coverage in critical paths creates risk.

## 2. Clear Mapping to TinyEMU C Code

**All logic must have clear, documented mapping to the original TinyEMU C code.**

When implementing or modifying functionality:

1. **Reference the C source** - Include comments indicating the corresponding C file and line numbers:
   ```go
   // Reference: riscv_cpu.c lines 462-468
   func (m *PhysMemoryMap) Write32(paddr uint64, val uint32) error {
   ```

2. **Match C behavior exactly** - TinyEMU C has specific behaviors that Linux depends on.

3. **Document intentional differences** - If Go behavior must differ from C, document why:
   ```go
   // NOTE: Differs from C implementation.
   // C uses global state; Go uses per-CPU state for thread safety.
   ```

## 3. Test-First Bug Fixes

**Write a failing test before fixing any bug.**

The correct workflow for bug fixes:

1. **Identify the bug** - Note the symptom and root cause
2. **Write a test that fails** - Reproduce the bug in a test
3. **Fix the code** - Make the test pass
4. **Verify no regressions** - Run full test suite

Example for the unmapped memory bug:
```go
// mem/physmem_test.go
func TestWriteUnmappedAddress(t *testing.T) {
    m := NewPhysMemoryMap()
    m.RegisterRAM(0, 0x10000, 0)  // 64KB at 0x0

    // Write to first unmapped address (TinyEMU C silently ignores)
    err := m.Write32(0x10000, 0xDEADBEEF)
    assert.NoError(t, err)  // Should NOT error
}
```

## 4. Commit Message Standards

Commit messages should follow this format:

```
<package>: <short description>

<longer description if needed>

Reference: <C source file and lines if applicable>
```

Example:
```
mem: silently ignore writes to unmapped addresses

TinyEMU C silently ignores writes to unmapped physical addresses
rather than raising exceptions. This matches the behavior Linux
expects during early boot probing.

Reference: riscv_cpu.c lines 462-468
```

## 5. Pre-Commit Checklist

Before every commit:

- [ ] `go test ./...` passes
- [ ] `go test -cover ./...` shows adequate coverage
- [ ] `go vet ./...` reports no issues
- [ ] Code has reference comments to corresponding C code
- [ ] Bug fixes include regression tests

### Testing Order

1. **Unit tests** - Test individual functions and edge cases
2. **RISC-V compliance tests (riscv-tests)** - Validate each instruction against reference
3. **Small programs** - Test subsystem interactions (timer, MMU, devices)
4. **Linux boot** - Final integration validation

### Why This Matters

The boot-blocking bugs in this project were instruction-level issues (compressed JAL link address, trap handling). These would have been caught immediately by compliance tests. Hours of boot debugging could have been avoided with 30 minutes of compliance testing.

### Running Compliance Tests

```bash
# Run all compliance tests
go test -v -run TestRISCVCompliance ./cpu/

# Run specific test suite
go test -v -run "TestRISCVCompliance/rv64ui" ./cpu/
```

## Summary

The TinyEMU-Go project is a transliteration of C code. Success depends on:

1. **Matching C behavior exactly** - Even "improvements" like explicit error handling can break Linux boot
2. **Testing edge cases** - Boundary conditions (like address 0x10000) are where bugs hide
3. **Documenting the mapping** - Future developers need to trace Go code back to C
4. **Testing bottom-up** - Validate instructions before attempting Linux boot

When in doubt, check what the C code does and match it.
