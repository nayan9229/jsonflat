---
title: Home
nav_order: 1
---

# jsonflat

Zero-allocation JSON flattening and transformation for Go, driven by a JSON
config. Flatten nested documents into flat rows, normalise keys to snake_case,
fix misspelt keys, rename, drop, merge and default, whitelist the columns you
want, split a batch into records, and route each record to one or more named
outputs. Built for high-throughput pipelines that feed columnar storage.

{% if site.data.release %}
This site documents **{{ site.data.release.tag }}** (published {{ site.data.release.date }}).
API reference for this version: [pkg.go.dev]({{ site.data.release.pkgdoc }}).
The Markdown behind these pages, at this version: [`docs/`]({{ site.data.release.source }}).
For another version, open `https://pkg.go.dev/github.com/nayan9229/jsonflat@vX.Y.Z`
or the `docs/` folder of that tag on GitHub.
{% endif %}

## Install

```
go get github.com/nayan9229/jsonflat
```

Go 1.24 or newer. The only dependency is
[`github.com/valyala/fastjson`](https://github.com/valyala/fastjson).

## Thirty seconds

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

var out []byte // reuse between calls: that is what keeps Append allocation-free
out, err := t.Append(out[:0], src)
```

Every config and example on this site is compiled and replayed by
`docs_test.go` in the repository, so what you read is what the code does.

## Where to go

| Page | What it answers |
|---|---|
| [Config reference](config-reference) | Every section and field, defaults, the order things happen in, validation errors, troubleshooting. |
| [Recipes](recipes) | Worked examples: cleaning keys, whitelisting columns, exploding a batch, routing to outputs, merging duplicates, per-event tables. |
| [Presets](presets) | The RudderStack warehouse-schema preset: what it produces, the choices it makes, how to extend it, the Firehose/Glue warning. |
| [Performance](performance) | Benchmarks with hardware and versions, how to run them, what "zero allocations" and "steady state" mean, memory. |
| [FAQ](faq) | Why the package has its own JSON escaper, why fastjson, hashes for collisions, streaming, type casting, when a `/v2` comes. |
| [Changelog](changelog) | What changed in each version. |

The Go API itself (types, functions, runnable examples) is on
[pkg.go.dev](https://pkg.go.dev/github.com/nayan9229/jsonflat).

## Two ideas to hold on to

**Source paths and output keys are different things.** A source path addresses
the input (`rule.from`, `section.from`, every source): always `.` between
segments, keys spelt as the producer spells them. An output key is what gets
written (`rule.to`, `column.to`, `alias.to`, `keys.keep`, `keys.drop`): used
exactly as given, after normalisation. With `flatten.separator: "_"` the same
value is `user.first` on the way in and `user_first` on the way out.

**One document becomes records, a record becomes rows.** Without `input.explode`
the document is the one record. Without `outputs` there is one implicit output,
so one record gives one row. With both, a batch of ten events matching two
outputs each gives twenty rows, delivered to your callback one by one, each
with the output's name.
