---
title: Config reference
nav_order: 2
---

# Config reference
{: .no_toc }

Everything a config can say, in the order the sections appear in a config file.
Each behaviour names the test that proves it; every JSON block on this page is
compiled, and every `example` block is replayed, by `docs_test.go`.

<details open markdown="block">
  <summary>Contents</summary>
  {: .text-delta }
- TOC
{:toc}
</details>

## A config is a JSON object

```json
{
  "input":    {"explode": "batch", "inherit": ["sentAt"]},
  "flatten":  {"separator": "_", "arrays": "string", "drop_nulls": true},
  "keys":     {"normalize": "snake", "on_collision": "first", "keep": ["id", "event", "context_*"]},
  "derive":   {"table": {"from": "event", "normalize": "snake", "required": true}},
  "columns":  [{"to": "id", "from": "messageId", "as": "string"}],
  "sections": [{"id": "context", "from": "context", "prefix": "context"}],
  "rules":    [{"op": "drop", "path": "context.ip"}],
  "outputs":  [{"name": "$table"}],
  "expose":   ["messageId"],
  "newline":  false
}
```

- Every section is optional and they combine freely. The empty config `{}`
  flattens the whole document with `.` between key segments.
- `Compile` is strict: an unknown field anywhere, or data after the closing
  brace, is an error. A misspelt section is reported, not ignored.
  *`TestCompileErrors`*
- `New` takes the same thing as a Go `Config` value; see
  [Building a config in Go](#building-a-config-in-go).
- A config is compiled once into a `Transformer`, which is immutable and safe
  for concurrent use. Compile, keep, reuse.

## Two kinds of path

This is the rule that trips people up, so it comes first.

| | Source paths | Output keys |
|---|---|---|
| **Where** | `input.explode`, `input.inherit`, `sections[].from`, `rules[].path` and `from`, every source (`columns[].from`, `derive.*.from`, `expose`, `when.path`, `clock_skew` arguments) | `rules[].to`, the `path` of a `default` rule, `columns[].to`, `aliases[].to`, `keys.keep`, `keys.drop`, `keys.rest`, `sections[].prefix` |
| **Spelling** | Keys as the input spells them, joined with `.` whatever `flatten.separator` is. Array elements by index: `tags.0`. | Used exactly as given, joined to nothing. Compared against keys **after** normalisation and aliases. |
| **Think of it as** | An address in the document | The column name in the row |

```json
{
  "flatten": {"separator": "__"},
  "rules": [
    {"op": "drop",  "path": "a.secret"},
    {"op": "merge", "from": ["a.first", "a.last"], "to": "name", "sep": " "}
  ]
}
```

```example
in:  {"a":{"first":"Ada","secret":1,"last":"L","x":[1]}}
out: {"a__x__0":1,"name":"Ada L"}
```

The rule paths say `a.secret` and `a.first` although the output uses `__`.
*`TestKeysWithAppend/"rule paths use dots whatever the separator is"`*

## What happens to one record

1. The record must be an object or an array, else the record fails with
   `ErrRootNotContainer`.
2. `expose` is resolved into `Record.Fields`, before anything can fail, so an
   error record still carries its IDs. *`TestFieldsOnErrorRecords`*
3. `derive` values are computed, in name order.
4. Conditions decide which alias groups, columns and sections are active for
   this record. A condition is evaluated once per record however often the
   config repeats it.
5. For each output whose `when` passes, in config order, one row is built:
   `{`, the active columns in order, the output's sections in order, merge
   results in rule order, defaults in rule order, the `keys.rest` column if
   set, `}`, and `"\n"` if `newline`.
6. No configured output matched: the record fails with `ErrNoOutput`. Without
   an `outputs` section there is one implicit output that always matches and
   has no name.

All rows of a record are built before any is delivered. If one row fails, the
callback gets one `Record` with `Err` set and none of the rows.
*`TestRudderBatch`, `TestRudderCollisionPolicy`*

## Key correction, step by step

Every flattened leaf goes through these steps, in this order, before it is
written. Columns, merge results and defaults skip steps 1 to 6 and go through
7 and 8 only.

```
 walk               1 normalize each segment (snake)
                    2 segment_aliases
                    3 join with flatten.separator            = the output key
 at the leaf        4 aliases on the whole key
                    5 digit_prefix
                    6 drop, then keep (what keep removes -> keys.rest, if set)
                    7 reserved by an active column?  -> dropped, counted
                    8 on_collision                   -> keep | first | error
 write              "key":value
```

*`TestKeysWithAppend/"aliases, segment aliases, digit prefix, drop"`,
`TestFlattenEdgeCases/"keep is matched after aliases and the digit prefix"`*

---

## `input`

How one document becomes one or more records.

| Field | Type | Default | Meaning |
|---|---|---|---|
| `explode` | source path | none | Path of an array whose elements are the records. The enclosing document becomes the *envelope*. If the path is absent from a document, the document itself is the single record, index 0. Present but not an array: `Each` returns `ErrExplode`. |
| `inherit` | list of top-level keys | `[]` | A source lookup of exactly that one-segment path that finds nothing in the record (missing or `null`) falls back to the envelope. Applies to sources only, not to section walks. Entries must be single keys without dots. |

```json
{
  "input": {"explode": "rows", "inherit": ["tenant"]},
  "columns": [{"to": "tenant", "from": "tenant"}],
  "expose": ["tenant"]
}
```

```example
in: {"tenant":"env","rows":[{"a":1},{"tenant":"own"},{"tenant":null}]}
row: {"tenant":"env","a":1}
  fields: env
row: {"tenant":"own"}
  fields: own
row: {"tenant":"env"}
```

The third record has `"tenant": null`, which counts as missing, so it inherits.
Without the `rows` array the document is the record and has no envelope.
*`TestInheritFallsBackOnlyWhenMissing`, `TestExplodeInheritColumns`*

Errors: `inherit` entry empty or containing a dot.

## `flatten`

How nested values become flat keys.

| Field | Type | Default | Meaning |
|---|---|---|---|
| `separator` | string | `"."` | Joins output key segments. |
| `arrays` | `"index"` \| `"raw"` \| `"string"` | `"index"` | `index`: elements become `key.0`, `key.1`, … `raw`: the array is written as one JSON value. `string`: the array is written as a JSON **string** (`"[{\"sku\":\"A1\"}]"`), which gives a typed column one stable type. An empty array is `[]`, or `"[]"` in string mode. |
| `max_depth` | integer ≥ 0 | `0` (no limit) | Container levels to flatten inside a section; the section root is level 1. A container that would be level `max_depth + 1` is written as raw JSON (or, for an array in `string` mode, as a JSON string). |
| `drop_nulls` | bool | `false` | Leave out keys whose value is `null`. |
| `drop_empty_objects` | bool | `false` | Leave out keys whose value is `{}`. |

Without the two `drop_` options, `null`, `{}` and `[]` are written as values, so
no key is lost. *`TestTransform/"empty containers are kept as values"`*

```json
{"flatten": {"separator": "__"}}
```

```example
in:  {"a":{"b":[1]}}
out: {"a__b__0":1}
```

```json
{"flatten": {"arrays": "raw"}}
```

```example
in:  {"a":{"b":[1,{"c":"q\"x"}]}}
out: {"a.b":[1,{"c":"q\"x"}]}
```

```json
{"flatten": {"arrays": "string", "max_depth": 1}}
```

```example
in:  {"o":{"a":[1]},"a":[{"b":"c"}]}
out: {"o":{"a":[1]},"a":"[{\"b\":\"c\"}]"}
```

At the depth limit only arrays become strings; an object stays raw JSON.
*`TestFlattenEdgeCases/"string arrays: at the depth limit only arrays become strings"`*

```json
{"flatten": {"max_depth": 2}}
```

```example
in:  {"a":{"b":{"c":1},"d":2},"e":3}
out: {"a.b":{"c":1},"a.d":2,"e":3}
```

Two more things the flattener guarantees:

- **A root array** is flattened with keys `0`, `1`, … and is always entered,
  whatever `arrays` says, because there is no key yet to write it under.
  *`TestTransform/"root array"`, `TestFlattenEdgeCases/"a root array is entered whatever the arrays mode is"`*
- **An empty key does not swallow the separator**: `{"":{"a":1}}` gives
  `{".a":1}`. Whether a separator goes in front of a segment depends on the
  depth, never on whether the path so far is empty.
  *`TestTransform/"empty key does not swallow the separator"`*
- **Number text is copied unchanged**, so `12345678901234567890.123456789` and
  `1E+2` survive; every number is checked against RFC 8259 and a bad one
  (`00`, `+1`, `NaN`) fails the record with `ErrInvalidNumber`.
  *`TestTransform/"number text is preserved"`, `TestErrors`, `TestInvalidNumberEverywhere`*

Errors: unknown `arrays` mode; negative `max_depth`.

## `keys`

Output-side correction, applied to every flattened leaf (steps 1 to 8 above).

| Field | Type | Default | Meaning |
|---|---|---|---|
| `normalize` | `"none"` \| `"snake"` | `"none"` | `snake` normalises each object key segment (not array indices). |
| `digit_prefix` | string | `""` | Put in front of an output key that starts with a digit (step 5). |
| `on_collision` | `"keep"` \| `"first"` \| `"error"` | `"keep"` | What to do when a key already written in this row comes up again (step 8). |
| `segment_aliases` | object: segment → segment | `{}` | Replace one (normalised) key segment wherever it appears (step 2). |
| `aliases` | list of `{to, from, when?}` | `[]` | Replace a full output key (step 4). |
| `keep` | list of output keys | `[]` (keep all) | Whitelist. When set, a flattened key is written only if it matches an entry: exact, or prefix for entries ending in `*` (step 6). |
| `drop` | list of output keys | `[]` | Blacklist. A flattened key that matches is left out (step 6). |
| `rest` | output key | none | With `keep`: the name of a column that collects every key `keep` left out, as a JSON string of one flat object, so nothing is lost. |

### `normalize: "snake"`

`appendSnake` in `names.go` does this, and `TestKeysWithAppend/"snake and string arrays"`
covers it:

| Input | Output |
|---|---|
| `productId`, `Product ID`, `product-id` | `product_id` |
| `userID` | `user_id` |
| `HTTPServer` | `http_server` |
| `v2Beta` | `v2_beta` |
| `address1` | `address1` |
| `$price` | `price` |
| `$$` | *(nothing: the key and everything below it are skipped)* |

Any run of characters that are not letters or digits becomes one underscore;
leading and trailing underscores are removed; bytes ≥ 0x80 are copied unchanged
and treated as lower-case letters.

```json
{"flatten": {"separator": "_", "arrays": "string", "drop_nulls": true, "drop_empty_objects": true},
 "keys": {"normalize": "snake"}}
```

```example
in:  {"userID":1,"Home Address":{"zipCode":"38","$$":{"x":1}},"tags":["a",{"b":"\u0001"}],"n":null,"e":{},"ea":[]}
out: {"user_id":1,"home_address_zip_code":"38","tags":"[\"a\",{\"b\":\"\\u0001\"}]","ea":"[]"}
```

### `on_collision`

```json
{"flatten": {"separator": "_"}, "keys": {"normalize": "snake", "on_collision": "first"}}
```

```example
in:  {"productId":1,"product_id":2,"a":{"b":3},"a_b":4}
out: {"product_id":1,"a_b":3}
```

- `keep` (default): duplicates are written as they come and nothing is
  tracked, so a plain flatten costs nothing extra. `{"aB":1,"a_b":2}` gives
  `{"a_b":1,"a_b":2}`. *`TestKeysWithAppend/"collision keep is the default"`*
- `first`: the first value wins; each later one is counted in `Record.Dropped`.
  *`TestKeysWithAppend/"collision first"`*
- `error`: the record fails with `ErrCollision`. *`TestKeysWithAppend`*

A clash with a **column** name is never an error, whatever the policy: the
column wins and the key is counted (see [`columns`](#columns)).
*`TestRudderCollisionPolicy`*

### `segment_aliases`, `aliases`, `digit_prefix`, `drop`

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

```example
in:  {"adress":{"postcode":"38","city":"A"},"2fa":true,"secret":1,"internal":{"a":1,"b":{"c":2}},"internals":3}
out: {"address_zip":"38","address_city":"A","n2fa":true,"internals":3}
```

- With `normalize: "snake"`, alias and segment-alias `from` entries are
  normalised when the config is compiled, so write them the way the producer
  writes them (`Address ZIP Code`). Without it they match literally:
  `{"to":"id","from":["ID"]}` matches `ID` and not `Id`.
  *`TestKeysWithAppend/"aliases without normalising match the literal key"`*
- Alias `from` entries are normalised segment by segment, split on the
  separator, so `userInfo.ZIP Code` matches `user_info.zip_code`.
  *`TestFlattenEdgeCases/"alias from entries are normalised segment by segment"`*
- Alias `from` entries do **not** go through `segment_aliases`. Write them as
  the key looks after step 2: with the segment alias `adress → address`, the
  alias entry is `address_postcode`.
- An alias may have a `when` condition. Groups with a condition are tried
  first, in config order, then the group without one. *`TestRudderBatch`*
- `drop` matches the whole key (`secret`) or a prefix (`internal_*`).
  `internals` stays because it does not start with `internal_`.
- There is no fuzzy or edit-distance matching, on purpose: a wrong guess would
  corrupt a column silently.

### `keep`

A whitelist of output keys, written as they look after steps 1 to 5. When it
is set, a flattened key is written only if it matches `keep` **and** does not
match `drop`.

```json
{"flatten": {"separator": "_"}, "keys": {"normalize": "snake", "keep": ["user_id", "tags_*"]}}
```

```example
in:  {"userId":1,"tags":["a","b"],"x":{"y":2},"tagsx":3}
out: {"user_id":1,"tags_0":"a","tags_1":"b"}
```

```json
{"flatten": {"separator": "_"}, "keys": {"keep": ["context_*"], "drop": ["context_ip"]}}
```

```example
in:  {"context":{"ip":"1.2.3.4","app":{"name":"Shop"}},"event":"x"}
out: {"context_app_name":"Shop"}
```

- Keys that `keep` or `drop` remove are silent: not counted in
  `Record.Dropped`, and they take no part in collisions.
  *`TestKeepIsSilent`, `TestFlattenEdgeCases/"a key that keep removes takes no part in collisions"`*
- Columns, merge results and defaults are explicit and bypass both lists.
  *`TestFlattenEdgeCases/"columns, merge results and defaults are written whether kept or not"`*
- `keep` is matched after aliases and the digit prefix, so list the new names.
  *`TestFlattenEdgeCases/"keep is matched after aliases and the digit prefix"`*
- The filters do not allocate. *`TestKeepZeroAllocs`*

### `rest`

A whitelist throws information away. `rest` names a column that catches it:
every flattened key that `keep` left out is collected, under its final name,
into one JSON **string** holding a flat object. One column, one type, and a
downstream job can still parse it.

```json
{"flatten": {"separator": "_"},
 "keys": {"normalize": "snake", "digit_prefix": "_",
          "aliases": [{"to": "zip", "from": ["address_postcode"]}],
          "keep": ["user_id", "zip"], "rest": "extra"}}
```

```example
in:  {"userId":1,"address":{"postcode":"38","city":"A"},"2fa":true,"tags":["x"]}
out: {"user_id":1,"zip":"38","extra":"{\"address_city\":\"A\",\"_2fa\":true,\"tags_0\":\"x\"}"}
```

- Written at the end of the row, after merge results and defaults, and only
  when something was left out. Values keep the form a flattened leaf would
  have had (arrays as strings in `"string"` mode).
- Not collected: keys that `drop` matched (removed on purpose), copies of an
  active column (duplicates, see `columns`), and keys outside every section
  (never walked). Duplicates among the collected keys are kept as they come.
- `rest` is explicit like a column: it bypasses `keep` and `drop` itself and
  goes through column reservation and `on_collision`.
- Rows cannot extend a previous row when `rest` is set (it sits at the end),
  which changes nothing in the output. *`TestRestPerOutput`*
- *`TestFlattenEdgeCases/"rest collects what keep removed…"`, `"rest takes
  nothing that drop or a column removed"`, `"rest is absent when nothing was
  removed"`, `"rest keeps the string form of arrays"`, `TestKeepZeroAllocs`*

### `keys` errors

*`TestNewConfigErrors`, `TestConfigValidation`*

| Config | Error |
|---|---|
| `normalize` or `on_collision` with an unknown value | unknown mode / policy |
| `segment_aliases` entry with an empty side, or mapping a segment to itself, or two entries that normalise alike | rejected |
| alias with empty `to` or empty `from` | rejected |
| alias `from` entry that normalises to nothing (`$$`), or that already equals `to` (`productId → product_id` under snake), or listed twice in one alias | rejected |
| alias `to` that is a column name | `"id" is a column` |
| alias `to` that `keep` does not match, or that `drop` matches | the alias would be dead |
| `keep` or `drop` entry `""` or `"*"` | matches every key |
| an entry listed twice, or the same entry in both `keep` and `drop` | rejected |
| `rest` without `keep`, or named like a column, or the target of an alias | rejected |

## Sources

A *source* says where a value comes from. Columns, `derive`, `expose` and
conditions all take sources. Wherever a list is accepted, one source may be
written without the brackets: `"from": "userId"` is `"from": ["userId"]`.

| Form | Value |
|---|---|
| `"user.id"` | The value at that source path in the record. `null` counts as missing. Falls back to the envelope for `input.inherit` keys. |
| `"$now"` | `Options.Now` in UTC as `2006-01-02T15:04:05.000Z`; the zero time means `time.Now()`. Formatted at most once per call. |
| `"$name"` | The derived value `name` if `derive` defines one, otherwise `Options.Vars["name"]`. An empty value counts as missing. |
| `{"fn": "clock_skew", "sent": P1, "original": P2}` | `now - (sent - original)` in the `$now` layout, when both paths hold RFC 3339 strings; otherwise missing. Corrects timestamps taken on a client whose clock is off. |

A list is tried in order and the first source with a value wins.

Two notions matter here:

- The **text** of a source is what `equals`, `in`, `as: "string"`, `derive` and
  `expose` see: a string's bytes, or a number's text if it is a valid number.
  Booleans, objects and arrays have no text, so they are skipped in a list.
- The **presence** of a source is what `exists` sees: any value that is not
  missing and not `null`, objects and arrays included.

```json
{"columns": [{"to": "ts", "from": [{"fn": "clock_skew", "sent": "sent", "original": "orig"}, "orig", "$now"]}],
 "sections": [{"from": "none"}]}
```

```example
now: 2026-09-21T10:00:05.5Z
in:  {"sent":"2026-09-21T11:00:02Z","orig":"2026-09-21T11:00:00Z"}
out: {"ts":"2026-09-21T10:00:03.500Z"}
```

The client clock was an hour fast: it sent at 11:00:02 what the server received
at 10:00:05.5, so the event happened 2 seconds before receipt, at 10:00:03.5.
With `"sent":"garbage"` the function has no value and the list falls through
to `orig`. *`TestClockSkew`*

## Conditions: `when`

A condition tests one value of the record. It has a `path` (one source or a
list, tried in order) and exactly one of:

| Field | Passes when |
|---|---|
| `equals` | the text of the first source that has text equals the string |
| `in` | that text is one of the strings |
| `exists` | `true`: some source is present (any type, objects included). `false`: none is. |

```json
{"columns": [{"to": "c", "from": "$now", "when": {"path": ["a", "b"], "exists": false}}],
 "sections": [{"from": "none"}]}
```

```example
now: 2026-09-21T10:00:05.5Z
in:  {"a":null}
out: {"c":"2026-09-21T10:00:05.500Z"}
```

Equal conditions anywhere in the config are compiled once and evaluated once
per record; a real config repeats `"type is track"` dozens of times.
*`TestOutputsSectionsConditions`, `TestFlattenEdgeCases/"exists false, and null counts as missing"`*

Errors: none or more than one of `equals`/`in`/`exists`; empty `in`; a `path`
that reads a derived value computed later than the condition is first needed.

## `derive`

A map from name to a value computed once per record, usable as `"$name"` in any
source and as an output name.

| Field | Type | Default | Meaning |
|---|---|---|---|
| `from` | sources | required | The first source with text wins. |
| `normalize` | `"none"` \| `"snake"` | `"none"` | Normalise the text. |
| `reserved` | list of strings | `[]` | If the result is one of these, `reserved_prefix` goes in front. |
| `reserved_prefix` | string | `""` | See `reserved`. |
| `digit_prefix` | string | `""` | Otherwise, if the result starts with a digit, this goes in front. |
| `required` | bool | `false` | An empty value fails the record with `ErrRequired` (only when `when` passes). |
| `when` | condition | none | When it fails, the value is empty and `required` does not apply. |

Values are computed in **name order**, and a value may read (`$other`) only
derived values that sort before it. Names `""` and `"now"` are not allowed.
*`TestDerive`, `TestDeriveOrder`, `TestConfigValidation`*

```json
{
  "derive": {"table": {"from": ["kind", "$kind"], "normalize": "snake", "digit_prefix": "t",
                       "reserved": ["default"], "reserved_prefix": "x_", "required": true}},
  "columns": [{"to": "table", "from": "$table"}],
  "sections": [{"from": "data"}],
  "outputs": [{"name": "$table"}]
}
```

```example
var kind=fromVar
in: {"kind":"Order Placed","data":{"a":1}}
row order_placed: {"table":"order_placed","a":1}
```

```example
var kind=fromVar
in: {"kind":"3D","data":{"table":9}}
row t3_d: {"table":"t3_d"}
```

```example
var kind=fromVar
in: {"kind":"Default"}
row x_default: {"table":"x_default"}
```

```example
var kind=fromVar
in: {"data":{"a":1}}
row from_var: {"table":"from_var","a":1}
```

```example
in: {"kind":" - "}
err: ErrRequired
```

In the second example `data.table` is dropped because `table` is a column name.
In the last one the name normalises to nothing and `required` fails the record.

## `columns`

Explicit output keys, written first, in config order. A column is *active*
when its `when` passes or it has none.

| Field | Type | Default | Meaning |
|---|---|---|---|
| `to` | output key | required | The column name. |
| `from` | sources | required | In order of preference; the first with a value wins. |
| `as` | `"raw"` \| `"string"` | `"raw"` | `raw`: a path source is written as it is, any type; `$` and `fn` sources are written as a JSON string. `string`: the text of the first source that has one, as a JSON string, so `"userId": 42` becomes `"42"`. |
| `when` | condition | none | Condition for the column to be active. |
| `quiet` | bool | `false` | Keys dropped in favour of this column are not counted in `Dropped`. |

**Column names are reserved whether or not the column had a value.** A
flattened key, merge result or default whose output key is the name of an
active column is dropped and counted in `Record.Dropped`, unless the column or
the current section is `quiet`. This is never an error, even with
`on_collision: "error"`. With the RudderStack preset this is what stops a
property called `user_id` from becoming the `user_id` column on an event
without a `userId`.
*`TestFlattenEdgeCases/"a column without a value is still reserved"`, `TestRudderCollisionPolicy`*

```json
{"columns": [{"to": "id", "from": "missing"}]}
```

```example
in:  {"id":1,"x":2}
out: {"x":2}
```

```json
{"columns": [{"to": "o", "from": "obj"}, {"to": "s", "from": ["obj", "flag", "n"], "as": "string"}],
 "sections": [{"from": "none"}]}
```

```example
in:  {"obj":{"k":[1]},"flag":true,"n":1.50}
out: {"o":{"k":[1]},"s":"1.50"}
```

A raw column keeps the value's type; a string column needs text, so it skips
the object and the boolean and takes the number.
*`TestFlattenEdgeCases/"a raw column keeps the type, a string column needs text"`*

Two columns may share a name (for example one per condition); the first active
one that has a value is written.
*`TestFlattenEdgeCases/"of two columns with one name, the first with a value wins"`*

Errors: empty `to`; unknown `as`; bad source; an alias whose `to` is a column.

## `sections`

Which parts of the record are flattened, and under which prefix.

| Field | Type | Default | Meaning |
|---|---|---|---|
| `id` | string | none | Name that `outputs[].sections` refers to. |
| `from` | source path | `""` (the whole record) | The subtree to flatten. |
| `prefix` | output key | `""` | Put in front of every key of this section, joined with the separator. |
| `when` | condition | none | Condition for the section to be walked. |
| `quiet` | bool | `false` | Collisions inside this section are neither counted nor turned into errors. Meant for a second copy of data that is expected to repeat. |

Without `sections` there is one implicit section for the whole record with no
prefix. **Anything outside the listed sections is not walked**, which is how
unknown top-level fields get dropped. A missing subtree, or one that is not an
object or array, is skipped. A `drop` rule at or above a section's `from`
disables the section.

```json
{"sections": [{"from": "items", "prefix": "item"}]}
```

```example
in:  {"items":[{"sku":"A"},"x"],"other":1}
out: {"item.0.sku":"A","item.1":"x"}
```

*`TestFlattenEdgeCases/"a section root array is flattened under its prefix"`,
`TestFlattenEdgeCases/"a dropped subtree takes the sections below it with it"`*

Errors: an `id` used twice; a bad `when`.

## `rules`

Source-side steps, matched by **source path** during the walk. Each rule is an
object with an `op` and the fields for that op.

| `op` | Fields | Meaning |
|---|---|---|
| `rename` | `from` (one source path), `to` (output key) | Renames the exact path, or the whole subtree below it: `to` replaces the entire output key built so far, and the children are joined below it. `to` is used as given, not normalised. Lookup uses the source path, so nested renames work. |
| `drop` | `path` (source path) | Removes the path or the whole subtree. Drop wins: a merge source below a dropped subtree counts as missing. |
| `merge` | `from` (source paths), `to` (output key), `mode`, `sep`, `keep_sources` | Combines scalar sources into `to`. Modes below. Sources are removed from the output unless `keep_sources` is true; a source is removed if **any** merge using it has `keep_sources` false. A source that is an object or array is not merged and is flattened as usual. Array elements can be sources (`tags.0`). A merge with no present source writes nothing. |
| `default` | `path` (output key), `value` (any JSON) | Writes `value` (stored compacted) when no key equal to `path` was written in this row: renamed keys, merge results and columns included. |

Merge modes:

| `mode` | Result |
|---|---|
| `concat` (default) | One JSON string joined with `sep`. Strings use their unescaped bytes, escaped once; numbers and booleans their text. Missing and `null` sources are skipped. |
| `array` | A JSON array of the present sources in rule order. A present `null` is kept. |
| `first` | The first present, non-null source, unchanged. |

```json
{"rules": [
  {"op": "rename",  "from": "user.id", "to": "uid"},
  {"op": "merge",   "from": ["user.first", "user.last"], "to": "user.name", "sep": " "},
  {"op": "merge",   "from": ["geo.lat", "geo.lon"], "to": "geo.point", "mode": "array"},
  {"op": "drop",    "path": "debug"},
  {"op": "default", "path": "env", "value": "prod"}
]}
```

```example
in:  {"user":{"id":7,"first":"Ada","last":"Lovelace","roles":["a"]},"geo":{"lat":23.02,"lon":72.57},"debug":{"x":1}}
out: {"uid":7,"user.roles.0":"a","user.name":"Ada Lovelace","geo.point":[23.02,72.57],"env":"prod"}
```

More behaviour, each with its test in `TestTransform`:

- Renaming a subtree restores the parent prefix afterwards:
  `{"op":"rename","from":"a.b","to":"B"}` on `{"a":{"b":{"c":1},"z":9}}` gives
  `{"B.c":1,"a.z":9}`. *"rename subtree, then parent prefix is restored"*
- A rename happens before key correction and `to` is used as given:
  `Bad Key → Good` stays `Good` under snake normalisation.
  *`TestFlattenEdgeCases/"a rename comes before key correction, and to is used as given"`*
- Concat mixes types and skips null and missing:
  `["a","b","c","d","e"]` with `sep: "-"` on `{"a":"x","b":12.5,"c":null,"e":true}`
  gives `"x-12.5-true"`. *"merge concat mixes types, skips null and missing"*
- One source may feed several merges. *"one source feeds two merges"*
- A default sees renamed and merged keys, so it does not write over them.
  *"default sees renamed and merged keys"*
- Merge results and defaults go through column reservation and collision
  handling like any other key.
  *`TestKeysWithAppend/"merge and default go through collision handling"`*
- A rename or drop at or above a section's `from` path: drop disables the
  section; rename has no effect there, because a section root is not a node the
  walk visits. Set the section's `prefix` instead.

Errors (*`TestCompileErrors`, `TestConfigValidation`*): unknown `op`; empty
paths; `rename` without exactly one `from` or without `to`; two renames of one
path; rename and drop of one path, in either order; `merge` without `from` or
`to`, or with an unknown `mode`; `default` without `path` or `value`, with
invalid JSON, or duplicated.

## `outputs`

Named destinations. A record can match several outputs and then produces one
row for each, in config order.

| Field | Type | Default | Meaning |
|---|---|---|---|
| `name` | string | required | A literal, or `"$name"` of a derived value (which must exist). An output whose derived name is empty matches nothing. |
| `when` | condition | none | Condition for the record to go to this output. |
| `sections` | list of section IDs | all sections | Limits the output to these sections. Columns, merges and defaults are part of every output. Unknown ID: compile error. |

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

```example
in: {"level":"info","payload":{"msg":"hi"}}
err: ErrNoOutput
```

```example
var env=dev
now: 2026-09-21T10:00:05.5Z
in: {"level":"info","payload":{"msg":"hi"}}
row debug: {"ts":"2026-09-21T10:00:05.500Z","level":"info","msg":"hi"}
```

`ignored` is outside every section. `payload.level` collides with the `level`
column and is dropped. *`TestOutputsSectionsConditions`*

When a record matches two outputs and the second contains everything the first
does, the engine copies the first row and walks only the new sections. This is
an optimisation you cannot observe: the rows are byte for byte the same as a
fresh build. *`TestRowExtensionMatchesFreshBuild`, `TestRowExtensionOnlyForPrefixes`*

Errors: empty `name`; `$name` of a derived value that does not exist; unknown
section ID.

## `expose`

A list of single sources whose **text** lands in `Record.Fields`, in order. An
entry is `nil` when the source has no value. Fields are resolved before
anything can fail, so an error record still carries the IDs you need for
dead-lettering. For the same reason a derived value cannot be exposed: it
does not exist yet. *`TestFieldsOnErrorRecords`, `TestConfigValidation`*

## `newline`

`true` appends `"\n"` to every row, for newline-delimited JSON. (In the
examples on this site a trailing newline is written as `\n`.)

```json
{"newline": true}
```

```example
in:  {"a":1}
out: {"a":1}\n
```

---

## Using the result

| Call | When | Returns |
|---|---|---|
| `t.Append(dst, src)` | one document → one row; the config has no `input.explode` and no `outputs` | `dst` with the row appended; on error, `dst` unchanged. Otherwise `ErrNotSimple`. |
| `t.Transform(src)` | same, with a fresh buffer | the row |
| `t.Each(src, opt, fn)` | one document → any number of rows | calls `fn` per row; returns an error only for invalid JSON, `ErrExplode`, or an error from `fn` (which stops the iteration and is returned as is) |

`Options` carries `Now` (the value of `$now`; zero means `time.Now()`) and
`Vars` (the values of `$name` sources that are not derived values; the map is
only read, so one map can serve many calls).

`Record`, valid only until the callback returns:

| Field | Meaning |
|---|---|
| `Index` | position of the source record in the exploded array; 0 without `explode` |
| `Err` | set when the source record failed; `JSON` and `Name` are then nil |
| `Name` | name of the matching output; nil without configured outputs |
| `JSON` | the flat object |
| `Fields` | values of the `expose` sources, in order; nil entries for missing |
| `Dropped` | keys left out because their output key was taken by a column or an earlier key |

Per-record errors (in `Record.Err` from `Each`; returned by `Append`):
`ErrRootNotContainer`, `ErrInvalidNumber`, `ErrCollision`, `ErrRequired`,
`ErrNoOutput`. Per-call errors: `ErrExplode`, `ErrNotSimple`, and a wrapped
parse error for invalid JSON. Test with `errors.Is`.

## Building a config in Go

The JSON and the Go struct are the same thing; `New(cfg)` runs the same
validation as `Compile`. Three things to know:

- A single source in JSON (`"from": "userId"`) is `SourceList{{Path: "userId"}}`
  in Go. A function source is `Source{Fn: FnClockSkew, Sent: "sentAt", Original: "originalTimestamp"}`.
- `Cond.Equals` and `Cond.Exists` are pointers, so that "not set" is
  distinguishable from `""` and `false`: `Equals: &s` (see `eq` in
  `preset_test.go`).
- `Rule.Value` is a `json.RawMessage`; it must be valid JSON.

The usual pattern is to start from a preset, edit, and build
(`Example_rudderStackPreset` on pkg.go.dev):

```go
var cfg jsonflat.Config
if err := json.Unmarshal(presets.RudderStack, &cfg); err != nil { ... }
cfg.Keys.Aliases = []jsonflat.Alias{{To: "product_id", From: []string{"prodcutId", "pid"}}}
cfg.Keys.Drop = []string{"context_ip"}
t, err := jsonflat.New(cfg)
```

`ExampleNew` on pkg.go.dev builds a config from scratch.

## Troubleshooting: "my key disappeared"

In the order the engine would have removed it:

1. **Outside every section.** With a `sections` list, only the listed subtrees
   are walked.
2. **A `drop` rule** on the path or above it (source path, dots).
3. **Consumed by a merge** without `keep_sources`.
4. **The segment normalises to nothing** under `snake` (`$$`), taking the
   subtree with it.
5. **`drop_nulls` / `drop_empty_objects`.**
6. **Not in `keys.keep`, or matched by `keys.drop`** (output key, after
   aliases and the digit prefix). Silent. Set `keys.rest` to have the keys
   that `keep` removes collected into one column instead.
7. **Reserved by an active column** of the same name, even one without a value.
   Counted in `Record.Dropped`.
8. **Collision under `on_collision: "first"`**: an earlier key of the same
   name won. Counted in `Record.Dropped`.

A quick way to see what a config does to a document:
`go run ./example -in doc.ndjson -config cfg.json -verify -columns` lists every
column it produced.

## A full annotated config

```json
{
  "input":   {"explode": "batch", "inherit": ["sentAt"]},
  "flatten": {"separator": "_", "arrays": "string", "drop_nulls": true, "drop_empty_objects": true},
  "keys":    {"normalize": "snake", "digit_prefix": "_", "on_collision": "first",
              "segment_aliases": {"shiping": "shipping"},
              "aliases": [{"to": "product_id", "from": ["prodcutId", "pid"]}],
              "drop": ["context_ip", "context_traits_*"]},
  "derive":  {"event_table": {"from": "event", "normalize": "snake", "digit_prefix": "_",
                              "reserved": ["tracks"], "reserved_prefix": "_", "required": true,
                              "when": {"path": ["type", "$type"], "equals": "track"}}},
  "columns": [{"to": "id", "from": "messageId", "as": "string"},
              {"to": "received_at", "from": "$now"},
              {"to": "timestamp", "as": "string",
               "from": ["timestamp", {"fn": "clock_skew", "sent": "sentAt", "original": "originalTimestamp"}, "$now"]},
              {"to": "event", "from": "$event_table", "when": {"path": ["type", "$type"], "equals": "track"}}],
  "sections": [{"id": "context", "from": "context", "prefix": "context"},
               {"id": "properties", "from": "properties", "when": {"path": ["type", "$type"], "equals": "track"}}],
  "outputs": [{"name": "tracks", "sections": ["context"], "when": {"path": ["type", "$type"], "equals": "track"}},
              {"name": "$event_table", "when": {"path": ["type", "$type"], "equals": "track"}}],
  "expose":  ["messageId"]
}
```

```example
now: 2026-09-21T10:00:05.5Z
in: {"batch":[
      {"type":"track","event":"Order Completed","messageId":"m-1",
       "originalTimestamp":"2026-09-21T10:00:00.000Z","sentAt":"2026-09-21T10:00:02.000Z",
       "context":{"ip":"1.2.3.4","app":{"name":"Shop"}},
       "properties":{"prodcutId":"P1","shiping":{"City":"Ahmedabad"},"2fa":true,"coupon":null}},
      {"type":"page","messageId":"m-2"}
    ], "sentAt":"2026-09-21T10:00:03.000Z"}
row tracks: {"id":"m-1","received_at":"2026-09-21T10:00:05.500Z","timestamp":"2026-09-21T10:00:03.500Z","event":"order_completed","context_app_name":"Shop"}
  fields: m-1
row order_completed: {"id":"m-1","received_at":"2026-09-21T10:00:05.500Z","timestamp":"2026-09-21T10:00:03.500Z","event":"order_completed","context_app_name":"Shop","product_id":"P1","shipping_city":"Ahmedabad","_2fa":true}
  fields: m-1
err: ErrNoOutput
```

Line by line:

1. `input`: each element of `batch` is a record; a record without `sentAt`
   reads the batch's.
2. `flatten`: `_` between segments, arrays as JSON strings, no key for `null`
   or `{}` (the `coupon` disappears).
3. `keys`: snake_case, `2fa` becomes `_2fa`, first value wins on a clash,
   `shiping` is corrected wherever it appears, `prodcutId` becomes `product_id`,
   `context_ip` and everything under `context_traits_` is dropped.
4. `derive`: `event_table` is the snake_case event name, `_tracks` if it would
   clash with the standard table, required for track events only.
5. `columns`: `id` as a string; `received_at` is now; `timestamp` is the
   payload's, else the clock-skew correction, else now; `event` only on track
   events.
6. `sections`: context under `context_`; properties only for track events.
7. `outputs`: a `tracks` row with context only, and a per-event row with
   everything. The `page` record matches neither and fails with `ErrNoOutput`.
8. `expose`: `messageId` rides along on every row and on the failed record.

This is a trimmed version of the RudderStack preset; the full one is in
[Presets](presets).
