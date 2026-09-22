---
title: Presets
nav_order: 4
---

# Presets
{: .no_toc }

A preset is plain config JSON, embedded in the `presets` package. Compile it as
it is, or unmarshal it into a `jsonflat.Config`, add your own aliases, drops and
rules, and pass it to `jsonflat.New`. There is no vendor code in the engine; if
a vendor layout needs something the config cannot express, the config language
gets a general feature.

<details open markdown="block">
  <summary>Contents</summary>
  {: .text-delta }
- TOC
{:toc}
</details>

## RudderStack

`presets.RudderStack` turns RudderStack SDK payloads, either `{"batch":[...]}`
or a single event, into rows that follow the
[RudderStack warehouse schema](https://www.rudderstack.com/docs/destinations/warehouse-destinations/warehouse-schema/).
`preset_test.go` checks its output byte for byte against a realistic batch
(`TestRudderBatch`), and `TestRudderZeroAllocs` keeps it allocation-free.

| Record | Output | Content |
|---|---|---|
| `track` | `tracks` | columns + `context_*` |
| `track` | snake_case of the event name | columns + `context_*` + properties |
| `identify` | `identifies` | columns + `context_*` + traits, then `context.traits` quietly |
| `page` / `screen` | `pages` / `screens` | columns incl. `name`, `category` + `context_*` + properties |
| `group` | `groups` | columns incl. `group_id` + `context_*` + traits |
| `alias` | `aliases` | columns incl. `previous_id` + `context_*` |

The standard columns are `id`, `anonymous_id`, `user_id`, `channel`, `sent_at`,
`received_at`, `original_timestamp`, `timestamp`, and for track events `event`
and `event_text`.

### The preset itself

This is `presets/rudderstack.json`, unchanged:

```json
{
  "input": {"explode": "batch", "inherit": ["sentAt"]},
  "flatten": {"separator": "_", "arrays": "string", "drop_nulls": true, "drop_empty_objects": true},
  "keys": {"normalize": "snake", "digit_prefix": "_", "on_collision": "first"},

  "derive": {
    "event_table": {
      "from": "event", "normalize": "snake", "digit_prefix": "_",
      "reserved": ["tracks", "identifies", "users", "pages", "screens", "groups", "aliases", "rudder_discards"],
      "reserved_prefix": "_", "required": true,
      "when": {"path": ["type", "$type"], "equals": "track"}
    }
  },

  "columns": [
    {"to": "id", "from": "messageId", "as": "string"},
    {"to": "anonymous_id", "from": "anonymousId", "as": "string"},
    {"to": "user_id", "from": "userId", "as": "string"},
    {"to": "channel", "from": "channel", "as": "string"},
    {"to": "sent_at", "from": "sentAt", "as": "string"},
    {"to": "received_at", "from": "$now"},
    {"to": "original_timestamp", "from": "originalTimestamp", "as": "string"},
    {"to": "timestamp", "as": "string",
     "from": ["timestamp", {"fn": "clock_skew", "sent": "sentAt", "original": "originalTimestamp"}, "originalTimestamp", "$now"]},
    {"to": "event", "from": "$event_table", "when": {"path": ["type", "$type"], "equals": "track"}},
    {"to": "event_text", "from": "event", "as": "string", "when": {"path": ["type", "$type"], "equals": "track"}},
    {"to": "group_id", "from": "groupId", "as": "string", "when": {"path": ["type", "$type"], "equals": "group"}},
    {"to": "previous_id", "from": "previousId", "as": "string", "when": {"path": ["type", "$type"], "equals": "alias"}},
    {"to": "name", "from": "name", "as": "string", "quiet": true, "when": {"path": ["type", "$type"], "in": ["page", "screen"]}},
    {"to": "category", "from": "category", "as": "string", "quiet": true, "when": {"path": ["type", "$type"], "in": ["page", "screen"]}}
  ],

  "sections": [
    {"id": "context", "from": "context", "prefix": "context"},
    {"id": "properties", "from": "properties", "when": {"path": ["type", "$type"], "in": ["track", "page", "screen"]}},
    {"id": "traits", "from": "traits", "when": {"path": ["type", "$type"], "in": ["identify", "group"]}},
    {"id": "context_traits", "from": "context.traits", "quiet": true, "when": {"path": ["type", "$type"], "equals": "identify"}}
  ],

  "outputs": [
    {"name": "tracks", "sections": ["context"], "when": {"path": ["type", "$type"], "equals": "track"}},
    {"name": "$event_table", "when": {"path": ["type", "$type"], "equals": "track"}},
    {"name": "identifies", "when": {"path": ["type", "$type"], "equals": "identify"}},
    {"name": "pages", "when": {"path": ["type", "$type"], "equals": "page"}},
    {"name": "screens", "when": {"path": ["type", "$type"], "equals": "screen"}},
    {"name": "groups", "when": {"path": ["type", "$type"], "equals": "group"}},
    {"name": "aliases", "when": {"path": ["type", "$type"], "equals": "alias"}}
  ],

  "expose": ["messageId", "anonymousId", "userId"]
}
```

```example
now: 2026-09-21T10:00:00Z
in: {"batch":[{
      "type": "track", "event": "Product Added", "messageId": "m-1", "anonymousId": "anon-1",
      "context": {"ip": "1.2.3.4", "app": {"name": "Shop"}},
      "properties": {"productId": "P1", "Total Price": 499.75, "user_id": "ignored"}
    }]}
row tracks: {"id":"m-1","anonymous_id":"anon-1","received_at":"2026-09-21T10:00:00.000Z","timestamp":"2026-09-21T10:00:00.000Z","event":"product_added","event_text":"Product Added","context_ip":"1.2.3.4","context_app_name":"Shop"}
  fields: m-1,anon-1,-
row product_added: {"id":"m-1","anonymous_id":"anon-1","received_at":"2026-09-21T10:00:00.000Z","timestamp":"2026-09-21T10:00:00.000Z","event":"product_added","event_text":"Product Added","context_ip":"1.2.3.4","context_app_name":"Shop","product_id":"P1","total_price":499.75}
  fields: m-1,anon-1,-
  dropped: 1
```

The property called `user_id` did not become the `user_id` column, although
the event has no `userId`: column names are reserved whether or not the column
had a value, and the drop is counted. *`TestRudderCollisionPolicy`*

### Rules taken from the published warehouse schema

- Keys are snake_case, and nested keys are joined with `_`.
- Everything under `context` gets the `context_` prefix.
- Standard columns win over properties of the same name.
- Top-level fields outside the spec (`integrations`, anything unknown) are
  dropped: they are outside every section.
- Names that start with a digit get a `_` in front.
- `timestamp` falls back to `received_at - (sent_at - original_timestamp)`,
  which corrects for a client clock that is off.

### Choices made here

The RudderStack docs do not pin these down, so the preset decides:

- Arrays are written as JSON strings (`"[{\"sku\":\"A1\"}]"`), so a typed
  column keeps one type.
- `null` and `{}` produce no key.
- IDs are strings: a numeric `userId` of `42` becomes `"42"`.
- For `identify`, `traits` wins over `context.traits`.
- An event name equal to a standard output (`Tracks`) becomes `_tracks`.
- A track event whose name normalises to nothing fails with `ErrRequired`.
- An unknown `type` fails with `ErrNoOutput`.

### Using it

```go
var cfg jsonflat.Config
if err := json.Unmarshal(presets.RudderStack, &cfg); err != nil {
	return err
}
// Correct keys the way your producers get them wrong.
cfg.Keys.Aliases = []jsonflat.Alias{{To: "product_id", From: []string{"prodcutId", "pid"}}}
cfg.Keys.SegmentAliases = map[string]string{"shiping": "shipping"}
cfg.Keys.Drop = []string{"context_ip", "context_traits_*"}
t, err := jsonflat.New(cfg)
```

`Example_rudderStackPreset` on pkg.go.dev is a complete program.

- Bodies from the single-event routes (`/v1/track`, `/v1/identify`, …) carry
  no `type`. Pass `Options.Vars = map[string]string{"type": "track"}`, and keep
  one such map per route: the map is only read. *`TestRudderSingleEvent`*
- `Record.Fields` is `messageId`, `anonymousId`, `userId`, in that order.
  `anonymousId` is a good Kinesis partition key. The fields are filled in for
  failed records too, so they can be dead-lettered.
- To leave out the `tracks` rows, remove that entry from `cfg.Outputs`.
- Set `cfg.Newline = true` for newline-delimited JSON.
- To keep only known columns per your warehouse, add `cfg.Keys.Keep` with the
  final column names (`context_*` covers the context block).

### Not covered

The `users` upsert table, `rudder_discards`, `uuid_ts`, `context_request_ip`
and type casting.

If you already have RudderStack-managed tables, compare a day of column names
before switching. The snake_case rules approximate RudderStack's and may differ
at the edges: digits and consecutive capitals.

### Warning: one Firehose stream is not enough

> One Firehose stream converts to Parquet using **one** Glue table, and leaves
> out attributes that are not in that table. Per-event outputs therefore do not
> fit a single Firehose stream with format conversion. Options: (1) one Kinesis
> stream and your own consumer that groups rows by `Record.Name` and writes
> Parquet per output; (2) one Firehose per high-volume output, with option 1 or
> 3 for the long tail; (3) one Firehose with dynamic partitioning on the output
> name that delivers JSON, then a scheduled Athena or Glue job that compacts
> each prefix to Parquet.

## Proposing a preset

See [How to propose a preset](https://github.com/nayan9229/jsonflat/blob/main/CONTRIBUTING.md#how-to-propose-a-preset)
in CONTRIBUTING.md: an issue with the vendor's published schema first, then
the JSON file, an embedded variable with a doc comment, a byte-for-byte test
with a zero-allocation guard, and a section on this page.
