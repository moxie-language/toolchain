#!/usr/bin/env bash
# Builds and runs the slice and its wake benchmark, and prints the host clock.
# The numbers are only meaningful next to the clock; a 4x4 node at 50 MHz is a
# different machine, and the per-message cost is what has to be compared.
set -uo pipefail
cd "$(dirname "$0")"
ROOT="$(cd ../.. && pwd)"
MOXIE="${MOXIE:-$ROOT/moxie}"
OUT="${TMPDIR:-/tmp}/moxie-slice-$$"
mkdir -p "$OUT"
trap 'rm -rf "$OUT"' EXIT

echo "# host: $(grep -m1 'model name' /proc/cpuinfo | cut -d: -f2- | xargs), $(grep -m1 'cpu MHz' /proc/cpuinfo | cut -d: -f2- | xargs) MHz"
"$MOXIE" build -o "$OUT/waitbench" ./waitbench || exit 1
"$MOXIE" build -o "$OUT/slice" . || exit 1
"$OUT/waitbench"
"$OUT/slice"
