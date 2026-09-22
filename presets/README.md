# Presets

A preset is plain `jsonflat` config JSON, embedded in the `presets` package.
Compile it as it is, or unmarshal it into a `jsonflat.Config`, add your own
aliases, drops and rules, and pass it to `jsonflat.New`. There is no vendor
code in the engine.

| Preset | Variable | Docs |
|---|---|---|
| RudderStack warehouse schema | `presets.RudderStack` ([rudderstack.json](rudderstack.json)) | [docs/presets.md](../docs/presets.md), or [the published page](https://nayan9229.github.io/jsonflat/presets) |

The docs page has the table of what each record type becomes, the rules taken
from RudderStack's warehouse schema, the choices the preset makes, how to use
and extend it, what it does not cover, and a warning about Firehose and Glue.

To propose a preset, see
[CONTRIBUTING.md](../CONTRIBUTING.md#how-to-propose-a-preset).
