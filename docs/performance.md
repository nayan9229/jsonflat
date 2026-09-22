---
title: Performance
nav_order: 5
---

# Performance
{: .no_toc }

<details open markdown="block">
  <summary>Contents</summary>
  {: .text-delta }
- TOC
{:toc}
</details>

## Benchmarks

Measured on an Apple M5 (10 cores), macOS, `darwin/arm64`, Go 1.26.4, fastjson
v1.6.10, with `make bench` (`go test -run xxx -bench . -benchmem ./...`).
Single run; medians over ten runs are within 3 % of these.

| Benchmark | What it does | Time | Throughput | Memory |
|---|---|---|---|---|
| `BenchmarkAppend` | 650-byte record; rename, two merges, drop, default | 1.88 µs/op | 346 MB/s | 0 B/op, 0 allocs/op |
| `BenchmarkAppendFlattenOnly` | same record, `{}` config | 1.64 µs/op | 397 MB/s | 0 B/op, 0 allocs/op |
| `BenchmarkAppendParallel` | `BenchmarkAppend` on 10 goroutines | 0.32 µs/op | 2053 MB/s | 0 B/op, 0 allocs/op |
| `BenchmarkBaselineStdlib` | `encoding/json` into `map[string]any`, flatten, marshal | 10.21 µs/op | 64 MB/s | 12.9 KB/op, 242 allocs/op |
| `BenchmarkRudderEach` | RudderStack preset, 1.8 KB batch of 3 events → 5 rows | 7.80 µs/op | 234 MB/s | 0 B/op, 0 allocs/op |

The release workflow runs the same command and refuses a release in which any
`BenchmarkAppend*` or `BenchmarkRudderEach` line reports anything but
`0 allocs/op` (`scripts/check-allocs.sh`). Numbers on your hardware:

```
make bench            # or: go test -run xxx -bench . -benchmem ./...
```

The generic engine has a price. While this package was designed, a
purpose-built RudderStack transformer was measured against the config-driven
engine on the same batch (on different hardware from the table above), and the
generic engine took roughly 40 % more CPU, still with zero allocations. That is
the cost of vendor layouts being configs instead of code.

## What "zero allocations" means

`Append` and `Each` do not allocate on the heap in the steady state. Three tests
enforce it with `testing.AllocsPerRun`: `TestZeroAllocs` (`Append` with rules),
`TestRudderZeroAllocs` (`Each` with the preset) and `TestKeepZeroAllocs`
(`Append` with `keep` and `drop`).

"Steady state" means: the pooled per-call state exists and its buffers have
grown to the size of your traffic. The first call after a garbage-collection
cycle may allocate once while the `sync.Pool` refills. `Compile`, `New` and
error paths allocate; a timestamp with a numeric zone offset such as `+05:30`
can make `time.Parse` allocate once inside `clock_skew` (`Z` timestamps do
not).

How it is done, for the curious (all in `transform.go`):

- one pooled `state` per call holds the parser and every scratch buffer;
- output is appended into a reused buffer; `Append` writes straight into the
  caller's `dst`;
- method values used as callbacks are created once, in the state constructor;
- map lookups use `m[string(b)]`, which the compiler does not allocate for;
- paths are split at compile time; the walk reslices buffers to `[:0]`;
- conditions are evaluated once per record and cached;
- when a record matches two outputs and the second contains everything the
  first does, the second row starts as a copy of the first and only the new
  sections are walked.

Use `Append` with a buffer you keep between calls, or `Each`, and the callback
must copy anything it keeps: the `Record` and its slices belong to the pooled
state.

## Memory

- Memory per pooled state is proportional to the largest document it has
  parsed, because fastjson keeps its value cache. State from an input above
  4 MiB is not returned to the pool, so an occasional huge document does not
  pin memory for good (`maxPooledInput`). *`TestLargeInput`*
- Put a size limit on request bodies in front of the library.
- The parser rejects JSON nested deeper than 300 levels.
- Collision tracking (`on_collision: first` or `error`) keeps a set of 64-bit
  FNV-1a hashes per row, sized to the row, in the pooled state.

## When not to use this

- Documents above a few MB: the whole document is parsed into memory.
- Streaming from an `io.Reader`: the API takes a complete `[]byte`. Read
  line-delimited input with `bufio.Scanner` and call `Each` per line, as
  `example/main.go` does; it reaches about 250 MB/s on one goroutine including
  gzip decompression on the hardware above.
- You need to keep the nesting: this package only flattens.
- You need type casting today: values keep the type they came with. Casting
  and a schema mode are on the roadmap.
