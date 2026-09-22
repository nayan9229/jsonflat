package jsonflat_test

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nayan9229/jsonflat"
)

// TestDocs keeps the documentation honest. In README.md and every Markdown
// file under docs/, example/ and presets/:
//
//   - a fenced block with the info string "json" is a complete config and
//     must compile (fragments use "jsonc" and are left alone);
//   - a fenced block with the info string "example" is replayed against the
//     json block before it and compared byte for byte.
//
// An example block is a list of directives, one per line; a line that starts
// with none of them continues the previous directive:
//
//	in: <document>                the input; may span lines
//	now: <RFC 3339>               Options.Now; default 2026-09-21T10:00:05.5Z
//	var <name>=<value>            an entry of Options.Vars
//	out: <json>                   the one row of a config that Append accepts
//	row <name>: <json>            one row of Each, in order ("row: " without outputs)
//	err: <ErrName>                a failed record, or the error of the call
//	fields: <a>,<b>               Record.Fields of the row above, "-" for nil
//	dropped: <n>                  Record.Dropped of the row above
//	# ...                         a comment
//
// A trailing newline in a row (from "newline": true) is written as \n.
func TestDocs(t *testing.T) {
	var files []string
	for _, pattern := range []string{"README.md", "docs/*.md", "example/*.md", "presets/*.md"} {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, matches...)
	}
	if len(files) < 5 {
		t.Fatalf("found only %d Markdown files: %v", len(files), files)
	}
	configs, examples := 0, 0
	for _, file := range files {
		blocks, err := fencedBlocks(file)
		if err != nil {
			t.Fatal(err)
		}
		var last *fenced
		for i := range blocks {
			b := &blocks[i]
			switch b.info {
			case "json":
				if _, err := jsonflat.Compile([]byte(b.body)); err != nil {
					t.Errorf("%s:%d: config does not compile: %v", file, b.line, err)
					last = nil
					continue
				}
				configs++
				last = b
			case "example":
				if last == nil {
					t.Errorf("%s:%d: example block without a json config block before it", file, b.line)
					continue
				}
				examples++
				if err := replay(last.body, b.body); err != nil {
					t.Errorf("%s:%d: %v", file, b.line, err)
				}
			}
		}
	}
	t.Logf("%d configs compiled, %d examples replayed", configs, examples)
	if configs == 0 || examples == 0 {
		t.Fatal("no configs or examples found; the fence detection is broken")
	}
}

type fenced struct {
	info string
	line int
	body string
}

// fencedBlocks returns the fenced code blocks of a Markdown file. Fences are
// three or more backticks at the start of a line; the info string is the first
// word after them.
func fencedBlocks(path string) ([]fenced, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var blocks []fenced
	var cur *fenced
	var fence string
	var body strings.Builder
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64<<10), 4<<20)
	for n := 1; sc.Scan(); n++ {
		line := sc.Text()
		trimmed := strings.TrimLeft(line, " ")
		if cur == nil {
			if strings.HasPrefix(trimmed, "```") {
				fence = trimmed[:len(trimmed)-len(strings.TrimLeft(trimmed, "`"))]
				info := strings.Fields(strings.TrimPrefix(trimmed, fence))
				cur = &fenced{line: n}
				if len(info) > 0 {
					cur.info = info[0]
				}
				body.Reset()
			}
			continue
		}
		if strings.TrimSpace(line) == fence {
			cur.body = body.String()
			blocks = append(blocks, *cur)
			cur = nil
			continue
		}
		body.WriteString(line)
		body.WriteByte('\n')
	}
	if cur != nil {
		return nil, fmt.Errorf("%s:%d: fenced block is not closed", path, cur.line)
	}
	return blocks, sc.Err()
}

var docErrors = map[string]error{
	"ErrRootNotContainer": jsonflat.ErrRootNotContainer,
	"ErrInvalidNumber":    jsonflat.ErrInvalidNumber,
	"ErrCollision":        jsonflat.ErrCollision,
	"ErrRequired":         jsonflat.ErrRequired,
	"ErrNoOutput":         jsonflat.ErrNoOutput,
	"ErrExplode":          jsonflat.ErrExplode,
	"ErrNotSimple":        jsonflat.ErrNotSimple,
}

// docExample is one parsed example block.
type docExample struct {
	in   string
	opt  jsonflat.Options
	want []string // expected result lines, normalised
}

func parseExample(body string) (*docExample, error) {
	ex := &docExample{opt: jsonflat.Options{
		Now:  time.Date(2026, 9, 21, 10, 0, 5, 500_000_000, time.UTC),
		Vars: map[string]string{},
	}}
	var key, value string
	flush := func() error {
		if key == "" {
			return nil
		}
		value = strings.TrimSpace(value)
		switch key {
		case "in":
			ex.in = value
		case "now":
			at, err := time.Parse(time.RFC3339Nano, value)
			if err != nil {
				return fmt.Errorf("now: %w", err)
			}
			ex.opt.Now = at
		case "var":
			name, val, ok := strings.Cut(value, "=")
			if !ok {
				return fmt.Errorf("var: want name=value, got %q", value)
			}
			ex.opt.Vars[strings.TrimSpace(name)] = strings.TrimSpace(val)
		case "out", "err":
			ex.want = append(ex.want, key+": "+value)
		case "fields", "dropped":
			if len(ex.want) == 0 {
				return fmt.Errorf("%s: no row before it", key)
			}
			ex.want[len(ex.want)-1] += "\n  " + key + ": " + value
		default: // row, row <name>
			ex.want = append(ex.want, key+": "+value)
		}
		key, value = "", ""
		return nil
	}
	for _, line := range strings.Split(body, "\n") {
		switch {
		case strings.HasPrefix(line, "#"):
			continue
		case strings.HasPrefix(line, "var "):
			if err := flush(); err != nil {
				return nil, err
			}
			key, value = "var", line[4:]
			continue
		}
		if k, v, ok := strings.Cut(line, ":"); ok {
			word := strings.Fields(k)
			if len(word) > 0 {
				switch word[0] {
				case "in", "now", "out", "err", "fields", "dropped", "row":
					if len(word) == 1 || word[0] == "row" {
						if err := flush(); err != nil {
							return nil, err
						}
						key, value = strings.TrimSpace(k), v
						continue
					}
				}
			}
		}
		if key != "" {
			value += "\n" + line
		} else if strings.TrimSpace(line) != "" {
			return nil, fmt.Errorf("unexpected line %q", line)
		}
	}
	if err := flush(); err != nil {
		return nil, err
	}
	if ex.in == "" {
		return nil, errors.New("example has no in:")
	}
	return ex, nil
}

// replay runs an example and compares the result with what the docs say.
func replay(config, body string) error {
	ex, err := parseExample(body)
	if err != nil {
		return err
	}
	tr, err := jsonflat.Compile([]byte(config))
	if err != nil {
		return err
	}

	// Each is used for everything, so that Options apply; a config that
	// Append accepts is shown as "out:", which is what Append would return.
	_, err = tr.Transform([]byte(ex.in))
	simple := !errors.Is(err, jsonflat.ErrNotSimple)

	var got []string
	err = tr.Each([]byte(ex.in), ex.opt, func(r *jsonflat.Record) error {
		switch {
		case r.Err != nil:
			got = append(got, "err: "+errName(r.Err))
		case simple:
			got = append(got, "out: "+shown(r.JSON))
		case r.Name != nil:
			got = append(got, "row "+string(r.Name)+": "+shown(r.JSON))
		default:
			got = append(got, "row: "+shown(r.JSON))
		}
		return nil
	})
	if err != nil {
		got = []string{"err: " + errName(err)}
	}

	if len(got) != len(ex.want) {
		return fmt.Errorf("want %d result lines, got %d:\n%s\n--- want:\n%s",
			len(ex.want), len(got), strings.Join(got, "\n"), strings.Join(ex.want, "\n"))
	}
	for i := range got {
		want := ex.want[i]
		// fields: and dropped: are checked only when the docs state them.
		wantRow, _, _ := strings.Cut(want, "\n")
		if got[i] != wantRow {
			return fmt.Errorf("result %d\n got: %s\nwant: %s", i, got[i], wantRow)
		}
	}
	return checkRowDetails(tr, ex)
}

// checkRowDetails re-runs Each for the fields: and dropped: directives.
func checkRowDetails(tr *jsonflat.Transformer, ex *docExample) error {
	needed := false
	for _, w := range ex.want {
		if strings.Contains(w, "\n  fields: ") || strings.Contains(w, "\n  dropped: ") {
			needed = true
		}
	}
	if !needed {
		return nil
	}
	i := 0
	return tr.Each([]byte(ex.in), ex.opt, func(r *jsonflat.Record) error {
		defer func() { i++ }()
		if i >= len(ex.want) {
			return nil
		}
		for _, detail := range strings.Split(ex.want[i], "\n")[1:] {
			key, value, _ := strings.Cut(strings.TrimSpace(detail), ": ")
			switch key {
			case "fields":
				var parts []string
				for _, f := range r.Fields {
					if f == nil {
						parts = append(parts, "-")
					} else {
						parts = append(parts, string(f))
					}
				}
				if got := strings.Join(parts, ","); got != value {
					return fmt.Errorf("result %d fields: got %q, want %q", i, got, value)
				}
			case "dropped":
				if got := fmt.Sprint(r.Dropped); got != value {
					return fmt.Errorf("result %d dropped: got %s, want %s", i, got, value)
				}
			}
		}
		return nil
	})
}

// shown renders a row the way the docs write it: a trailing newline (from
// "newline": true) becomes the two characters \n.
func shown(row []byte) string {
	s := string(row)
	if strings.HasSuffix(s, "\n") {
		return strings.TrimSuffix(s, "\n") + `\n`
	}
	return s
}

func errName(err error) string {
	for name, sentinel := range docErrors {
		if errors.Is(err, sentinel) {
			return name
		}
	}
	return err.Error()
}
