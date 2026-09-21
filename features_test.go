package jsonflat

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

type row struct {
	name, json string
	fields     []string
	err        error
	dropped    int
}

func each(t *testing.T, config, src string, opt Options) []row {
	t.Helper()
	tr, err := Compile([]byte(config))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	var out []row
	err = tr.Each([]byte(src), opt, func(r *Record) error {
		rw := row{name: string(r.Name), json: string(r.JSON), err: r.Err, dropped: r.Dropped}
		for _, f := range r.Fields {
			rw.fields = append(rw.fields, string(f))
		}
		if r.Err == nil && !json.Valid(r.JSON) {
			t.Errorf("invalid JSON: %s", r.JSON)
		}
		out = append(out, rw)
		return nil
	})
	if err != nil {
		t.Fatalf("Each: %v", err)
	}
	return out
}

// An order with line items becomes one row per item, and each row carries the
// order's ID. Nothing here is specific to any vendor.
func TestExplodeInheritColumns(t *testing.T) {
	config := `{
	  "input": {"explode": "items", "inherit": ["orderId", "currency"]},
	  "flatten": {"separator": "_"},
	  "keys": {"normalize": "snake"},
	  "columns": [
	    {"to": "order_id", "from": "orderId", "as": "string"},
	    {"to": "currency", "from": ["currency", "$default_currency"]}
	  ],
	  "expose": ["sku", "orderId"]
	}`
	src := `{"orderId": 981, "items": [
	  {"sku": "A1", "unitPrice": 499.75, "dims": {"W": 2, "H": 3}},
	  {"sku": "B7", "unitPrice": 500.00, "currency": "USD"},
	  7
	]}`
	rows := each(t, config, src, Options{Vars: map[string]string{"default_currency": "INR"}})
	want := []string{
		`{"order_id":"981","currency":"INR","sku":"A1","unit_price":499.75,"dims_w":2,"dims_h":3}`,
		`{"order_id":"981","currency":"USD","sku":"B7","unit_price":500.00}`,
	}
	for i, w := range want {
		if rows[i].json != w || rows[i].name != "" {
			t.Errorf("row %d\n got: %s\nwant: %s", i, rows[i].json, w)
		}
	}
	// The item's own currency is a column name, so the flattened copy is dropped.
	if rows[1].dropped != 1 {
		t.Errorf("dropped = %d, want 1", rows[1].dropped)
	}
	if got := strings.Join(rows[0].fields, ","); got != "A1,981" {
		t.Errorf("fields = %q", got)
	}
	if !errors.Is(rows[2].err, ErrRootNotContainer) || len(rows) != 3 {
		t.Errorf("scalar element: %+v", rows[2])
	}
}

// Application logs routed by level, with different content per output.
func TestOutputsSectionsConditions(t *testing.T) {
	config := `{
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
	}`
	src := `{"level":"error","time":"T1","payload":{"msg":"boom","level":"shadow"},
	         "http":{"status":500,"path":"/x"},"error":{"kind":"io"},"ignored":1}`

	rows := each(t, config, src, Options{})
	if len(rows) != 2 {
		t.Fatalf("got %d rows: %+v", len(rows), rows)
	}
	if rows[0].name != "errors" || rows[0].json !=
		`{"ts":"T1","level":"error","msg":"boom","http.status":500,"http.path":"/x","error.kind":"io"}` {
		t.Errorf("errors row: %s %s", rows[0].name, rows[0].json)
	}
	if rows[1].name != "access" || rows[1].json != `{"ts":"T1","level":"error","http.status":500,"http.path":"/x"}` {
		t.Errorf("access row: %s %s", rows[1].name, rows[1].json)
	}

	rows = each(t, config, `{"level":"info","payload":{"msg":"hi"}}`, Options{})
	if len(rows) != 1 || !errors.Is(rows[0].err, ErrNoOutput) {
		t.Errorf("no output: %+v", rows)
	}
	rows = each(t, config, `{"level":"info","payload":{"msg":"hi"}}`,
		Options{Now: recvAt, Vars: map[string]string{"env": "dev"}})
	if len(rows) != 1 || rows[0].json != `{"ts":"2026-09-21T10:00:05.500Z","level":"info","msg":"hi"}` {
		t.Errorf("debug row: %+v", rows)
	}
}

func TestKeysWithAppend(t *testing.T) {
	tests := []struct{ name, config, in, want string }{
		{"snake and string arrays",
			`{"flatten":{"separator":"_","arrays":"string","drop_nulls":true,"drop_empty_objects":true},"keys":{"normalize":"snake"}}`,
			`{"userID":1,"Home Address":{"zipCode":"38","$$":{"x":1}},"tags":["a",{"b":"\u0001"}],"n":null,"e":{},"ea":[]}`,
			`{"user_id":1,"home_address_zip_code":"38","tags":"[\"a\",{\"b\":\"\\u0001\"}]","ea":"[]"}`},
		{"collision first",
			`{"flatten":{"separator":"_"},"keys":{"normalize":"snake","on_collision":"first"}}`,
			`{"productId":1,"product_id":2,"a":{"b":3},"a_b":4}`,
			`{"product_id":1,"a_b":3}`},
		{"collision keep is the default",
			`{"keys":{"normalize":"snake"}}`, `{"aB":1,"a_b":2}`, `{"a_b":1,"a_b":2}`},
		{"aliases, segment aliases, digit prefix, drop",
			`{"flatten":{"separator":"_"},"keys":{"normalize":"snake","digit_prefix":"n",
			  "segment_aliases":{"adress":"address"},
			  "aliases":[{"to":"address_zip","from":["address_postcode","Address ZIP Code"]}],
			  "drop":["secret","internal_*"]}}`,
			`{"adress":{"postcode":"38","city":"A"},"2fa":true,"secret":1,"internal":{"a":1,"b":{"c":2}},"internals":3}`,
			`{"address_zip":"38","address_city":"A","n2fa":true,"internals":3}`},
		{"aliases without normalising match the literal key",
			`{"keys":{"aliases":[{"to":"id","from":["ID"]}]}}`, `{"ID":1,"Id":2}`, `{"id":1,"Id":2}`},
		{"rule paths use dots whatever the separator is",
			`{"flatten":{"separator":"__"},"rules":[
			  {"op":"drop","path":"a.secret"},
			  {"op":"merge","from":["a.first","a.last"],"to":"name","sep":" "}]}`,
			`{"a":{"first":"Ada","secret":1,"last":"L","x":[1]}}`,
			`{"a__x__0":1,"name":"Ada L"}`},
		{"merge and default go through collision handling",
			`{"keys":{"on_collision":"first"},"rules":[
			  {"op":"merge","from":["x","y"],"to":"taken","keep_sources":true},
			  {"op":"default","path":"taken","value":1}]}`,
			`{"taken":"t","x":"1","y":"2"}`,
			`{"taken":"t","x":"1","y":"2"}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tr, err := Compile([]byte(tc.config))
			if err != nil {
				t.Fatal(err)
			}
			for i := 0; i < 2; i++ {
				got, err := tr.Transform([]byte(tc.in))
				if err != nil {
					t.Fatal(err)
				}
				if string(got) != tc.want {
					t.Fatalf("\n got: %s\nwant: %s", got, tc.want)
				}
			}
		})
	}

	tr := MustCompile([]byte(`{"keys":{"normalize":"snake","on_collision":"error"}}`))
	if _, err := tr.Transform([]byte(`{"aB":1,"a_b":2}`)); !errors.Is(err, ErrCollision) {
		t.Errorf("got %v, want ErrCollision", err)
	}
}

func TestDerive(t *testing.T) {
	config := `{
	  "derive": {"table": {"from": ["kind", "$kind"], "normalize": "snake", "digit_prefix": "t",
	                        "reserved": ["default"], "reserved_prefix": "x_", "required": true}},
	  "columns": [{"to": "table", "from": "$table"}],
	  "sections": [{"from": "data"}],
	  "outputs": [{"name": "$table"}]
	}`
	for in, want := range map[string]string{
		`{"kind":"Order Placed","data":{"a":1}}`: `order_placed|{"table":"order_placed","a":1}`,
		`{"kind":"3D","data":{"table":9}}`:       `t3_d|{"table":"t3_d"}`,
		`{"kind":"Default"}`:                     `x_default|{"table":"x_default"}`,
		`{"data":{"a":1}}`:                       `from_var|{"table":"from_var","a":1}`,
	} {
		rows := each(t, config, in, Options{Vars: map[string]string{"kind": "fromVar"}})
		if got := rows[0].name + "|" + rows[0].json; got != want {
			t.Errorf("%s\n got: %s\nwant: %s", in, got, want)
		}
	}
	rows := each(t, config, `{"kind":" - "}`, Options{})
	if !errors.Is(rows[0].err, ErrRequired) {
		t.Errorf("got %v, want ErrRequired", rows[0].err)
	}
}

func TestClockSkew(t *testing.T) {
	config := `{"columns":[{"to":"ts","from":[{"fn":"clock_skew","sent":"sent","original":"orig"},"orig","$now"]}],
	            "sections":[{"from":"none"}]}`
	for in, want := range map[string]string{
		// The client clock is 1h fast: sent 11:00:02 when the server saw 10:00:05.5.
		`{"sent":"2026-09-21T11:00:02Z","orig":"2026-09-21T11:00:00Z"}`: `{"ts":"2026-09-21T10:00:03.500Z"}`,
		`{"sent":"garbage","orig":"2026-09-21T11:00:00Z"}`:              `{"ts":"2026-09-21T11:00:00Z"}`,
		`{}`: `{"ts":"2026-09-21T10:00:05.500Z"}`,
	} {
		rows := each(t, config, in, Options{Now: recvAt})
		if rows[0].json != want {
			t.Errorf("%s\n got: %s\nwant: %s", in, rows[0].json, want)
		}
	}
}

func TestNewConfigErrors(t *testing.T) {
	bad := []string{
		`{"flatten":{"arrays":"csv"}}`,
		`{"keys":{"normalize":"camel"}}`,
		`{"keys":{"on_collision":"last"}}`,
		`{"keys":{"drop":["*"]}}`,
		`{"keys":{"aliases":[{"to":"","from":["a"]}]}}`,
		`{"keys":{"aliases":[{"to":"x","from":[]}]}}`,
		`{"keys":{"aliases":[{"to":"x","from":["a"]},{"to":"y","from":["a"]}]}}`,
		`{"keys":{"normalize":"snake","aliases":[{"to":"product_id","from":["productId"]}]}}`,
		`{"keys":{"normalize":"snake","aliases":[{"to":"x","from":["$$"]}]}}`,
		`{"columns":[{"to":"id","from":"a"}],"keys":{"aliases":[{"to":"id","from":["ident"]}]}}`,
		`{"columns":[{"to":"","from":"a"}]}`,
		`{"columns":[{"to":"a","from":"a","as":"int"}]}`,
		`{"columns":[{"to":"a","from":{"fn":"nope"}}]}`,
		`{"columns":[{"to":"a","from":{"fn":"clock_skew","sent":"s"}}]}`,
		`{"columns":[{"to":"a","from":"a","when":{"path":"t"}}]}`,
		`{"columns":[{"to":"a","from":"a","when":{"path":"t","equals":"x","exists":true}}]}`,
		`{"columns":[{"to":"a","from":"a","when":{"equals":"x"}}]}`,
		`{"sections":[{"id":"a","from":"x"},{"id":"a","from":"y"}]}`,
		`{"outputs":[{"name":""}]}`,
		`{"outputs":[{"name":"$missing"}]}`,
		`{"sections":[{"id":"a","from":"x"}],"outputs":[{"name":"o","sections":["b"]}]}`,
		`{"input":{"inherit":["a.b"]}}`,
		`{"derive":{"now":{"from":"a"}}}`,
		`{"derive":{"t":{"from":"a","normalize":"kebab"}}}`,
	}
	for _, c := range bad {
		if _, err := Compile([]byte(c)); err == nil {
			t.Errorf("Compile(%s): want an error", c)
		}
	}
}
