package jsonflat

import (
	"bytes"
	"runtime"
	"sync"
	"testing"
	"time"
)

// These tests and benchmarks back the concurrency claims in README.md and
// docs/performance.md: one Transformer serves any number of goroutines, and
// a Record belongs to the pooled state that produced it.

var discard = func(*Record) error { return nil }

const keepConfig = `{"flatten":{"separator":"_"},"keys":{"normalize":"snake",
  "keep":["user_*","order_id","geo_city","http_*"],"drop":["http_ms"],"rest":"extra"}}`

// singleTrack is a body from the single-event route: no "type", so the
// preset needs Options.Vars["type"].
var singleTrack = []byte(`{"event":"Signed Up","messageId":"m","anonymousId":"a","properties":{"plan":"pro","seats":3}}`)

// Sixteen goroutines share three Transformers and alternate between Append
// and Each with different Options. Every result must equal the sequential
// one. Meant to run under -race as well.
func TestSharedTransformerMixed(t *testing.T) {
	tr := rudder(t, withTestKeys)
	simple := MustCompile([]byte(benchConfig))
	keep := MustCompile([]byte(keepConfig))

	batchOpt := Options{Now: recvAt}
	trackOpt := Options{Now: recvAt, Vars: map[string]string{"type": "track"}}
	pageOpt := Options{Now: recvAt.Add(time.Hour), Vars: map[string]string{"type": "page"}}

	// viaEach concatenates everything a callback is given, so a mix-up in
	// any field shows.
	viaEach := func(src []byte, opt Options) func([]byte) ([]byte, error) {
		return func(out []byte) ([]byte, error) {
			out = out[:0]
			err := tr.Each(src, opt, func(r *Record) error {
				out = append(out, r.Name...)
				out = append(out, '|')
				out = append(out, r.JSON...)
				if r.Err != nil {
					out = append(out, r.Err.Error()...)
				}
				for _, f := range r.Fields {
					out = append(out, f...)
					out = append(out, ',')
				}
				return nil
			})
			return out, err
		}
	}
	type job struct {
		name string
		run  func([]byte) ([]byte, error)
	}
	jobs := []job{
		{"each, bench batch", viaEach([]byte(rudderBenchBatch), batchOpt)},
		{"each, test batch", viaEach([]byte(testBatch), batchOpt)},
		{"each, single track", viaEach(singleTrack, trackOpt)},
		{"each, single page", viaEach([]byte(`{"messageId":"p","name":"Cart","properties":{"path":"/cart"}}`), pageOpt)},
		{"append, rules", func(out []byte) ([]byte, error) { return simple.Append(out[:0], []byte(benchDoc)) }},
		{"append, keep and rest", func(out []byte) ([]byte, error) { return keep.Append(out[:0], []byte(benchDoc)) }},
	}
	want := make([][]byte, len(jobs))
	for i, j := range jobs {
		var err error
		if want[i], err = j.run(nil); err != nil {
			t.Fatalf("%s: %v", j.name, err)
		}
	}

	var wg sync.WaitGroup
	for g := 0; g < 16; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			var out []byte
			for i := 0; i < 300; i++ {
				j := (g + i) % len(jobs)
				var err error
				out, err = jobs[j].run(out)
				if err != nil || !bytes.Equal(out, want[j]) {
					t.Errorf("goroutine %d, iteration %d, %s: %v\n got: %s\nwant: %s", g, i, jobs[j].name, err, out, want[j])
					return
				}
			}
		}(g)
	}
	wg.Wait()
}

// A Record kept past the callback is invalid: it is zeroed when Each
// returns, and its JSON points into a buffer that the next call, on any
// goroutine, overwrites. This is why callers copy what they keep.
func TestRetainedRecordIsInvalid(t *testing.T) {
	// One P, so that the next call finds the same pooled state.
	defer runtime.GOMAXPROCS(runtime.GOMAXPROCS(1))
	tr := MustCompile([]byte(`{}`))

	var kept *Record
	var keptJSON []byte
	err := tr.Each([]byte(`{"a":"first"}`), Options{}, func(r *Record) error {
		kept, keptJSON = r, r.JSON
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if kept.JSON != nil || kept.Name != nil || kept.Fields != nil {
		t.Errorf("the retained *Record still holds data after Each returned: %+v", *kept)
	}

	if raceEnabled {
		t.Skip("the race detector makes sync.Pool drop items, so the buffer may never be reused")
	}
	snapshot := string(keptJSON)
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = tr.Each([]byte(`{"a":"SECOND"}`), Options{}, discard)
	}()
	<-done
	if string(keptJSON) == snapshot {
		t.Errorf("the retained JSON slice still reads %q after another goroutine's call", keptJSON)
	}
	t.Logf("retained slice read %q, now reads %q", snapshot, keptJSON)
}

func BenchmarkEachParallel(b *testing.B) {
	tr := rudder(b, withTestKeys)
	src := []byte(rudderBenchBatch)
	opt := Options{Now: recvAt}
	b.SetBytes(int64(len(src)))
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if err := tr.Each(src, opt, discard); err != nil {
				b.Fatal(err)
			}
		}
	})
}

// Options.Vars is only read, so one map per route can be shared. These two
// put a number on building it per call instead.
func BenchmarkVarsShared(b *testing.B) {
	tr := rudder(b, nil)
	opt := Options{Now: recvAt, Vars: map[string]string{"type": "track"}}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = tr.Each(singleTrack, opt, discard)
	}
}

func BenchmarkVarsPerCall(b *testing.B) {
	tr := rudder(b, nil)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		opt := Options{Now: recvAt, Vars: map[string]string{"type": "track"}}
		_ = tr.Each(singleTrack, opt, discard)
	}
}
