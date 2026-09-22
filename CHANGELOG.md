# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

<!--
Release procedure (details in RELEASING.md):
1. Move the entries under [Unreleased] to a new heading
   `## [X.Y.Z] - YYYY-MM-DD` with today's date, and add the compare links at
   the bottom.
2. Commit to main, then tag and push:
     git tag vX.Y.Z && git push origin vX.Y.Z
   The release workflow refuses a tag without a matching changelog section.
-->

## [Unreleased]

## [0.2.0] - 2026-09-22

### Added

- `max_pooled_input`: the largest input whose per-call state is kept for
  reuse; the default is 4 MiB, as before. A kept state that has seen one
  large document is now dropped after 64 consecutive inputs below an eighth
  of that size, so one burst of large documents no longer pins memory for
  good.
- Tests and benchmarks behind the concurrency and memory claims:
  `TestSharedTransformerMixed` (16 goroutines mixing `Append`, `Each` and
  `Options` on shared Transformers, under the race detector),
  `TestRetainedRecordIsInvalid`, `BenchmarkEachParallel`,
  `BenchmarkVarsShared`/`BenchmarkVarsPerCall`, and the report tests
  `TestColdPoolReport`, `TestBurstReport`, `TestMemoryReport`,
  `TestKeySetReport`. `Example_lazyCompile` shows `sync.OnceValues`.
- `make profile`: CPU, memory, mutex and block profiles of the hot
  benchmarks, top 20 of each.

### Changed

- Key lookups skip the hash when no entry has the key's length. `Append` is
  16 % faster and the RudderStack preset 14 % faster on the benchmarks, still
  at 0 allocs/op.

### Fixed

- Docs: zero allocations is reached on the third call of a fresh pooled
  state, not the first. The performance page now carries measured numbers
  for scaling across CPUs, the cost of an empty pool, and the memory one
  state holds, including the 8× ratio to the largest input.

## [0.1.0] - 2026-09-22

### Added

- `jsonflat` package: `Compile`, `MustCompile`, `New`, and a `Transformer` with
  `Append`, `Transform` and `Each`.
- Config sections `input` (explode, inherit), `flatten` (separator, arrays,
  max_depth, drop_nulls, drop_empty_objects), `keys` (snake_case normalisation,
  digit prefix, collision policy, segment aliases, aliases, keep, drop),
  `derive`, `columns`, `sections`, `rules` (rename, drop, merge, default),
  `outputs`, `expose` and `newline`.
- `keys.keep`: an output-key whitelist next to `keys.drop`. A flattened key is
  written only if it matches `keep` (when set) and does not match `drop`.
- `keys.rest`: a column that collects the keys `keep` left out, as a JSON
  string of one flat object, so a whitelist loses nothing.
- Sources with fallbacks: paths, `$now`, derived values, `Options.Vars`, and
  the `clock_skew` function.
- Zero heap allocations in `Append` and `Each` in the steady state, guarded by
  tests.
- Output that is always valid JSON: an RFC 8259 string escaper and number check
  in `internal/jsonenc`, because the parser's own serialiser and number parsing
  are more lenient than the standard.
- `presets` package with the RudderStack warehouse-schema preset.
- `example/`: a command that flattens an NDJSON event export (gzip or plain)
  into one flat row per line, merging duplicate keys and duplicate fields
  through its config, with an optional independent check of every row.
- Fuzz targets `FuzzAppend` and `FuzzEach`, benchmarks, and examples.

[Unreleased]: https://github.com/nayan9229/jsonflat/compare/v0.2.0...HEAD
[0.2.0]: https://github.com/nayan9229/jsonflat/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/nayan9229/jsonflat/releases/tag/v0.1.0
