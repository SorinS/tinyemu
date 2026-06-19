# TinyEMU-Go Agent Prompt

You're porting TinyEMU (a RISC-V emulator) from C to Go. This is a 
**transliteration**, not a rewrite. Please match C behavior exactly. Even 
implementing what might otherwise seem like improved error handling could 
cause behavior to diverge and the system to no longer boot.

## Workflow

```bash
# 1. Get next ticket
tk ready                          # Show unblocked work (pick highest priority)
tk show <ticket-id>               # Read full description

# 2. Claim it
tk start <ticket-id>

# 3. Do the work (see expectations below)

# 4. Close and commit
tk close <ticket-id>
git add -A && git commit          # Follow commit format below

# 5. Create tickets for any bugs or issues you discover.
tk help
```

## Key Paths

| Resource | Path |
|----------|------|
| C reference | `tinyemu-2019-12-21/` |
| Go packages | `cpu/`, `mem/`, `devices/`, `virtio/`, `softfp/`, `machine/` |
| Tests | `*_test.go` in each package |
| Porting plan | `docs/tinyemu-porting-plan.md` |
| RISC-V test binaries | `testdata/riscv-tests/isa/` |
| Temp space or testing | `tmp/` |

## Commit Expectations

**Every commit must:**

- [ ] Pass `go test ./...`
- [ ] Pass `go vet ./...`
- [ ] Have `gofmt -s` and `goimports -local github.com/sorins/tinyemu-go` run.
- [ ] Maintain/improve test coverage (`go test -cover ./...`)
- [ ] Reference corresponding C code in comments
- [ ] Include regression tests for any bug fixes

**Match C behavior exactly:**

If a C method has no bearing on emulator correctness (e.g. logging),
substituting a Go stdlib function is okay instead of reimplementing.

Note that for 128 bit int support, we are using
lukechampine.com/uint128. Please make sure to support 128 bit this
way even though it's not a native Go type.

**Commit message format:**
```
<package>: <short description>

<details if needed>

Reference: <C file>:<lines>
```

## Before Writing Code

1. **Read the C source** for the feature you're implementing
2. **Read existing Go code** to understand patterns used
3. **Write failing test first** for bug fixes
4. **Check ticket description** for specific files/references

### Commands

```bash
go test ./...                    # Run all tests
go test -short ./...	         # Run fast tests
go test -cover ./...             # Check coverage
go test -v ./cpu/...             # Verbose for one package
```

Target coverage: 75% minimum.

### Do Not

- Skip writing regression tests for bugs found during integration testing
- Use /tmp/. Use tmp/ in this folder instead.

## Quick Reference: Key C Files

| C File | Go Package | Purpose |
|--------|------------|---------|
| `riscv_cpu.c` | `cpu/` | CPU state, memory ops |
| `riscv_cpu_template.h` | `cpu/` | Instruction execution |
| `riscv_machine.c` | `machine/`, `devices/` | CLINT, PLIC, HTIF |
| `virtio.c` | `virtio/` | VirtIO devices |
| `softfp.c` | `softfp/` | IEEE754 float ops |
| `iomem.c` | `mem/` | Physical memory |

## Quick Reference: Tickets

```
tk - minimal ticket system with dependency tracking

Usage: tk <command> [args]

Commands:
  create [title] [options] Create ticket, prints ID
    -d, --description      Description text
    --design               Design notes
    --acceptance           Acceptance criteria
    -t, --type             Type (bug|feature|task|epic|chore) [default: task]
    -p, --priority         Priority 0-4, 0=highest [default: 2]
    -a, --assignee         Assignee
    --external-ref         External reference (e.g., gh-123, JIRA-456)
    --parent               Parent ticket ID
    --tags                 Comma-separated tags (e.g., --tags ui,backend,urgent)
  start <id>               Set status to in_progress
  close <id>               Set status to closed
  reopen <id>              Set status to open
  status <id> <status>     Update status (open|in_progress|closed)
  dep <id> <dep-id>        Add dependency (id depends on dep-id)
  dep tree [--full] <id>   Show dependency tree (--full disables dedup)
  dep cycle                Find dependency cycles in open tickets
  undep <id> <dep-id>      Remove dependency
  link <id> <id> [id...]   Link tickets together (symmetric)
  unlink <id> <target-id>  Remove link between tickets
  ls [--status=X] [-a X] [-T X]   List tickets
  ready [-a X] [-T X]      List open/in-progress tickets with deps resolved
  blocked [-a X] [-T X]    List open/in-progress tickets with unresolved deps
  closed [--limit=N] [-a X] [-T X] List recently closed tickets (default 20, by mtime)
  show <id>                Display ticket
  edit <id>                Open ticket in $EDITOR
  add-note <id> [text]     Append timestamped note (or pipe via stdin)
  query [jq-filter]        Output tickets as JSON, optionally filtered
  migrate-beads            Import tickets from .beads/issues.jsonl

Tickets stored as markdown files in .tickets/
Supports partial ID matching (e.g., 'tk show 5c4' matches 'nw-5c46')
```
