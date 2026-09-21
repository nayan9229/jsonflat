// Package presets holds ready-made jsonflat configs.
//
// A preset is plain config JSON. Compile it as it is, or unmarshal it into a
// jsonflat.Config, add your own aliases, drops and rules, and pass it to
// jsonflat.New.
package presets

import _ "embed"

// RudderStack turns RudderStack SDK payloads ({"batch":[...]} or one event)
// into rows that follow the RudderStack warehouse schema: tracks plus one
// output per event name, identifies, pages, screens, groups and aliases, with
// snake_case keys and a context_ prefix.
//
// Set Options.Vars["type"] for bodies from the single-event routes, which carry
// no "type". Record.Fields holds messageId, anonymousId and userId.
//
//go:embed rudderstack.json
var RudderStack []byte
