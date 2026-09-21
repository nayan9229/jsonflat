# Presets

A preset is plain `jsonflat` config JSON, embedded in the `presets` package.
Compile it as it is, or unmarshal it into a `jsonflat.Config`, add your own
aliases, drops and rules, and pass it to `jsonflat.New`.

There is no vendor code in `jsonflat`. If a vendor layout needs something the
config cannot express, the config language gets a general feature.

## RudderStack

`presets.RudderStack` ([rudderstack.json](rudderstack.json)) turns RudderStack
SDK payloads, either `{"batch":[...]}` or a single event, into rows that follow
the [RudderStack warehouse schema](https://www.rudderstack.com/docs/destinations/warehouse-destinations/warehouse-schema/).

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

### Rules taken from the published warehouse schema

- Keys are snake_case, and nested keys are joined with `_`.
- Everything under `context` gets the `context_` prefix.
- Standard columns win over properties of the same name. A property called
  `user_id` never becomes the `user_id` column, not even on an event without a
  `userId`; it is counted in `Record.Dropped`.
- Top-level fields outside the spec (`integrations`, anything unknown) are
  dropped.
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

See [example_preset_test.go](../example_preset_test.go) for a complete program.

- Bodies from the single-event routes (`/v1/track`, `/v1/identify`, ...) carry
  no `type`. Pass `Options.Vars = map[string]string{"type": "track"}`, and keep
  one such map per route: the map is only read.
- `Record.Fields` is `messageId`, `anonymousId`, `userId`, in that order.
  `anonymousId` is a good Kinesis partition key. The fields are filled in for
  failed records too, so they can be dead-lettered.
- To leave out the `tracks` rows, remove that entry from `cfg.Outputs`.
- Set `cfg.Newline = true` for newline-delimited JSON.

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

See [CONTRIBUTING.md](../CONTRIBUTING.md#how-to-propose-a-preset).
