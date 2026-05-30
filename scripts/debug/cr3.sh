#!/bin/sh
# TINYEMU_X64_CR3=1 — logs every CR3 write with the resulting
# PML4[0]/[273]/[511] (identity / direct-map / kernel-text). Diagnostic
# for "missing PML4 entry at PF" / page-table-related boot stalls.
export TINYEMU_X64_CR3=1
DEBUG_NAME=cr3
. "$(dirname "$0")/_runner.sh" "$@"
