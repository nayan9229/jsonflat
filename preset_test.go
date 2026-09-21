package jsonflat

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/nayan9229/jsonflat/presets"
)

var recvAt = time.Date(2026, 9, 21, 10, 0, 5, 500_000_000, time.UTC)

// rudder returns the RudderStack preset with the test's key corrections added.
// This is how a user extends a preset: unmarshal, edit, New.
func rudder(t testing.TB, edit func(*Config)) *Transformer {
	t.Helper()
	var cfg Config
	if err := json.Unmarshal(presets.RudderStack, &cfg); err != nil {
		t.Fatalf("preset: %v", err)
	}
	if edit != nil {
		edit(&cfg)
	}
	tr, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return tr
}

func eq(s string) *string { return &s }

func withTestKeys(c *Config) {
	c.Keys.Aliases = []Alias{
		{To: "product_id", From: []string{"prodcutId", "pid"}},
		{To: "total", From: []string{"Total Price"}, When: &Cond{Path: SourceList{{Path: "event"}}, Equals: eq("Order Completed")}},
	}
	c.Keys.SegmentAliases = map[string]string{"shiping": "shipping"}
	c.Keys.Drop = []string{"context_ip", "context_traits_*"}
}

const testBatch = `{
 "batch": [
  {"type":"track","event":"Order Completed","messageId":"m-1","anonymousId":"anon-1","userId":"u-1","channel":"web",
   "originalTimestamp":"2026-09-21T10:00:00.000Z","sentAt":"2026-09-21T10:00:02.000Z",
   "context":{"app":{"name":"Shop","version":"1.2.3"},"library":{"name":"RudderLabs JavaScript SDK"},
              "ip":"1.2.3.4","screen":{"density":2},"traits":{"email":"a@b.c"}},
   "properties":{"orderId":"o-9","prodcutId":"P1","Total Price":1499.50,"currency":"INR","user_id":"hacker",
                 "products":[{"sku":"A1","qty":2}],"coupon":null,"meta":{},
                 "shiping":{"City":"Ahmedabad","zipCode":"380001"},"2fa":true},
   "integrations":{"All":true},"somethingElse":1},
  {"type":"identify","messageId":"m-2","anonymousId":"anon-1","userId":42,
   "traits":{"firstName":"Ada","plan":"pro"},
   "context":{"traits":{"firstName":"IGNORED","email":"a@b.c"}}},
  {"type":"page","messageId":"m-3","anonymousId":"anon-2","name":"Cart","category":"Shop",
   "properties":{"name":"Cart","path":"/cart","Total Price":5}},
  {"type":"group","messageId":"m-4","groupId":"g1","traits":{"name":"Acme","employeeCount":12}},
  {"type":"alias","messageId":"m-5","previousId":"old","userId":"new"},
  {"type":"bogus","messageId":"m-6"},
  42,
  {"type":"track","event":" - ","messageId":"m-8"},
  {"type":"track","event":"3D Secure","messageId":"m-9","properties":{"n":00}},
  {"type":"track","event":"Tracks","messageId":"m-10","timestamp":"2026-01-01T00:00:00Z","properties":{"a":1}}
 ],
 "sentAt": "2026-09-21T10:00:03.000Z"
}`

type got struct {
	index   int
	table   string
	json    string
	err     error
	dropped int
	anon    string
	user    string
}

func collect(t *testing.T, tr *Transformer, src string, opt Options) []got {
	t.Helper()
	var out []got
	err := tr.Each([]byte(src), opt, func(r *Record) error {
		g := got{index: r.Index, table: string(r.Name), json: string(r.JSON), err: r.Err, dropped: r.Dropped}
		if len(r.Fields) == 3 {
			g.anon, g.user = string(r.Fields[1]), string(r.Fields[2])
		}
		out = append(out, g)
		if r.Err == nil && !json.Valid(r.JSON) {
			t.Errorf("record %d: invalid JSON: %s", r.Index, r.JSON)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Each: %v", err)
	}
	return out
}

func asMap(t *testing.T, s string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		t.Fatalf("unmarshal %s: %v", s, err)
	}
	return m
}

func TestRudderBatch(t *testing.T) {
	tr := rudder(t, withTestKeys)
	recs := collect(t, tr, testBatch, Options{Now: recvAt})

	const common = `"id":"m-1","anonymous_id":"anon-1","user_id":"u-1","channel":"web",` +
		`"sent_at":"2026-09-21T10:00:02.000Z","received_at":"2026-09-21T10:00:05.500Z",` +
		`"original_timestamp":"2026-09-21T10:00:00.000Z","timestamp":"2026-09-21T10:00:03.500Z",` +
		`"event":"order_completed","event_text":"Order Completed",` +
		`"context_app_name":"Shop","context_app_version":"1.2.3",` +
		`"context_library_name":"RudderLabs JavaScript SDK","context_screen_density":2`

	// Event 0: a track event gives a tracks row and an event-table row.
	if recs[0].table != "tracks" || recs[0].json != "{"+common+"}" {
		t.Errorf("tracks row\n got: %s %s", recs[0].table, recs[0].json)
	}
	wantEvent := "{" + common + `,"order_id":"o-9","product_id":"P1","total":1499.50,"currency":"INR",` +
		`"products":"[{\"sku\":\"A1\",\"qty\":2}]","shipping_city":"Ahmedabad","shipping_zip_code":"380001","_2fa":true}`
	if recs[1].table != "order_completed" || recs[1].json != wantEvent {
		t.Errorf("event row\n got: %s %s\nwant: %s", recs[1].table, recs[1].json, wantEvent)
	}
	if recs[1].dropped != 1 { // properties.user_id must not replace the standard column
		t.Errorf("dropped = %d, want 1", recs[1].dropped)
	}
	if recs[1].anon != "anon-1" || recs[1].user != "u-1" {
		t.Errorf("ids = %q %q", recs[1].anon, recs[1].user)
	}

	// Event 1: identify. Numeric userId becomes a string, traits win over
	// context.traits, and the batch-level sentAt is inherited.
	id := asMap(t, recs[2].json)
	if recs[2].table != "identifies" || id["user_id"] != "42" || id["first_name"] != "Ada" ||
		id["plan"] != "pro" || id["email"] != "a@b.c" || id["sent_at"] != "2026-09-21T10:00:03.000Z" {
		t.Errorf("identify row: %s", recs[2].json)
	}
	if recs[2].dropped != 0 || recs[2].user != "42" {
		t.Errorf("identify: dropped=%d user=%q", recs[2].dropped, recs[2].user)
	}
	if _, ok := id["context_traits_email"]; ok {
		t.Errorf("context_traits_* should be dropped by config: %s", recs[2].json)
	}

	// Event 2: page. The event-scoped alias must not apply here.
	pg := asMap(t, recs[3].json)
	if recs[3].table != "pages" || pg["name"] != "Cart" || pg["category"] != "Shop" ||
		pg["path"] != "/cart" || pg["total_price"] != float64(5) || recs[3].dropped != 0 {
		t.Errorf("page row: %s dropped=%d", recs[3].json, recs[3].dropped)
	}
	if _, ok := pg["timestamp"]; !ok {
		t.Errorf("page row has no timestamp: %s", recs[3].json)
	}

	gr := asMap(t, recs[4].json)
	if recs[4].table != "groups" || gr["group_id"] != "g1" || gr["name"] != "Acme" || gr["employee_count"] != float64(12) {
		t.Errorf("group row: %s", recs[4].json)
	}
	al := asMap(t, recs[5].json)
	if recs[5].table != "aliases" || al["previous_id"] != "old" || al["user_id"] != "new" {
		t.Errorf("alias row: %s", recs[5].json)
	}

	// Bad events are reported one by one and do not stop the batch.
	for i, want := range map[int]error{6: ErrNoOutput, 7: ErrRootNotContainer, 8: ErrRequired, 9: ErrInvalidNumber} {
		if !errors.Is(recs[i].err, want) || recs[i].json != "" {
			t.Errorf("record %d: err=%v json=%q, want %v", i, recs[i].err, recs[i].json, want)
		}
	}

	// An event named like a standard table gets an underscore, and a
	// timestamp in the payload is used as is.
	last := asMap(t, recs[11].json)
	if recs[10].table != "tracks" || recs[11].table != "_tracks" || last["event"] != "_tracks" ||
		last["timestamp"] != "2026-01-01T00:00:00Z" {
		t.Errorf("last rows: %s / %s %s", recs[10].table, recs[11].table, recs[11].json)
	}
	if len(recs) != 12 {
		t.Errorf("got %d records, want 12", len(recs))
	}
}

func TestRudderSingleEvent(t *testing.T) {
	// Dropping the tracks output is a config edit, not a special option.
	tr := rudder(t, func(c *Config) {
		c.Newline = true
		c.Outputs = c.Outputs[1:]
	})
	vars := map[string]string{"type": "track"} // body from /v1/track carries no type
	recs := collect(t, tr, `{"event":"Signed Up","messageId":"m","properties":{"plan":"pro"}}`,
		Options{Now: recvAt, Vars: vars})
	if len(recs) != 1 || recs[0].table != "signed_up" {
		t.Fatalf("got %+v", recs)
	}
	want := `{"id":"m","received_at":"2026-09-21T10:00:05.500Z","timestamp":"2026-09-21T10:00:05.500Z",` +
		`"event":"signed_up","event_text":"Signed Up","plan":"pro"}` + "\n"
	if recs[0].json != want {
		t.Errorf("\n got: %s\nwant: %s", recs[0].json, want)
	}
}

func TestRudderCollisionPolicy(t *testing.T) {
	src := `{"type":"track","event":"E","properties":{"productId":"first","product_id":"second","user_id":"x"}}`

	recs := collect(t, rudder(t, nil), src, Options{Now: recvAt})
	m := asMap(t, recs[1].json)
	if m["product_id"] != "first" || recs[1].dropped != 2 {
		t.Errorf("first policy: %s dropped=%d", recs[1].json, recs[1].dropped)
	}
	if _, ok := m["user_id"]; ok { // reserved even though the event set no userId
		t.Errorf("a property took the user_id column: %s", recs[1].json)
	}

	strict := rudder(t, func(c *Config) { c.Keys.OnCollision = CollisionError })
	recs = collect(t, strict, src, Options{Now: recvAt})
	if len(recs) != 1 || !errors.Is(recs[0].err, ErrCollision) {
		t.Errorf("error policy: %+v", recs)
	}
	// A clash with a column alone is never an error.
	recs = collect(t, strict, `{"type":"track","event":"E","properties":{"user_id":"x"}}`, Options{Now: recvAt})
	if len(recs) != 2 || recs[1].err != nil || recs[1].dropped != 1 {
		t.Errorf("column clash: %+v", recs)
	}
}

func TestRudderStableAcrossRuns(t *testing.T) {
	// Pooled state must not leak anything from one request into the next.
	tr := rudder(t, withTestKeys)
	a := collect(t, tr, testBatch, Options{Now: recvAt})
	collect(t, tr, `{"type":"page","properties":{"zzz":1,"user_id":2}}`, Options{Now: recvAt})
	b := collect(t, tr, testBatch, Options{Now: recvAt})
	if !reflect.DeepEqual(a, b) {
		t.Error("results differ between runs")
	}
}

func TestEachErrors(t *testing.T) {
	tr := rudder(t, nil)
	nop := func(*Record) error { return nil }
	if err := tr.Each([]byte(`{"batch":`), Options{}, nop); err == nil {
		t.Error("want a parse error")
	}
	if err := tr.Each([]byte(`{"batch":{}}`), Options{}, nop); !errors.Is(err, ErrExplode) {
		t.Errorf("got %v, want ErrExplode", err)
	}
	stop := errors.New("stop")
	calls := 0
	err := tr.Each([]byte(testBatch), Options{}, func(*Record) error { calls++; return stop })
	if !errors.Is(err, stop) || calls != 1 {
		t.Errorf("callback error: err=%v calls=%d", err, calls)
	}
	if _, err := tr.Append(nil, []byte(`{}`)); !errors.Is(err, ErrNotSimple) {
		t.Errorf("Append on a multi-record config: got %v, want ErrNotSimple", err)
	}
}

func TestRudderZeroAllocs(t *testing.T) {
	if raceEnabled {
		t.Skip("the race detector makes sync.Pool drop items, which allocates")
	}
	tr := rudder(t, withTestKeys)
	src := []byte(rudderBenchBatch)
	opt := Options{Now: recvAt}
	n := 0
	fn := func(r *Record) error { n += len(r.JSON); return nil }
	if err := tr.Each(src, opt, fn); err != nil {
		t.Fatal(err)
	}
	allocs := testing.AllocsPerRun(200, func() { _ = tr.Each(src, opt, fn) })
	if allocs != 0 {
		t.Fatalf("Each allocates %v times per call, want 0", allocs)
	}
}

func FuzzEach(f *testing.F) {
	f.Add([]byte(testBatch))
	f.Add([]byte(rudderBenchBatch))
	f.Add([]byte(`{"type":"identify","traits":{"a":[{"b":"\u0001"}]},"context":{"traits":{"A":1}}}`))
	tr := rudder(f, withTestKeys)
	vars := map[string]string{"type": "track"}
	f.Fuzz(func(t *testing.T, in []byte) {
		_ = tr.Each(in, Options{Now: recvAt, Vars: vars}, func(r *Record) error {
			if r.Err == nil && !json.Valid(r.JSON) {
				t.Fatalf("invalid JSON output\n in: %q\nout: %q", in, r.JSON)
			}
			return nil
		})
	})
}

func BenchmarkRudderEach(b *testing.B) {
	tr := rudder(b, withTestKeys)
	src := []byte(rudderBenchBatch)
	opt := Options{Now: recvAt}
	rows := 0
	fn := func(r *Record) error { rows++; return nil }
	b.SetBytes(int64(len(src)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := tr.Each(src, opt, fn); err != nil {
			b.Fatal(err)
		}
	}
	b.ReportMetric(float64(rows)/float64(b.N), "rows/op")
}

// rudderBenchBatch is three events, as a JavaScript SDK would send them.
const rudderBenchBatch = `{"batch":[
 {"type":"page","messageId":"b-1","anonymousId":"anon-7","channel":"web","name":"Cart","category":"Shop",
  "originalTimestamp":"2026-09-21T10:00:00.000Z","sentAt":"2026-09-21T10:00:02.000Z",
  "context":{"app":{"name":"Shop","version":"1.2.3","namespace":"com.shop"},"library":{"name":"RudderLabs JavaScript SDK","version":"3.7.1"},
             "locale":"en-IN","screen":{"density":2,"width":1440,"height":900},"userAgent":"Mozilla/5.0","timezone":"GMT+0530",
             "campaign":{"source":"google","medium":"cpc"},"page":{"path":"/cart","url":"https://shop.example/cart","title":"Cart"}},
  "properties":{"name":"Cart","path":"/cart","url":"https://shop.example/cart","title":"Cart","referrer":"https://google.com"}},
 {"type":"track","event":"Product Added","messageId":"b-2","anonymousId":"anon-7","userId":"u-42","channel":"web",
  "originalTimestamp":"2026-09-21T10:00:01.000Z","sentAt":"2026-09-21T10:00:02.000Z",
  "context":{"app":{"name":"Shop","version":"1.2.3"},"library":{"name":"RudderLabs JavaScript SDK","version":"3.7.1"},"locale":"en-IN"},
  "properties":{"prodcutId":"P1","sku":"A1","Total Price":499.75,"currency":"INR","quantity":2,"category":"masks"}},
 {"type":"track","event":"Order Completed","messageId":"b-3","anonymousId":"anon-7","userId":"u-42","channel":"web",
  "originalTimestamp":"2026-09-21T10:00:01.500Z","sentAt":"2026-09-21T10:00:02.000Z",
  "context":{"app":{"name":"Shop","version":"1.2.3"},"library":{"name":"RudderLabs JavaScript SDK","version":"3.7.1"},"locale":"en-IN"},
  "properties":{"orderId":"o-9","Total Price":1499.50,"currency":"INR","coupon":null,
                "products":[{"sku":"A1","qty":2,"price":499.75},{"sku":"B7","qty":1,"price":500.00}],
                "shiping":{"City":"Ahmedabad","zipCode":"380001"}}}
],"sentAt":"2026-09-21T10:00:02.000Z"}`
