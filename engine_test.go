package jsonflat

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"
)

// These tests cover what the acceptance tests leave open, and pin down how
// this implementation settled it.

// A row that extends the previous row must be byte for byte what a fresh build
// gives.
func TestRowExtensionMatchesFreshBuild(t *testing.T) {
	fast := rudder(t, withTestKeys)
	if !fast.extend {
		t.Fatal("the preset has no merges or defaults, so rows should extend")
	}
	slow := rudder(t, withTestKeys)
	slow.extend = false

	for _, src := range []string{testBatch, rudderBenchBatch} {
		a := collect(t, fast, src, Options{Now: recvAt})
		b := collect(t, slow, src, Options{Now: recvAt})
		if len(a) != len(b) {
			t.Fatalf("%d rows against %d", len(a), len(b))
		}
		for i := range a {
			if a[i].json != b[i].json || a[i].table != b[i].table || a[i].dropped != b[i].dropped {
				t.Errorf("row %d\nextended: %s %s %d\n   fresh: %s %s %d",
					i, a[i].table, a[i].json, a[i].dropped, b[i].table, b[i].json, b[i].dropped)
			}
		}
	}
}

// A row extends the previous one only when that one's sections are a prefix of
// its own. The outputs below alternate between rows that can and cannot.
func TestRowExtensionOnlyForPrefixes(t *testing.T) {
	config := `{
	  "sections": [{"id":"a","from":"a"},{"id":"b","from":"b"},{"id":"c","from":"c"}],
	  "outputs": [
	    {"name":"ab","sections":["a","b"]},
	    {"name":"abc"},
	    {"name":"b","sections":["b"]},
	    {"name":"bc","sections":["b","c"]},
	    {"name":"ac","sections":["a","c"]},
	    {"name":"all"}
	  ]}`
	want := []string{
		`ab|{"x":1,"y":2}`,
		`abc|{"x":1,"y":2,"z":3}`,
		`b|{"y":2}`,
		`bc|{"y":2,"z":3}`,
		`ac|{"x":1,"z":3}`,
		`all|{"x":1,"y":2,"z":3}`,
	}
	rows := each(t, config, `{"a":{"x":1},"b":{"y":2},"c":{"z":3}}`, Options{})
	if len(rows) != len(want) {
		t.Fatalf("got %d rows, want %d", len(rows), len(want))
	}
	for i, w := range want {
		if got := rows[i].name + "|" + rows[i].json; got != w {
			t.Errorf("row %d\n got: %s\nwant: %s", i, got, w)
		}
	}
}

// An error record still carries the exposed fields, for dead-lettering.
func TestFieldsOnErrorRecords(t *testing.T) {
	tr := rudder(t, nil)
	src := `{"batch":[{"type":"bogus","messageId":"m-6","anonymousId":"a-6"}]}`
	var fields []string
	err := tr.Each([]byte(src), Options{}, func(r *Record) error {
		if !errors.Is(r.Err, ErrNoOutput) || r.JSON != nil || r.Name != nil {
			t.Errorf("record: %+v", r)
		}
		for _, f := range r.Fields {
			fields = append(fields, string(f))
		}
		if r.Fields[2] != nil {
			t.Errorf("a missing field should be nil, got %q", r.Fields[2])
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(fields, ","); got != "m-6,a-6," {
		t.Errorf("fields = %q", got)
	}
}

// A callback may scribble over everything it is given without harming the
// Transformer or the next call.
func TestRecordIsACopy(t *testing.T) {
	tr := rudder(t, withTestKeys)
	want := collect(t, tr, testBatch, Options{Now: recvAt})

	scribble := func(b []byte) {
		for i := range b {
			b[i] = 'X'
		}
	}
	err := tr.Each([]byte(testBatch), Options{Now: recvAt}, func(r *Record) error {
		scribble(r.Name)
		scribble(r.JSON)
		for _, f := range r.Fields {
			scribble(f)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	got := collect(t, tr, testBatch, Options{Now: recvAt})
	for i := range want {
		if got[i].json != want[i].json || got[i].table != want[i].table || got[i].anon != want[i].anon {
			t.Errorf("row %d changed: %s %s", i, got[i].table, got[i].json)
		}
	}
}

func TestAppendKeepsPrefixAndInput(t *testing.T) {
	tr := MustCompile([]byte(`{}`))
	src := []byte(`{"a":{"b":"x"}}`)
	keep := bytes.Clone(src)
	out, err := tr.Append([]byte("row: "), src)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != `row: {"a.b":"x"}` {
		t.Errorf("got %s", out)
	}
	clear(out)
	if !bytes.Equal(src, keep) {
		t.Error("the output aliases the input")
	}
}

// "$now" belongs to one call. A pooled state must not carry it into the next,
// neither from Each into Append nor from one Append into another.
func TestNowIsPerCall(t *testing.T) {
	tr := MustCompile([]byte(`{"columns":[{"to":"at","from":"$now"}],"sections":[{"from":"none"}]}`))
	old := time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC)
	const stale = `{"at":"2001-01-01T00:00:00.000Z"}`

	for i := 0; i < 4; i++ {
		// Each leaves a state in the pool that has formatted the old time.
		err := tr.Each([]byte(`{}`), Options{Now: old}, func(r *Record) error {
			if string(r.JSON) != stale {
				t.Errorf("Options.Now is not used: %s", r.JSON)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		got, err := tr.Transform([]byte(`{}`))
		if err != nil {
			t.Fatal(err)
		}
		if string(got) == stale {
			t.Fatalf("Append wrote the $now of an earlier call: %s", got)
		}
	}
}

// State from a very large input is not pooled; the call itself must still work.
func TestLargeInput(t *testing.T) {
	tr := MustCompile([]byte(`{"flatten":{"arrays":"raw"}}`))
	src := []byte(`{"a":"` + strings.Repeat("x", maxPooledInput) + `","b":[1,2]}`)
	out, err := tr.Transform(src)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != len(src) || !bytes.HasSuffix(out, []byte(`","b":[1,2]}`)) {
		t.Errorf("unexpected output of %d bytes", len(out))
	}
	if got, err := tr.Transform([]byte(`{"c":{"d":1}}`)); err != nil || string(got) != `{"c.d":1}` {
		t.Errorf("after a large input: %s %v", got, err)
	}
}

func TestDeriveOrder(t *testing.T) {
	// b may read a, which sorts before it.
	config := `{
	  "derive": {"a": {"from": "x", "normalize": "snake"}, "b": {"from": ["y", "$a"]}},
	  "columns": [{"to": "a", "from": "$a"}, {"to": "b", "from": "$b"}],
	  "sections": [{"from": "none"}]}`
	rows := each(t, config, `{"x":"Some Value"}`, Options{})
	if rows[0].json != `{"a":"some_value","b":"some_value"}` {
		t.Errorf("got %s", rows[0].json)
	}
}

func TestConfigValidation(t *testing.T) {
	bad := []struct{ why, config string }{
		{"derive reads a later value", `{"derive":{"a":{"from":"$b"},"b":{"from":"x"}}}`},
		{"derive reads itself", `{"derive":{"a":{"from":["x","$a"]}}}`},
		{"derive condition reads a later value", `{"derive":{"a":{"from":"x","when":{"path":"$b","exists":true}},"b":{"from":"y"}}}`},
		{"derive without a name", `{"derive":{"":{"from":"x"}}}`},
		{"derive without sources", `{"derive":{"a":{"from":[]}}}`},
		{"expose reads a derived value", `{"derive":{"a":{"from":"x"}},"expose":["$a"]}`},
		{"expose with an empty source", `{"expose":[""]}`},

		{"column with an empty source", `{"columns":[{"to":"a","from":""}]}`},
		{"column from a bare $", `{"columns":[{"to":"a","from":"$"}]}`},
		{"column without sources", `{"columns":[{"to":"a","from":[]}]}`},
		{"clock_skew given a variable", `{"columns":[{"to":"a","from":{"fn":"clock_skew","sent":"$now","original":"o"}}]}`},
		{"function with an unknown field", `{"columns":[{"to":"a","from":{"fn":"clock_skew","sent":"s","original":"o","extra":1}}]}`},
		{"condition with an empty in", `{"columns":[{"to":"a","from":"a","when":{"path":"t","in":[]}}]}`},
		{"condition with two tests", `{"sections":[{"from":"x","when":{"path":"a","equals":"1","in":["2"]}}]}`},

		{"empty inherit key", `{"input":{"inherit":[""]}}`},
		{"empty drop key", `{"keys":{"drop":[""]}}`},
		{"drop key listed twice", `{"keys":{"drop":["a","a"]}}`},
		{"empty keep key", `{"keys":{"keep":[""]}}`},
		{"keep everything", `{"keys":{"keep":["*"]}}`},
		{"keep key listed twice", `{"keys":{"keep":["a","a"]}}`},
		{"same key kept and dropped", `{"keys":{"keep":["a","b"],"drop":["b"]}}`},
		{"alias to a key that is not kept", `{"keys":{"keep":["x"],"aliases":[{"to":"y","from":["z"]}]}}`},
		{"alias to a dropped key", `{"keys":{"drop":["y"],"aliases":[{"to":"y","from":["z"]}]}}`},
		{"alias to a key under a dropped prefix", `{"keys":{"drop":["internal_*"],"aliases":[{"to":"internal_id","from":["z"]}]}}`},
		{"rest without keep", `{"keys":{"rest":"extra"}}`},
		{"rest named like a column", `{"columns":[{"to":"extra","from":"x"}],"keys":{"keep":["a"],"rest":"extra"}}`},
		{"alias to the rest column", `{"keys":{"keep":["a","extra"],"rest":"extra","aliases":[{"to":"extra","from":["z"]}]}}`},
		{"segment alias to nothing", `{"keys":{"segment_aliases":{"a":""}}}`},
		{"segment alias to itself", `{"keys":{"segment_aliases":{"a":"a"}}}`},
		{"segment alias that normalises to nothing", `{"keys":{"normalize":"snake","segment_aliases":{"$$":"a"}}}`},
		{"two segment aliases that normalise alike", `{"keys":{"normalize":"snake","segment_aliases":{"userId":"a","user_id":"b"}}}`},
		{"alias from listed twice", `{"keys":{"aliases":[{"to":"x","from":["a","a"]}]}}`},
		{"alias with a bad condition", `{"keys":{"aliases":[{"to":"x","from":["a"],"when":{"path":"t"}}]}}`},

		{"output named after $now", `{"outputs":[{"name":"$now"}]}`},
		{"output with an empty section id", `{"outputs":[{"name":"o","sections":[""]}],"sections":[{"from":"x"}]}`},

		{"drop then rename of one path", `{"rules":[{"op":"drop","path":"a"},{"op":"rename","from":"a","to":"b"}]}`},
		{"rename of an empty path", `{"rules":[{"op":"rename","from":"","to":"b"}]}`},
		{"merge with an empty source path", `{"rules":[{"op":"merge","from":["a",""],"to":"c"}]}`},
		{"merge without to", `{"rules":[{"op":"merge","from":["a"]}]}`},
		{"default without path", `{"rules":[{"op":"default","value":1}]}`},
	}
	for _, tc := range bad {
		if _, err := Compile([]byte(tc.config)); err == nil {
			t.Errorf("%s: Compile(%s) should fail", tc.why, tc.config)
		}
	}

	// Two things that only a Config built in Go can get wrong.
	badDefault := Config{Rules: []Rule{{Op: OpDefault, Path: "a", Value: []byte(`{"a":`)}}}
	if _, err := New(badDefault); err == nil {
		t.Error("New should reject a default value that is not valid JSON")
	}
	pathAndFn := Source{Path: "p", Fn: FnClockSkew, Sent: "s", Original: "o"}
	if _, err := New(Config{Columns: []Column{{To: "a", From: SourceList{pathAndFn}}}}); err == nil {
		t.Error("New should reject a source that is both a path and a function")
	}
}

func TestFlattenEdgeCases(t *testing.T) {
	now := recvAt.Format(timeLayout)
	tests := []struct {
		name   string
		config string
		in     string
		want   string
	}{{
		name:   "a root array is entered whatever the arrays mode is",
		config: `{"flatten":{"arrays":"raw"}}`,
		in:     `[[1,2],{"a":[3]}]`,
		want:   `{"0":[1,2],"1.a":[3]}`,
	}, {
		name:   "a section root array is flattened under its prefix",
		config: `{"sections":[{"from":"items","prefix":"item"}]}`,
		in:     `{"items":[{"sku":"A"},"x"],"other":1}`,
		want:   `{"item.0.sku":"A","item.1":"x"}`,
	}, {
		name:   "string arrays: at the depth limit only arrays become strings",
		config: `{"flatten":{"arrays":"string","max_depth":1}}`,
		in:     `{"o":{"a":[1]},"a":[{"b":"c"}]}`,
		want:   `{"o":{"a":[1]},"a":"[{\"b\":\"c\"}]"}`,
	}, {
		name:   "index arrays are raw at the depth limit",
		config: `{"flatten":{"max_depth":1}}`,
		in:     `{"a":[1,[2]]}`,
		want:   `{"a":[1,[2]]}`,
	}, {
		name:   "alias from entries are normalised segment by segment",
		config: `{"keys":{"normalize":"snake","aliases":[{"to":"user.zip","from":["userInfo.ZIP Code"]}]}}`,
		in:     `{"userInfo":{"ZIP Code":"38","city":"A"}}`,
		want:   `{"user.zip":"38","user_info.city":"A"}`,
	}, {
		name:   "array indices are not normalised or aliased",
		config: `{"keys":{"normalize":"snake","segment_aliases":{"0":"zero"}}}`,
		in:     `{"a":["x"],"0":1}`,
		want:   `{"a.0":"x","zero":1}`,
	}, {
		name: "a rename comes before key correction, and to is used as given",
		config: `{"keys":{"normalize":"snake","digit_prefix":"_"},
		          "rules":[{"op":"rename","from":"a.Bad Key","to":"Good"}]}`,
		in:   `{"a":{"Bad Key":{"x":1},"B":2}}`,
		want: `{"Good.x":1,"a.b":2}`,
	}, {
		name: "a rule inside a section uses the full source path",
		config: `{"sections":[{"from":"p","prefix":"q"}],
		          "rules":[{"op":"drop","path":"p.secret"},{"op":"rename","from":"p.a","to":"A"}]}`,
		in:   `{"p":{"secret":1,"a":2,"b":3},"secret":4}`,
		want: `{"A":2,"q.b":3}`,
	}, {
		name:   "a dropped subtree takes the sections below it with it",
		config: `{"sections":[{"from":"p.q"},{"from":"r"}],"rules":[{"op":"drop","path":"p"}]}`,
		in:     `{"p":{"q":{"a":1}},"r":{"b":2}}`,
		want:   `{"b":2}`,
	}, {
		name: "exists false, and null counts as missing",
		config: `{"columns":[{"to":"c","from":"$now","when":{"path":["a","b"],"exists":false}}],
		          "sections":[{"from":"none"}]}`,
		in:   `{"a":null}`,
		want: `{"c":"` + now + `"}`,
	}, {
		name:   "a column without a value is still reserved",
		config: `{"columns":[{"to":"id","from":"missing"}]}`,
		in:     `{"id":1,"x":2}`,
		want:   `{"x":2}`,
	}, {
		name:   "of two columns with one name, the first with a value wins",
		config: `{"columns":[{"to":"id","from":"a"},{"to":"id","from":"b"}]}`,
		in:     `{"b":2,"a":1}`,
		want:   `{"id":1,"b":2,"a":1}`,
	}, {
		name: "a raw column keeps the type, a string column needs text",
		config: `{"columns":[{"to":"o","from":"obj"},{"to":"s","from":["obj","flag","n"],"as":"string"}],
		          "sections":[{"from":"none"}]}`,
		in:   `{"obj":{"k":[1]},"flag":true,"n":1.50}`,
		want: `{"o":{"k":[1]},"s":"1.50"}`,
	}, {
		name:   "a merge into a column name is dropped",
		config: `{"columns":[{"to":"m","from":"id"}],"rules":[{"op":"merge","from":["a","b"],"to":"m"}]}`,
		in:     `{"id":7,"a":"x","b":"y"}`,
		want:   `{"m":7,"id":7}`,
	}, {
		name:   "newline",
		config: `{"newline":true}`,
		in:     `{"a":1}`,
		want:   "{\"a\":1}\n",
	}, {
		name:   "keep: exact names and prefixes, array indices included",
		config: `{"flatten":{"separator":"_"},"keys":{"normalize":"snake","keep":["user_id","tags_*"]}}`,
		in:     `{"userId":1,"tags":["a","b"],"x":{"y":2},"tagsx":3}`,
		want:   `{"user_id":1,"tags_0":"a","tags_1":"b"}`,
	}, {
		name:   "keep and drop together: the family minus one",
		config: `{"flatten":{"separator":"_"},"keys":{"keep":["context_*"],"drop":["context_ip"]}}`,
		in:     `{"context":{"ip":"1.2.3.4","app":{"name":"Shop"}},"event":"x"}`,
		want:   `{"context_app_name":"Shop"}`,
	}, {
		name: "keep is matched after aliases and the digit prefix",
		config: `{"flatten":{"separator":"_"},"keys":{"normalize":"snake","digit_prefix":"_",
		          "aliases":[{"to":"address_zip","from":["address_postcode"]}],"keep":["address_zip","_2fa"]}}`,
		in:   `{"address":{"postcode":"38","city":"A"},"2fa":true}`,
		want: `{"address_zip":"38","_2fa":true}`,
	}, {
		name: "columns, merge results and defaults are written whether kept or not",
		config: `{"keys":{"keep":["a"]},"columns":[{"to":"id","from":"id"}],
		          "rules":[{"op":"merge","from":["x","y"],"to":"xy"},{"op":"default","path":"env","value":"p"}]}`,
		in:   `{"id":1,"a":2,"b":3,"x":"1","y":"2"}`,
		want: `{"id":1,"a":2,"xy":"12","env":"p"}`,
	}, {
		name:   "a key that keep removes takes no part in collisions",
		config: `{"keys":{"normalize":"snake","on_collision":"error","keep":["x"]}}`,
		in:     `{"aB":1,"a_b":2,"x":3}`,
		want:   `{"x":3}`,
	}, {
		name:   "keep on a root array",
		config: `{"keys":{"keep":["0"]}}`,
		in:     `[1,2]`,
		want:   `{"0":1}`,
	}, {
		name: "rest collects what keep removed, under final names, as one JSON string",
		config: `{"flatten":{"separator":"_"},"keys":{"normalize":"snake","digit_prefix":"_",
		          "aliases":[{"to":"zip","from":["address_postcode"]}],"keep":["user_id","zip"],"rest":"extra"}}`,
		in:   `{"userId":1,"address":{"postcode":"38","city":"A"},"2fa":true,"tags":["x"]}`,
		want: `{"user_id":1,"zip":"38","extra":"{\"address_city\":\"A\",\"_2fa\":true,\"tags_0\":\"x\"}"}`,
	}, {
		name:   "rest takes nothing that drop or a column removed",
		config: `{"keys":{"keep":["a"],"drop":["b"],"rest":"extra"},"columns":[{"to":"c","from":"c"}]}`,
		in:     `{"c":1,"a":2,"b":3,"c":9,"d":{"e":null}}`,
		want:   `{"c":1,"a":2,"extra":"{\"d.e\":null}"}`,
	}, {
		name:   "rest keeps duplicates as they come",
		config: `{"keys":{"keep":["a"],"on_collision":"first","rest":"extra"}}`,
		in:     `{"a":1,"x":1,"x":2}`,
		want:   `{"a":1,"extra":"{\"x\":1,\"x\":2}"}`,
	}, {
		name:   "rest is absent when nothing was removed",
		config: `{"keys":{"keep":["a"],"rest":"extra"}}`,
		in:     `{"a":1}`,
		want:   `{"a":1}`,
	}, {
		name:   "rest keeps the string form of arrays",
		config: `{"flatten":{"arrays":"string"},"keys":{"keep":["a"],"rest":"extra"}}`,
		in:     `{"a":1,"t":[1,"x"]}`,
		want:   `{"a":1,"extra":"{\"t\":\"[1,\\\"x\\\"]\"}"}`,
	}}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rows := each(t, tc.config, tc.in, Options{Now: recvAt})
			if len(rows) != 1 || rows[0].err != nil {
				t.Fatalf("rows: %+v", rows)
			}
			if rows[0].json != tc.want {
				t.Errorf("\n got: %s\nwant: %s", rows[0].json, tc.want)
			}
		})
	}
}

// With rest set, rows cannot extend the previous row (the rest column sits at
// the end); the output must still be what a fresh build gives, per output.
func TestRestPerOutput(t *testing.T) {
	config := `{
	  "keys": {"keep": ["x"], "rest": "extra"},
	  "sections": [{"id": "a", "from": "a"}, {"id": "b", "from": "b"}],
	  "outputs": [{"name": "a", "sections": ["a"]}, {"name": "all"}]}`
	rows := each(t, config, `{"a":{"x":1,"p":2},"b":{"y":3}}`, Options{})
	want := []string{
		`a|{"x":1,"extra":"{\"p\":2}"}`,
		`all|{"x":1,"extra":"{\"p\":2,\"y\":3}"}`,
	}
	for i, w := range want {
		if got := rows[i].name + "|" + rows[i].json; got != w {
			t.Errorf("row %d\n got: %s\nwant: %s", i, got, w)
		}
	}
}

// Keys that keep removes are silent: not counted, not an error.
func TestKeepIsSilent(t *testing.T) {
	rows := each(t, `{"keys":{"on_collision":"first","keep":["a"]},"columns":[{"to":"c","from":"c"}]}`,
		`{"a":1,"b":2,"c":3}`, Options{})
	if rows[0].json != `{"c":3,"a":1}` || rows[0].dropped != 0 || rows[0].err != nil {
		t.Errorf("got %+v", rows[0])
	}
}

// The filters are on the hot path, so they must not allocate either.
func TestKeepZeroAllocs(t *testing.T) {
	if raceEnabled {
		t.Skip("the race detector makes sync.Pool drop items, which allocates")
	}
	tr := MustCompile([]byte(`{"flatten":{"separator":"_"},"keys":{"normalize":"snake",
	  "keep":["user_*","order_id","geo_city","http_*"],"drop":["http_ms"],"rest":"extra"}}`))
	src := []byte(benchDoc)
	out, err := tr.Append(nil, src)
	if err != nil || !strings.HasPrefix(string(out), `{"user_id":48213,`) || strings.Contains(string(out), "http_ms") ||
		!strings.Contains(string(out), `,"extra":"{\"ts\":`) {
		t.Fatalf("unexpected output: %s %v", out, err)
	}
	allocs := testing.AllocsPerRun(200, func() {
		out, _ = tr.Append(out[:0], src)
	})
	if allocs != 0 {
		t.Fatalf("Append with keep allocates %v times per call, want 0", allocs)
	}
}

func TestInheritFallsBackOnlyWhenMissing(t *testing.T) {
	config := `{"input":{"explode":"rows","inherit":["tenant"]},
	  "columns":[{"to":"tenant","from":"tenant"}],"expose":["tenant"]}`

	rows := each(t, config, `{"tenant":"env","rows":[{"a":1},{"tenant":"own"},{"tenant":null}]}`, Options{})
	want := []string{`{"tenant":"env","a":1}`, `{"tenant":"own"}`, `{"tenant":"env"}`}
	for i, w := range want {
		if rows[i].json != w {
			t.Errorf("row %d\n got: %s\nwant: %s", i, rows[i].json, w)
		}
	}
	if rows[0].fields[0] != "env" || rows[1].fields[0] != "own" {
		t.Errorf("fields: %v %v", rows[0].fields, rows[1].fields)
	}

	// Without the explode path the document is the record and has no envelope.
	rows = each(t, config, `{"tenant":"solo","a":1}`, Options{})
	if len(rows) != 1 || rows[0].json != `{"tenant":"solo","a":1}` {
		t.Errorf("single record: %+v", rows)
	}
}

// A number is checked wherever it is written, not only in flattened leaves.
func TestInvalidNumberEverywhere(t *testing.T) {
	tests := []struct{ where, config, in string }{
		{"column", `{"columns":[{"to":"n","from":"n"}],"sections":[{"from":"none"}]}`, `{"n":01}`},
		{"merge concat", `{"rules":[{"op":"merge","from":["a","n"],"to":"m"}]}`, `{"a":"x","n":1e}`},
		{"merge array", `{"rules":[{"op":"merge","from":["n"],"to":"m","mode":"array"}]}`, `{"n":1.}`},
		{"merge first", `{"rules":[{"op":"merge","from":["n"],"to":"m","mode":"first"}]}`, `{"n":+1}`},
		{"inside a string array", `{"flatten":{"arrays":"string"}}`, `{"a":[{"b":[inf]}]}`},
	}
	for _, tc := range tests {
		rows := each(t, tc.config, tc.in, Options{})
		if len(rows) != 1 || !errors.Is(rows[0].err, ErrInvalidNumber) {
			t.Errorf("%s: %+v", tc.where, rows)
		}
	}

	// As text, an invalid number is simply not there.
	config := `{"columns":[{"to":"n","from":["n","$d"],"as":"string"}],"sections":[{"from":"none"}]}`
	rows := each(t, config, `{"n":00}`, Options{Vars: map[string]string{"d": "fallback"}})
	if rows[0].json != `{"n":"fallback"}` {
		t.Errorf("got %s", rows[0].json)
	}
}
