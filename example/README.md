# Example: flatten an NDJSON event export

A small command that reads an event export, one JSON document per line, gzip or
plain, and writes one flat JSON row per line with `jsonflat`. Duplicate keys
and duplicate fields are merged by the config, not by code.

It was written for a Jitsu-style export of `track` events: snake_case standard
fields (`message_id`, `anonymous_id`, `sent_at`, ...) and the payload in
`event_details`, `device_details` and `user_details`, plus a few loose
top-level keys.

```
go run ./example -in events.ndjson.gz -out flat.ndjson
go run ./example -in events.ndjson.gz -out flat.ndjson -verify -columns
gzip -dc events.ndjson.gz | go run ./example -in - > flat.ndjson
```

| Flag | Meaning |
|---|---|
| `-in` | Input file, or `-` for stdin. Gzip is detected from the data, not from the name. |
| `-out` | Output file, or `-` for stdout (default). The summary always goes to stderr. |
| `-config` | A jsonflat config file. Default: the embedded [config.json](config.json). |
| `-verify` | Check every row independently with `encoding/json`: valid JSON, no repeated key. Also collects the column names. Slow, because it allocates. |
| `-columns` | With `-verify`, list every column and how many rows have it. |
| `-limit` | Stop after this many lines. |

Event exports hold real IP addresses and device IDs. `*.ndjson` and
`*.ndjson.gz` are in `.gitignore`; keep them out of the repository.

## What the config does

```json
{
  "flatten": {"separator": "_", "arrays": "string", "drop_nulls": true, "drop_empty_objects": true},
  "keys": {"normalize": "snake", "digit_prefix": "_", "on_collision": "first"},
  "rules": [
    {"op": "merge", "mode": "first", "from": ["event", "event_name"], "to": "event"},
    {"op": "merge", "mode": "first", "from": ["context.ip", "request_ip"], "to": "context_ip"},
    {"op": "merge", "mode": "first", "from": ["device_details.app_bundle", "d"], "to": "device_details_app_bundle"}
  ],
  "expose": ["message_id"],
  "newline": true
}
```

- **Flatten**: nested keys are joined with `_` and normalised to snake_case, so
  `device_details.app_name` becomes `device_details_app_name`, `ad-id` becomes
  `ad_id`, and `_cb` becomes `cb`. `{}` and `null` produce no column.
- **Duplicate keys**: some events carry both `visit-id` and `visit_id`. After
  normalising they have the same name. `on_collision: "first"` keeps the first
  and counts the other in `Record.Dropped`, which the summary reports as
  "merged keys".
- **Duplicate fields**: three pairs of fields always hold the same value in this
  export. Each `merge` rule with `mode: "first"` folds a pair into one column:
  the first source that is present and not null wins, and both sources are
  removed from the output. Note that rule paths are *source paths*
  (`context.ip`, always with `.`), while `to` is an *output key*
  (`context_ip`). Merge results are written at the end of the row.
  `first` does not compare the values. If the two fields can disagree in your
  data and you need to know, keep both (`"keep_sources": true`) and compare
  downstream.
- **`expose`** puts `message_id` in `Record.Fields`, so a record that fails can
  be reported by ID.

## What the code shows

[main.go](main.go) is the usual way to drive `jsonflat` over a stream:

- Compile the config once and reuse the `Transformer`.
- Create the callback and the `Options` once, outside the loop, and call
  `Each` per line. The loop then does not allocate.
- `Record.JSON` is valid only until the callback returns, so the callback
  writes it out (a `bufio.Writer` copies it) and keeps nothing.
- One bad line or bad record never stops the run. An error from `Each` means
  the line is not valid JSON; `Record.Err` means the record itself was
  rejected. Both are counted, and the first few are printed with their line
  number and `message_id`.

## Sample run

A 14 MB gzip export with 105,541 `track` events (128 MB unpacked), Apple M5,
Go 1.26, one goroutine:

```
lines            105541
rows written     105541
bad lines        0
failed records   0
merged keys      1014   (same name after normalising; the first value is kept)
input            128.1 MB
output           145.4 MB
elapsed          495ms   (213033 lines/s, 258.7 MB/s, including I/O)
```

That time includes unpacking the gzip. On the same data already unpacked it is
about 360 ms (350 MB/s). With `-verify` the run takes about 2.2 s, reports 70
distinct columns, and found no invalid row and no repeated key. The output is
larger than the input because every key now carries its full path.
