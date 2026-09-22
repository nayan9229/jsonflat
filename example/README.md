# Example: flatten RudderStack track events

A small command that turns RudderStack SDK `track` events into one flat JSON
row each: a fixed set of columns with fallback sources, and one `extra` column
that catches whatever no column maps. It ships with seven real-shaped sample
events, so it runs with no arguments:

```
go run ./example            # one row per event, NDJSON, to the console
go run ./example -pretty    # the same, indented
go run ./example -verify -columns   # plus an independent check and the column list
```

It also reads an NDJSON file or stream, gzip or plain:

```
go run ./example -in events.ndjson.gz -out flat.ndjson -verify
gzip -dc events.ndjson.gz | go run ./example -in - > flat.ndjson
```

| Flag | Meaning |
|---|---|
| `-in` | Input file, or `-` for stdin. Default: the bundled [sample-events.jsonl](sample-events.jsonl). Gzip is detected from the data, not from the name. |
| `-out` | Output file, or `-` for stdout (default). The summary always goes to stderr. |
| `-pretty` | Indent every row, for reading on a console. |
| `-config` | A jsonflat config file. Default: the embedded [config.json](config.json). |
| `-verify` | Check every row independently with `encoding/json`: valid JSON, no repeated key. Also collects the column names. Slow, because it allocates. |
| `-columns` | With `-verify`, list every column and how many rows have it. |
| `-limit` | Stop after this many lines. |

Event exports hold real IP addresses and device IDs. `*.ndjson` and
`*.ndjson.gz` are in `.gitignore`; keep them out of the repository. The
bundled samples are seven events supplied for this example.

## The events

Two producers write into the same stream and the payload sits under
`properties`:

- the **JavaScript SDK** (`context.library.name` = `RudderLabs JavaScript SDK`)
  with `context.app`, `context.screen`, `context.page`, and
  `properties.event_details` / `device_details` / `user_details`;
- **analytics-go** with a smaller `context` and a few loose keys in
  `properties` (`appc`, `d`, `h`, `ho`, `r`, `w`, `tagId`, `_cb`).

Both carry the same fact more than once: `event` and `properties.event_name`;
`requestIp` and `context.ip`; `properties.d` and
`properties.device_details.app_bundle`; `user_details.ifa` and
`user_details.deviceid`; `context.userAgent` and
`device_details.user_agent`; and one event has both `tag_id` and `tagId`
inside `event_details`.

## What the config does

A schema in three parts: explicit **columns** with fallback sources, a
**drop** list for junk and for copies of what the columns already carry, and
**`rest`**, which catches anything neither of those mentions, so a new field
from a producer turns up in `extra` instead of vanishing. This is
[config.json](config.json):

```json
{
  "flatten": {"separator": "_", "arrays": "string", "drop_nulls": false, "drop_empty_objects": true},

  "derive": {
    "event_name": {"from": "event", "normalize": "snake", "digit_prefix": "_"}
  },

  "keys": {
    "normalize": "snake",
    "digit_prefix": "_",
    "on_collision": "first",

    "keep": [
      "id", "anonymous_id", "user_id", "type", "event", "event_text", "channel",
      "original_timestamp", "sent_at", "received_at", "timestamp", "request_ip",
      "session_id", "visit_id", "brand_id", "tag_id", "page", "station_id",
      "ad_id", "request_id", "impression_id", "ad_source", "ad_type", "provider",
      "fill_src", "src", "init_src", "delivery_method",
      "advertiser_domain", "campaign", "creative_id", "creative_version",
      "app_name", "app_version", "app_bundle", "app_store_id", "app_store_url", "app_category",
      "device_type", "os_type", "user_agent", "ifa",
      "country", "region", "city", "latitude", "longitude", "metro", "app_dma",
      "geoip_ip", "geoip_country", "geoip_region", "geoip_city", "geoip_postal",
      "geoip_latitude", "geoip_longitude",
      "gdpr", "us_privacy", "dnt",
      "width", "height", "volume", "is_muted", "passback",
      "unit_visible", "unit_truly_visible", "unit_visible_cross_origin", "unmute_blocked",
      "quartile", "latency_ms", "start_delay",
      "context_library", "context_library_version"
    ],
    "rest": "extra",

    "drop": [
      "context_app_*", "context_library_*", "context_os_*", "context_page_*",
      "context_timezone", "context_session_id", "context_user_agent",
      "context_device_advertising_id",

      "event_details_host_*", "event_details_build_id", "event_details_loop_share_string",
      "event_details_cb", "event_details_ts", "event_details_ad_url",

      "event_details_visit_id", "event_details_brand_id", "event_details_tag_id",
      "event_details_page", "event_details_ad_id", "event_details_req_id",
      "event_details_imp_id", "event_details_ad_source", "event_details_ad_type",
      "event_details_provider", "event_details_fill_src", "event_details_src", "event_details_macro_source",
      "event_details_delivery_method", "event_details_advertiser_domain",
      "event_details_campaign", "event_details_creative_id", "event_details_creative_ver",
      "event_details_app_version", "event_details_app_store_url", "event_details_metro",
      "event_details_gdpr", "event_details_us_privacy", "event_details_dnt",
      "event_details_tag_width", "event_details_tag_height", "event_details_volume",
      "event_details_is_muted", "event_details_passback", "event_details_unit_visible",
      "event_details_unit_truly_visible", "event_details_unit_visible_cross_origin",
      "event_details_unmute_blocked", "event_details_quartile", "event_details_latency_ms",
      "event_details_startdelay_iv", "event_details_stid",

      "device_details_app_name", "device_details_app_version", "device_details_app_bundle",
      "device_details_device_type", "device_details_os_type", "device_details_user_agent",
      "device_details_app_country", "device_details_app_region", "device_details_app_loc",
      "device_details_app_lat", "device_details_app_long", "device_details_app_metro", "device_details_app_dma",
      "device_details_geoip_ip", "device_details_geoip_country_code", "device_details_geoip_region",
      "device_details_geoip_city_en", "device_details_geoip_postal", "device_details_geoip_lat",
      "device_details_geoip_lng", "event_details_valid", "context_locale",

      "user_details_ifa", "user_details_deviceid", "user_details_app_store_id",

      "appc", "d", "w", "h", "ho", "r", "m", "cb"
    ]
  },

  "columns": [
    {"to": "id", "from": ["messageId", "id"], "as": "string"},
    {"to": "anonymous_id", "from": ["anonymousId", "anonymous_id"], "as": "string"},
    {"to": "user_id", "from": ["userId", "user_id"], "as": "string"},
    {"to": "type", "from": "type", "as": "string"},
    {"to": "event", "from": "$event_name"},
    {"to": "event_text", "from": "event", "as": "string"},
    {"to": "channel", "from": "channel", "as": "string"},

    {"to": "original_timestamp", "from": ["originalTimestamp", "original_timestamp"], "as": "string"},
    {"to": "sent_at", "from": ["sentAt", "sent_at"], "as": "string"},
    {"to": "received_at", "from": ["receivedAt", "received_at", "$now"], "as": "string"},
    {"to": "timestamp", "as": "string",
     "from": ["timestamp", {"fn": "clock_skew", "sent": "sentAt", "original": "originalTimestamp"}, "$now"]},
    {"to": "request_ip", "from": ["requestIp", "request_ip", "context.ip"], "as": "string"},

    {"to": "session_id", "from": "context.sessionId", "as": "string"},
    {"to": "visit_id", "from": "properties.event_details.visit_id", "as": "string"},
    {"to": "brand_id", "from": "properties.event_details.brand_id", "as": "raw"},
    {"to": "tag_id", "from": ["properties.event_details.tag_id", "properties.tagId"], "as": "string"},
    {"to": "page", "from": "properties.event_details.page", "as": "string"},
    {"to": "station_id", "from": "properties.event_details.stid", "as": "string"},

    {"to": "ad_id", "from": "properties.event_details.ad_id", "as": "string"},
    {"to": "request_id", "from": "properties.event_details.req_id", "as": "string"},
    {"to": "impression_id", "from": "properties.event_details.imp_id", "as": "string"},
    {"to": "ad_source", "from": "properties.event_details.ad_source", "as": "string"},
    {"to": "ad_type", "from": "properties.event_details.ad_type", "as": "string"},
    {"to": "provider", "from": "properties.event_details.provider", "as": "string"},
    {"to": "fill_src", "from": "properties.event_details.fill_src", "as": "string"},
    {"to": "src", "from": "properties.event_details.src", "as": "string"},
    {"to": "init_src", "from": "properties.event_details.macro_source", "as": "string"},
    {"to": "delivery_method", "from": "properties.event_details.delivery_method", "as": "string"},

    {"to": "advertiser_domain", "from": "properties.event_details.advertiser_domain", "as": "string"},
    {"to": "campaign", "from": "properties.event_details.campaign", "as": "string"},
    {"to": "creative_id", "from": "properties.event_details.creative_id", "as": "string"},
    {"to": "creative_version", "from": "properties.event_details.creative_ver", "as": "string"},

    {"to": "app_name", "from": "properties.device_details.app_name", "as": "string"},
    {"to": "app_version", "from": ["properties.device_details.app_version", "properties.event_details.app_version"], "as": "string"},
    {"to": "app_bundle", "from": ["properties.device_details.app_bundle", "properties.d"], "as": "string"},
    {"to": "app_store_id", "from": "properties.user_details.app_store_id", "as": "string"},
    {"to": "app_store_url", "from": "properties.event_details.app_store_url", "as": "string"},
    {"to": "app_category", "from": "properties.appc", "as": "string"},

    {"to": "device_type", "from": "properties.device_details.device_type", "as": "string"},
    {"to": "os_type", "from": "properties.device_details.os_type", "as": "string"},
    {"to": "user_agent", "from": ["properties.device_details.user_agent", "context.userAgent"], "as": "string"},
    {"to": "ifa", "from": ["properties.user_details.ifa", "properties.user_details.deviceid", "context.device.advertisingId"], "as": "string"},

    {"to": "country", "from": "properties.device_details.app_country", "as": "string"},
    {"to": "region", "from": "properties.device_details.app_region", "as": "string"},
    {"to": "city", "from": "properties.device_details.app_loc", "as": "string"},
    {"to": "latitude", "from": "properties.device_details.app_lat", "as": "string"},
    {"to": "longitude", "from": "properties.device_details.app_long", "as": "string"},
    {"to": "metro", "from": ["properties.device_details.app_metro", "properties.event_details.metro"], "as": "string"},
    {"to": "app_dma", "from": "properties.device_details.app_dma", "as": "string"},

    {"to": "geoip_ip", "from": "properties.device_details.geoip.ip", "as": "string"},
    {"to": "geoip_country", "from": "properties.device_details.geoip.country_code", "as": "string"},
    {"to": "geoip_region", "from": "properties.device_details.geoip.region", "as": "string"},
    {"to": "geoip_city", "from": "properties.device_details.geoip.city_en", "as": "string"},
    {"to": "geoip_postal", "from": "properties.device_details.geoip.postal", "as": "string"},
    {"to": "geoip_latitude", "from": "properties.device_details.geoip.lat", "as": "raw"},
    {"to": "geoip_longitude", "from": "properties.device_details.geoip.lng", "as": "raw"},

    {"to": "gdpr", "from": "properties.event_details.gdpr", "as": "string"},
    {"to": "us_privacy", "from": "properties.event_details.us_privacy", "as": "string"},
    {"to": "dnt", "from": "properties.event_details.dnt", "as": "string"},

    {"to": "width", "from": ["properties.event_details.tag_width", "properties.w"], "as": "string"},
    {"to": "height", "from": ["properties.event_details.tag_height", "properties.h"], "as": "string"},
    {"to": "volume", "from": "properties.event_details.volume", "as": "raw"},
    {"to": "is_muted", "from": "properties.event_details.is_muted", "as": "raw"},
    {"to": "passback", "from": "properties.event_details.passback", "as": "raw"},

    {"to": "unit_visible", "from": "properties.event_details.unit_visible", "as": "raw"},
    {"to": "unit_truly_visible", "from": "properties.event_details.unit_truly_visible", "as": "raw"},
    {"to": "unit_visible_cross_origin", "from": "properties.event_details.unit_visible_cross_origin", "as": "raw"},
    {"to": "unmute_blocked", "from": "properties.event_details.unmute_blocked", "as": "raw"},

    {"to": "quartile", "from": "properties.event_details.quartile", "as": "string"},
    {"to": "latency_ms", "from": "properties.event_details.latency_ms", "as": "string"},
    {"to": "start_delay", "from": "properties.event_details.startdelay_iv", "as": "string"},

    {"to": "context_library", "from": "context.library.name", "as": "string"},
    {"to": "context_library_version", "from": "context.library.version", "as": "string"}
  ],

  "sections": [
    {"id": "context", "from": "context", "prefix": "context"},
    {"id": "properties", "from": "properties"}
  ],

  "rules": [
    {"op": "drop", "path": "properties.event_name"},
    {"op": "drop", "path": "context.ip"}
  ],

  "expose": ["messageId", "anonymousId"],
  "newline": true
}
```

```example
in:  {"type":"track","messageId":"m-1","anonymousId":"a-1","userId":"","event":"Ad Error","channel":"web",
      "originalTimestamp":"2026-09-11T16:59:39.334Z","sentAt":"2026-09-11T16:59:39.338Z",
      "receivedAt":"2026-09-11T16:59:40.290Z","timestamp":"2026-09-11T16:59:40.290Z","requestIp":"10.0.0.1",
      "context":{"traits":{},"ip":"10.0.0.1","userAgent":"UA-1","library":{"name":"RudderLabs JavaScript SDK"},"screen":{"width":427}},
      "properties":{"event_name":"Ad Error","event_details":{"tag_id":"t-1","tagId":"t-1","brand_id":3252,"is_muted":true,"ad_slots":"5"},
                    "device_details":{"app_bundle":"com.example.app","user_agent":"UA-1","geoip":{"ip":"1.2.3.4"}},
                    "user_details":{"ifa":"ifa-1","deviceid":"ifa-1","app_store_id":"s-1"}},
      "integrations":{"All":true}}
out: {"id":"m-1","anonymous_id":"a-1","user_id":"","type":"track","event":"ad_error","event_text":"Ad Error","channel":"web","original_timestamp":"2026-09-11T16:59:39.334Z","sent_at":"2026-09-11T16:59:39.338Z","received_at":"2026-09-11T16:59:40.290Z","timestamp":"2026-09-11T16:59:40.290Z","request_ip":"10.0.0.1","brand_id":3252,"tag_id":"t-1","app_bundle":"com.example.app","app_store_id":"s-1","user_agent":"UA-1","ifa":"ifa-1","geoip_ip":"1.2.3.4","is_muted":true,"context_library":"RudderLabs JavaScript SDK","extra":"{\"context_screen_width\":427,\"event_details_ad_slots\":\"5\"}"}\n
```

- **`derive.event_name`** snake-cases the event name once per record
  (`Ad Media Quartile` → `ad_media_quartile`, `px-lo` → `px_lo`); the `event`
  column reads it as `$event_name` and `event_text` keeps the original. The
  same value could name an output (`{"name": "$event_name"}`) for one table
  per event.
- **Columns** are the schema. Each reads one or more **source paths** (dots,
  the producer's spelling) in order of preference, so a fact that two
  producers put in different places lands in one column: `app_bundle` from
  `device_details.app_bundle` or `d`, `user_agent` from the device details or
  `context.userAgent`, `ifa` from `ifa`, `deviceid` or the context's
  advertising ID, `request_ip` from `requestIp` or `context.ip`.
- **`as`** is `"string"` for text and for values whose type differs between
  producers (`width` is `300` from one SDK and `"320"` from the other;
  `latitude` is always a string); `"raw"` where the source is already typed
  (`brand_id`, `volume`, the booleans, `geoip_latitude`). There is no cast:
  values keep the type they came with, or become strings.
- **`keys.drop`** lists **output keys** (after snake_case, so
  `context_app_*`, not `context.app_name`). Two kinds of entry: junk
  (`event_details_host_*`, `context_page_*`, the loose `w`, `h`, `ho`, `r`)
  and every flattened copy of a column's source (`event_details_brand_id`,
  `device_details_app_bundle`, `user_details_deviceid`, …). A column reserves
  its own name but not its sources' names; without these entries the copies
  would fill `extra`. Dropped keys are never collected.
- **`rest: "extra"`** catches everything else: on the sample events that is
  `context_screen_*`, `event_details_m`, `event_details_ad_slots`,
  `user_details_user_id`, … New producer fields land there without a config
  change. When one earns a column, add the column and a `drop` entry for its
  flattened copy, as `station_id` and `app_dma` show; when one is noise, add
  it to `drop` alone, as `event_details_valid` and `context_locale` are.
- **`keep`** names the columns. Columns bypass `keep` anyway, so its only job
  here is to turn the whitelist on, which `rest` requires; every non-column
  key is unmapped by definition.
- The two `rules` drop `properties.event_name` and `context.ip` at the
  source, since `event_text` and `request_ip` carry them.
- `properties.tagId` on the analytics-go events becomes `tag_id`, a column
  name, so it is dropped in the column's favour and counted: the summary's
  "merged keys 1".

## Keeping only some columns, with short names

`-config` takes any jsonflat config. This one writes a handful of columns per
event and gives two of them short names. `keys.keep` is a whitelist of
**output** keys, matched after normalisation and aliases, so it lists the new
names:

```json
{
  "flatten": {"separator": "_", "drop_nulls": true, "drop_empty_objects": true},
  "keys": {
    "normalize": "snake",
    "on_collision": "first",
    "aliases": [{"to": "app", "from": ["device_details_app_name"]}],
    "keep": ["app", "event_details_tag_id"],
    "rest": "extra"
  },
  "columns": [
    {"to": "id", "from": "messageId", "as": "string"},
    {"to": "event", "from": "event", "as": "string"},
    {"to": "timestamp", "from": ["timestamp", "$now"], "as": "string"}
  ],
  "sections": [{"from": "properties"}],
  "rules": [
    {"op": "merge", "mode": "first", "to": "bundle",
     "from": ["properties.device_details.app_bundle", "properties.d"]}
  ],
  "expose": ["messageId"],
  "newline": true
}
```

```example
in:  {"type":"track","messageId":"m-1","event":"px-lo","timestamp":"2026-09-11T16:59:39Z",
      "context":{"library":{"name":"analytics-go"},"ip":"10.0.0.1"},
      "properties":{"event_name":"px-lo","d":"com.example.app","appc":"IAB9",
                    "device_details":{"app_bundle":"com.example.app","app_name":"Wordle!"},
                    "event_details":{"tag_id":"t-1","valid":"false"}}}
out: {"id":"m-1","event":"px-lo","timestamp":"2026-09-11T16:59:39Z","app":"Wordle!","event_details_tag_id":"t-1","bundle":"com.example.app","extra":"{\"event_name\":\"px-lo\",\"appc\":\"IAB9\",\"event_details_valid\":\"false\"}"}\n
```

- `device_details_app_name` is a flattened key, so an **alias** renames it to
  `app`, and `keep` lists `app`.
- `bundle` is a **merge result**, so it is renamed through the merge's `to`
  and is written regardless of `keep`; aliases never see merge results. The
  merge consumed `d` and `app_bundle`, so neither is a leftover.
- Columns (`id`, `event`, `timestamp`) are explicit and bypass `keep` too.
- Everything else the walk produces (`event_name`, `appc`,
  `event_details_valid`) is not lost: `rest` collects it into the `extra`
  column as one JSON string. Leave `rest` out to drop those keys silently.
  The `context` block is outside the one section and is never walked.

Save it as `keep.json` and run
`go run ./example -config keep.json -verify -columns`; the column list at the
end is exactly the kept set plus the columns, the merge result and `extra`.

## What the code shows

[main.go](main.go) is the usual way to drive `jsonflat` over a stream:

- Compile the config once and reuse the `Transformer`.
- Create the callback once, outside the loop, and call `Each` per line. The
  loop then does not allocate (unless `-pretty` asks for indentation).
- `Record.JSON` is valid only until the callback returns, so the callback
  writes it out (a `bufio.Writer` copies it) and keeps nothing.
- One bad line or bad record never stops the run. An error from `Each` means
  the line is not valid JSON; `Record.Err` means the record itself was
  rejected. Both are counted, and the first few are printed with their line
  number and the exposed fields.

## Sample run

The seven bundled events, `go run ./example -verify`:

```
lines            7
rows written     7
bad lines        0
failed records   0
merged keys      1   (same name after normalising; the first value is kept)
verify failures  0
distinct columns 73
```

On a 14 MB gzip export with 105,541 events (128 MB unpacked, Apple M5, Go 1.26,
one goroutine) the same loop with a config for that export ran at about
210,000 lines/s (250 MB/s) including decompression.
