#!/bin/sh
# Shared runner for the scripts/debug/*.sh suite. Each wrapper sets one
# or more TINYEMU_* env vars, then sources this script.
#
# Environment in:
#   DEBUG_NAME    -- short name; used to derive the log path. Required.
#   DEBUG_TARGET  -- run64_iso.sh target (alpine, tinycore, alpine-debug...).
#                    Defaults to $1 or "alpine".
#   DEBUG_BUDGET  -- wall-clock timeout in seconds. Defaults to $2 or 480.
#   DEBUG_VARIANT -- run64_iso.sh variant (bare, fast, ...). Defaults to "".
#   DEBUG_OUT     -- full path to log file. Defaults to /tmp/debug_$DEBUG_NAME.log.
#
# What it does:
#   1. Captures stdout+stderr from one Alpine (or other) boot to $DEBUG_OUT.
#   2. Prints a compact summary (wall time, last kernel timestamp, tail).
#   3. Greps for panic/oops markers so failures are loud.

set -u

if [ -z "${DEBUG_NAME:-}" ]; then
  echo "debug runner: DEBUG_NAME not set (call from a scripts/debug/<name>.sh wrapper)" >&2
  exit 2
fi

TARGET=${DEBUG_TARGET:-${1:-alpine}}
BUDGET=${DEBUG_BUDGET:-${2:-480}}
VARIANT=${DEBUG_VARIANT:-}
OUT=${DEBUG_OUT:-/tmp/debug_${DEBUG_NAME}.log}

ROOT=$(cd "$(dirname "$0")/../.." && pwd)
cd "$ROOT"

# Show which env vars the wrapper set, so the log itself records the
# debug context.
printf '[debug_%s] env:' "$DEBUG_NAME"
env | grep -E '^TINYEMU_' | sort | sed 's/^/  /'
printf '[debug_%s] target=%s variant=%s budget=%ss out=%s\n' \
  "$DEBUG_NAME" "$TARGET" "${VARIANT:-<none>}" "$BUDGET" "$OUT"

T0=$(date +%s)
timeout "$BUDGET" ./run64_iso.sh "$TARGET" $VARIANT > "$OUT" 2>&1
RC=$?
WALL=$(($(date +%s) - T0))

LAST_TS=$(grep -oE '^\[ *[0-9]+\.[0-9]+\]' "$OUT" | tail -1)
LINES=$(wc -l < "$OUT" | tr -d ' ')

printf '\n[debug_%s] done rc=%d wall=%ss lines=%s last_kernel_ts=%s\n' \
  "$DEBUG_NAME" "$RC" "$WALL" "$LINES" "${LAST_TS:-none}"

# Any kernel-level fatals?
if grep -qE 'panic|Oops:|Unable to handle|BUG: ' "$OUT"; then
  printf '[debug_%s] WARNING: panic/oops detected:\n' "$DEBUG_NAME"
  grep -nE 'panic|Oops:|Unable to handle|BUG: ' "$OUT" | head -5
fi

printf '\n--- last 25 lines of %s ---\n' "$OUT"
tail -25 "$OUT"
