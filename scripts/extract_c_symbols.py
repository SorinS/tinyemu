#!/usr/bin/env python3
"""
Extract all functions and macros from C source files in the tinyemu C reference.
Uses universal-ctags for reliable parsing (with end line support).
Outputs a CSV with: name, type, file_path, line_start, line_end
"""

import csv
import subprocess
import sys
from pathlib import Path

# Directory containing C reference implementation
C_REF_DIR = Path(__file__).parent.parent / "tinyemu-2019-12-21"

# Output CSV file
OUTPUT_CSV = Path(__file__).parent.parent / "tmp" / "c_symbols.csv"

# Universal ctags binary
CTAGS_BIN = "ctags-universal"


def run_ctags(root_dir):
    """Run ctags on all C files and return parsed symbols."""
    # Find all .c and .h files
    c_files = list(root_dir.rglob("*.c")) + list(root_dir.rglob("*.h"))

    if not c_files:
        print("No C files found!", file=sys.stderr)
        return []

    # Run ctags with output format that includes line numbers and end lines
    cmd = [
        CTAGS_BIN,
        "-f", "-",           # Output to stdout
        "--fields=+ne",      # n=line numbers, e=end line
        "--c-kinds=+dfp",    # d=macro definitions, f=functions, p=prototypes
        "--sort=no",         # Don't sort (we'll sort ourselves)
    ] + [str(f) for f in c_files]

    print(f"Running {CTAGS_BIN} on {len(c_files)} files...", file=sys.stderr)

    result = subprocess.run(cmd, capture_output=True, text=True)

    if result.returncode != 0:
        print(f"ctags error: {result.stderr}", file=sys.stderr)
        sys.exit(1)

    symbols = []

    for line in result.stdout.strip().split("\n"):
        if not line or line.startswith("!"):  # Skip empty lines and comments
            continue

        # ctags format: name<TAB>file<TAB>pattern;"<TAB>kind<TAB>line:N<TAB>end:M
        parts = line.split("\t")
        if len(parts) < 4:
            continue

        name = parts[0]
        filepath = parts[1]
        kind = None
        line_start = None
        line_end = None

        # Parse the extended fields
        for part in parts[3:]:
            if part in ("d", "define"):
                kind = "macro"
            elif part in ("f", "function"):
                kind = "function"
            elif part in ("p", "prototype"):
                kind = "prototype"
            elif part.startswith("line:"):
                line_start = int(part.split(":")[1])
            elif part.startswith("end:"):
                line_end = int(part.split(":")[1])

        if kind and line_start:
            # Convert to relative path
            try:
                rel_path = str(Path(filepath).relative_to(C_REF_DIR.parent))
            except ValueError:
                rel_path = filepath

            symbols.append({
                "name": name,
                "type": kind,
                "file": rel_path,
                "line_start": line_start,
                "line_end": line_end or ""  # Empty if not available (macros, prototypes)
            })

    return symbols


def main():
    # Ensure output directory exists
    OUTPUT_CSV.parent.mkdir(parents=True, exist_ok=True)

    symbols = run_ctags(C_REF_DIR)

    # Sort by file, then line number
    symbols.sort(key=lambda x: (x["file"], x["line_start"]))

    # Write CSV
    with open(OUTPUT_CSV, "w", newline="", encoding="utf-8") as f:
        writer = csv.DictWriter(f, fieldnames=["name", "type", "file", "line_start", "line_end"])
        writer.writeheader()
        writer.writerows(symbols)

    # Summary
    func_count = sum(1 for s in symbols if s["type"] == "function")
    macro_count = sum(1 for s in symbols if s["type"] == "macro")
    proto_count = sum(1 for s in symbols if s["type"] == "prototype")
    with_end = sum(1 for s in symbols if s["line_end"])

    print(f"\nWrote {len(symbols)} symbols to {OUTPUT_CSV}", file=sys.stderr)
    print(f"  - Functions: {func_count}", file=sys.stderr)
    print(f"  - Macros: {macro_count}", file=sys.stderr)
    print(f"  - Prototypes: {proto_count}", file=sys.stderr)
    print(f"  - With end lines: {with_end}", file=sys.stderr)


if __name__ == "__main__":
    main()
