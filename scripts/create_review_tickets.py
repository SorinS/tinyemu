#!/usr/bin/env python3
"""
Create review tickets for each C code batch.
Uses the 'tk' (ticket) CLI to create tickets.
"""

import csv
import subprocess
import sys
from pathlib import Path

BATCHES_DIR = Path(__file__).parent.parent / "tmp" / "review_batches"
INDEX_FILE = BATCHES_DIR / "index.csv"

# Template for ticket description
DESCRIPTION_TEMPLATE = """Review C code batch {batch_num} and ensure Go implementation matches exactly.

## C Functions to Review

{function_list}

## Instructions

1. **Read the C code carefully** for each function listed above
2. If the C code is **only** for x86 emulation or /dev/kvm support, you can
   skip that function and move on. We are also skipping graphics such as
   framebuffer support. Everything else (including the network stack) we are
   porting. Please ask if you have uncertainty or this seems unclear regarding
   any function at all.
3. **Find the corresponding Go code** in the appropriate package
4. **If Go code doesn't exist:**
   - Write the Go implementation matching C behavior exactly
   - Write tests following docs/COMMIT_EXPECTATIONS.md
   - Target 80%+ test coverage.
5. **If Go code exists:**
   - Compare line-by-line for exact behavioral match
   - Add/update comments referencing C code: `// Reference: {file}:{line_start}-{line_end}`
   - Fix ANY deviations including error handling differences
6. **Write tests** to confirm behavior matches C code

## Critical Reminders

- Match C behavior exactly - even "improved" error handling can break Linux boot
- The C code works. Our Go code doesn't boot Linux. Any deviation is suspect.

## Files

{file_list}

## Acceptance Criteria

- [ ] All functions reviewed against C source
- [ ] Go implementations exist for all functions (or documented as intentionally skipped)
- [ ] Comments reference C code with file:line format
- [ ] No behavioral deviations from C (especially error handling)
- [ ] Tests written confirming C-matching behavior
- [ ] `go test ./...` passes
- [ ] `go vet ./...` passes

## Last notes 

Finally, once you are done, close the ticket with `tk` and commit any changes 
you have made.
"""

# Template for design notes
DESIGN_TEMPLATE = """## C to Go Package Mapping

Examples:

| C File | Go Package |
|--------|------------|
| riscv_cpu.c | cpu/ |
| riscv_cpu_template.h | cpu/ |
| riscv_machine.c | machine/, devices/ |
| virtio.c | virtio/ |
| softfp.c | softfp/ |
| iomem.c | mem/ |

## How to Review

```bash
# Read C function
cat tinyemu-2019-12-21/{file} | sed -n '{start},{end}p'

# Find Go equivalent

# Run tests
go test -cover ./...
```
"""


def load_batches():
    """Load batch information from index."""
    batches = []
    with open(INDEX_FILE) as f:
        reader = csv.DictReader(f)
        for row in reader:
            batch_file = BATCHES_DIR / row["batch"]
            functions = []

            with open(batch_file) as bf:
                # Skip comment lines
                lines = [l for l in bf if not l.startswith("#")]
                breader = csv.DictReader(lines)
                for func in breader:
                    functions.append(func)

            batches.append({
                "batch": row["batch"],
                "num_functions": int(row["num_functions"]),
                "total_lines": int(row["total_lines"]),
                "files": row["files"].split(";"),
                "functions": functions
            })
    return batches


def create_ticket(batch_num, batch_info, dry_run=False):
    """Create a ticket for a batch."""

    # Build function list
    func_lines = []
    for func in batch_info["functions"]:
        func_lines.append(
            f"- `{func['name']}` ({func['file']}:{func['line_start']}-{func['line_end']}, {func['size']} lines)"
        )
    function_list = "\n".join(func_lines)

    # Build file list
    file_list = "\n".join(f"- `{f}`" for f in batch_info["files"])

    # Primary file for title
    primary_file = Path(batch_info["files"][0]).name

    # Create description
    description = DESCRIPTION_TEMPLATE.format(
        batch_num=batch_num,
        function_list=function_list,
        file_list=file_list,
        file=batch_info["files"][0],
        line_start=batch_info["functions"][0]["line_start"] if batch_info["functions"] else "?",
        line_end=batch_info["functions"][-1]["line_end"] if batch_info["functions"] else "?"
    )

    # Create title
    if len(batch_info["files"]) == 1:
        title = f"Review batch {batch_num:03d}: {primary_file} ({batch_info['num_functions']} functions, {batch_info['total_lines']} lines)"
    else:
        title = f"Review batch {batch_num:03d}: {primary_file} + {len(batch_info['files'])-1} more ({batch_info['num_functions']} functions, {batch_info['total_lines']} lines)"

    # Acceptance criteria
    acceptance = """- All functions reviewed against C source
- Go implementations exist with C reference comments
- No behavioral deviations from C
- Tests confirm C-matching behavior
- go test and go vet pass"""

    # Build command
    cmd = [
        "tk", "create", title,
        "-d", description,
        "--acceptance", acceptance,
        "-t", "task",
        "-p", "2",
        "--tags", "c-review,port-verification"
    ]

    if dry_run:
        print(f"Would create: {title}")
        return None

    result = subprocess.run(cmd, capture_output=True, text=True)

    if result.returncode != 0:
        print(f"Error creating ticket for batch {batch_num}: {result.stderr}", file=sys.stderr)
        return None

    ticket_id = result.stdout.strip()
    print(f"Created {ticket_id}: {title}")
    return ticket_id


def main():
    import argparse
    parser = argparse.ArgumentParser(description="Create review tickets for C code batches")
    parser.add_argument("--dry-run", action="store_true", help="Print what would be created without creating")
    parser.add_argument("--start", type=int, default=1, help="Start from batch N")
    parser.add_argument("--end", type=int, default=None, help="End at batch N")
    parser.add_argument("--only", type=int, nargs="+", help="Only create specific batch numbers")
    args = parser.parse_args()

    batches = load_batches()
    print(f"Loaded {len(batches)} batches")

    created = []

    for i, batch in enumerate(batches, 1):
        # Filter based on arguments
        if args.only and i not in args.only:
            continue
        if i < args.start:
            continue
        if args.end and i > args.end:
            continue

        ticket_id = create_ticket(i, batch, dry_run=args.dry_run)
        if ticket_id:
            created.append(ticket_id)

    if not args.dry_run:
        print(f"\nCreated {len(created)} tickets")
    else:
        print(f"\nWould create {len(created)} tickets")


if __name__ == "__main__":
    main()
