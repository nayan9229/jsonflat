package jsonflat

import (
	"encoding/json"
	"testing"
)

const benchConfig = `{
  "flatten": {"separator": "."},
  "rules": [
    {"op": "rename",  "from": "user.id", "to": "uid"},
    {"op": "merge",   "from": ["user.first", "user.last"], "to": "user.name", "sep": " "},
    {"op": "merge",   "from": ["geo.lat", "geo.lon"], "to": "geo.point", "mode": "array"},
    {"op": "drop",    "path": "debug"},
    {"op": "default", "path": "env", "value": "prod"}
  ]
}`

const benchDoc = `{
  "ts": "2026-09-21T10:15:00Z",
  "level": "info",
  "msg": "order placed",
  "user": {"id": 48213, "first": "Ada", "last": "Lovelace", "roles": ["admin", "ops"],
           "prefs": {"lang": "en", "tz": "Asia/Kolkata", "beta": true}},
  "geo": {"lat": 23.0225, "lon": 72.5714, "city": "Ahmedabad"},
  "order": {"id": "o-99812", "total": 1499.50, "currency": "INR",
            "items": [{"sku": "A1", "qty": 2, "price": 499.75},
                      {"sku": "B7", "qty": 1, "price": 500.00}]},
  "debug": {"trace": "abc123", "spans": [1, 2, 3, 4], "raw": {"k": "v"}},
  "http": {"method": "POST", "path": "/v1/orders", "status": 201, "ms": 38.2}
}`

func BenchmarkAppend(b *testing.B) {
	tr := MustCompile([]byte(benchConfig))
	src := []byte(benchDoc)
	out, _ := tr.Append(nil, src)
	b.SetBytes(int64(len(src)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		out, _ = tr.Append(out[:0], src)
	}
}

func BenchmarkAppendFlattenOnly(b *testing.B) {
	tr := MustCompile([]byte(`{}`))
	src := []byte(benchDoc)
	out, _ := tr.Append(nil, src)
	b.SetBytes(int64(len(src)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		out, _ = tr.Append(out[:0], src)
	}
}

func BenchmarkAppendParallel(b *testing.B) {
	tr := MustCompile([]byte(benchConfig))
	src := []byte(benchDoc)
	b.SetBytes(int64(len(src)))
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		var out []byte
		for pb.Next() {
			out, _ = tr.Append(out[:0], src)
		}
	})
}

// BenchmarkBaselineStdlib is the usual map[string]any approach, flatten only,
// for comparison.
func BenchmarkBaselineStdlib(b *testing.B) {
	src := []byte(benchDoc)
	b.SetBytes(int64(len(src)))
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		var v any
		if err := json.Unmarshal(src, &v); err != nil {
			b.Fatal(err)
		}
		flat := map[string]any{}
		referenceFlatten("", v, flat)
		if _, err := json.Marshal(flat); err != nil {
			b.Fatal(err)
		}
	}
}
