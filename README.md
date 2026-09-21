# jsonflat: zero-allocation JSON flattening and transformation for Go

`jsonflat` turns nested JSON into flat rows, driven entirely by a JSON config,
without allocating on the hot path. Flatten, normalise keys to snake_case, fix
misspelt keys, rename, drop, merge and default, split a batch into records,
and route each record to one or more named outputs. It is built for
high-throughput pipelines that feed columnar storage.

Vendor layouts are configs, not code. The bundled RudderStack preset produces
rows that follow the RudderStack warehouse schema, ready for Kinesis, Firehose,
Parquet and Athena.

[![Go Reference](https://pkg.go.dev/badge/github.com/nayan9229/jsonflat.svg)](https://pkg.go.dev/github.com/nayan9229/jsonflat)
[![CI](https://github.com/nayan9229/jsonflat/actions/workflows/ci.yml/badge.svg)](https://github.com/nayan9229/jsonflat/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/nayan9229/jsonflat)](https://goreportcard.com/report/github.com/nayan9229/jsonflat)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

## Install

```
go get github.com/nayan9229/jsonflat
```

Requires Go 1.24 or newer. The only dependency is
[`github.com/valyala/fastjson`](https://github.com/valyala/fastjson).

## Quick start

```go
t := jsonflat.MustCompile([]byte(`{
  "rules": [
    {"op": "rename",  "from": "user.id", "to": "uid"},
    {"op": "merge",   "from": ["user.first", "user.last"], "to": "user.name", "sep": " "},
    {"op": "drop",    "path": "debug"},
    {"op": "default", "path": "env", "value": "prod"}
  ]
}`))

src := []byte(`{"user":{"id":7,"first":"Ada","last":"Lovelace"},"tags":["a","b"],"debug":{"x":1}}`)

var out []byte // reuse this buffer between calls
out, err := t.Append(out[:0], src)
if err != nil {
	panic(err)
}
fmt.Println(string(out))
// {"uid":7,"tags.0":"a","tags.1":"b","user.name":"Ada Lovelace","env":"prod"}
```

Compile a config once and share the `Transformer`: it is immutable and safe for
concurrent use. There are two ways to run it:

- `Append(dst, src)` / `Transform(src)`: one document in, one flat object out.
- `Each(src, opt, fn)`: one document in, any number of rows out. Needed for
  configs with `input.explode` or `outputs`.

## Recipes

Every input, config and output below is taken from the tests, so it is known to
be right.

### 1. Plain flatten with rename, merge, drop, default

An empty config `{}` flattens everything with `.` between segments:

```
in:   {"a":{"b":1,"c":{"d":"x"}},"e":[true,null,{"f":2.50}]}
out:  {"a.b":1,"a.c.d":"x","e.0":true,"e.1":null,"e.2.f":2.50}
```

Number text is copied through unchanged (`2.50` stays `2.50`). Rules work on
source paths; see the quick start above for all four together. Merges come in
three modes:

```
config: {"rules":[{"op":"merge","from":["a","b","c","d","e"],"to":"m","sep":"-"}]}
in:     {"a":"x","b":12.5,"c":null,"e":true}
out:    {"m":"x-12.5-true"}

config: {"rules":[{"op":"merge","from":["a","b","c"],"to":"m","mode":"array"}]}
in:     {"c":"z","a":null}
out:    {"m":[null,"z"]}

config: {"rules":[{"op":"merge","from":["nick","name","id"],"to":"display","mode":"first"}]}
in:     {"id":5,"name":"bob","nick":null}
out:    {"display":"bob"}
```

### 2. Clean keys: snake_case, aliases, collisions

```json
{
  "flatten": {"separator": "_"},
  "keys": {
    "normalize": "snake",
    "digit_prefix": "n",
    "segment_aliases": {"adress": "address"},
    "aliases": [{"to": "address_zip", "from": ["address_postcode", "Address ZIP Code"]}],
    "drop": ["secret", "internal_*"]
  }
}
```

```
in:   {"adress":{"postcode":"38","city":"A"},"2fa":true,"secret":1,"internal":{"a":1,"b":{"c":2}},"internals":3}
out:  {"address_zip":"38","address_city":"A","n2fa":true,"internals":3}
```

Write alias `from` entries the way the producer writes them (`prodcutId`,
`Total Price`): with snake normalisation they are normalised when the config is
compiled. There is no fuzzy matching, on purpose. A wrong guess would corrupt a
column silently.

When two keys end up with the same name, `on_collision` decides:

```
config: {"flatten":{"separator":"_"},"keys":{"normalize":"snake","on_collision":"first"}}
in:     {"productId":1,"product_id":2,"a":{"b":3},"a_b":4}
out:    {"product_id":1,"a_b":3}
```

`"keep"` (the default) writes duplicates as they come and costs nothing,
`"first"` keeps the first and counts the rest in `Record.Dropped`, and
`"error"` fails the record with `ErrCollision`.

### 3. One row per line item, with inherited order fields

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

```go
opt := jsonflat.Options{Vars: map[string]string{"default_currency": "INR"}}
err := t.Each(src, opt, func(r *jsonflat.Record) error { ... })
```

```
in:   {"orderId": 981, "items": [
        {"sku": "A1", "unitPrice": 499.75, "dims": {"W": 2, "H": 3}},
        {"sku": "B7", "unitPrice": 500.00, "currency": "USD"},
        7
      ]}

row 0:  {"order_id":"981","currency":"INR","sku":"A1","unit_price":499.75,"dims_w":2,"dims_h":3}
        Fields = ["A1", "981"]
row 1:  {"order_id":"981","currency":"USD","sku":"B7","unit_price":500.00}
        Dropped = 1   (the item's own "currency" is a column name, so the flattened copy is dropped)
row 2:  Err = ErrRootNotContainer   (7 is not a record; the batch carries on)
```

### 4. Route logs to several outputs

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
    {"name": "access", "sections": ["http"], "when": {"path": "http.status", "exists": true}},
    {"name": "debug",  "when": {"path": "$env", "equals": "dev"}}
  ]
}
```

```
in:  {"level":"error","time":"T1","payload":{"msg":"boom","level":"shadow"},
      "http":{"status":500,"path":"/x"},"error":{"kind":"io"},"ignored":1}

errors:  {"ts":"T1","level":"error","msg":"boom","http.status":500,"http.path":"/x","error.kind":"io"}
access:  {"ts":"T1","level":"error","http.status":500,"http.path":"/x"}
```

`ignored` is outside every section, so it is never walked. `payload.level`
would collide with the `level` column, so it is dropped. A record that matches
no output is reported with `ErrNoOutput`.

### 5. RudderStack, via the preset

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
		return deadLetter(r.Index, r.Fields, r.Err)
	}
	// r.Fields is messageId, anonymousId, userId. Copy what you keep.
	return putRecord(r.Name, r.Fields[1], r.JSON)
})
```

```
in:  {"batch":[{
       "type": "track", "event": "Product Added", "messageId": "m-1", "anonymousId": "anon-1",
       "context": {"ip": "1.2.3.4", "app": {"name": "Shop"}},
       "properties": {"prodcutId": "P1", "Total Price": 499.75, "user_id": "ignored"}
     }]}

tracks:         {"id":"m-1","anonymous_id":"anon-1","received_at":"2026-09-21T10:00:00.000Z","timestamp":"2026-09-21T10:00:00.000Z","event":"product_added","event_text":"Product Added","context_app_name":"Shop"}
product_added:  {"id":"m-1","anonymous_id":"anon-1","received_at":"2026-09-21T10:00:00.000Z","timestamp":"2026-09-21T10:00:00.000Z","event":"product_added","event_text":"Product Added","context_app_name":"Shop","product_id":"P1","total_price":499.75}
```

See [presets/README.md](presets/README.md) for what the preset covers, the
choices it makes, and an important note on Firehose and Glue.

### A complete program

[example/](example/) is a small command that flattens an NDJSON event export,
gzip or plain, into one flat row per line. Its config merges keys that collide
after normalising (`visit-id` and `visit_id`) and folds fields that repeat the
same value into one column. It also shows how to drive `Each` over a stream
without allocating, and how to count bad lines and bad records without stopping.

```
go run ./example -in events.ndjson.gz -out flat.ndjson -verify
```

## Config reference

An empty config `{}` flattens the whole document with `.` as the separator.
Every section is optional and they combine freely. `Compile` rejects unknown
fields, so a misspelt section is an error, not a silent no-op.

### Two kinds of path. Keep them apart

| Kind | Where | Rule |
|---|---|---|
| **Source path** | rule `path` and `from`, section `from`, `input.explode`, every source | Addresses the input. Always uses `.` between segments, whatever `flatten.separator` is, with keys spelt as in the input. Array elements are addressed by index: `tags.0`. |
| **Output key** | rule `to`, `path` of a `default` rule, column `to`, alias `to`, `keys.drop` | What gets written. Used exactly as given. |

### Order of work for one record

1. The record must be an object or an array, else `ErrRootNotContainer`.
2. `expose` is resolved into `Record.Fields`.
3. `derive` values are computed, in name order.
4. Conditions decide which alias groups, columns and sections are active.
5. For each output whose `when` passes, in config order, one row is built:
   active columns, then sections, then merge results, then defaults.
6. No configured output matched: `ErrNoOutput`. Without `outputs` there is one
   implicit output that always matches and has no name.

### `input`

| Field | Meaning |
|---|---|
| `explode` | Source path of an array. Each element is one record and the enclosing document is the *envelope*. Path absent: the document itself is the single record, index 0. Present but not an array: `Each` returns `ErrExplode`. |
| `inherit` | Top-level keys (no dots). A source lookup of exactly that one-segment path that finds nothing in the record falls back to the envelope. Applies to sources (columns, conditions, derive, expose, `clock_skew` arguments), not to section walks. |

### Sources and conditions

A source is one of:

| Form | Value |
|---|---|
| `"user.id"` | The value at that source path. `null` counts as missing. |
| `"$now"` | `Options.Now` (or `time.Now()`) in UTC as `2006-01-02T15:04:05.000Z`. |
| `"$name"` | The derived value `name` if one is defined, otherwise `Options.Vars["name"]`. Empty counts as missing. |
| `{"fn":"clock_skew","sent":P1,"original":P2}` | `now - (sent - original)` in the same layout, when both paths hold RFC 3339 strings; otherwise missing. Corrects timestamps taken on a client whose clock is off. |

Wherever a list of sources is accepted, a single source may be written without
the brackets. The list is tried in order and the first source with a value
wins. The *text* of a path source is its string bytes, or its number text if
that is a valid number. Booleans, objects and arrays have no text.

A condition (`when`) has a `path` (one source or a list) and exactly one of:

| Field | Passes when |
|---|---|
| `equals` | the text equals the string |
| `in` | the text is one of the strings |
| `exists` | `true`: some source is present and not null, of any type, objects included. `false`: none is. |

Equal conditions are compiled once and evaluated once per record, however
often a config repeats them.

### `derive`

A map from name to a value computed once per record, usable as `"$name"` in
sources and as an output name. Names `""` and `"now"` are not allowed. Values
are computed in name order, and a derived value may read only derived values
that sort before it.

| Field | Meaning |
|---|---|
| `from` | Sources; the first with text wins. |
| `normalize` | `"snake"` to normalise the text. |
| `reserved`, `reserved_prefix` | If the result is in `reserved`, `reserved_prefix` goes in front. |
| `digit_prefix` | Otherwise, if the result starts with a digit, this goes in front. |
| `required` | An empty value fails the record with `ErrRequired` (only when `when` passes). |
| `when` | When it fails, the value is empty. |

### `columns`

Explicit output keys, written first, in config order. A column is *active* when
its `when` passes or it has none.

| Field | Meaning |
|---|---|
| `to` | Output key. |
| `from` | Sources in order of preference. |
| `as` | `"raw"` (default): a path source is written as it is, any type; `$` and `fn` sources are written as a JSON string. `"string"`: the text of the first source that has one, as a JSON string, so `"userId": 42` becomes `"42"`. |
| `when` | Condition for the column to be active. |
| `quiet` | Keys dropped in favour of this column are not counted in `Dropped`. |

**Column names are reserved whether or not the column got a value.** A
flattened key, merge result or default with the name of an active column is
dropped and counted in `Record.Dropped`, unless the column or the current
section is `quiet`. This is never an error, even with `on_collision: "error"`.
An alias may not target a column name. When two active columns share a name,
the first one that has a value wins.

### `sections`

| Field | Meaning |
|---|---|
| `id` | Name that `outputs[].sections` refers to. |
| `from` | Source path of the subtree to flatten. Empty: the whole record. |
| `prefix` | Output key prefix. Empty: none. |
| `when` | Condition for the section to be walked. |
| `quiet` | Collisions inside this section are neither counted nor turned into errors. Meant for a second copy of data that is expected to repeat. |

Without `sections` there is one implicit section for the whole record with no
prefix. Anything outside the listed sections is not walked, which is how
unknown top-level fields get dropped. A missing subtree, or one that is not an
object or array, is skipped.

### `flatten`

| Field | Meaning |
|---|---|
| `separator` | Joins output key segments. Default `.`. |
| `arrays` | `"index"` (default): `key.0`, `key.1`. `"raw"`: the array is written as a JSON value. `"string"`: the array is written as a JSON **string** (`"[{\"sku\":\"A1\"}]"`), which gives a typed column one stable type. An empty array is `[]`, or `"[]"` in string mode. |
| `max_depth` | Container levels to flatten inside a section; the section root is level 1. A container one level deeper is written as raw JSON. 0: no limit. |
| `drop_nulls` | Leave out `null` leaves. |
| `drop_empty_objects` | Leave out `{}`. |

Without the two `drop_` options, `null` and `{}` are written as values, so no
key is lost. A root array is flattened with keys `0`, `1`, and so on. An empty
key is kept: `{"":{"a":1}}` gives `{".a":1}`.

### `keys`: output-side correction, applied to every flattened leaf

In this order:

| Step | Field | What happens |
|---|---|---|
| 1 | `normalize` | `"snake"` normalises each object key segment: `productId`, `Product ID` → `product_id`; `HTTPServer` → `http_server`; `v2Beta` → `v2_beta`. Array indices are left alone. A segment that normalises to nothing is skipped with everything below it. |
| 2 | `segment_aliases` | Replaces one (normalised) segment wherever it appears. |
| 3 | | The segment is joined onto the output key. |
| 4 | `aliases` | Replaces a full output key: `{"to": ..., "from": [...], "when": ...}`. Groups whose `when` passes are tried first, in config order, then those without `when`. |
| 5 | `digit_prefix` | Goes in front of a key that starts with a digit. |
| 6 | `drop` | Exact match, or prefix match for entries ending in `*`. |
| 7 | | A key reserved by an active column is dropped (see `columns`). |
| 8 | `on_collision` | `"keep"` (default), `"first"` or `"error"`; see recipe 2. |

Columns, merge results and defaults skip steps 1 to 6 and go through 7 and 8.

Alias `from` entries are normalised, but they do not go through
`segment_aliases`. Write them as the key looks after step 2: with the segment
alias `adress` → `address`, the alias entry is `address_postcode`.

### `rules`: source-side steps, matched by source path

| `op` | Fields | Meaning |
|---|---|---|
| `rename` | `from`, `to` | The exact path or the whole subtree below it. `to` replaces the entire output key built so far. Nested renames work. Rules match the nodes a section walks, so a rename of a section's own `from` path, or of a path above it, has no effect: set the section's `prefix` instead. |
| `drop` | `path` | Removes the path or the whole subtree, a section whose `from` lies inside it included. Drop wins: a merge source below a dropped subtree counts as missing. |
| `merge` | `from`, `to`, `mode`, `sep`, `keep_sources` | Combines scalar sources into `to`. `concat` (default): one string joined with `sep`; missing and `null` sources are skipped. `array`: the present sources in rule order; a present `null` is kept. `first`: the first present, non-null source, unchanged. Sources are removed from the output unless `keep_sources` is true. A source that is an object or array is not merged and is flattened as usual. A merge with no present source writes nothing. |
| `default` | `path`, `value` | Writes `value` (any JSON) when no key equal to `path` was written in this row, renamed keys, merge results and columns included. |

### `outputs`, `expose`, `newline`

| Field | Meaning |
|---|---|
| `outputs[].name` | A literal, or `"$name"` of a derived value. |
| `outputs[].when` | Condition for the record to go to this output. |
| `outputs[].sections` | Section IDs this output contains. Empty: all. Columns, merges and defaults are part of every output. |
| `expose` | Sources whose text lands in `Record.Fields`, in order; `nil` when missing. Filled in before anything can fail, so an error record still carries the IDs you need for dead-lettering. For the same reason a derived value cannot be exposed: it does not exist yet. |
| `newline` | Appends `"\n"` to every row. |

A record that matches several outputs produces one row per output, in config
order.

## Guarantees

- **Zero heap allocations in `Append` and `Each` in the steady state.** Two
  tests enforce it. "Steady state" means: the pooled per-call state exists and
  its buffers have grown to the size of your traffic. The first call after a
  garbage-collection cycle may allocate once while the pool refills. A
  timestamp with a numeric zone offset such as `+05:30` can make `time.Parse`
  allocate once inside `clock_skew`; `Z` timestamps do not. `Compile`, `New`
  and error paths allocate.
- **The output is always valid JSON.** This is why the package has its own
  string escaper and number check: fastjson's serialiser escapes some strings
  the Go way (`\x01`, `\a`), which is not JSON, and its parser accepts `00`,
  `+1`, `1.2.3`, `NaN` and `inf` as numbers. Number text is copied through
  unchanged to keep precision (`12345678901234567890.123456789` and `1E+2`
  survive), so each number is checked against RFC 8259 and a bad one fails the
  record with `ErrInvalidNumber`. Two fuzz targets assert validity.
- **Concurrency.** A `Transformer` is immutable after compile and safe for
  concurrent use.
- **No aliasing.** The output never aliases the input. The `Record` passed to
  the `Each` callback, and every slice in it, is valid only until the callback
  returns. Copy what you keep.
- **`dst` unchanged on error.** `Append` returns the `dst` it was given.
- **All-or-nothing rows per record.** One bad record never fails the batch, and
  a record never arrives in part: all of its rows are built first, and if any
  fails, one `Record` with `Err` set is delivered instead.
- **Invalid UTF-8 inside strings passes through** unchanged.
- **Collision tracking compares 64-bit hashes of keys**, not the keys. Two
  different keys in one row with the same hash would be treated as one: a risk
  of roughly 1 in 10^15 per row.

## Memory

Each pooled state holds a parser whose memory is proportional to the largest
document it has parsed. State from an input above 4 MiB is not returned to the
pool, so an occasional huge document does not pin memory. The parser rejects
JSON nested deeper than 300 levels.

## When not to use this

- Documents above a few MB: the whole document is parsed into memory.
- Streaming from an `io.Reader`: the API takes a complete `[]byte`.
- You need to keep the nesting: this package only flattens.
- You need type casting today: values keep the type they came with. Casting
  and a schema mode are on the roadmap.

## Benchmarks

Measured on an Apple M5 (10 cores), macOS, `darwin/arm64`, Go 1.26.4, fastjson
v1.6.10. Median of `go test -run xxx -bench . -benchmem -count=10 .`:

| Benchmark | Time | Throughput | Memory |
|---|---|---|---|
| `BenchmarkAppend`: 650-byte record; rename, two merges, drop, default | 1.95 µs/op | 334 MB/s | 0 B/op, 0 allocs/op |
| `BenchmarkAppendFlattenOnly`: same record, `{}` config | 1.72 µs/op | 379 MB/s | 0 B/op, 0 allocs/op |
| `BenchmarkAppendParallel`: 10 goroutines | 0.34 µs/op | 1941 MB/s | 0 B/op, 0 allocs/op |
| `BenchmarkBaselineStdlib`: `encoding/json` into `map[string]any`, flatten, marshal | 10.79 µs/op | 60 MB/s | 12.9 KB/op, 242 allocs/op |
| `BenchmarkRudderEach`: RudderStack preset, 1.8 KB batch of 3 events → 5 rows | 8.00 µs/op | 228 MB/s | 0 B/op, 0 allocs/op |

The generic engine has a price. During the design of this package, a
purpose-built RudderStack transformer was measured against the config-driven
engine on the same batch (on different hardware from the table above), and the
generic engine took roughly 40% more CPU, still with zero allocations. That
is the cost of vendor layouts being configs instead of code.

Run them yourself:

```
go test -run xxx -bench . -benchmem .
```

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md), including how to propose a preset, and
[SECURITY.md](SECURITY.md).

## License

[MIT](LICENSE)
