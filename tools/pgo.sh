#!/usr/bin/env bash
# With arguments, merge CPU profiles captured from representative server loads.
# Without arguments, profile the existing generation and mob-tick benchmarks.
set -euo pipefail
cd "$(dirname "$0")/.."

scratch=$(mktemp -d)
trap 'rm -rf "$scratch"' EXIT
if (($# == 0)); then
  go test -pgo=off ./internal/server/world -run '^$' -bench '^BenchmarkOverworldGenerate$' \
    -benchtime=30s -cpuprofile="$scratch/generation.pprof" -o "$scratch/world.test"
  go test -pgo=off ./internal/server -run '^$' -bench '^BenchmarkTickMobs$' \
    -benchtime=30s -cpuprofile="$scratch/tick.pprof" -o "$scratch/server.test"
  set -- "$scratch/generation.pprof" "$scratch/tick.pprof"
fi

# A failed capture or merge must leave the previous build input intact.
# pprof can exit successfully after loading only some of its inputs.
for profile in "$@"; do
  go tool pprof -proto "$profile" > /dev/null
done
go tool pprof -proto "$@" > "$scratch/default.pgo"
test -s "$scratch/default.pgo"
mv "$scratch/default.pgo" default.pgo
