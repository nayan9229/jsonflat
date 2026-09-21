// Package jsonflat flattens JSON documents and reshapes them with a JSON
// config: it normalises and corrects keys, renames, drops, merges and defaults
// them, splits batches into records and routes records to named outputs, with
// zero heap allocations on the hot path.
//
// A config is compiled once into a Transformer, which is immutable and safe
// for concurrent use:
//
//	t := jsonflat.MustCompile([]byte(`{
//	  "flatten": {"separator": "_"},
//	  "keys":    {"normalize": "snake"},
//	  "rules":   [{"op": "drop", "path": "debug"}]
//	}`))
//
// An empty config, {}, flattens the whole document with "." between key
// segments. Every section of the config is optional and they combine freely;
// see Config for all of them.
//
// # One document, one row
//
// Append and Transform turn one document into one flat object. Append writes
// into a buffer that the caller reuses, which is what keeps the call free of
// allocations:
//
//	out, err = t.Append(out[:0], src)
//
// # One document, many rows
//
// Each is for configs that split a batch into records (input.explode) or route
// a record to several outputs (outputs). It calls a function for every row and
// hands it the name of the output, the JSON, and the values the config asked
// to expose, for example a partition key:
//
//	err := t.Each(body, jsonflat.Options{}, func(r *jsonflat.Record) error {
//		if r.Err != nil {
//			return deadLetter(r.Index, r.Fields, r.Err)
//		}
//		return put(r.Name, r.Fields[0], r.JSON)
//	})
//
// A record that fails is reported through Record.Err and never stops the
// batch; none of its rows are delivered, so a record never arrives in part.
// The Record and its slices are valid only until the callback returns.
//
// # Two kinds of path
//
// Source paths address the input (rule path and from, section from,
// input.explode, and every Source). They always use "." between segments,
// whatever flatten.separator is, and use the keys as the input spells them.
// Output keys are what gets written (rule to, the path of a default rule,
// column to, alias to, keys.drop) and are used exactly as given.
//
// # Guarantees
//
// The output is always valid JSON and never aliases the input. Number text is
// copied through unchanged, so no precision is lost, and is checked against
// RFC 8259 because the parser is more lenient than that. On error, Append
// returns dst unchanged.
//
// Vendor layouts are configs, not code: see the presets package.
package jsonflat
