package main

import (
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nayan9229/jsonflat"
)

// Synthetic lines in the shape of the export the example was written for.
const input = `{"context":{"ip":"10.0.0.1","traits":{}},"event_name":"ad_request","event":"ad_request","request_ip":"10.0.0.1","d":"com.example.app","device_details":{"app_bundle":"com.example.app","appName":"Demo"},"ad-id":"a1","visit-id":"v1","visit_id":"v1","_cb":"9","message_id":"m-1","type":"track"}
{"event":"px-lo","d":"com.example.two","device_details":{},"message_id":"m-2"}

42
{"broken":
{"event_name":"only_name","request_ip":"10.0.0.9","message_id":"m-5"}
`

const want = `{"device_details_app_name":"Demo","ad_id":"a1","visit_id":"v1","cb":"9","message_id":"m-1","type":"track","event":"ad_request","context_ip":"10.0.0.1","device_details_app_bundle":"com.example.app"}
{"message_id":"m-2","event":"px-lo","device_details_app_bundle":"com.example.two"}
{"message_id":"m-5","event":"only_name","context_ip":"10.0.0.9"}
`

func TestFlatten(t *testing.T) {
	tr, err := jsonflat.Compile(defaultConfig)
	if err != nil {
		t.Fatalf("the embedded config does not compile: %v", err)
	}
	for _, verify := range []bool{false, true} {
		var out bytes.Buffer
		st, err := flatten(strings.NewReader(input), &out, tr, verify, 0)
		if err != nil {
			t.Fatal(err)
		}
		if out.String() != want {
			t.Errorf("verify=%v\n got: %s\nwant: %s", verify, out.String(), want)
		}
		if st.lines != 5 || st.rows != 3 || st.badLines != 1 || st.merged != 1 || st.invalid != 0 {
			t.Errorf("verify=%v: stats %+v", verify, st)
		}
		if st.recordErrs[jsonflat.ErrRootNotContainer.Error()] != 1 || len(st.recordErrs) != 1 {
			t.Errorf("record errors: %v", st.recordErrs)
		}
		if len(st.samples) != 2 || !strings.Contains(st.samples[0], "line 4") || !strings.Contains(st.samples[1], "line 5") {
			t.Errorf("samples: %q", st.samples)
		}
		if verify && (len(st.columns) != 9 || st.columns["event"] != 3 || st.columns["visit_id"] != 1) {
			t.Errorf("columns: %v", st.columns)
		}
	}
}

func TestLimit(t *testing.T) {
	tr := jsonflat.MustCompile(defaultConfig)
	var out bytes.Buffer
	st, err := flatten(strings.NewReader(input), &out, tr, false, 2)
	if err != nil || st.lines != 2 || st.rows != 2 {
		t.Errorf("stats %+v, err %v", st, err)
	}
}

func TestCheckRow(t *testing.T) {
	for row, ok := range map[string]bool{
		`{"a":1,"b":{"a":2},"c":[1,{"a":3}]}`: true,
		`{}`:                                  true,
		`{"a":1,"a":2}`:                       false,
		`{"a":1`:                              false,
		`[1]`:                                 false,
	} {
		err := checkRow([]byte(row), map[string]struct{}{}, map[string]int{})
		if (err == nil) != ok {
			t.Errorf("checkRow(%s) = %v", row, err)
		}
	}
}

// Gzip is detected from the data, not from the file name.
func TestOpenInput(t *testing.T) {
	dir := t.TempDir()
	plain := filepath.Join(dir, "plain.gz") // misleading on purpose
	packed := filepath.Join(dir, "packed.ndjson")
	if err := os.WriteFile(plain, []byte(input), 0o600); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	zw.Write([]byte(input))
	zw.Close()
	if err := os.WriteFile(packed, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{plain, packed} {
		r, closeIn, err := openInput(path)
		if err != nil {
			t.Fatal(err)
		}
		got, err := io.ReadAll(r)
		closeIn()
		if err != nil || string(got) != input {
			t.Errorf("%s: read %d bytes, err %v", path, len(got), err)
		}
	}
}
