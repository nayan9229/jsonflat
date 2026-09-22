package main

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nayan9229/jsonflat"
)

// Synthetic lines in the shape of the events the example was written for:
// RudderStack SDK payloads with the ad payload under properties.
const input = `{"type":"track","messageId":"m-1","anonymousId":"a-1","userId":"","event":"Ad Error","channel":"web","originalTimestamp":"2026-09-11T16:59:39.334Z","sentAt":"2026-09-11T16:59:39.338Z","receivedAt":"2026-09-11T16:59:40.290Z","timestamp":"2026-09-11T16:59:40.290Z","requestIp":"10.0.0.1","context":{"traits":{},"ip":"10.0.0.1","userAgent":"UA-1","library":{"name":"RudderLabs JavaScript SDK"}},"properties":{"event_name":"Ad Error","event_details":{"tag_id":"t-1","tagId":"t-1","brand_id":3252,"is_muted":true},"device_details":{"app_bundle":"com.example.app","user_agent":"UA-1","geoip":{"ip":"1.2.3.4"}},"user_details":{"ifa":"ifa-1","deviceid":"ifa-1","app_store_id":"s-1"}},"integrations":{"All":true}}
{"type":"track","messageId":"m-2","anonymousId":"brand_1","userId":"brand_1","event":"px-lo","timestamp":"2026-09-11T16:59:39.598134831Z","sentAt":"2026-09-11T16:59:40.328541957Z","receivedAt":"2026-09-11T16:59:40.340Z","requestIp":"10.0.0.2","context":{"library":{"name":"analytics-go"},"ip":"10.0.0.2","userAgent":"UA-2"},"properties":{"event_name":"px-lo","d":"com.example.two","device_details":{"app_bundle":"com.example.two","user_agent":"UA-2"},"h":"50","w":"320","_cb":"9"}}

42
{"broken":
{"type":"track","messageId":"m-5","anonymousId":"a-5","event":"Tag Captured","originalTimestamp":"2026-09-11T10:00:00.000Z","sentAt":"2026-09-11T10:00:02.000Z","context":{"ip":"10.0.0.5"},"properties":{"event_name":"Tag Captured"}}
`

const want = `{"id":"m-1","anonymous_id":"a-1","user_id":"","type":"track","event":"ad_error","event_text":"Ad Error","channel":"web","original_timestamp":"2026-09-11T16:59:39.334Z","sent_at":"2026-09-11T16:59:39.338Z","received_at":"2026-09-11T16:59:40.290Z","timestamp":"2026-09-11T16:59:40.290Z","request_ip":"10.0.0.1","brand_id":3252,"tag_id":"t-1","app_bundle":"com.example.app","app_store_id":"s-1","user_agent":"UA-1","ifa":"ifa-1","geoip_ip":"1.2.3.4","is_muted":true,"context_library":"RudderLabs JavaScript SDK"}
{"id":"m-2","anonymous_id":"brand_1","user_id":"brand_1","type":"track","event":"px_lo","event_text":"px-lo","sent_at":"2026-09-11T16:59:40.328541957Z","received_at":"2026-09-11T16:59:40.340Z","timestamp":"2026-09-11T16:59:39.598134831Z","request_ip":"10.0.0.2","app_bundle":"com.example.two","user_agent":"UA-2","width":"320","height":"50","context_library":"analytics-go"}
`

func TestFlatten(t *testing.T) {
	tr, err := jsonflat.Compile(defaultConfig)
	if err != nil {
		t.Fatalf("the embedded config does not compile: %v", err)
	}
	for _, verify := range []bool{false, true} {
		var out bytes.Buffer
		st, err := flatten(strings.NewReader(input), &out, tr, options{verify: verify})
		if err != nil {
			t.Fatal(err)
		}
		// The last record has no receivedAt and no timestamp, so both fall
		// back to $now; it is checked without the clock-dependent values.
		got := strings.SplitN(out.String(), "\n", 3)
		if len(got) != 3 || got[0]+"\n"+got[1]+"\n" != want {
			t.Errorf("verify=%v\n got: %s\nwant: %s", verify, out.String(), want)
		}
		last := map[string]any{}
		if err := json.Unmarshal([]byte(got[2]), &last); err != nil {
			t.Fatalf("third row: %v", err)
		}
		// timestamp comes from clock_skew (now - 2 s), received_at from now.
		if last["event"] != "tag_captured" || last["event_text"] != "Tag Captured" || last["request_ip"] != "10.0.0.5" || last["timestamp"] == nil ||
			last["received_at"] == nil || last["timestamp"] == last["received_at"] {
			t.Errorf("third row: %s", got[2])
		}
		if st.lines != 5 || st.rows != 3 || st.badLines != 1 || st.merged != 0 || st.invalid != 0 {
			t.Errorf("verify=%v: stats %+v", verify, st)
		}
		if st.recordErrs[jsonflat.ErrRootNotContainer.Error()] != 1 || len(st.recordErrs) != 1 {
			t.Errorf("record errors: %v", st.recordErrs)
		}
		if len(st.samples) != 2 || !strings.Contains(st.samples[0], "line 4") || !strings.Contains(st.samples[1], "line 5") {
			t.Errorf("samples: %q", st.samples)
		}
		if verify && (st.columns["event"] != 3 || st.columns["event_text"] != 3 || st.columns["user_agent"] != 2 || st.columns["tag_id"] != 1 || st.columns["extra"] != 0) {
			t.Errorf("columns: %v", st.columns)
		}
	}
}

// The bundled sample events are the default input and must all flatten.
func TestSampleEvents(t *testing.T) {
	tr := jsonflat.MustCompile(defaultConfig)
	var out bytes.Buffer
	st, err := flatten(bytes.NewReader(sampleEvents), &out, tr, options{verify: true})
	if err != nil {
		t.Fatal(err)
	}
	if st.lines != 7 || st.rows != 7 || st.badLines != 0 || len(st.recordErrs) != 0 || st.invalid != 0 {
		t.Fatalf("stats %+v", st)
	}
	// Every copy of a column's source is dropped, so it is neither a column
	// nor a leftover in extra.
	for _, gone := range []string{`"event_name"`, `"context_ip"`, `"context_user_agent"`, `"device_details_user_agent"`,
		`"user_details_deviceid"`, `"event_details_tag_id"`, `"device_details_app_bundle"`, `\"d\":`} {
		if strings.Contains(out.String(), gone) {
			t.Errorf("%s should have been dropped", gone)
		}
	}
	if st.columns["app_bundle"] != 7 || st.columns["user_agent"] != 7 || st.columns["request_ip"] != 7 || st.columns["extra"] == 0 {
		t.Errorf("columns missing from some rows: %v", st.columns)
	}
	// What is left in extra is only what no column maps and drop does not name.
	if !strings.Contains(out.String(), `\"context_screen_width\":`) || strings.Contains(out.String(), `\"event_details_valid\":`) {
		t.Errorf("extra holds the wrong keys:\n%s", out.String())
	}
}

func TestPretty(t *testing.T) {
	tr := jsonflat.MustCompile(defaultConfig)
	var out bytes.Buffer
	line := `{"type":"track","messageId":"m","event":"e","receivedAt":"2026-01-01T00:00:00Z","timestamp":"2026-01-01T00:00:00Z"}`
	if _, err := flatten(strings.NewReader(line), &out, tr, options{pretty: true}); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); !strings.HasPrefix(got, "{\n  \"id\": \"m\",\n") || !strings.HasSuffix(got, "}\n") {
		t.Errorf("not indented:\n%s", got)
	}
}

func TestLimit(t *testing.T) {
	tr := jsonflat.MustCompile(defaultConfig)
	var out bytes.Buffer
	st, err := flatten(strings.NewReader(input), &out, tr, options{limit: 2})
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
