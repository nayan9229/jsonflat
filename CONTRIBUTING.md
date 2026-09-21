# Contributing

Thanks for helping. Bug reports, test vectors and presets are all welcome.

## Before you open a pull request

Everything below must pass. CI runs the same commands.

```
gofmt -l .                                   # prints nothing
go vet ./...
go test -count=1 ./...
go test -count=1 -race ./...
go test -run xxx -bench . -benchmem .        # 0 allocs/op for Append*, RudderEach
go test -run xxx -fuzz 'FuzzAppend$' -fuzztime 60s .
go test -run xxx -fuzz 'FuzzEach$'   -fuzztime 60s .
```

## Rules of the house

- **The hot path does not allocate.** `TestZeroAllocs` and
  `TestRudderZeroAllocs` enforce it. If one fails, find the allocation with
  `go test -run TestZeroAllocs -memprofile mem.out -memprofilerate 1` or
  `go build -gcflags=-m`. Do not loosen the test.
- **The output is always valid JSON.** Strings never go through fastjson's
  `MarshalTo`, and every number written is checked with `validNumber`.
- **No vendor-specific code in the package.** Vendor layouts are presets. If a
  preset needs something the config cannot express, propose a general feature.
- **No new dependencies**, no `init` functions, no global mutable state.
- Every new config field needs a compile-time validation test.
- Keep fastjson calls inside `transform.go` (plus `fastjson.ValidateBytes` in
  `config.go`), so the parser can be swapped later.
- If a fuzz target finds a failure, commit the crasher that Go writes under
  `testdata/fuzz/` as a regression case together with the fix.
- Doc comments on every exported identifier. Comments explain why, not what.
- The acceptance tests (`transform_test.go`, `features_test.go`,
  `preset_test.go`) are the contract. If you believe a vector is wrong, open an
  issue and say why instead of editing it.

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
4. Document it in `presets/README.md`: what each record type becomes, which
   rules come from the vendor's docs (with the link), which choices are yours,
   and what is not covered.
5. If the layout cannot be expressed, do not work around it in Go. Describe the
   missing general feature in the issue.

## Reporting security issues

See [SECURITY.md](SECURITY.md).
