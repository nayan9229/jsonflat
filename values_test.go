package jsonflat

import (
	"strings"
	"testing"
)

// TestMapAndLower covers the behaviour the config reference shows and the
// compile-time errors it lists.
func TestMapAndLower(t *testing.T) {
	tr := MustCompile([]byte(`{"columns": [
	  {"to": "device", "from": [{"fn": "map", "from": "dt", "values": {"1": "mobile", "4": "mobile"}}, {"fn": "lower", "from": "dt"}]},
	  {"to": "origin", "from": {"fn": "map", "from": "fill", "values": {"local": "genuin", "pa": ""}, "else": "triton"}},
	  {"to": "muted", "from": {"fn": "map", "from": "mode", "values": {"auto_muted": true}}},
	  {"to": "muted_text", "from": {"fn": "map", "from": "mode", "values": {"auto_muted": true}}, "as": "string"},
	  {"to": "n", "from": {"fn": "map", "from": "k", "values": {"a": 1.5, "b": null, "c": {"x": 1}}}},
	  {"to": "n_text", "from": {"fn": "map", "from": "k", "values": {"a": 1.5, "b": null, "c": {"x": 1}}}, "as": "string"},
	  {"to": "os", "from": {"fn": "lower", "from": "$os"}},
	  {"to": "kind", "from": "$kind"}
	 ],
	 "derive": {"kind": {"from": {"fn": "map", "from": "dt", "values": {"4": "Phone Like"}}, "normalize": "snake"}},
	 "sections": [{"from": "none"}]}`))
	for _, tc := range []struct{ vars, in, want string }{
		{"Ünïcode", `{"dt":"4","fill":"fresh","mode":"auto_muted","k":"a"}`, `{"device":"mobile","origin":"triton","muted":true,"muted_text":"true","n":1.5,"n_text":"1.5","os":"ünïcode","kind":"phone_like"}`},
		{"", `{"dt":"CTV","fill":"local","mode":"x","k":"b"}`, `{"device":"ctv","origin":"genuin","n":null}`},
		{"", `{"dt":"","fill":"pa","k":"c"}`, `{"n":{"x":1}}`},
		{"", `{"dt":4}`, `{"device":"mobile","kind":"phone_like"}`},
	} {
		var opt Options
		if tc.vars != "" {
			opt.Vars = map[string]string{"os": tc.vars}
		}
		var got string
		err := tr.Each([]byte(tc.in), opt, func(r *Record) error {
			if r.Err != nil {
				return r.Err
			}
			got = string(r.JSON)
			return nil
		})
		if err != nil || got != tc.want {
			t.Errorf("in %s:\n got %s %v\nwant %s", tc.in, got, err, tc.want)
		}
	}

	for _, bad := range []string{
		`{"columns":[{"to":"a","from":{"fn":"map","from":"x"}}]}`,
		`{"columns":[{"to":"a","from":{"fn":"map","from":"x","values":{}}}]}`,
		`{"columns":[{"to":"a","from":{"fn":"map","values":{"a":"b"}}}]}`,
		`{"columns":[{"to":"a","from":{"fn":"lower"}}]}`,
		`{"columns":[{"to":"a","from":{"fn":"lower","from":"x","sent":"y"}}]}`,
		`{"columns":[{"to":"a","from":{"fn":"lower","from":"$"}}]}`,
		`{"columns":[{"to":"a","from":{"fn":"map","from":"x","values":{"a":"b"},"path":"y"}}]}`,
	} {
		if _, err := Compile([]byte(bad)); err == nil {
			t.Errorf("compiled: %s", bad)
		}
	}
}

func TestAsNumberAndBool(t *testing.T) {
	tr := MustCompile([]byte(`{"columns": [
	  {"to": "brand", "from": ["$brand", "brand_id"], "as": "number"},
	  {"to": "volume", "from": "giv", "as": "number"},
	  {"to": "blocked", "from": "flag", "as": "bool"},
	  {"to": "mapped", "from": {"fn": "map", "from": "mode", "values": {"on": true, "off": "false"}}, "as": "bool"}
	 ],
	 "sections": [{"from": "none"}]}`))
	for _, tc := range []struct{ brand, in, want string }{
		{"3252", `{"brand_id":"7","giv":"0.4","flag":"true","mode":"on"}`, `{"brand":3252,"volume":0.4,"blocked":true,"mapped":true}`},
		{"", `{"brand_id":7,"giv":-2e3,"flag":false,"mode":"off"}`, `{"brand":7,"volume":-2e3,"mapped":false}`},
		{"", `{"brand_id":"seven","giv":"loud","flag":"yes","mode":"x"}`, `{}`},
	} {
		var opt Options
		if tc.brand != "" {
			opt.Vars = map[string]string{"brand": tc.brand}
		}
		var got string
		err := tr.Each([]byte(tc.in), opt, func(r *Record) error { got = string(r.JSON); return r.Err })
		if err != nil || got != tc.want {
			t.Errorf("in %s:\n got %s %v\nwant %s", tc.in, got, err, tc.want)
		}
	}
	if _, err := Compile([]byte(`{"columns":[{"to":"a","from":"x","as":"int"}]}`)); err == nil {
		t.Error("unknown as compiled")
	}
}

// TestValueSourcesDoNotAllocate keeps map, lower, number and bool on the
// zero-allocation path of Append.
func TestValueSourcesDoNotAllocate(t *testing.T) {
	if raceEnabled {
		t.Skip("the race detector makes sync.Pool drop items, which allocates")
	}
	tr := MustCompile([]byte(`{"columns": [
	  {"to": "device", "from": [{"fn": "map", "from": "dt", "values": {"1": "mobile", "4": "mobile"}}, {"fn": "lower", "from": "dt"}]},
	  {"to": "origin", "from": {"fn": "map", "from": "fill", "values": {"local": "genuin"}, "else": "triton"}},
	  {"to": "volume", "from": "giv", "as": "number"},
	  {"to": "blocked", "from": "flag", "as": "bool"}
	 ],
	 "sections": [{"from": "none"}]}`))
	src := []byte(`{"dt":"CTV","fill":"fresh","giv":"0.4","flag":"true"}`)
	out, err := tr.Append(nil, src)
	if err != nil || !strings.HasPrefix(string(out), `{"device":"ctv","origin":"triton","volume":0.4,"blocked":true}`) {
		t.Fatalf("unexpected output: %s %v", out, err)
	}
	allocs := testing.AllocsPerRun(200, func() { out, _ = tr.Append(out[:0], src) })
	if allocs != 0 {
		t.Fatalf("value sources allocate %v times per call, want 0", allocs)
	}
}
