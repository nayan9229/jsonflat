package jsonflat

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"strconv"
	"sync"
	"testing"
)

func TestTransform(t *testing.T) {
	tests := []struct {
		name   string
		config string
		in     string
		want   string
	}{
		{
			name:   "flatten only",
			config: `{}`,
			in:     `{"a":{"b":1,"c":{"d":"x"}},"e":[true,null,{"f":2.50}]}`,
			want:   `{"a.b":1,"a.c.d":"x","e.0":true,"e.1":null,"e.2.f":2.50}`,
		},
		{
			name:   "custom separator",
			config: `{"flatten":{"separator":"__"}}`,
			in:     `{"a":{"b":[1]}}`,
			want:   `{"a__b__0":1}`,
		},
		{
			name:   "empty containers are kept as values",
			config: `{}`,
			in:     `{"a":{},"b":[],"c":{"d":{}}}`,
			want:   `{"a":{},"b":[],"c.d":{}}`,
		},
		{
			name:   "empty root",
			config: `{}`,
			in:     ` {} `,
			want:   `{}`,
		},
		{
			name:   "root array",
			config: `{}`,
			in:     `[{"a":1},2]`,
			want:   `{"0.a":1,"1":2}`,
		},
		{
			name:   "empty key does not swallow the separator",
			config: `{}`,
			in:     `{"":{"a":1}}`,
			want:   `{".a":1}`,
		},
		{
			name:   "arrays raw",
			config: `{"flatten":{"arrays":"raw"}}`,
			in:     `{"a":{"b":[1,{"c":"q\"x"}]}}`,
			want:   `{"a.b":[1,{"c":"q\"x"}]}`,
		},
		{
			name:   "max depth",
			config: `{"flatten":{"max_depth":2}}`,
			in:     `{"a":{"b":{"c":1},"d":2},"e":3}`,
			want:   `{"a.b":{"c":1},"a.d":2,"e":3}`,
		},
		{
			name:   "number text is preserved",
			config: `{}`,
			in:     `{"n":12345678901234567890.123456789,"e":1E+2}`,
			want:   `{"n":12345678901234567890.123456789,"e":1E+2}`,
		},
		{
			name:   "rename leaf",
			config: `{"rules":[{"op":"rename","from":"user.id","to":"uid"}]}`,
			in:     `{"user":{"id":7,"x":1}}`,
			want:   `{"uid":7,"user.x":1}`,
		},
		{
			name:   "rename subtree, then parent prefix is restored",
			config: `{"rules":[{"op":"rename","from":"a.b","to":"B"}]}`,
			in:     `{"a":{"b":{"c":1,"d":[2]},"z":9}}`,
			want:   `{"B.c":1,"B.d.0":2,"a.z":9}`,
		},
		{
			name: "nested renames",
			config: `{"rules":[
				{"op":"rename","from":"a","to":"A"},
				{"op":"rename","from":"a.b.c","to":"C"}]}`,
			in:   `{"a":{"b":{"c":1,"d":2},"e":3}}`,
			want: `{"C":1,"A.b.d":2,"A.e":3}`,
		},
		{
			name:   "drop leaf and subtree",
			config: `{"rules":[{"op":"drop","path":"debug"},{"op":"drop","path":"a.x"}]}`,
			in:     `{"debug":{"t":1,"u":[1,2]},"a":{"x":1,"y":2},"debugger":3}`,
			want:   `{"a.y":2,"debugger":3}`,
		},
		{
			name:   "merge concat, sources consumed, document order does not matter",
			config: `{"rules":[{"op":"merge","from":["user.first","user.last"],"to":"user.name","sep":" "}]}`,
			in:     `{"user":{"last":"Lovelace","age":36,"first":"Ada"}}`,
			want:   `{"user.age":36,"user.name":"Ada Lovelace"}`,
		},
		{
			name:   "merge concat mixes types, skips null and missing",
			config: `{"rules":[{"op":"merge","from":["a","b","c","d","e"],"to":"m","sep":"-"}]}`,
			in:     `{"a":"x","b":12.5,"c":null,"e":true}`,
			want:   `{"m":"x-12.5-true"}`,
		},
		{
			name:   "merge keep_sources",
			config: `{"rules":[{"op":"merge","from":["a","b"],"to":"ab","keep_sources":true}]}`,
			in:     `{"a":"1","b":"2"}`,
			want:   `{"a":"1","b":"2","ab":"12"}`,
		},
		{
			name:   "merge array keeps null, skips missing",
			config: `{"rules":[{"op":"merge","from":["a","b","c"],"to":"m","mode":"array"}]}`,
			in:     `{"c":"z","a":null}`,
			want:   `{"m":[null,"z"]}`,
		},
		{
			name:   "merge first",
			config: `{"rules":[{"op":"merge","from":["nick","name","id"],"to":"display","mode":"first"}]}`,
			in:     `{"id":5,"name":"bob","nick":null}`,
			want:   `{"display":"bob"}`,
		},
		{
			name:   "merge with no sources present emits nothing",
			config: `{"rules":[{"op":"merge","from":["a","b"],"to":"m"}]}`,
			in:     `{"x":1}`,
			want:   `{"x":1}`,
		},
		{
			name:   "merge escapes separator and values",
			config: `{"rules":[{"op":"merge","from":["a","b"],"to":"m","sep":"\"\n"}]}`,
			in:     `{"a":"q\\","b":"\u0001"}`,
			want:   `{"m":"q\\\"\n\u0001"}`,
		},
		{
			name:   "merge source that is a container is flattened, not merged",
			config: `{"rules":[{"op":"merge","from":["a","b"],"to":"m"}]}`,
			in:     `{"a":{"k":1},"b":"v"}`,
			want:   `{"a.k":1,"m":"v"}`,
		},
		{
			name: "one source feeds two merges",
			config: `{"rules":[
				{"op":"merge","from":["a","b"],"to":"ab"},
				{"op":"merge","from":["b","a"],"to":"ba"}]}`,
			in:   `{"a":"1","b":"2"}`,
			want: `{"ab":"12","ba":"21"}`,
		},
		{
			name:   "merge source from array element",
			config: `{"rules":[{"op":"merge","from":["tags.0","tags.1"],"to":"t","sep":","}]}`,
			in:     `{"tags":["x","y","z"]}`,
			want:   `{"tags.2":"z","t":"x,y"}`,
		},
		{
			name: "default only when missing",
			config: `{"rules":[
				{"op":"default","path":"env","value":"prod"},
				{"op":"default","path":"a.b","value":{"k": [1, 2]}}]}`,
			in:   `{"a":{"b":0}}`,
			want: `{"a.b":0,"env":"prod"}`,
		},
		{
			name: "default sees renamed and merged keys",
			config: `{"rules":[
				{"op":"rename","from":"e","to":"env"},
				{"op":"merge","from":["x","y"],"to":"xy"},
				{"op":"default","path":"env","value":"prod"},
				{"op":"default","path":"xy","value":"none"}]}`,
			in:   `{"e":"dev","x":"1"}`,
			want: `{"env":"dev","xy":"1"}`,
		},
		{
			name:   "drop wins over merge source below it",
			config: `{"rules":[{"op":"drop","path":"a"},{"op":"merge","from":["a.b","c"],"to":"m"}]}`,
			in:     `{"a":{"b":"x"},"c":"y"}`,
			want:   `{"m":"y"}`,
		},
		{
			name:   "keys and strings are escaped as JSON, not as Go",
			config: `{}`,
			in:     `{"k\u0001\"":"v\u0007\u007f\\ é 😀"}`,
			want:   "{\"k\\u0001\\\"\":\"v\\u0007\u007f\\\\ é 😀\"}",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tr, err := Compile([]byte(tc.config))
			if err != nil {
				t.Fatalf("Compile: %v", err)
			}
			// Run twice so the second run uses pooled state.
			for i := 0; i < 2; i++ {
				got, err := tr.Transform([]byte(tc.in))
				if err != nil {
					t.Fatalf("Transform: %v", err)
				}
				if string(got) != tc.want {
					t.Fatalf("run %d\n got: %s\nwant: %s", i, got, tc.want)
				}
				if !json.Valid(got) {
					t.Fatalf("output is not valid JSON: %s", got)
				}
			}
		})
	}
}

func TestErrors(t *testing.T) {
	tr := MustCompile([]byte(`{}`))

	prefix := []byte("keep")
	out, err := tr.Append(prefix, []byte(`{"a":`))
	if err == nil {
		t.Fatal("want a parse error")
	}
	if string(out) != "keep" {
		t.Fatalf("dst changed on error: %q", out)
	}

	if _, err := tr.Transform([]byte(`42`)); !errors.Is(err, ErrRootNotContainer) {
		t.Fatalf("got %v, want ErrRootNotContainer", err)
	}
	for _, in := range []string{`{"a":00}`, `{"a":+1}`, `{"a":1.2.3}`, `{"a":[NaN]}`, `{"a":{"b":-inf}}`, `{"a":1.}`, `{"a":1e}`} {
		out, err := tr.Append(prefix, []byte(in))
		if !errors.Is(err, ErrInvalidNumber) {
			t.Errorf("%s: got %v, want ErrInvalidNumber", in, err)
		}
		if string(out) != "keep" {
			t.Errorf("%s: dst changed on error: %q", in, out)
		}
	}
	if _, err := tr.Transform([]byte(`{"a":1} x`)); err == nil {
		t.Fatal("want an error for trailing data")
	}
}

func TestCompileErrors(t *testing.T) {
	bad := []string{
		`{"rules":[{"op":"nope"}]}`,
		`{"rules":[{"op":"drop"}]}`,
		`{"rules":[{"op":"rename","from":["a","b"],"to":"c"}]}`,
		`{"rules":[{"op":"rename","from":"a"}]}`,
		`{"rules":[{"op":"rename","from":"a","to":"b"},{"op":"rename","from":"a","to":"c"}]}`,
		`{"rules":[{"op":"rename","from":"a","to":"b"},{"op":"drop","path":"a"}]}`,
		`{"rules":[{"op":"merge","from":[],"to":"c"}]}`,
		`{"rules":[{"op":"merge","from":["a"],"to":"c","mode":"zip"}]}`,
		`{"rules":[{"op":"default","path":"a"}]}`,
		`{"rules":[{"op":"default","path":"a","value":1},{"op":"default","path":"a","value":2}]}`,
		`{"flatten":{"arrays":"sideways"}}`,
		`{"flatten":{"max_depth":-1}}`,
		`{"ruels":[]}`,
		`{} {}`,
	}
	for _, c := range bad {
		if _, err := Compile([]byte(c)); err == nil {
			t.Errorf("Compile(%s): want an error", c)
		}
	}
}

// referenceFlatten is a slow, obviously correct flattener built on
// encoding/json. The differential test compares it with Transform.
func referenceFlatten(prefix string, v any, out map[string]any) {
	switch x := v.(type) {
	case map[string]any:
		if len(x) == 0 && prefix != "" {
			out[prefix] = x
			return
		}
		for k, c := range x {
			p := k
			if prefix != "" {
				p = prefix + "." + k
			}
			referenceFlatten(p, c, out)
		}
	case []any:
		if len(x) == 0 && prefix != "" {
			out[prefix] = x
			return
		}
		for i, c := range x {
			p := strconv.Itoa(i)
			if prefix != "" {
				p = prefix + "." + p
			}
			referenceFlatten(p, c, out)
		}
	default:
		out[prefix] = x
	}
}

func decodeUseNumber(t *testing.T, b []byte) any {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		t.Fatalf("decode %s: %v", b, err)
	}
	return v
}

func TestDifferential(t *testing.T) {
	tr := MustCompile([]byte(`{}`))
	docs := []string{
		benchDoc,
		`{"a":[[1,[2,[3]]],{"b":[{"c":null}]}],"d":{"e":{"f":{"g":"h"}}}}`,
		`{"s":"tab\there \"quoted\" back\\slash \u00e9 \ud83d\ude00 \u0000","k\n":1}`,
		`[1,"two",[3,{"four":4.0e0}],{}]`,
		`{"x":-0,"y":1e400,"z":0.1}`,
	}
	for _, doc := range docs {
		got, err := tr.Transform([]byte(doc))
		if err != nil {
			t.Fatalf("Transform(%s): %v", doc, err)
		}
		want := map[string]any{}
		referenceFlatten("", decodeUseNumber(t, []byte(doc)), want)
		if !reflect.DeepEqual(decodeUseNumber(t, got), any(want)) {
			t.Errorf("mismatch for %s\n got: %s", doc, got)
		}
	}
}

// TestZeroAllocs is the guard for the package's main promise. It fails if a
// change introduces a heap allocation into the hot path.
func TestZeroAllocs(t *testing.T) {
	if raceEnabled {
		t.Skip("the race detector makes sync.Pool drop items, which allocates")
	}
	tr := MustCompile([]byte(benchConfig))
	src := []byte(benchDoc)
	out, err := tr.Append(nil, src) // warm up the pool and size the buffer
	if err != nil {
		t.Fatal(err)
	}
	allocs := testing.AllocsPerRun(200, func() {
		out, _ = tr.Append(out[:0], src)
	})
	if allocs != 0 {
		t.Fatalf("Append allocates %v times per call, want 0", allocs)
	}
}

func TestConcurrent(t *testing.T) {
	tr := MustCompile([]byte(benchConfig))
	want, err := tr.Transform([]byte(benchDoc))
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var out []byte
			for i := 0; i < 500; i++ {
				var err error
				out, err = tr.Append(out[:0], []byte(benchDoc))
				if err != nil || !bytes.Equal(out, want) {
					t.Errorf("unexpected result: %v %s", err, out)
					return
				}
			}
		}()
	}
	wg.Wait()
}

// FuzzAppend checks two properties on arbitrary input: no panics, and any
// input that is accepted produces valid JSON.
func FuzzAppend(f *testing.F) {
	f.Add([]byte(benchDoc))
	f.Add([]byte(`{"a":{"b":[1,2,{"c":"\u0001"}]},"user":{"first":"a","last":"b"}}`))
	f.Add([]byte(`[[],{},"\ud83d"]`))
	tr := MustCompile([]byte(benchConfig))
	raw := MustCompile([]byte(`{"flatten":{"arrays":"raw","max_depth":1}}`))
	f.Fuzz(func(t *testing.T, in []byte) {
		for _, x := range []*Transformer{tr, raw} {
			out, err := x.Transform(in)
			if err != nil {
				continue
			}
			if !json.Valid(out) {
				t.Fatalf("invalid JSON output\n in: %q\nout: %q", in, out)
			}
		}
	})
}
