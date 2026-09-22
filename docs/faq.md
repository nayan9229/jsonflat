---
title: FAQ
nav_order: 6
---

# FAQ
{: .no_toc }

<details open markdown="block">
  <summary>Contents</summary>
  {: .text-delta }
- TOC
{:toc}
</details>

## Why does the package have its own JSON string escaper and number check?

Because the parser's are not strict enough for output that must always be
valid JSON. fastjson's `MarshalTo` falls back to Go's `strconv.AppendQuote`
for strings that need escaping, which emits Go escapes such as `\x01` and `\a`
that are not JSON. Its number parser accepts `00`, `+1`, `1.2.3`, `NaN` and
`inf`. So strings go through `internal/jsonenc.AppendQuoted` (RFC 8259 escapes
only), number text is copied through unchanged to keep precision and then
checked with `ValidNumber`, and a bad number fails the record with
`ErrInvalidNumber`. Both fuzz targets assert `json.Valid` on every output.
*`TestTransform/"keys and strings are escaped as JSON, not as Go"`, `TestErrors`, `FuzzAppend`, `FuzzEach`*

## Why fastjson?

It was compared with `buger/jsonparser`, `bytedance/sonic`, `goccy/go-json`
and the standard library's `jsontext`. It won on structural validation plus
random access: values can be read in any order after one parse, which is what
makes merges, columns with fallbacks and conditions simple, and it lets a
pooled parser be reused without allocating. Every fastjson call is inside
`transform.go` (plus `ValidateBytes` in `config.go`), so the parser could be
swapped.

## Why 64-bit hashes for collision tracking?

Storing the keys themselves would allocate. The key set stores FNV-1a hashes
with a generation counter, so resetting it between rows costs nothing. Two
different keys in one row with the same 64-bit hash would be treated as one; with
a few hundred keys per row the chance is about 1 in 10^15. `on_collision`
defaults to `keep`, where nothing is tracked and this does not apply.

## Why are arrays written as JSON strings in the RudderStack preset?

A warehouse column has one type. An array of objects flattened as
`products_0_sku`, `products_1_sku`, … creates a column per index, and a raw
JSON array is a different type from a string. `"arrays": "string"` gives one
column with one type; parse it downstream when you need the elements. The
default for a plain config is `"index"`, and `"raw"` is available.

## My key disappeared. Why?

See the [troubleshooting checklist](config-reference#troubleshooting-my-key-disappeared)
in the config reference: outside every section, a `drop` rule, consumed by a
merge, normalises to nothing, `drop_nulls`/`drop_empty_objects`, not in `keep`
or matched by `drop`, reserved by a column, or a collision.

## Why does `Append` return `ErrNotSimple`?

The config has `input.explode` or `outputs`, so one document can produce any
number of rows, and `Append` returns exactly one. Use `Each`.

## Why did a record fail with `ErrNoOutput`?

The config has an `outputs` list and no output's `when` matched the record.
With the RudderStack preset that is an unknown `type`, or a single-event body
without `Options.Vars["type"]`. Without an `outputs` list there is an implicit
output that always matches.

## Can I stream from an `io.Reader`?

Not directly: `Append` and `Each` take a complete `[]byte`. For
newline-delimited input, read lines with `bufio.Scanner` and call `Each` per
line; `example/main.go` in the repository does this and stays allocation-free
in the loop.

## Can it cast values to a schema?

Not yet. Values keep the type they came with; `as: "string"` on a column is the
one conversion. A `schema` section with per-output types and a discard output
is the first item on the roadmap.

## Is a `Transformer` safe to share?

Yes. It is immutable after `Compile`/`New` and safe for concurrent use by any
number of goroutines. Compile once, share everywhere; a package-level
`MustCompile` runs at init and needs no `sync.Once`, and for lazy compilation
`sync.OnceValues` in your code is all it takes (`Example_lazyCompile`).
`Options.Vars` maps are only read, so one map per route can be shared too.
Scaling numbers and profiles are on the
[performance page](performance#concurrency). *`TestConcurrent`,
`TestSharedTransformerMixed`*

## How long is a `Record` valid?

Until the callback returns. The `Record` and all of its slices (`Name`,
`JSON`, `Fields`) belong to the pooled state and are reused for the next
record, possibly by another goroutine. Copy what you keep.
*`TestRecordIsACopy`, `TestRetainedRecordIsInvalid`*

## How are versions numbered?

Semantic versioning. Before `v1.0.0`, a minor version (`v0.2.0`) may change
the config language or the API and the changelog says so; a patch version only
fixes. From `v1.0.0` the public API and the meaning of every config field are
frozen: additions come in minors, and a breaking change would ship as a new
module path `github.com/nayan9229/jsonflat/v2`. Presets follow the same rules:
a change to what a preset produces is a minor before v1 and a major after.

## How do I report a bug or a vulnerability?

Bugs and feature requests: the issue templates on GitHub ask for the config,
the input and the expected output, which is what turns into a test. A
vulnerability: privately through GitHub's security advisories, as described in
`SECURITY.md`; please do not open a public issue for it.
