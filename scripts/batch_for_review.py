#!/usr/bin/env python3
"""
Group C functions into review-friendly batches of ~250 lines each.
Outputs one CSV per batch to tmp/review_batches/
"""

import csv
import os
from pathlib import Path

INPUT_CSV = Path(__file__).parent.parent / "tmp" / "c_symbols.csv"
OUTPUT_DIR = Path(__file__).parent.parent / "tmp" / "review_batches"
TARGET_BATCH_SIZE = 250  # lines per batch


def load_functions(csv_path):
    """Load function symbols from CSV."""
    functions = []
    with open(csv_path) as f:
        reader = csv.DictReader(f)
        for row in reader:
            if row["type"] == "function" and row["line_end"]:
                row["line_start"] = int(row["line_start"])
                row["line_end"] = int(row["line_end"])
                row["size"] = row["line_end"] - row["line_start"] + 1
                functions.append(row)
    return functions


def create_batches(functions, target_size):
    """
    Group functions into batches targeting ~target_size lines each.
    Large functions get their own batch.
    """
    # Sort by file, then line number for logical grouping
    functions.sort(key=lambda f: (f["file"], f["line_start"]))

    batches = []
    current_batch = []
    current_size = 0
    current_file = None

    for func in functions:
        # Large functions get their own batch
        if func["size"] > target_size:
            # Flush current batch first
            if current_batch:
                batches.append(current_batch)
            batches.append([func])
            current_batch = []
            current_size = 0
            current_file = None
            continue

        # Check if adding this function would exceed target
        # Also start new batch on file change if current batch is substantial
        would_exceed = current_size + func["size"] > target_size * 1.2
        file_change = current_file and func["file"] != current_file and current_size > target_size * 0.5

        if current_batch and (would_exceed or file_change):
            batches.append(current_batch)
            current_batch = []
            current_size = 0

        current_batch.append(func)
        current_size += func["size"]
        current_file = func["file"]

    # Don't forget the last batch
    if current_batch:
        batches.append(current_batch)

    return batches


def main():
    # Clean and create output directory
    OUTPUT_DIR.mkdir(parents=True, exist_ok=True)
    for f in OUTPUT_DIR.glob("batch_*.csv"):
        f.unlink()

    functions = load_functions(INPUT_CSV)
    print(f"Loaded {len(functions)} functions")

    batches = create_batches(functions, TARGET_BATCH_SIZE)
    print(f"Created {len(batches)} batches")

    # Write each batch to its own CSV
    fieldnames = ["name", "type", "file", "line_start", "line_end", "size"]

    for i, batch in enumerate(batches, 1):
        batch_file = OUTPUT_DIR / f"batch_{i:03d}.csv"
        total_lines = sum(f["size"] for f in batch)
        files = sorted(set(f["file"] for f in batch))

        with open(batch_file, "w", newline="") as f:
            # Write header comment with summary
            f.write(f"# Batch {i}: {len(batch)} functions, {total_lines} lines\n")
            f.write(f"# Files: {', '.join(files)}\n")

            writer = csv.DictWriter(f, fieldnames=fieldnames)
            writer.writeheader()
            writer.writerows(batch)

        print(f"  batch_{i:03d}.csv: {len(batch):3d} functions, {total_lines:4d} lines ({', '.join(Path(f).name for f in files)})")

    # Write summary index
    index_file = OUTPUT_DIR / "index.csv"
    with open(index_file, "w", newline="") as f:
        writer = csv.writer(f)
        writer.writerow(["batch", "num_functions", "total_lines", "files"])
        for i, batch in enumerate(batches, 1):
            total_lines = sum(func["size"] for func in batch)
            files = ";".join(sorted(set(func["file"] for func in batch)))
            writer.writerow([f"batch_{i:03d}.csv", len(batch), total_lines, files])

    print(f"\nWrote index to {index_file}")
    print(f"Batches written to {OUTPUT_DIR}/")


if __name__ == "__main__":
    main()
