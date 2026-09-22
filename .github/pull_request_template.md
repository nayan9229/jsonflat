<!-- Thanks. A short description of what changes and why is enough; the checklist
     below is what the reviewer will look for. -->

## What and why

## Checklist

- [ ] `make all` passes locally (lint, tests, race, 0 allocs/op, fuzz, docs)
- [ ] New behaviour has a test; a new config field has a compile-time validation test
- [ ] `CHANGELOG.md` has an entry under `[Unreleased]`
- [ ] Docs updated where behaviour is described (`docs/`, `README.md`, doc comments)
- [ ] No new Go dependency; every `MarshalTo` call is on a number, `true`, `false` or `null`
- [ ] No vendor-specific code in the package (vendor layouts are presets)
