package jsonflat

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/valyala/fastjson"
)

// Rule operations, merge modes and the other config keywords.
const (
	OpRename  = "rename"
	OpDrop    = "drop"
	OpMerge   = "merge"
	OpDefault = "default"

	MergeConcat = "concat"
	MergeArray  = "array"
	MergeFirst  = "first"

	ArraysIndex  = "index"  // flatten arrays as key.0, key.1
	ArraysRaw    = "raw"    // keep arrays as JSON values
	ArraysString = "string" // write arrays as a JSON string, for typed columns

	NormalizeNone  = "none"
	NormalizeSnake = "snake"

	CollisionKeep  = "keep"  // write duplicates as they come (default)
	CollisionFirst = "first" // the first value wins, later ones are counted
	CollisionError = "error" // the record fails with ErrCollision

	AsRaw    = "raw"    // column value is written as it is
	AsString = "string" // strings and numbers are written as strings

	FnClockSkew = "clock_skew"
)

// Config describes a transformation. Every section is optional; an empty
// Config flattens the whole document with "." as the separator.
type Config struct {
	Input    InputConfig       `json:"input,omitempty"`
	Flatten  FlattenConfig     `json:"flatten,omitempty"`
	Keys     KeysConfig        `json:"keys,omitempty"`
	Derive   map[string]Derive `json:"derive,omitempty"`
	Columns  []Column          `json:"columns,omitempty"`
	Sections []Section         `json:"sections,omitempty"`
	Rules    []Rule            `json:"rules,omitempty"`
	Outputs  []Output          `json:"outputs,omitempty"`
	Expose   SourceList        `json:"expose,omitempty"`
	// Newline appends "\n" to every record.
	Newline bool `json:"newline,omitempty"`
	// MaxPooledInput is the largest input, in bytes, whose per-call state is
	// kept for reuse after the call. That state holds about eight times the
	// input size, once per active P. 0 means 4 MiB.
	MaxPooledInput int `json:"max_pooled_input,omitempty"`
}

// InputConfig says how one document becomes one or more records.
type InputConfig struct {
	// Explode is the path of an array whose elements are the records. A
	// document without that path is treated as a single record.
	Explode string `json:"explode,omitempty"`
	// Inherit lists top-level keys of the enclosing document that a record
	// falls back to when it lacks them, for example a batch-level "sentAt".
	Inherit []string `json:"inherit,omitempty"`
}

// FlattenConfig controls how nested values become flat keys.
type FlattenConfig struct {
	// Separator joins output key segments. Default ".".
	Separator string `json:"separator,omitempty"`
	// Arrays is ArraysIndex (default), ArraysRaw or ArraysString.
	Arrays string `json:"arrays,omitempty"`
	// MaxDepth limits how many container levels are flattened. 0 = no limit.
	MaxDepth int `json:"max_depth,omitempty"`
	// DropNulls leaves out keys whose value is null.
	DropNulls bool `json:"drop_nulls,omitempty"`
	// DropEmptyObjects leaves out keys whose value is {}.
	DropEmptyObjects bool `json:"drop_empty_objects,omitempty"`
}

// KeysConfig corrects and polices output keys.
type KeysConfig struct {
	// Normalize is NormalizeNone (default) or NormalizeSnake.
	Normalize string `json:"normalize,omitempty"`
	// DigitPrefix is put in front of output keys that start with a digit.
	DigitPrefix string `json:"digit_prefix,omitempty"`
	// OnCollision is CollisionKeep (default), CollisionFirst or CollisionError.
	OnCollision string `json:"on_collision,omitempty"`
	// SegmentAliases replace one key segment wherever it appears.
	SegmentAliases map[string]string `json:"segment_aliases,omitempty"`
	// Aliases replace a full output key.
	Aliases []Alias `json:"aliases,omitempty"`
	// Drop lists output keys to leave out. A trailing * matches a prefix.
	Drop []string `json:"drop,omitempty"`
	// Keep lists the output keys to write; every other flattened key is left
	// out. Empty keeps all. A trailing * matches a prefix. A key must match
	// Keep and not match Drop.
	Keep []string `json:"keep,omitempty"`
	// Rest names a column that collects the keys Keep left out, as a JSON
	// string of one flat object, so that nothing is lost. Written at the end
	// of the row, only when something was left out. Needs Keep.
	Rest string `json:"rest,omitempty"`
}

// Alias maps wrong output keys to the right one. With snake normalisation the
// From entries are normalised when the config is compiled, so they can be
// written the way the producer writes them.
type Alias struct {
	To   string   `json:"to"`
	From []string `json:"from"`
	When *Cond    `json:"when,omitempty"`
}

// Derive defines a named value computed once per record and usable as
// "$name" in sources and output names.
type Derive struct {
	From           SourceList `json:"from"`
	Normalize      string     `json:"normalize,omitempty"`
	DigitPrefix    string     `json:"digit_prefix,omitempty"`
	Reserved       []string   `json:"reserved,omitempty"`
	ReservedPrefix string     `json:"reserved_prefix,omitempty"`
	// Required fails the record with ErrRequired when the value is empty.
	Required bool  `json:"required,omitempty"`
	When     *Cond `json:"when,omitempty"`
}

// Column is an explicit output key written before any section. Column names
// are reserved: a flattened key with the same name is dropped, whether or not
// the column had a value.
type Column struct {
	To string `json:"to"`
	// From lists sources in order of preference; the first with a value wins.
	From SourceList `json:"from"`
	// As is AsRaw (default) or AsString.
	As   string `json:"as,omitempty"`
	When *Cond  `json:"when,omitempty"`
	// Quiet stops keys dropped in favour of this column from being counted.
	Quiet bool `json:"quiet,omitempty"`
}

// Section flattens one subtree of the record under a key prefix. Without any
// sections the whole record is flattened with no prefix.
type Section struct {
	ID     string `json:"id,omitempty"`
	From   string `json:"from"`
	Prefix string `json:"prefix,omitempty"`
	When   *Cond  `json:"when,omitempty"`
	// Quiet stops collisions inside this section from being counted or
	// turned into errors.
	Quiet bool `json:"quiet,omitempty"`
}

// Output routes a record to a named destination. A record can match several
// outputs and then produces one row for each.
type Output struct {
	// Name is a literal, or "$name" for a derived value.
	Name string `json:"name"`
	When *Cond  `json:"when,omitempty"`
	// Sections limits the output to the sections with these IDs.
	Sections []string `json:"sections,omitempty"`
}

// Cond is a test on one value of the record. Exactly one of Equals, In and
// Exists must be set.
type Cond struct {
	Path   SourceList `json:"path"`
	Equals *string    `json:"equals,omitempty"`
	In     []string   `json:"in,omitempty"`
	Exists *bool      `json:"exists,omitempty"`
}

// Source is where a value comes from: a path in the record ("user.id"), a
// variable ("$now", "$name" of a derived value, or a key of Options.Vars), or
// a function ({"fn":"clock_skew","sent":"sentAt","original":"originalTimestamp"}).
// Paths always use "." between segments.
type Source struct {
	Path     string `json:"-"`
	Fn       string `json:"fn,omitempty"`
	Sent     string `json:"sent,omitempty"`
	Original string `json:"original,omitempty"`
}

// UnmarshalJSON accepts a string or a function object.
func (s *Source) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) > 0 && b[0] == '"' {
		return json.Unmarshal(b, &s.Path)
	}
	type fn Source
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	return dec.Decode((*fn)(s))
}

// MarshalJSON writes a path source as a string.
func (s Source) MarshalJSON() ([]byte, error) {
	if s.Fn == "" {
		return json.Marshal(s.Path)
	}
	type fn Source
	return json.Marshal(fn(s))
}

// SourceList unmarshals from one source or an array of sources.
type SourceList []Source

// UnmarshalJSON implements json.Unmarshaler.
func (l *SourceList) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) > 0 && b[0] == '[' {
		var ss []Source
		if err := json.Unmarshal(b, &ss); err != nil {
			return err
		}
		*l = ss
		return nil
	}
	var s Source
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	*l = SourceList{s}
	return nil
}

// Rule is one source-side step. Paths in Path and From are source paths and
// always use "." between segments. To (and Path of a default rule) are output
// keys and are written exactly as given.
type Rule struct {
	Op          string          `json:"op"`
	Path        string          `json:"path,omitempty"`
	From        StringList      `json:"from,omitempty"`
	To          string          `json:"to,omitempty"`
	Sep         string          `json:"sep,omitempty"`
	Mode        string          `json:"mode,omitempty"`
	KeepSources bool            `json:"keep_sources,omitempty"`
	Value       json.RawMessage `json:"value,omitempty"`
}

// StringList unmarshals from a JSON string or an array of strings.
type StringList []string

// UnmarshalJSON implements json.Unmarshaler.
func (l *StringList) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) > 0 && b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		*l = StringList{s}
		return nil
	}
	var ss []string
	if err := json.Unmarshal(b, &ss); err != nil {
		return err
	}
	*l = ss
	return nil
}

// Compile parses a JSON config and builds a Transformer from it. Unknown
// fields and data after the config object are errors, so a misspelt section
// name is reported instead of being ignored.
func Compile(config []byte) (*Transformer, error) {
	dec := json.NewDecoder(bytes.NewReader(config))
	dec.DisallowUnknownFields()
	var cfg Config
	if err := dec.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("jsonflat: config: %w", err)
	}
	if rest := bytes.TrimSpace(config[dec.InputOffset():]); len(rest) > 0 {
		return nil, errors.New("jsonflat: config: unexpected data after the config object")
	}
	return New(cfg)
}

// MustCompile is like Compile but panics on error. It is meant for configs
// that are part of the program, such as package-level variables.
func MustCompile(config []byte) *Transformer {
	t, err := Compile(config)
	if err != nil {
		panic(err)
	}
	return t
}

// New validates cfg and builds a Transformer from it. The Transformer keeps no
// reference to cfg, so cfg can be changed and used again afterwards.
func New(cfg Config) (*Transformer, error) {
	if cfg.MaxPooledInput < 0 {
		return nil, fmt.Errorf("jsonflat: config: max_pooled_input: %d is negative", cfg.MaxPooledInput)
	}
	c := &compiler{
		cfg:        &cfg,
		t:          &Transformer{},
		inherit:    map[string]bool{},
		condIdx:    map[string]int{},
		sectionIdx: map[string]int{},
	}
	// The order matters. Sources need input and derive, aliases need the
	// columns, sections need the drop rules, outputs need the sections.
	steps := []func() error{
		c.flatten, c.input, c.derive, c.columns,
		c.keyPolicy, c.keyFilters, c.segmentAliases, c.aliases,
		c.rules, c.sections, c.outputs, c.expose,
	}
	for _, step := range steps {
		if err := step(); err != nil {
			return nil, fmt.Errorf("jsonflat: config: %w", err)
		}
	}

	t := c.t
	t.newline = cfg.Newline
	t.fixKeys = t.snake || t.segAliases.lens != 0 || len(t.aliasGroups) > 0 ||
		len(t.digitPrefix) > 0 || !t.keep.empty() || !t.drop.empty()
	t.simple = t.explode == nil && !t.named
	// A row can start as a copy of the previous one only when nothing is
	// written after the sections.
	t.extend = len(t.merges) == 0 && len(t.defaults) == 0 && t.rest == nil
	t.maxPooled = cfg.MaxPooledInput
	if t.maxPooled == 0 {
		t.maxPooled = maxPooledInput
	}
	t.pool.New = func() any { return newState(t) }
	return t, nil
}

// The config keywords, compiled.

type arraysMode uint8

const (
	arraysIndex arraysMode = iota
	arraysRaw
	arraysString
)

type collisionPolicy uint8

const (
	collisionKeep collisionPolicy = iota
	collisionFirst
	collisionError
)

type mergeMode uint8

const (
	mergeConcat mergeMode = iota
	mergeArray
	mergeFirst
)

type sourceKind uint8

const (
	srcPath sourceKind = iota
	srcNow
	srcDerived
	srcVar
	srcSkew
)

type condOp uint8

const (
	condEquals condOp = iota
	condIn
	condExists
)

// pathRef is a source path, split once so that lookups do not have to.
type pathRef struct {
	keys []string
	// inherit is set for a one-segment path listed in input.inherit.
	inherit bool
}

type source struct {
	kind           sourceKind
	path           pathRef // srcPath
	idx            int     // srcDerived
	name           string  // srcVar
	sent, original pathRef // srcSkew
}

type cond struct {
	srcs   []source
	op     condOp
	equals string
	in     []string
	exists bool
	// maxDerived is the highest derived value the condition reads, or -1.
	maxDerived int
}

type derived struct {
	name           string
	srcs           []source
	snake          bool
	digitPrefix    []byte
	reserved       map[string]struct{}
	reservedPrefix []byte
	required       bool
	when           int
}

type column struct {
	name     []byte
	nameIdx  int // columns with the same name share one index
	srcs     []source
	asString bool
	when     int
	quiet    bool
}

type aliasGroup struct {
	when int
	m    *strMap[[]byte]
}

type section struct {
	from    []string
	srcBase []byte // what every source path in the section starts with: from + "."
	dstBase []byte // what every output key in the section starts with: prefix + separator
	when    int
	quiet   bool
	dead    bool // a drop rule covers the whole section
}

type output struct {
	name     []byte
	nameFrom int // the derived value that names the output, or -1
	when     int
	member   []bool // per section
}

// action is what the rules ask for at one source path.
type action struct {
	drop     bool
	renameTo []byte
	slots    []int // merge slots that this path fills
	consume  bool  // some merge wants the source removed from the output
}

type merge struct {
	to    []byte
	mode  mergeMode
	sep   []byte // already escaped
	first int    // first slot
	n     int
}

type defaultRule struct {
	key   []byte
	value []byte
}

// compiler turns a Config into a Transformer. Conditions are numbered from -1:
// a "when" that is absent compiles to -1, which always passes.
type compiler struct {
	cfg        *Config
	t          *Transformer
	inherit    map[string]bool
	condIdx    map[string]int // JSON of a condition -> its index
	sectionIdx map[string]int // section id -> its index
}

func splitPath(p string) []string { return strings.Split(p, ".") }

func snake(s string) string { return string(appendSnake(nil, []byte(s))) }

// sortStrings sorts a handful of names. The imports of this file belong to
// its fixed first half (see CLAUDE.md), hence no sort package.
func sortStrings(names []string) {
	for i := 1; i < len(names); i++ {
		for j := i; j > 0 && names[j] < names[j-1]; j-- {
			names[j], names[j-1] = names[j-1], names[j]
		}
	}
}

func (c *compiler) pathRef(p string) pathRef {
	keys := splitPath(p)
	return pathRef{keys: keys, inherit: len(keys) == 1 && c.inherit[p]}
}

func (c *compiler) flatten() error {
	f, t := c.cfg.Flatten, c.t
	t.sep = []byte(f.Separator)
	if f.Separator == "" {
		t.sep = []byte(".")
	}
	switch f.Arrays {
	case "", ArraysIndex:
		t.arrays = arraysIndex
	case ArraysRaw:
		t.arrays = arraysRaw
	case ArraysString:
		t.arrays = arraysString
	default:
		return fmt.Errorf("flatten.arrays: unknown mode %q", f.Arrays)
	}
	if f.MaxDepth < 0 {
		return fmt.Errorf("flatten.max_depth: %d is negative", f.MaxDepth)
	}
	t.maxDepth = f.MaxDepth
	t.dropNulls = f.DropNulls
	t.dropEmptyObjects = f.DropEmptyObjects
	return nil
}

func (c *compiler) input() error {
	in := c.cfg.Input
	if in.Explode != "" {
		c.t.explode = splitPath(in.Explode)
	}
	for _, key := range in.Inherit {
		if key == "" || strings.Contains(key, ".") {
			return fmt.Errorf("input.inherit: %q is not a single top-level key", key)
		}
		c.inherit[key] = true
	}
	return nil
}

func (c *compiler) source(s Source) (source, error) {
	if s.Fn != "" {
		return c.function(s)
	}
	if s.Sent != "" || s.Original != "" {
		return source{}, errors.New("sent and original need a fn")
	}
	if s.Path == "" {
		return source{}, errors.New("empty source")
	}
	if s.Path[0] != '$' {
		return source{kind: srcPath, path: c.pathRef(s.Path)}, nil
	}
	name := s.Path[1:]
	if name == "" {
		return source{}, errors.New(`"$" is not a variable`)
	}
	if name == "now" {
		return source{kind: srcNow}, nil
	}
	if i, ok := c.t.derivedIdx[name]; ok {
		return source{kind: srcDerived, idx: i}, nil
	}
	return source{kind: srcVar, name: name}, nil
}

func (c *compiler) function(s Source) (source, error) {
	if s.Fn != FnClockSkew {
		return source{}, fmt.Errorf("unknown function %q", s.Fn)
	}
	if s.Path != "" {
		return source{}, errors.New("a source is a path or a function, not both")
	}
	if s.Sent == "" || s.Original == "" {
		return source{}, fmt.Errorf("%s needs both sent and original", FnClockSkew)
	}
	if s.Sent[0] == '$' || s.Original[0] == '$' {
		return source{}, fmt.Errorf("%s takes paths, not variables", FnClockSkew)
	}
	return source{kind: srcSkew, sent: c.pathRef(s.Sent), original: c.pathRef(s.Original)}, nil
}

// sources compiles a non-empty source list. It also returns the highest
// derived value the list reads, or -1.
func (c *compiler) sources(list SourceList) ([]source, int, error) {
	if len(list) == 0 {
		return nil, -1, errors.New("no source")
	}
	srcs := make([]source, 0, len(list))
	maxDerived := -1
	for _, s := range list {
		src, err := c.source(s)
		if err != nil {
			return nil, -1, err
		}
		if src.kind == srcDerived && src.idx > maxDerived {
			maxDerived = src.idx
		}
		srcs = append(srcs, src)
	}
	return srcs, maxDerived, nil
}

// when returns the index of a compiled condition, or -1 for nil. Equal
// conditions share one index, so each is evaluated once per record however
// often the config repeats it. limit is the number of derived values that
// exist by the time the condition is first needed.
func (c *compiler) when(w *Cond, limit int) (int, error) {
	if w == nil {
		return -1, nil
	}
	key, err := json.Marshal(w)
	if err != nil {
		return -1, fmt.Errorf("when: %w", err)
	}
	idx, ok := c.condIdx[string(key)]
	if !ok {
		compiled, err := c.cond(w)
		if err != nil {
			return -1, fmt.Errorf("when: %w", err)
		}
		idx = len(c.t.conds)
		c.t.conds = append(c.t.conds, compiled)
		c.condIdx[string(key)] = idx
	}
	if c.t.conds[idx].maxDerived >= limit {
		return -1, errors.New("when: refers to a derived value that is computed later")
	}
	return idx, nil
}

func (c *compiler) cond(w *Cond) (cond, error) {
	srcs, maxDerived, err := c.sources(w.Path)
	if err != nil {
		return cond{}, fmt.Errorf("path: %w", err)
	}
	out := cond{srcs: srcs, maxDerived: maxDerived}
	set := 0
	if w.Equals != nil {
		out.op, out.equals = condEquals, *w.Equals
		set++
	}
	if len(w.In) > 0 {
		out.op, out.in = condIn, append([]string(nil), w.In...)
		set++
	}
	if w.Exists != nil {
		out.op, out.exists = condExists, *w.Exists
		set++
	}
	if set != 1 {
		return cond{}, errors.New("exactly one of equals, in and exists must be set")
	}
	return out, nil
}

func normalizeMode(mode string) (toSnake bool, err error) {
	switch mode {
	case "", NormalizeNone:
		return false, nil
	case NormalizeSnake:
		return true, nil
	}
	return false, fmt.Errorf("unknown normalize mode %q", mode)
}

func (c *compiler) derive() error {
	t := c.t
	names := make([]string, 0, len(c.cfg.Derive))
	for name := range c.cfg.Derive {
		names = append(names, name)
	}
	// Derived values are computed in name order, and one may read only
	// those before it. That keeps the order independent of the JSON.
	sortStrings(names)
	t.derivedIdx = make(map[string]int, len(names))
	for i, name := range names {
		t.derivedIdx[name] = i
	}
	for i, name := range names {
		d, err := c.derivedValue(name, c.cfg.Derive[name], i)
		if err != nil {
			return fmt.Errorf("derive %q: %w", name, err)
		}
		t.derived = append(t.derived, d)
	}
	return nil
}

func (c *compiler) derivedValue(name string, d Derive, idx int) (derived, error) {
	if name == "" || name == "now" {
		return derived{}, errors.New("the name is not allowed")
	}
	srcs, maxDerived, err := c.sources(d.From)
	if err != nil {
		return derived{}, fmt.Errorf("from: %w", err)
	}
	if maxDerived >= idx {
		return derived{}, errors.New("from: a derived value may only read derived values that sort before it")
	}
	toSnake, err := normalizeMode(d.Normalize)
	if err != nil {
		return derived{}, err
	}
	when, err := c.when(d.When, idx)
	if err != nil {
		return derived{}, err
	}
	out := derived{
		name:           name,
		srcs:           srcs,
		snake:          toSnake,
		digitPrefix:    []byte(d.DigitPrefix),
		reservedPrefix: []byte(d.ReservedPrefix),
		required:       d.Required,
		when:           when,
	}
	if len(d.Reserved) > 0 {
		out.reserved = make(map[string]struct{}, len(d.Reserved))
		for _, r := range d.Reserved {
			out.reserved[r] = struct{}{}
		}
	}
	return out, nil
}

func (c *compiler) columns() error {
	t := c.t
	for i, cfg := range c.cfg.Columns {
		col, err := c.column(cfg)
		if err != nil {
			return fmt.Errorf("columns[%d]: %w", i, err)
		}
		idx, ok := t.columnIdx.lookup(cfg.To)
		if !ok {
			idx = t.columnIdx.len()
			t.columnIdx.set(cfg.To, idx)
		}
		col.nameIdx = idx
		t.columns = append(t.columns, col)
	}
	return nil
}

func (c *compiler) column(cfg Column) (column, error) {
	if cfg.To == "" {
		return column{}, errors.New("to is empty")
	}
	col := column{name: []byte(cfg.To), quiet: cfg.Quiet}
	switch cfg.As {
	case "", AsRaw:
	case AsString:
		col.asString = true
	default:
		return column{}, fmt.Errorf("unknown as %q", cfg.As)
	}
	var err error
	if col.srcs, _, err = c.sources(cfg.From); err != nil {
		return column{}, fmt.Errorf("from: %w", err)
	}
	if col.when, err = c.when(cfg.When, len(c.t.derived)); err != nil {
		return column{}, err
	}
	return col, nil
}

func (c *compiler) keyPolicy() error {
	k, t := c.cfg.Keys, c.t
	var err error
	if t.snake, err = normalizeMode(k.Normalize); err != nil {
		return fmt.Errorf("keys.normalize: %w", err)
	}
	t.digitPrefix = []byte(k.DigitPrefix)
	switch k.OnCollision {
	case "", CollisionKeep:
		t.collision = collisionKeep
	case CollisionFirst:
		t.collision = collisionFirst
	case CollisionError:
		t.collision = collisionError
	default:
		return fmt.Errorf("keys.on_collision: unknown policy %q", k.OnCollision)
	}
	return nil
}

func (c *compiler) segmentAliases() error {
	t := c.t
	for from, to := range c.cfg.Keys.SegmentAliases {
		norm := from
		if t.snake {
			norm = snake(from)
		}
		switch {
		case norm == "" || to == "":
			return fmt.Errorf("keys.segment_aliases: %q -> %q: empty segment", from, to)
		case norm == to:
			return fmt.Errorf("keys.segment_aliases: %q is already %q", from, to)
		}
		if _, dup := t.segAliases.lookup(norm); dup {
			return fmt.Errorf("keys.segment_aliases: %q is listed twice", norm)
		}
		t.segAliases.set(norm, []byte(to))
	}
	return nil
}

// normKey normalises an alias "from" entry the way the walk normalises the
// keys it builds: segment by segment, so that an entry written with the
// configured separator still matches.
func (c *compiler) normKey(key string) string {
	if !c.t.snake {
		return key
	}
	sep := string(c.t.sep)
	var segs []string
	for _, seg := range strings.Split(key, sep) {
		if norm := snake(seg); norm != "" {
			segs = append(segs, norm)
		}
	}
	return strings.Join(segs, sep)
}

func (c *compiler) aliases() error {
	t := c.t
	// Aliases without a condition form one group, which is tried last.
	plain := &strMap[[]byte]{}
	for i, a := range c.cfg.Keys.Aliases {
		group := plain
		if a.When != nil {
			when, err := c.when(a.When, len(t.derived))
			if err != nil {
				return fmt.Errorf("keys.aliases[%d]: %w", i, err)
			}
			group = &strMap[[]byte]{}
			t.aliasGroups = append(t.aliasGroups, aliasGroup{when: when, m: group})
		}
		if err := c.alias(a, group); err != nil {
			return fmt.Errorf("keys.aliases[%d]: %w", i, err)
		}
	}
	if plain.len() > 0 {
		t.aliasGroups = append(t.aliasGroups, aliasGroup{when: -1, m: plain})
	}
	return nil
}

func (c *compiler) alias(a Alias, group *strMap[[]byte]) error {
	if a.To == "" {
		return errors.New("to is empty")
	}
	if len(a.From) == 0 {
		return errors.New("from is empty")
	}
	if _, isColumn := c.t.columnIdx.lookup(a.To); isColumn {
		return fmt.Errorf("%q is a column", a.To)
	}
	// An alias to a key that the filters remove would never be written.
	to := []byte(a.To)
	if !c.t.keep.empty() && !c.t.keep.matches(to) {
		return fmt.Errorf("to %q is not in keys.keep", a.To)
	}
	if c.t.drop.matches(to) {
		return fmt.Errorf("to %q is in keys.drop", a.To)
	}
	if c.t.rest != nil && a.To == string(c.t.rest) {
		return fmt.Errorf("to %q is keys.rest", a.To)
	}
	for _, from := range a.From {
		norm := c.normKey(from)
		switch {
		case norm == "":
			return fmt.Errorf("from %q normalises to nothing", from)
		case norm == a.To:
			return fmt.Errorf("from %q is already %q", from, a.To)
		}
		if _, dup := group.lookup(norm); dup {
			return fmt.Errorf("from %q is listed twice", from)
		}
		group.set(norm, []byte(a.To))
	}
	return nil
}

func (c *compiler) keyFilters() error {
	k, t := c.cfg.Keys, c.t
	var err error
	if t.keep, err = compileKeyFilter("keys.keep", k.Keep); err != nil {
		return err
	}
	if t.drop, err = compileKeyFilter("keys.drop", k.Drop); err != nil {
		return err
	}
	for _, entry := range k.Keep {
		if _, dropped := t.drop.exact.lookup(entry); dropped {
			return fmt.Errorf("keys.keep: %q is also in keys.drop", entry)
		}
	}
	if k.Rest != "" {
		if t.keep.empty() {
			return errors.New("keys.rest: needs keys.keep; without a whitelist nothing is left out")
		}
		if _, isColumn := t.columnIdx.lookup(k.Rest); isColumn {
			return fmt.Errorf("keys.rest: %q is a column", k.Rest)
		}
		t.rest = []byte(k.Rest)
	}
	return nil
}

func compileKeyFilter(field string, entries []string) (keyFilter, error) {
	var f keyFilter
	if len(entries) == 0 {
		return f, nil
	}
	seen := make(map[string]bool, len(entries))
	for _, entry := range entries {
		if entry == "" || entry == "*" {
			return f, fmt.Errorf("%s: %q matches every key", field, entry)
		}
		if seen[entry] {
			return f, fmt.Errorf("%s: %q is listed twice", field, entry)
		}
		seen[entry] = true
		if prefix, ok := strings.CutSuffix(entry, "*"); ok {
			f.prefixes = append(f.prefixes, []byte(prefix))
		} else {
			f.exact.set(entry, struct{}{})
		}
	}
	return f, nil
}

func (c *compiler) action(path string) *action {
	a, _ := c.t.actions.lookup(path)
	if a == nil {
		a = &action{}
		c.t.actions.set(path, a)
	}
	return a
}

func (c *compiler) rules() error {
	for i, r := range c.cfg.Rules {
		var err error
		switch r.Op {
		case OpRename:
			err = c.addRename(r)
		case OpDrop:
			err = c.addDrop(r)
		case OpMerge:
			err = c.addMerge(r)
		case OpDefault:
			err = c.addDefault(r)
		default:
			return fmt.Errorf("rules[%d]: unknown op %q", i, r.Op)
		}
		if err != nil {
			return fmt.Errorf("rules[%d]: %s: %w", i, r.Op, err)
		}
	}
	return nil
}

func (c *compiler) addRename(r Rule) error {
	if len(r.From) != 1 || r.From[0] == "" {
		return errors.New("needs exactly one from")
	}
	if r.To == "" {
		return errors.New("to is empty")
	}
	a := c.action(r.From[0])
	if a.renameTo != nil {
		return fmt.Errorf("%q is renamed twice", r.From[0])
	}
	if a.drop {
		return fmt.Errorf("%q is also dropped", r.From[0])
	}
	a.renameTo = []byte(r.To)
	return nil
}

func (c *compiler) addDrop(r Rule) error {
	if r.Path == "" {
		return errors.New("path is empty")
	}
	a := c.action(r.Path)
	if a.renameTo != nil {
		return fmt.Errorf("%q is also renamed", r.Path)
	}
	a.drop = true
	return nil
}

func (c *compiler) addMerge(r Rule) error {
	t := c.t
	if len(r.From) == 0 {
		return errors.New("from is empty")
	}
	if r.To == "" {
		return errors.New("to is empty")
	}
	m := merge{to: []byte(r.To), first: t.nslots, n: len(r.From)}
	switch r.Mode {
	case "", MergeConcat:
		m.mode = mergeConcat
		m.sep = appendEscaped(nil, []byte(r.Sep))
	case MergeArray:
		m.mode = mergeArray
	case MergeFirst:
		m.mode = mergeFirst
	default:
		return fmt.Errorf("unknown mode %q", r.Mode)
	}
	for _, from := range r.From {
		if from == "" {
			return errors.New("a from path is empty")
		}
		// Every source gets its own slot, numbered across all merges.
		a := c.action(from)
		a.slots = append(a.slots, t.nslots)
		a.consume = a.consume || !r.KeepSources
		t.nslots++
	}
	t.merges = append(t.merges, m)
	return nil
}

func (c *compiler) addDefault(r Rule) error {
	t := c.t
	if r.Path == "" {
		return errors.New("path is empty")
	}
	if len(r.Value) == 0 {
		return fmt.Errorf("%q has no value", r.Path)
	}
	if _, dup := t.defaultIdx.lookup(r.Path); dup {
		return fmt.Errorf("%q has two defaults", r.Path)
	}
	// A RawMessage built in Go has not been through encoding/json.
	if err := fastjson.ValidateBytes(r.Value); err != nil {
		return fmt.Errorf("%q: value: %w", r.Path, err)
	}
	var value bytes.Buffer
	if err := json.Compact(&value, r.Value); err != nil {
		return fmt.Errorf("%q: value: %w", r.Path, err)
	}
	t.defaultIdx.set(r.Path, len(t.defaults))
	t.defaults = append(t.defaults, defaultRule{key: []byte(r.Path), value: value.Bytes()})
	return nil
}

// dropped reports whether a drop rule covers path or anything above it.
func (c *compiler) dropped(path []string) bool {
	for n := 1; n <= len(path); n++ {
		if a, _ := c.t.actions.lookup(strings.Join(path[:n], ".")); a != nil && a.drop {
			return true
		}
	}
	return false
}

func (c *compiler) sections() error {
	t := c.t
	for i, cfg := range c.cfg.Sections {
		if cfg.ID != "" {
			if _, dup := c.sectionIdx[cfg.ID]; dup {
				return fmt.Errorf("sections[%d]: the id %q is used twice", i, cfg.ID)
			}
			c.sectionIdx[cfg.ID] = i
		}
		when, err := c.when(cfg.When, len(t.derived))
		if err != nil {
			return fmt.Errorf("sections[%d]: %w", i, err)
		}
		sec := section{when: when, quiet: cfg.Quiet}
		if cfg.From != "" {
			sec.from = splitPath(cfg.From)
			sec.srcBase = []byte(cfg.From + ".")
			sec.dead = c.dropped(sec.from) // drop wins, a section root included
		}
		if cfg.Prefix != "" {
			sec.dstBase = append([]byte(cfg.Prefix), t.sep...)
		}
		t.sections = append(t.sections, sec)
	}
	if len(t.sections) == 0 {
		t.sections = []section{{when: -1}} // the whole record, no prefix
	}
	return nil
}

func (c *compiler) outputs() error {
	t := c.t
	all := make([]bool, len(t.sections))
	for i := range all {
		all[i] = true
	}
	if len(c.cfg.Outputs) == 0 {
		// One implicit output that always matches and has no name.
		t.outputs = []output{{nameFrom: -1, when: -1, member: all}}
		return nil
	}
	t.named = true
	for i, cfg := range c.cfg.Outputs {
		out, err := c.output(cfg, all)
		if err != nil {
			return fmt.Errorf("outputs[%d]: %w", i, err)
		}
		t.outputs = append(t.outputs, out)
	}
	return nil
}

func (c *compiler) output(cfg Output, all []bool) (output, error) {
	if cfg.Name == "" {
		return output{}, errors.New("name is empty")
	}
	out := output{name: []byte(cfg.Name), nameFrom: -1, member: all}
	if cfg.Name[0] == '$' {
		idx, ok := c.t.derivedIdx[cfg.Name[1:]]
		if !ok {
			return output{}, fmt.Errorf("there is no derived value %q", cfg.Name)
		}
		out.nameFrom = idx
	}
	var err error
	if out.when, err = c.when(cfg.When, len(c.t.derived)); err != nil {
		return output{}, err
	}
	if len(cfg.Sections) > 0 {
		out.member = make([]bool, len(all))
		for _, id := range cfg.Sections {
			idx, ok := c.sectionIdx[id]
			if !ok {
				return output{}, fmt.Errorf("unknown section %q", id)
			}
			out.member[idx] = true
		}
	}
	return out, nil
}

func (c *compiler) expose() error {
	if len(c.cfg.Expose) == 0 {
		return nil
	}
	srcs, maxDerived, err := c.sources(c.cfg.Expose)
	if err != nil {
		return fmt.Errorf("expose: %w", err)
	}
	// Fields are resolved first, so that a record that fails later still
	// carries them. Derived values do not exist yet at that point.
	if maxDerived >= 0 {
		return errors.New("expose: a derived value cannot be exposed")
	}
	c.t.expose = srcs
	return nil
}
