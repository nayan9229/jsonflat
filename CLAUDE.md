# jsonflat

Config-driven JSON flattening for Go. One general package plus `presets/`
(configs only). Parser: `github.com/valyala/fastjson` >= v1.6.10, the only
dependency.

## Invariants. If a task seems to need breaking one, stop and ask

1. **Zero heap allocations in `Append` and `Each` in the steady state.**
   `TestZeroAllocs` and `TestRudderZeroAllocs` enforce it. `Compile`, `New` and
   error paths may allocate. Techniques in use: per-call state in a `sync.Pool`
   owned by the `Transformer`; append-style output; method values (`s.visit`)
   created once in `newState`; `m[string(b)]` lookups; paths split at compile
   time and passed to `Get(keys...)`; buffers resliced to `[:0]`;
   `unsafe.String`/`unsafe.Slice` only for read-only views within one call;
   `strMap` (a length bitmask in front of each small map) so that most key
   lookups never hash.
2. **Never use `MarshalTo` for strings or anything that can contain a string.**
   fastjson falls back to `strconv.AppendQuote`, which emits Go escapes that
   are not JSON. Use `appendQuoted` / `appendEscaped` / `appendRaw`.
   `MarshalTo` is allowed only for numbers, `true`, `false`, `null`, and each
   call carries a comment saying so.
3. **Validate every number written** with `validNumber`. Number text is copied
   unchanged to keep precision, and the parser accepts `00`, `+1`, `NaN`.
4. **Output is always valid JSON.** Both fuzz targets assert `json.Valid`.
5. **Output never aliases the input.** A `Record` is valid only until the
   callback returns; the docs say so.
6. **A `Transformer` is immutable after compile** and safe for concurrent use.
7. **On error, `Append` returns `dst` unchanged.**
8. **Column names are reserved whether or not the column had a value.**
9. **One bad record never fails the batch, and a record never arrives in
   part.** Build every row first; on failure deliver one `Record` with `Err`.
10. The zero-alloc tests skip under the race detector (`sync.Pool` drops items
    on purpose there). Keep the skip.
11. **Never edit the acceptance tests** (`transform_test.go`,
    `features_test.go`, `preset_test.go`, `bench_test.go`, examples) to make
    them pass. If a vector looks wrong, stop and say why.
12. State from an input above `max_pooled_input` (default 4 MiB) is not
    returned to the pool, and a state is dropped after 64 consecutive inputs
    below an eighth of its peak (`worthPooling`). A pooled state holds about
    8× its largest input, once per active P; `TestMemoryReport` measures it.
13. **No vendor-specific code in the package.** Vendor layouts are presets; a
    missing capability becomes a general config feature.

Verbatim files, do not reformat: `internal/jsonenc/jsonenc.go`, `names.go`,
the type declarations at the top of `config.go` (extended by
`KeysConfig.Keep`, `KeysConfig.Rest` and `Config.MaxPooledInput`),
`presets/presets.go`, `presets/rudderstack.json`.

## Working rules

- Zero-alloc test fails: find the allocation with
  `go test -run TestZeroAllocs -memprofile mem.out -memprofilerate 1` or
  `go build -gcflags=-m`. Do not loosen the test.
- A fuzz failure: keep the crasher under `testdata/fuzz/` and commit it.
- Doc comments on every exported identifier. Comments explain why, not what.
- Keep every fastjson call inside `transform.go` (and
  `fastjson.ValidateBytes` in `config.go`), so the parser can be swapped.
- No dependencies beyond fastjson. No `init` functions. No global mutable
  state.
- Every new config field needs a compile-time validation test.
- When RudderStack's or AWS's behaviour matters, check their current docs and
  cite the page in a comment.
- Two kinds of path: source paths always use "."; output keys are used as
  given and joined with `flatten.separator`.
- Every documented behaviour has a worked example in `docs/` or `README.md`;
  `docs_test.go` compiles every ```json block and replays every ```example
  block, so update the docs with the code.
- Do not tag releases; RELEASING.md is the maintainer's procedure.
- A performance change needs a `benchstat -count=10` table in the commit
  message and at least 5 % on a hot benchmark, unless it also simplifies the
  code. The numbers in `docs/performance.md` come from the report tests
  (`go test -run 'Report|Reuse|Shrinks' -v .`), the `-cpu` sweep of the
  parallel benchmarks and `make profile`, and name the CPU, Go and fastjson
  versions they were measured with.

## Commands that must all pass

`make all` runs everything below with the same pinned tools as CI.

```
gofmt -l .                                   # prints nothing
go vet ./...
go test -count=1 ./...
go test -count=1 -race ./...
go test -run xxx -bench . -benchmem .        # 0 allocs/op for Append*, RudderEach, EachParallel
go test -run xxx -fuzz 'FuzzAppend$' -fuzztime 60s .
go test -run xxx -fuzz 'FuzzEach$'   -fuzztime 60s .
grep -n "MarshalTo" *.go                     # numbers, true, false, null only
grep -rni "rudder" --include=*.go . | grep -v _test.go | grep -v presets/   # nothing
```
