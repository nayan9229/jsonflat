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

[Unreleased]: https://github.com/nayan9229/jsonflat/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/nayan9229/jsonflat/releases/tag/v0.1.0
