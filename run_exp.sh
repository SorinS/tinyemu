#!/usr/bin/env bash
#
# run_exp.sh — convenience wrapper for the x86 boot experiment in
# bin/boot_experiment.go. Compiled in only with the `boot_experiment` build
# tag so it's excluded from `go test ./...`.
#
# Usage:
#   ./run_exp.sh                       # default: yocto preset, no debug
#   ./run_exp.sh alpine                # alpine preset
#   ./run_exp.sh yocto pf              # yocto + page-fault tracing
#   ./run_exp.sh yocto int             # yocto + interrupt tracing
#   ./run_exp.sh yocto pf int io       # yocto + all traces
#   ./run_exp.sh --help
#
# Output:
#   stdout      → live UART output from the guest
#   stderr      → periodic CPU/PIC state + diagnostic traces
#   /tmp/boot_exp_stdout.log, /tmp/boot_exp_stderr.log → tee'd copies
#
set -euo pipefail

cd "$(dirname "$0")"

print_help() {
    cat <<'EOF'
run_exp.sh — x86 boot experiment runner.

PRESETS (first positional arg):
    yocto    Yocto qemux86 bzImage with earlyprintk. Default.
    alpine   Alpine 3.19 linux-lts + initramfs.

DEBUG FLAGS (any subsequent positional args):
    pf       Page-fault tracing (TINYEMU_X86_PF_DEBUG=1)
    int      Interrupt/exception tracing (TINYEMU_X86_INT_DEBUG=1)
    io       Port I/O tracing (TINYEMU_X86_IO_DEBUG=1, very noisy)

EXAMPLES:
    ./run_exp.sh
    ./run_exp.sh alpine
    ./run_exp.sh yocto pf int
    ./run_exp.sh alpine io | grep '\[io\] out 0x21'

Logs are also captured to /tmp/boot_exp_stdout.log and /tmp/boot_exp_stderr.log
so you can grep them after the run finishes.
EOF
}

case "${1:-}" in
    -h|--help|help)
        print_help
        exit 0
        ;;
esac

PRESET="${1:-yocto}"
shift || true

env_vars=()
for flag in "$@"; do
    case "$flag" in
        pf)  env_vars+=("TINYEMU_X86_PF_DEBUG=1") ;;
        int) env_vars+=("TINYEMU_X86_INT_DEBUG=1") ;;
        io)  env_vars+=("TINYEMU_X86_IO_DEBUG=1") ;;
        *)
            echo "unknown flag: $flag" >&2
            print_help
            exit 2
            ;;
    esac
done

mkdir -p /tmp
STDOUT_LOG=/tmp/boot_exp_stdout.log
STDERR_LOG=/tmp/boot_exp_stderr.log
: > "$STDOUT_LOG"
: > "$STDERR_LOG"

echo "[run_exp] preset=$PRESET  flags=${env_vars[*]:-(none)}" >&2
echo "[run_exp] tee stdout → $STDOUT_LOG"  >&2
echo "[run_exp] tee stderr → $STDERR_LOG"  >&2
echo >&2

if [ "${#env_vars[@]}" -gt 0 ]; then
    env "${env_vars[@]}" go run -tags boot_experiment ./bin/boot_experiment.go "$PRESET" \
        > >(tee "$STDOUT_LOG") \
        2> >(tee "$STDERR_LOG" >&2)
else
    go run -tags boot_experiment ./bin/boot_experiment.go "$PRESET" \
        > >(tee "$STDOUT_LOG") \
        2> >(tee "$STDERR_LOG" >&2)
fi
