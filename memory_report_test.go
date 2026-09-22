package jsonflat

import (
	"bytes"
	"fmt"
	"reflect"
	"runtime"
	"runtime/debug"
	"sync"
	"testing"
	"unsafe"
)

// The tests in this file measure rather than assert, unless the name says
// otherwise. Run them with -v; docs/performance.md quotes their output.

func memDelta(f func()) (mallocs, bytes uint64) {
	var a, b runtime.MemStats
	runtime.ReadMemStats(&a)
	f()
	runtime.ReadMemStats(&b)
	return b.Mallocs - a.Mallocs, b.TotalAlloc - a.TotalAlloc
}

// drainPool empties every sync.Pool: one GC moves items to the victim
// cache, the next drops them.
func drainPool() {
	runtime.GC()
	runtime.GC()
}

// The first calls on a state that an empty pool had to build.
func TestColdPoolReport(t *testing.T) {
	if raceEnabled {
		t.Skip("the race detector drops pooled items on purpose")
	}
	trA := MustCompile([]byte(benchConfig))
	srcA := []byte(benchDoc)
	dst := make([]byte, 0, 8192)
	trR := rudder(t, withTestKeys)
	srcR := []byte(rudderBenchBatch)
	opt := Options{Now: recvAt}
	for _, p := range []struct {
		name string
		call func()
	}{
		{"Append, rules config, 650 B record", func() { dst, _ = trA.Append(dst[:0], srcA) }},
		{"Each, preset, 1.8 KB batch", func() { _ = trR.Each(srcR, opt, discard) }},
	} {
		drainPool()
		line := p.name + ":"
		for call := 1; call <= 5; call++ {
			m, b := memDelta(p.call)
			line += fmt.Sprintf("  call %d: %d allocs, %d B", call, m, b)
		}
		t.Log(line)
	}
}

// How many states a burst of goroutines builds on an empty pool.
func TestBurstReport(t *testing.T) {
	if raceEnabled {
		t.Skip("the race detector drops pooled items on purpose")
	}
	tr := MustCompile([]byte(benchConfig))
	src := []byte(benchDoc)
	dst := make([]byte, 0, 8192)
	dst, _ = tr.Append(dst, src)
	drainPool()
	_, perState := memDelta(func() { dst, _ = tr.Append(dst[:0], src) })

	for _, n := range []int{1, 10, 64, 256} {
		dsts := make([][]byte, n)
		for i := range dsts {
			dsts[i] = make([]byte, 0, 8192)
		}
		drainPool()
		start := make(chan struct{})
		var wg sync.WaitGroup
		_, total := memDelta(func() {
			for i := 0; i < n; i++ {
				wg.Add(1)
				go func(i int) {
					defer wg.Done()
					<-start
					for k := 0; k < 200; k++ {
						dsts[i], _ = tr.Append(dsts[i][:0], src)
					}
				}(i)
			}
			close(start)
			wg.Wait()
		})
		t.Logf("%3d goroutines x 200 Append on an empty pool: %7d B allocated = %4.1f states of %d B (GOMAXPROCS=%d)",
			n, total, float64(total)/float64(perState), perState, runtime.GOMAXPROCS(0))
	}
}

// sizer sums the backing arrays of the slices reachable from a value, each
// array once. Strings are not counted: in a parsed document they point into
// the parser's copy of the input, which is counted as a []byte. Pointers are
// not followed: in a state they only point into the parser's value array,
// which is counted through the parser.
type sizer struct{ seen map[uintptr]bool }

func (z *sizer) size(v reflect.Value) (n uintptr) {
	switch v.Kind() {
	case reflect.Slice:
		if v.Cap() == 0 || z.seen[v.Pointer()] {
			return 0
		}
		z.seen[v.Pointer()] = true
		elem := v.Type().Elem()
		n = uintptr(v.Cap()) * elem.Size()
		switch elem.Kind() {
		case reflect.Slice, reflect.Struct:
			// Elements beyond len still hold allocations: fastjson reuses
			// its values by reslicing.
			full := v.Slice3(0, v.Cap(), v.Cap())
			for i := 0; i < full.Len(); i++ {
				n += z.size(full.Index(i))
			}
		}
		return n
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			n += z.size(v.Field(i))
		}
		return n
	}
	return 0
}

// stateSize is the memory one state owns, and the fields above 1 KiB. The
// Transformer, the method values and the Record (views into other buffers)
// are left out.
func stateSize(s *state) (total uintptr, byField string) {
	z := &sizer{seen: map[uintptr]bool{}}
	rv := reflect.ValueOf(s).Elem()
	var big []string
	for i := 0; i < rv.NumField(); i++ {
		f := rv.Type().Field(i)
		switch f.Name {
		case "t", "record", "fields", "visitFn", "rawVisitFn", "opt", "out":
			continue // out aliases buf after Each
		}
		n := z.size(rv.Field(i))
		total += n
		if n >= 1024 {
			big = append(big, fmt.Sprintf("%s=%dK", f.Name, n/1024))
		}
	}
	return total + unsafe.Sizeof(state{}), fmt.Sprint(big)
}

// runDoc puts doc through a state outside the pool, so that the test knows
// which state did the work.
func (s *state) runDoc(t testing.TB, doc []byte, opt Options) {
	s.begin(s.buf[:0], opt)
	if err := s.each(doc, discard); err != nil {
		t.Fatal(err)
	}
	s.buf = s.out
}

// bigBatch is a RudderStack batch of about target bytes.
func bigBatch(target int) []byte {
	var b bytes.Buffer
	b.WriteString(`{"batch":[`)
	for i := 0; b.Len() < target; i++ {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, `{"type":"track","event":"Product Added","messageId":"m-%d","anonymousId":"anon-%d","userId":"u-%d","channel":"web",`+
			`"originalTimestamp":"2026-09-21T10:00:01.000Z","sentAt":"2026-09-21T10:00:02.000Z",`+
			`"context":{"app":{"name":"Shop","version":"1.2.3"},"library":{"name":"RudderLabs JavaScript SDK","version":"3.7.1"},"locale":"en-IN",`+
			`"screen":{"density":2,"width":1440,"height":900}},`+
			`"properties":{"sku":"A%d","price":499.75,"currency":"INR","quantity":%d,"category":"masks","tags":["a","b","c"]}}`,
			i, i%100, i%1000, i, i%5)
	}
	b.WriteString(`],"sentAt":"2026-09-21T10:00:02.000Z"}`)
	return b.Bytes()
}

// What one state holds after the benchmark payloads and after a 3 MiB batch.
func TestMemoryReport(t *testing.T) {
	big := bigBatch(3 << 20)
	t.Logf("large document: %d bytes (%.2f MiB)", len(big), float64(len(big))/(1<<20))

	cases := []struct {
		name string
		tr   *Transformer
		docs [][]byte
	}{
		{"{} config, 650 B record", MustCompile([]byte(`{}`)), [][]byte{[]byte(benchDoc)}},
		{"rules config, 650 B record", MustCompile([]byte(benchConfig)), [][]byte{[]byte(benchDoc)}},
		{"preset, 1.8 KB batch", rudder(t, withTestKeys), [][]byte{[]byte(rudderBenchBatch)}},
		{"preset, 2.3 KB batch", rudder(t, withTestKeys), [][]byte{[]byte(testBatch)}},
		{"{} config, 3 MiB batch", MustCompile([]byte(`{}`)), [][]byte{big}},
		{"preset, 3 MiB batch", rudder(t, withTestKeys), [][]byte{big}},
		{"preset, 3 MiB batch then 1.8 KB batches", rudder(t, withTestKeys), [][]byte{big, []byte(rudderBenchBatch)}},
	}
	for _, c := range cases {
		s := newState(c.tr)
		fresh, _ := stateSize(s)
		for _, d := range c.docs {
			for i := 0; i < 3; i++ {
				s.runDoc(t, d, Options{Now: recvAt})
			}
		}
		total, fields := stateSize(s)
		t.Logf("%-42s fresh %5d B -> %9d B (%8.1f KiB) %s", c.name, fresh, total, float64(total)/1024, fields)
	}
}

// The collision set grows to the widest row and stays there.
func TestKeySetReport(t *testing.T) {
	tr := MustCompile([]byte(`{"keys":{"on_collision":"first"}}`))
	var b bytes.Buffer
	b.WriteByte('{')
	for i := 0; i < 5000; i++ {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, `"k%d":%d`, i, i)
	}
	b.WriteByte('}')
	s := newState(tr)
	s.runDoc(t, b.Bytes(), Options{})
	slots := len(s.keys.slots)
	s.runDoc(t, []byte(`{"a":1}`), Options{})
	t.Logf("collision set after a 5000-key row: %d slots x %d B = %d KiB; after a 1-key row: %d slots",
		slots, unsafe.Sizeof(keySlot{}), slots*int(unsafe.Sizeof(keySlot{}))/1024, len(s.keys.slots))
}

// The Each buffer is sized once and reused.
func TestEachBufferReuse(t *testing.T) {
	tr := rudder(t, withTestKeys)
	src := []byte(rudderBenchBatch)
	opt := Options{Now: recvAt}
	s := newState(tr)
	s.runDoc(t, src, opt)
	first := cap(s.buf)
	for i := 0; i < 1000; i++ {
		s.runDoc(t, src, opt)
	}
	t.Logf("Each buffer: cap %d after the first call, %d after 1000 more (%d B of rows)", first, cap(s.buf), len(s.buf))
	if cap(s.buf) != first {
		t.Errorf("the buffer was reallocated")
	}
}

// The rule that drops a state whose buffers have outgrown the traffic.
func TestWorthPooling(t *testing.T) {
	s := &state{}
	if !s.worthPooling(3 << 20) {
		t.Fatal("the first input must be pooled")
	}
	for i := 1; i < shrinkAfter; i++ {
		if !s.worthPooling(1000) {
			t.Fatalf("small input %d dropped the state early", i)
		}
	}
	if s.worthPooling(1000) {
		t.Errorf("small input %d should drop the state", shrinkAfter)
	}

	// An input within the ratio of the peak resets the count.
	s = &state{}
	s.worthPooling(8000)
	for i := 1; i < shrinkAfter; i++ {
		s.worthPooling(999)
	}
	if !s.worthPooling(1000) || s.small != 0 {
		t.Errorf("an input at peak/%d should count as normal traffic (small=%d)", shrinkRatio, s.small)
	}
	for i := 1; i < shrinkAfter; i++ {
		if !s.worthPooling(999) {
			t.Fatalf("the count was not reset")
		}
	}

	// A new peak starts over.
	s.worthPooling(1 << 20)
	if s.peak != 1<<20 || s.small != 0 {
		t.Errorf("peak=%d small=%d after a new peak", s.peak, s.small)
	}
}

// One large batch, then small ones: the state built for the large batch
// leaves the pool and a state sized to the small ones replaces it.
func TestPoolShrinks(t *testing.T) {
	if raceEnabled {
		t.Skip("the race detector drops pooled items on purpose")
	}
	// One P and no GC, so that the pool keeps exactly what release puts.
	defer runtime.GOMAXPROCS(runtime.GOMAXPROCS(1))
	defer debug.SetGCPercent(debug.SetGCPercent(-1))

	tr := rudder(t, withTestKeys)
	opt := Options{Now: recvAt}
	if err := tr.Each(bigBatch(3<<20), opt, discard); err != nil {
		t.Fatal(err)
	}
	large := tr.pool.Get().(*state)
	before, _ := stateSize(large)
	tr.pool.Put(large)

	small := []byte(rudderBenchBatch)
	for i := 0; i <= shrinkAfter; i++ {
		if err := tr.Each(small, opt, discard); err != nil {
			t.Fatal(err)
		}
	}
	s := tr.pool.Get().(*state)
	after, _ := stateSize(s)
	tr.pool.Put(s)
	t.Logf("state after a 3 MiB batch: %d KiB; after %d small batches: %d KiB", before/1024, shrinkAfter+1, after/1024)
	if s == large {
		t.Error("the state built for the large batch is still pooled")
	}
	if after > 100<<10 {
		t.Errorf("the replacement state holds %d KiB", after/1024)
	}
}

// max_pooled_input decides which inputs leave their state in the pool.
func TestMaxPooledInput(t *testing.T) {
	if raceEnabled {
		t.Skip("the race detector drops pooled items on purpose")
	}
	defer runtime.GOMAXPROCS(runtime.GOMAXPROCS(1))
	defer debug.SetGCPercent(debug.SetGCPercent(-1))

	tr := MustCompile([]byte(`{"max_pooled_input": 100}`))
	over := []byte(`{"a":"` + string(bytes.Repeat([]byte("x"), 100)) + `"}`)
	under := []byte(`{"a":1}`)

	if _, err := tr.Transform(over); err != nil {
		t.Fatal(err)
	}
	if s := tr.pool.Get().(*state); s.peak != 0 {
		t.Errorf("a %d-byte input was pooled with max_pooled_input 100 (peak %d)", len(over), s.peak)
	} else {
		tr.pool.Put(s)
	}
	if _, err := tr.Transform(under); err != nil {
		t.Fatal(err)
	}
	if s := tr.pool.Get().(*state); s.peak != len(under) {
		t.Errorf("a %d-byte input was not pooled (peak %d)", len(under), s.peak)
	}
}
