---
title: Recipes
nav_order: 3
---

# Recipes
{: .no_toc }

Worked examples for the jobs people reach for this package to do. Every config
here compiles and every example replays in `docs_test.go`; inputs and outputs
are taken from the tests where one exists.

<details open markdown="block">
  <summary>Contents</summary>
  {: .text-delta }
- TOC
{:toc}
</details>

## Flatten, and fix up a few keys

The four rules: rename a path, merge two into one, drop a subtree, default a
missing key. Rule paths are source paths (dots); `to` is the output key.

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

Merge results and defaults are written at the end of the row; renamed keys stay
where the walk finds them. *`Example`, `TestTransform`*

## Clean keys for a warehouse

snake_case everything, join with `_`, keep arrays as one typed string column,
fix a producer's misspellings, and decide what a clash means.

```json
{
  "flatten": {"separator": "_", "arrays": "string", "drop_nulls": true, "drop_empty_objects": true},
  "keys": {
    "normalize": "snake",
    "digit_prefix": "_",
    "on_collision": "first",
    "segment_aliases": {"shiping": "shipping"},
    "aliases": [{"to": "product_id", "from": ["prodcutId", "pid"]}],
    "drop": ["internal_*"]
  }
}
```

```example
in:  {"prodcutId":"P1","shiping":{"zipCode":"380001"},"2fa":true,"coupon":null,"meta":{},
      "products":[{"sku":"A1","qty":2}],"productId":"P2","internal":{"trace":"t"}}
out: {"product_id":"P1","shipping_zip_code":"380001","_2fa":true,"products":"[{\"sku\":\"A1\",\"qty\":2}]"}
  dropped: 1
```

`prodcutId` is aliased to `product_id`; the real `productId` then normalises to
the same name and, under `first`, is counted in `Record.Dropped` rather than
written twice. Use `"on_collision": "error"` to fail such records instead.
*`TestKeysWithAppend`, `TestRudderBatch`*

## Whitelist the columns you want, and give them short names

`keys.keep` writes only the listed output keys; everything else that the walk
produces is left out. List the names as they look **after** aliases: the alias
makes `context_user_agent` into `ua`, and `keep` allows `ua`.

```json
{
  "flatten": {"separator": "_"},
  "keys": {
    "normalize": "snake",
    "aliases": [{"to": "ua", "from": ["context_user_agent"]}],
    "keep": ["message_id", "event", "ua", "event_details_*"],
    "drop": ["event_details_dnt"]
  },
  "rules": [
    {"op": "merge", "mode": "first", "from": ["device_details.app_bundle", "d"], "to": "bundle"}
  ]
}
```

```example
in:  {"message_id":"m-1","event":"px-lo","context":{"user_agent":"Python-urllib/3.12","ip":"10.0.0.1"},
      "device_details":{"app_bundle":"com.shop.app","app_name":"Shop"},"d":"com.shop.app",
      "event_details":{"ad_slots":"3","dnt":"0"},"r":"FL"}
out: {"message_id":"m-1","event":"px-lo","ua":"Python-urllib/3.12","event_details_ad_slots":"3","bundle":"com.shop.app"}
```

Three things to notice:

- `bundle` is a **merge result**, so it is written regardless of `keep`, and it
  is renamed through the merge's `to`; an alias would never see it, because
  merge results skip the alias step.
- `keep` and `drop` combine: the `event_details_*` family minus `dnt`.
- Keys removed this way are silent: `Dropped` stays 0 and they cannot collide.

*`TestFlattenEdgeCases/"keep: exact names and prefixes, array indices included"`,
`TestKeepIsSilent`*

### … and keep the leftovers in one column

Add `keys.rest` and nothing is lost: everything the whitelist removed lands
in one JSON-string column, so a later schema change can still recover it.

```json
{
  "flatten": {"separator": "_"},
  "keys": {"normalize": "snake", "keep": ["message_id", "event"], "rest": "extra"}
}
```

```example
in:  {"message_id":"m-1","event":"px-lo","context":{"ip":"10.0.0.1"},"event_details":{"ad_slots":"3","dnt":"0"}}
out: {"message_id":"m-1","event":"px-lo","extra":"{\"context_ip\":\"10.0.0.1\",\"event_details_ad_slots\":\"3\",\"event_details_dnt\":\"0\"}"}
```

`drop` still removes for good: a key matched by `drop` is not collected.
*`TestFlattenEdgeCases/"rest takes nothing that drop or a column removed"`*

## One row per line item, with the order's fields on each

`input.explode` makes each element of an array a record; `inherit` lets a
record read named top-level keys of the enclosing document when it lacks them.
`expose` puts IDs in `Record.Fields` for partitioning or dead-lettering.

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
  fields: B7,981
  dropped: 1
err: ErrRootNotContainer
```

The second item's own `currency` is a column name, so the flattened copy is
dropped and counted. The scalar `7` is not a record; it is reported and the
batch carries on. *`TestExplodeInheritColumns`*

## Route records to several outputs

Sections give each part of the record a home; outputs pick sections and match
on conditions. A record can land in several outputs.

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

```example
in: {"level":"error","time":"T1","payload":{"msg":"boom","level":"shadow"},
     "http":{"status":500,"path":"/x"},"error":{"kind":"io"},"ignored":1}
row errors: {"ts":"T1","level":"error","msg":"boom","http.status":500,"http.path":"/x","error.kind":"io"}
row access: {"ts":"T1","level":"error","http.status":500,"http.path":"/x"}
```

`ignored` is outside every section. `payload.level` would collide with the
`level` column, so it is dropped. A record that matches no output fails with
`ErrNoOutput`. *`TestOutputsSectionsConditions`*

## One table per event name

A derived value can name an output. Here every `track` event goes to a shared
`tracks` table and to a table named after the event; other types go to fixed
tables. This is the shape of the RudderStack preset.

```json
{
  "flatten": {"separator": "_"},
  "keys": {"normalize": "snake"},
  "derive": {
    "event_table": {"from": "event", "normalize": "snake", "digit_prefix": "_",
                    "reserved": ["tracks", "identifies"], "reserved_prefix": "_", "required": true,
                    "when": {"path": "type", "equals": "track"}}
  },
  "columns": [{"to": "id", "from": "messageId", "as": "string"}, {"to": "type", "from": "type"}],
  "sections": [
    {"id": "context", "from": "context", "prefix": "context"},
    {"id": "props", "from": "properties"},
    {"id": "traits", "from": "traits"}
  ],
  "outputs": [
    {"name": "tracks", "sections": ["context"], "when": {"path": "type", "equals": "track"}},
    {"name": "$event_table", "sections": ["context", "props"], "when": {"path": "type", "equals": "track"}},
    {"name": "identifies", "sections": ["context", "traits"], "when": {"path": "type", "equals": "identify"}}
  ]
}
```

```example
in: {"type":"track","event":"Order Completed","messageId":"m-1","context":{"app":"Shop"},"properties":{"total":1499.5}}
row tracks: {"id":"m-1","type":"track","context_app":"Shop"}
row order_completed: {"id":"m-1","type":"track","context_app":"Shop","total":1499.5}
```

```example
in: {"type":"track","event":"Tracks","messageId":"m-2","properties":{"a":1}}
row tracks: {"id":"m-2","type":"track"}
row _tracks: {"id":"m-2","type":"track","a":1}
```

```example
in: {"type":"identify","messageId":"m-3","traits":{"plan":"pro"}}
row identifies: {"id":"m-3","type":"identify","plan":"pro"}
```

An event called `Tracks` would clash with the standard table, so `reserved`
gives it an underscore. An output's `sections` list picks which sections it
carries; leaving it out means all of them, so an output that should carry
columns only names a section that the record does not have to contain.
*`TestDerive`, `TestRudderBatch`*

## Merge duplicate keys and duplicate fields

Exports often carry the same fact twice: `visit-id` and `visit_id`, or
`event_name` next to `event`. Two mechanisms handle the two cases:

- keys that become the **same name** after normalisation: `on_collision: "first"`
  keeps the first and counts the rest in `Dropped`;
- **different fields with the same value**: a `merge` with `mode: "first"`
  folds them into one column and removes the sources.

```json
{
  "flatten": {"separator": "_", "drop_empty_objects": true},
  "keys": {"normalize": "snake", "on_collision": "first"},
  "rules": [
    {"op": "merge", "mode": "first", "from": ["event", "event_name"], "to": "event"},
    {"op": "merge", "mode": "first", "from": ["context.ip", "request_ip"], "to": "context_ip"}
  ],
  "expose": ["message_id"]
}
```

```example
in:  {"context":{"ip":"10.0.0.1","traits":{}},"event_name":"ad_request","event":"ad_request",
      "request_ip":"10.0.0.1","visit-id":"v1","visit_id":"v1","message_id":"m-1"}
out: {"visit_id":"v1","message_id":"m-1","event":"ad_request","context_ip":"10.0.0.1"}
  dropped: 1
```

`first` does not compare the two values; it takes the first present one. If
the fields can disagree in your data and you need to know, keep both
(`"keep_sources": true`) and compare downstream. The `example/` command in the
repository does this for RudderStack track events, with seven bundled samples
(`go run ./example -pretty`), and reports the counts.

## Dead-letter failed records

`Record.Fields` is filled in before anything can fail, so a failed record still
carries the IDs you need to put it on a dead-letter queue.

```json
{
  "input": {"explode": "batch"},
  "columns": [{"to": "n", "from": "n"}],
  "expose": ["id"]
}
```

```example
in: {"batch":[{"id":"a","n":1},{"id":"b","n":00},"x"]}
row: {"n":1,"id":"a"}
  fields: a
err: ErrInvalidNumber
err: ErrRootNotContainer
```

```go
err := t.Each(body, jsonflat.Options{}, func(r *jsonflat.Record) error {
	if r.Err != nil {
		return dlq.Put(r.Index, r.Fields[0], r.Err) // copy Fields if you keep them
	}
	return sink.Put(r.Name, r.JSON) // valid until this function returns
})
```

The second record's `00` is not a valid JSON number: the parser accepts it, the
output must not, so the record fails with its `id` attached. In the test, the
failed record's `Fields[0]` is `b`. *`TestFieldsOnErrorRecords`, `TestRudderBatch`*

## Single-event routes: supply the type from the URL

Bodies from `/v1/track`, `/v1/identify`, … carry no `type`. Conditions can read
a variable as a fallback, and `Options.Vars` supplies it per route; keep one
map per route, it is only read.

```json
{
  "columns": [{"to": "type", "from": ["type", "$type"]}],
  "sections": [{"from": "properties"}],
  "outputs": [{"name": "events", "when": {"path": ["type", "$type"], "equals": "track"}}]
}
```

```example
var type=track
in: {"event":"Signed Up","properties":{"plan":"pro"}}
row events: {"type":"track","plan":"pro"}
```

*`TestRudderSingleEvent`*
