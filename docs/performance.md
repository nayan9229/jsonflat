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

Every number on this page was measured on an Apple M5 (4 performance and
6 efficiency cores, `GOMAXPROCS=10`), macOS, `darwin/arm64`, Go 1.26.4,
fastjson v1.6.10. Times are medians of ten runs (`benchstat -count=10`);
the spread is within 1 %. `make bench` gives the numbers for your hardware,
`make profile` the CPU, memory, mutex and block profiles, and the report
tests below print the memory figures: `go test -run 'Report|Reuse|Shrinks'
-v .`

## Benchmarks

| Benchmark | What it does | Time | Throughput | Memory |
|---|---|---|---|---|
| `BenchmarkAppend` | 650-byte record; rename, two merges, drop, default | 1.70 µs/op | 384 MB/s | 0 B/op, 0 allocs/op |
| `BenchmarkAppendFlattenOnly` | same record, `{}` config | 1.73 µs/op | 376 MB/s | 0 B/op, 0 allocs/op |
| `BenchmarkBaselineStdlib` | `encoding/json` into `map[string]any`, flatten, marshal | 10.6 µs/op | 61 MB/s | 12.9 KB/op, 242 allocs/op |
| `BenchmarkRudderEach` | RudderStack preset, 1.8 KB batch of 3 events → 5 rows | 6.97 µs/op | 262 MB/s | 0 B/op, 0 allocs/op |

The release workflow runs the same benchmarks and refuses a release in which
any `BenchmarkAppend*`, `BenchmarkRudderEach` or `BenchmarkEachParallel` line
reports anything but `0 allocs/op` (`scripts/check-allocs.sh`).

Where the time goes (`make profile`, top of the CPU profile by flat time):
parsing in fastjson about 20 %, copying bytes into the output 9 %, the JSON
string escaper 8 %, map lookups for rules, aliases and column names 8 %,
and for the preset `snake_case` normalisation 6 % and the two `time.Parse`
calls of `clock_skew` 7 %. The generic engine has a price: a purpose-built
RudderStack transformer measured against the config-driven engine on the
same batch took roughly 40 % less CPU, still with zero allocations. That is
the cost of vendor layouts being configs instead of code.

## Concurrency

One `Transformer` is shared by every goroutine. It is read-only after
`Compile` or `New`; all per-call state lives in a `sync.Pool` the
Transformer owns, so the hot path takes no lock. `TestSharedTransformerMixed`
runs 16 goroutines that mix `Append` and `Each` with different `Options` on
three shared Transformers under the race detector and compares every result
with a sequential run.

The `-cpu` sweep of the two parallel benchmarks, in ns/op:

| CPUs | `BenchmarkAppendParallel` | speed-up | `BenchmarkEachParallel` | speed-up |
|---|---|---|---|---|
| 1 | 1692 | 1.0× | 6974 | 1.0× |
| 2 | 882 | 1.9× | 3646 | 1.9× |
| 4 | 436 | 3.9× | 1807 | 3.9× |
| 8 | 299 | 5.7× | 1207 | 5.8× |
| 10 | 276 | 6.1× | 1134 | 6.1× |
| 16 | 290 | 5.8× | 1164 | 6.0× |

Scaling is linear on the four performance cores; the six efficiency cores
add less, and 16 goroutines on 10 cores cost nothing extra. In the mutex
profiles of both parallel benchmarks (`make profile`, 3 s runs) no function
of this package has contention time of its own; the one lock the package
touches is `sync.Pool`'s own, taken on the first call per P after a GC, at
1 µs in total. The rest is the scheduler and the garbage collector
(`runtime.unlock` under `findRunnable` and `gcMarkDone`, under 1 ms). The
block profiles hold only the benchmark harness (`WaitGroup.Wait`).

No `sync.Once` is needed anywhere: compiling is the explicit step you do
once and store. A package-level `var t = jsonflat.MustCompile(...)` runs at
init, which is once-only and goroutine-safe. To compile lazily, wrap the
call in `sync.OnceValues` in your own code (`Example_lazyCompile`).

## What "zero allocations" means

`Append` and `Each` do not allocate on the heap in the steady state. Three
tests enforce it with `testing.AllocsPerRun`: `TestZeroAllocs`,
`TestRudderZeroAllocs` and `TestKeepZeroAllocs`.

Steady state is reached on the **third** call of a pooled state
(`TestColdPoolReport`):

| Call on a fresh state | `Append`, rules config, 650 B record | `Each`, preset, 1.8 KB batch |
|---|---|---|
| 1: the parser and the buffers are built | 65 allocs, 17.4 KB | 129 allocs, 42.5 KB |
| 2: the parser finishes sizing its value cache | 4 allocs, 528 B | 11 allocs, 1.2 KB |
| 3 and later | 0 | 0 |

When is a state built? `sync.Pool` keeps one state per P that has made a
call. A garbage collection moves pooled states to a victim cache that only
the same P sees, so after a GC cycle each active P typically rebuilds one
state on its next call. A burst of goroutines on an empty pool builds about
as many states as there are calls in flight at once: on ten cores, 10
goroutines built 7 to 9, 64 built 11 to 12 and 256 built 21 to 40 across
runs (`TestBurstReport`). That is the whole GC-related cost of the library:
tens of kilobytes per active P per GC cycle.

`Compile`, `New` and error paths allocate. A timestamp with a numeric zone
offset such as `+05:30` can make `time.Parse` allocate once inside
`clock_skew`; `Z` timestamps do not.

## Memory

A pooled state holds the parser's value cache, a copy of the input, and
the scratch and output buffers, all sized to the largest document that
state has seen (`TestMemoryReport`):

| Traffic | Bytes held by one state |
|---|---|
| `{}` config, 650 B records | 9.7 KB |
| RudderStack preset, 1.8 KB batches | 23.7 KB |
| `{}` config, one 3 MiB batch | 33 MB (parser 25.8 MB, `Each` buffer 6.6 MB) |
| preset, one 3 MiB batch, then 1.8 KB batches | 26.4 MB, unchanged by the small batches |
| preset, one 3 MiB batch, then 65 small batches | 23 KB: the state was dropped and replaced (`TestPoolShrinks`) |

Rule of thumb: the parser keeps about 8× the input size, and `Each` keeps
up to 2× the output. Two rules keep that in check:

- **`max_pooled_input`** (default 4 MiB): state from a larger input is not
  returned to the pool at all (`TestLargeInput`, `TestMaxPooledInput`). The
  worst case is therefore about 8× this value per active P, pinned until the
  next GC drops the pool. Lower it if memory matters more than the
  occasional rebuild, and put a size limit on request bodies in front of the
  library either way.
- **Shrinking:** a state that has seen one large document and then 64
  inputs in a row below an eighth of that size is dropped instead of pooled,
  and the next call builds one sized to the current traffic
  (`TestWorthPooling`, `TestPoolShrinks`).

Also:

- Collision tracking (`on_collision: first` or `error`) keeps a set of
  64-bit FNV-1a hashes per row. It grows to the widest row seen and never
  shrinks: a 5,000-key row costs 256 KiB per state (`TestKeySetReport`).
  With the default `keep` policy the set is not used.
- The `Each` buffer is reused across calls and is not reallocated in the
  steady state (`TestEachBufferReuse`).
- Pooled buffers keep the bytes of the last document until they are
  overwritten. If that matters for your data, do not count on the pool to
  erase anything.
- The parser rejects JSON nested deeper than 300 levels.

## GC pressure comes from the caller

Since the hot path allocates nothing, garbage comes from what you do with
the rows. For a Kinesis or Firehose producer, append `r.JSON` into a batch
buffer you keep between records and hand the batch to the SDK; never
`string(r.JSON)` per row:

```go
batch = batch[:0]
err := t.Each(body, opt, func(r *jsonflat.Record) error {
    if r.Err != nil {
        return deadLetter(r) // r.Fields carries the IDs
    }
    batch = append(batch, r.JSON...) // one copy, into memory you own
    return nil
})
```

`Options.Vars` is only read, so keep one map per route. Building the map
per call costs 2 allocations and 336 B, and 10 % of a single-event preset
call: 815 ns shared against 895 ns per call (`BenchmarkVarsShared`,
`BenchmarkVarsPerCall`).

`GOGC` and `GOMEMLIMIT`: the library has no preference. A low `GOGC` empties
the pool more often, which costs the one-off state builds above (tens of
kilobytes per active P per cycle), not per-call allocations. A `GOMEMLIMIT`
should leave room for one state per P sized to your largest document, per
the table.

## When not to use this

- Documents above a few MB: the whole document is parsed into memory, and
  a pooled state keeps about 8× that size until the shrink rule or a GC
  drops it.
- Streaming from an `io.Reader`: the API takes a complete `[]byte`. Read
  line-delimited input with `bufio.Scanner` and call `Each` per line, as
  `example/main.go` does.
- You need to keep the nesting: this package only flattens.
- You need type casting today: values keep the type they came with. Casting
  and a schema mode are on the roadmap.
