<!--
Repository settings (set by hand in GitHub → About):

Description:
  Zero-allocation JSON flattening and transformation for Go, driven by a JSON config: flatten, snake_case and fix keys, rename, drop, merge, whitelist, split batches, route to outputs. Ships a RudderStack warehouse-schema preset for Kinesis, Parquet and Athena.

Topics:
  go golang json json-flatten flatten-json json-transform json-mapping config-driven
  zero-allocation high-performance fastjson etl data-pipeline event-streaming
  rudderstack cdp kinesis parquet athena snake-case

Website: https://nayan9229.github.io/jsonflat/
-->

# jsonflat: zero-allocation JSON flattening and transformation for Go

`jsonflat` turns nested JSON into flat rows, driven entirely by a JSON config,
without allocating on the hot path. Flatten, normalise keys to snake_case, fix
misspelt keys, rename, drop, merge and default, whitelist the columns you want,
split a batch into records, and route each record to one or more named
outputs. It is built for high-throughput pipelines that feed columnar storage.
Vendor layouts are configs, not code: the bundled RudderStack preset produces
rows that follow the RudderStack warehouse schema, ready for Kinesis, Firehose,
Parquet and Athena.

[![Go Reference](https://pkg.go.dev/badge/github.com/nayan9229/jsonflat.svg)](https://pkg.go.dev/github.com/nayan9229/jsonflat)
[![Release](https://github.com/nayan9229/jsonflat/actions/workflows/release.yml/badge.svg)](https://github.com/nayan9229/jsonflat/actions/workflows/release.yml)
[![CI](https://github.com/nayan9229/jsonflat/actions/workflows/ci.yml/badge.svg)](https://github.com/nayan9229/jsonflat/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/nayan9229/jsonflat)](https://goreportcard.com/report/github.com/nayan9229/jsonflat)
[![Go version](https://img.shields.io/github/go-mod/go-version/nayan9229/jsonflat)](go.mod)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

**Documentation:** [nayan9229.github.io/jsonflat](https://nayan9229.github.io/jsonflat/) —
[config reference](https://nayan9229.github.io/jsonflat/config-reference),
[recipes](https://nayan9229.github.io/jsonflat/recipes),
[presets](https://nayan9229.github.io/jsonflat/presets),
[performance](https://nayan9229.github.io/jsonflat/performance),
[FAQ](https://nayan9229.github.io/jsonflat/faq).
The same pages are in [`docs/`](docs/). API reference on
[pkg.go.dev](https://pkg.go.dev/github.com/nayan9229/jsonflat).

## Install

```
go get github.com/nayan9229/jsonflat
```

Go 1.24 or newer. The only dependency is
[`github.com/valyala/fastjson`](https://github.com/valyala/fastjson).

## Quick start

A config is a JSON object; compile it once and share the `Transformer`.

```json
{
  "rules": [
    {"op": "rename",  "from": "user.id", "to": "uid"},
    {"op": "merge",   "from": ["user.first", "user.last"], "to": "user.name", "sep": " "},
    {"op": "drop",    "path": "debug"},
    {"op": "default", "path": "env", "value": "prod"}
  ]
}
```

```example
in:  {"user":{"id":7,"first":"Ada","last":"Lovelace"},"tags":["a","b"],"debug":{"x":1}}
out: {"uid":7,"tags.0":"a","tags.1":"b","user.name":"Ada Lovelace","env":"prod"}
```

```go
t := jsonflat.MustCompile(config)

var out []byte // reuse this buffer between calls
out, err := t.Append(out[:0], src)
if err != nil {
	return err
}
fmt.Println(string(out))
```

Two ways to run a `Transformer`:

- `Append(dst, src)` / `Transform(src)`: one document in, one flat object out.
- `Each(src, opt, fn)`: one document in, any number of rows out, one callback
  per row with the output's name. Needed for configs with `input.explode` or
  `outputs`.

## Recipes

Inputs, configs and outputs below are taken from the tests, and `docs_test.go`
replays them, so they are known to be right. More in the
[recipes page](https://nayan9229.github.io/jsonflat/recipes).

### Clean keys for a warehouse

```json
{
  "flatten": {"separator": "_", "arrays": "string", "drop_nulls": true, "drop_empty_objects": true},
  "keys": {
    "normalize": "snake",
    "digit_prefix": "_",
    "on_collision": "first",
    "segment_aliases": {"shiping": "shipping"},
    "aliases": [{"to": "product_id", "from": ["prodcutId", "pid"]}],
    "keep": ["product_id", "shipping_*", "_2fa", "products"]
  }
}
```

```example
in:  {"prodcutId":"P1","shiping":{"zipCode":"380001"},"2fa":true,"coupon":null,"products":[{"sku":"A1"}],"internal":{"trace":"t"}}
out: {"product_id":"P1","shipping_zip_code":"380001","_2fa":true,"products":"[{\"sku\":\"A1\"}]"}
```

Alias `from` entries are written the way the producer writes them; `keep` and
`drop` are written as final column names. Add `"rest": "extra"` to collect
whatever `keep` left out into one JSON-string column instead of losing it.
There is no fuzzy matching, on purpose: a wrong guess would corrupt a column
silently.

### One row per line item, with inherited order fields

```json
{
  "input": {"explode": "items", "inherit": ["orderId", "currency"]},
  "flatten": {"separator": "_"},
  "keys": {"normalize": "snake"},
  "columns": [
    {"to": "order_id", "from": "orderId", "as": "string"},
    {"to": "currency", "from": ["currency", "$default_currency"]}
  ],
  "expose": ["sku", "orderId"]
}
```

```example
var default_currency=INR
in: {"orderId": 981, "items": [
      {"sku": "A1", "unitPrice": 499.75, "dims": {"W": 2, "H": 3}},
      {"sku": "B7", "unitPrice": 500.00, "currency": "USD"},
      7
    ]}
row: {"order_id":"981","currency":"INR","sku":"A1","unit_price":499.75,"dims_w":2,"dims_h":3}
  fields: A1,981
row: {"order_id":"981","currency":"USD","sku":"B7","unit_price":500.00}
  dropped: 1
err: ErrRootNotContainer
```

The item's own `currency` is a column name, so the flattened copy is dropped and
counted. `7` is not a record; it is reported and the batch carries on.

### Route logs to several outputs

```json
{
  "columns": [{"to": "ts", "from": ["time", "$now"]}, {"to": "level", "from": "level"}],
  "sections": [
    {"id": "msg",  "from": "payload"},
    {"id": "http", "from": "http", "prefix": "http", "when": {"path": "http", "exists": true}},
    {"id": "err",  "from": "error", "prefix": "error"}
  ],
  "outputs": [
    {"name": "errors", "when": {"path": "level", "in": ["error", "fatal"]}},
    {"name": "access", "sections": ["http"], "when": {"path": "http.status", "exists": true}}
  ]
}
```

```example
in: {"level":"error","time":"T1","payload":{"msg":"boom"},"http":{"status":500,"path":"/x"},"error":{"kind":"io"},"ignored":1}
row errors: {"ts":"T1","level":"error","msg":"boom","http.status":500,"http.path":"/x","error.kind":"io"}
row access: {"ts":"T1","level":"error","http.status":500,"http.path":"/x"}
```

### RudderStack, via the preset

```go
var cfg jsonflat.Config
if err := json.Unmarshal(presets.RudderStack, &cfg); err != nil {
	panic(err)
}
cfg.Keys.Aliases = []jsonflat.Alias{{To: "product_id", From: []string{"prodcutId", "pid"}}}
cfg.Keys.Drop = []string{"context_ip"}
t, err := jsonflat.New(cfg)
if err != nil {
	panic(err)
}

err = t.Each(body, jsonflat.Options{}, func(r *jsonflat.Record) error {
	if r.Err != nil {
		return deadLetter(r.Index, r.Fields, r.Err) // Fields: messageId, anonymousId, userId
	}
	return putRecord(r.Name, r.Fields[1], r.JSON) // valid until this function returns
})
```

A `track` event gives a `tracks` row and a row in a table named after the
event; see [presets](https://nayan9229.github.io/jsonflat/presets) for the
full table, the choices the preset makes, and a warning about Firehose and
Glue.

### A complete program

[`example/`](example/) turns RudderStack SDK track events into one flat row
each: a fixed set of columns with fallback sources, a drop list for copies
and junk, and a `rest` column that catches whatever no column maps. It ships
with seven sample events, so it runs with no arguments, and it reads gzip or
plain NDJSON files or stdin.

```
go run ./example -pretty
go run ./example -in events.ndjson.gz -out flat.ndjson -verify
```

## Config reference

Every section and field, defaults, the order things happen in, validation
errors and a troubleshooting checklist:
**[config reference](https://nayan9229.github.io/jsonflat/config-reference)**
([`docs/config-reference.md`](docs/config-reference.md)).

The one rule to know before reading it: **source paths and output keys are
different things**. Source paths address the input (`rules[].from`,
`sections[].from`, every source) and always use `.` between segments, with the
keys spelt as the producer spells them. Output keys are what gets written
(`rules[].to`, `columns[].to`, `aliases[].to`, `keys.keep`, `keys.drop`) and
are used exactly as given, after normalisation.

## Guarantees

- **Zero heap allocations in `Append` and `Each` in the steady state**, which
  means once the pooled state's buffers have grown to the size of your traffic.
  `TestZeroAllocs`, `TestRudderZeroAllocs` and `TestKeepZeroAllocs` enforce it,
  and the release workflow refuses a build whose benchmarks allocate.
- **The output is always valid JSON.** The package has its own RFC 8259
  string escaper and number check because the parser's are more lenient; a
  number such as `00` fails the record with `ErrInvalidNumber`. `FuzzAppend`
  and `FuzzEach` assert `json.Valid` on every output.
- **A `Transformer` is immutable and safe for concurrent use.** `TestConcurrent`.
- **The output never aliases the input**, and `Append` returns `dst` unchanged
  on error. `TestAppendKeepsPrefixAndInput`, `TestErrors`.
- **All-or-nothing rows per record.** One bad record never fails the batch, and
  a record never arrives in part: all of its rows are built first, and if any
  fails, one `Record` with `Err` set is delivered instead. `TestRudderBatch`.
- **A `Record` is valid only until the callback returns.** Copy what you keep.
  `TestRecordIsACopy`.

Details, including memory behaviour and the 64-bit hash used for collision
tracking, are on the [performance page](https://nayan9229.github.io/jsonflat/performance).

## When not to use this

- Documents above a few MB: the whole document is parsed into memory.
- Streaming from an `io.Reader`: the API takes a complete `[]byte` (read
  lines and call `Each` per line, as `example/` does).
- You need to keep the nesting: this package only flattens.
- You need type casting today: values keep the type they came with.

## Versioning

Semantic versioning, tags `vMAJOR.MINOR.PATCH`, one changelog entry per
version.

- **`v0.x`**: a minor version may change the config language or the Go API,
  and the changelog says so; a patch version only fixes. Pin a minor
  (`go get github.com/nayan9229/jsonflat@v0.1`) if you want no surprises.
- **`v1.0.0`** freezes the public API and the meaning of every config field.
  Additions come in minors.
- A breaking change after v1 ships as a new module path,
  `github.com/nayan9229/jsonflat/v2`, and the v1 line keeps receiving fixes.
- Presets follow the same rules: a change to what a preset produces is a minor
  before v1 and a major after.

## Reporting bugs and security issues

- Bugs and feature requests: open an
  [issue](https://github.com/nayan9229/jsonflat/issues/new/choose). The
  templates ask for the config, the input and the expected output, which is
  exactly what becomes a test case.
- Vulnerabilities: privately, via
  [GitHub security advisories](https://github.com/nayan9229/jsonflat/security/advisories/new).
  See [SECURITY.md](SECURITY.md) for what counts and the known limits.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md): `make all` runs the same checks as CI,
and there is a section on how to propose a preset. Maintainers release with
[RELEASING.md](RELEASING.md).

## License

[MIT](LICENSE)
