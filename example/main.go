// Command example flattens an NDJSON event export with jsonflat: one flat JSON
// row per input line, with duplicate keys and duplicate fields merged by the
// config in config.json.
//
//	go run ./example -in events.ndjson.gz -out flat.ndjson
//	go run ./example -in events.ndjson.gz -out flat.ndjson -verify
//	gzip -dc events.ndjson.gz | go run ./example -in - > flat.ndjson
//
// Rows go to -out (stdout by default) and a summary goes to stderr, so the two
// never mix.
package main

import (
	"bufio"
	"bytes"
	"compress/gzip"
	_ "embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"maps"
	"os"
	"slices"
	"time"

	"github.com/nayan9229/jsonflat"
)

//go:embed config.json
var defaultConfig []byte

// maxLine is the longest input line accepted. Memory use follows the largest
// document, so a cap belongs in front of jsonflat anyway.
const maxLine = 8 << 20

// maxSamples is how many problems are shown in full; the rest are only counted.
const maxSamples = 5

type options struct {
	in, out, config string
	verify, columns bool
	limit           int
}

func main() {
	var o options
	flag.StringVar(&o.in, "in", "", "input NDJSON file, gzip or plain; - for stdin")
	flag.StringVar(&o.out, "out", "-", "output NDJSON file; - for stdout")
	flag.StringVar(&o.config, "config", "", "jsonflat config file (default: the embedded config.json)")
	flag.BoolVar(&o.verify, "verify", false, "check every row: valid JSON, no repeated key (slow)")
	flag.BoolVar(&o.columns, "columns", false, "with -verify, list every column")
	flag.IntVar(&o.limit, "limit", 0, "stop after this many lines; 0 means all")
	flag.Parse()
	if o.in == "" || flag.NArg() > 0 {
		flag.Usage()
		os.Exit(2)
	}
	if err := run(o); err != nil {
		fmt.Fprintln(os.Stderr, "example:", err)
		os.Exit(1)
	}
}

func run(o options) error {
	config := defaultConfig
	if o.config != "" {
		var err error
		if config, err = os.ReadFile(o.config); err != nil {
			return err
		}
	}
	// Compile once. The Transformer is safe to share between goroutines.
	t, err := jsonflat.Compile(config)
	if err != nil {
		return err
	}

	in, closeIn, err := openInput(o.in)
	if err != nil {
		return err
	}
	defer closeIn()

	out := os.Stdout
	if o.out != "-" {
		if out, err = os.Create(o.out); err != nil {
			return err
		}
	}

	start := time.Now()
	st, err := flatten(in, out, t, o.verify, o.limit)
	elapsed := time.Since(start)
	if out != os.Stdout {
		if cerr := out.Close(); err == nil {
			err = cerr
		}
	}
	st.print(os.Stderr, elapsed, o.columns)
	if err != nil {
		return err
	}
	if st.invalid > 0 {
		return fmt.Errorf("%d rows failed verification", st.invalid)
	}
	return nil
}

// openInput opens path, or stdin for "-", and unpacks gzip when the data
// starts with the gzip magic bytes, whatever the file is called.
func openInput(path string) (io.Reader, func() error, error) {
	f := os.Stdin
	if path != "-" {
		var err error
		if f, err = os.Open(path); err != nil {
			return nil, nil, err
		}
	}
	br := bufio.NewReaderSize(f, 1<<20)
	if magic, err := br.Peek(2); err != nil || magic[0] != 0x1f || magic[1] != 0x8b {
		return br, f.Close, nil
	}
	zr, err := gzip.NewReader(br)
	if err != nil {
		f.Close()
		return nil, nil, err
	}
	return zr, f.Close, nil
}

type stats struct {
	lines      int            // non-empty input lines
	rows       int            // rows written
	badLines   int            // lines that are not valid JSON
	recordErrs map[string]int // failed records, by error
	merged     int            // keys left out because their name was taken
	invalid    int            // rows that failed -verify
	bytesIn    int64
	bytesOut   int64
	columns    map[string]int // column -> rows that have it; only with -verify
	samples    []string
}

func (st *stats) sample(format string, args ...any) {
	if len(st.samples) < maxSamples {
		st.samples = append(st.samples, fmt.Sprintf(format, args...))
	}
}

// flatten turns every line of in into a row on out. A bad line or a bad
// record is counted and the run carries on. It stops for an I/O error and for
// a line longer than maxLine.
func flatten(in io.Reader, out io.Writer, t *jsonflat.Transformer, verify bool, limit int) (*stats, error) {
	st := &stats{recordErrs: map[string]int{}}
	var rowKeys map[string]struct{}
	if verify {
		st.columns = map[string]int{}
		rowKeys = map[string]struct{}{}
	}

	w := bufio.NewWriterSize(out, 1<<20)
	var writeErr error
	lineNo := 0 // position in the file, blank lines included

	// The callback is created once, outside the loop, so the loop itself
	// does not allocate.
	onRecord := func(r *jsonflat.Record) error {
		if r.Err != nil {
			st.recordErrs[kind(r.Err)]++
			st.sample("line %d (%s): %v", lineNo, id(r), r.Err)
			return nil
		}
		if verify {
			if err := checkRow(r.JSON, rowKeys, st.columns); err != nil {
				st.invalid++
				st.sample("line %d (%s): bad row: %v", lineNo, id(r), err)
				return nil
			}
		}
		// r.JSON is only valid until this function returns. The writer
		// copies it.
		if _, err := w.Write(r.JSON); err != nil {
			writeErr = err
			return err
		}
		st.rows++
		st.merged += r.Dropped
		st.bytesOut += int64(len(r.JSON))
		return nil
	}

	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 0, 64<<10), maxLine)
	for sc.Scan() {
		lineNo++
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		if limit > 0 && st.lines == limit {
			break
		}
		st.lines++
		st.bytesIn += int64(len(sc.Bytes())) + 1

		err := t.Each(line, jsonflat.Options{}, onRecord)
		if writeErr != nil {
			return st, writeErr
		}
		if err != nil { // the line is not valid JSON
			st.badLines++
			st.sample("line %d: %v", lineNo, err)
		}
	}
	if err := sc.Err(); err != nil {
		return st, fmt.Errorf("line %d: %w", lineNo+1, err)
	}
	return st, w.Flush()
}

// recordErrors are the ways jsonflat can reject one record.
var recordErrors = []error{
	jsonflat.ErrRootNotContainer,
	jsonflat.ErrInvalidNumber,
	jsonflat.ErrCollision,
	jsonflat.ErrRequired,
	jsonflat.ErrNoOutput,
}

// kind names an error by its sentinel, without the text that varies per record.
func kind(err error) string {
	for _, sentinel := range recordErrors {
		if errors.Is(err, sentinel) {
			return sentinel.Error()
		}
	}
	return "other"
}

// id describes a record by its exposed fields, for error messages.
func id(r *jsonflat.Record) string {
	if len(r.Fields) == 0 {
		return fmt.Sprintf("record %d", r.Index)
	}
	return fmt.Sprintf("%q", r.Fields)
}

// checkRow is the slow, independent check behind -verify: the row must be one
// valid JSON object without a repeated key. seen is scratch space; columns
// collects the column names.
func checkRow(row []byte, seen map[string]struct{}, columns map[string]int) error {
	if !json.Valid(row) {
		return errors.New("not valid JSON")
	}
	dec := json.NewDecoder(bytes.NewReader(row))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return errors.New("not an object")
	}
	clear(seen)
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return err
		}
		key, ok := tok.(string)
		if !ok {
			return fmt.Errorf("unexpected token %v", tok)
		}
		if _, dup := seen[key]; dup {
			return fmt.Errorf("key %q appears twice", key)
		}
		seen[key] = struct{}{}
		var value json.RawMessage
		if err := dec.Decode(&value); err != nil {
			return err
		}
	}
	for key := range seen {
		columns[key]++
	}
	return nil
}

func (st *stats) print(w io.Writer, elapsed time.Duration, listColumns bool) {
	failed := 0
	for _, n := range st.recordErrs {
		failed += n
	}
	fmt.Fprintf(w, "lines            %d\n", st.lines)
	fmt.Fprintf(w, "rows written     %d\n", st.rows)
	fmt.Fprintf(w, "bad lines        %d\n", st.badLines)
	fmt.Fprintf(w, "failed records   %d\n", failed)
	for _, name := range slices.Sorted(maps.Keys(st.recordErrs)) {
		fmt.Fprintf(w, "  %d  %s\n", st.recordErrs[name], name)
	}
	fmt.Fprintf(w, "merged keys      %d   (same name after normalising; the first value is kept)\n", st.merged)
	if st.columns != nil {
		fmt.Fprintf(w, "verify failures  %d\n", st.invalid)
		fmt.Fprintf(w, "distinct columns %d\n", len(st.columns))
	}
	fmt.Fprintf(w, "input            %.1f MB\n", float64(st.bytesIn)/1e6)
	fmt.Fprintf(w, "output           %.1f MB\n", float64(st.bytesOut)/1e6)
	if secs := elapsed.Seconds(); secs > 0 {
		fmt.Fprintf(w, "elapsed          %s   (%.0f lines/s, %.1f MB/s, including I/O)\n",
			elapsed.Round(time.Millisecond), float64(st.lines)/secs, float64(st.bytesIn)/1e6/secs)
	}
	for _, s := range st.samples {
		fmt.Fprintf(w, "  ! %s\n", s)
	}
	if listColumns {
		fmt.Fprintln(w, "columns (rows that have them):")
		for _, name := range slices.Sorted(maps.Keys(st.columns)) {
			fmt.Fprintf(w, "  %-40s %d\n", name, st.columns[name])
		}
	}
}
