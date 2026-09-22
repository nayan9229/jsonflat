#!/usr/bin/env bash
# check-allocs.sh [bench.txt]
#
# Runs the benchmarks and fails unless every Append and RudderEach benchmark
# reports 0 allocs/op. Zero allocation on the hot path is the library's main
# promise, so a regression must not reach a release.
set -euo pipefail

out=${1:-bench.txt}

go test -run xxx -bench . -benchmem ./... | tee "$out"

awk '
	/^Benchmark(Append|RudderEach)/ && $NF == "allocs/op" {
		seen++
		if ($(NF-1) != "0") { bad++; print "::error::allocates: " $0 }
	}
	END {
		if (seen == 0) { print "::error::no Append or RudderEach benchmark ran"; exit 1 }
		if (bad) exit 1
		print "ok: " seen " hot-path benchmarks at 0 allocs/op"
	}
' "$out"
