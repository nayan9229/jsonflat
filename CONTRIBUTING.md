# Contributing

Thanks for helping. Bug reports, test vectors, docs fixes and presets are all
welcome. Please use the issue templates; for a vulnerability see
[SECURITY.md](SECURITY.md) instead.

## Running the checks locally

Everything CI runs is in the `Makefile`, with the same pinned tool versions:

```
make lint    # gofmt, go vet, tidy go.mod, staticcheck, govulncheck, actionlint, doc comments
make test    # unit, acceptance and example tests
make race    # the same under the race detector
make bench   # benchmarks; fails unless the hot paths report 0 allocs/op
make fuzz    # both fuzz targets, 60 s each (FUZZTIME=10s make fuzz for a quick run)
make docs    # regenerate derived docs and replay every example in docs/ and README.md
make all     # all of the above
```

The tools run through `go run …@version`, so nothing needs installing beyond
Go 1.24 or newer. CI does not run on pushes or pull requests; a maintainer
starts the CI workflow by hand before a release, and the release run repeats
every check. So a green `make all` on your branch is what the review relies
on. (One difference: the runners have shellcheck, which actionlint uses on
`run:` scripts in the workflows.)

## Pull requests

- Keep one change per pull request, with a short description of what and why.
- Add a test for new behaviour. A new config field needs a compile-time
  validation test as well, an entry in `docs/config-reference.md`, and a
  worked example (the docs test replays it).
- Add a line under `[Unreleased]` in `CHANGELOG.md`.
- The template's checklist is what the reviewer looks for.

## Rules of the house

- **The hot path does not allocate.** `TestZeroAllocs`, `TestRudderZeroAllocs`
  and `TestKeepZeroAllocs` enforce it, and the release gate reads the
  benchmarks. If one fails, find the allocation with
  `go test -run TestZeroAllocs -memprofile mem.out -memprofilerate 1` or
  `go build -gcflags=-m`. Do not loosen the test.
- **The output is always valid JSON.** Strings never go through fastjson's
  `MarshalTo`, and every number written is checked with `validNumber`.
- **No vendor-specific code in the package.** Vendor layouts are presets. If a
  preset needs something the config cannot express, propose a general feature.
- **No new dependencies**, no `init` functions, no global mutable state.
- Keep fastjson calls inside `transform.go` (plus `fastjson.ValidateBytes` in
  `config.go`), so the parser can be swapped later.
- If a fuzz target finds a failure, commit the crasher that Go writes under
  `testdata/fuzz/` as a regression case together with the fix.
- Doc comments on every exported identifier, starting with its name;
  `make lint` checks this. Comments explain why, not what.
- The acceptance tests (`transform_test.go`, `features_test.go`,
  `preset_test.go`, `bench_test.go`, `example_test.go`,
  `example_preset_test.go`) are the contract. If you believe a vector is wrong,
  open an issue and say why instead of editing it.

## How to propose a preset

A preset is a config file, not code.

1. Open an issue first with a link to the vendor's published schema, so the
   target layout is not a matter of opinion.
2. Add `presets/<vendor>.json` and an embedded variable for it in
   `presets/presets.go`, with a doc comment that says which `Options.Vars` it
   expects and what `Record.Fields` holds.
3. Add a byte-for-byte test in the style of `preset_test.go`: a realistic
   payload, the exact expected rows, the error cases, a zero-allocation guard
   and a fuzz seed.
4. Document it in `docs/presets.md`: what each record type becomes, which
   rules come from the vendor's docs (with the link), which choices are yours,
   and what is not covered.
5. If the layout cannot be expressed, do not work around it in Go. Describe the
   missing general feature in the issue.

## Releases

Maintainers only; see [RELEASING.md](RELEASING.md).
