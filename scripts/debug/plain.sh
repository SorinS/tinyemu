#!/bin/sh
# Plain boot — no debug env vars. The baseline measurement; everything
# else compares against this. Usage:
#   ./scripts/debug/plain.sh                       # alpine, 480s
#   ./scripts/debug/plain.sh tinycore              # tinycore, 480s
#   ./scripts/debug/plain.sh alpine 900            # alpine, 15 min
DEBUG_NAME=plain
. "$(dirname "$0")/_runner.sh" "$@"
