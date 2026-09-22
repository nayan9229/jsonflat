package jsonflat

import (
	"bytes"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"
	"unsafe"

	"github.com/valyala/fastjson"
)

// Errors that concern one record. Each reports them in Record.Err and carries
// on with the next record; Append returns them.
var (
	// ErrRootNotContainer means the record is a scalar, not an object or array.
	ErrRootNotContainer = errors.New("jsonflat: record is not an object or an array")
	// ErrInvalidNumber means the record holds a number that is not valid JSON,
	// such as 00 or NaN. The parser accepts these; the output must not.
	ErrInvalidNumber = errors.New("jsonflat: invalid number")
	// ErrCollision means two keys got the same name under on_collision "error".
	ErrCollision = errors.New("jsonflat: key collision")
	// ErrRequired means a required derived value came out empty.
	ErrRequired = errors.New("jsonflat: required value is empty")
	// ErrNoOutput means no configured output matched the record.
	ErrNoOutput = errors.New("jsonflat: no output matches the record")
)

// Errors that concern the whole call.
var (
	// ErrExplode means the input.explode path exists but is not an array.
	ErrExplode = errors.New("jsonflat: input.explode path is not an array")
	// ErrNotSimple is returned by Append and Transform for a config that can
	// produce several rows: one with input.explode or with outputs. Use Each.
	ErrNotSimple = errors.New("jsonflat: the config can produce several rows; use Each")
)

// maxPooledInput is the largest input whose state goes back to the pool.
// fastjson keeps memory in proportion to the largest document it has parsed,
// so one huge document would otherwise pin that memory for good.
const maxPooledInput = 4 << 20

// timeLayout is the layout of "$now" and clock_skew values.
const timeLayout = "2006-01-02T15:04:05.000Z"

// Transformer applies one compiled config. It is immutable and safe for
// concurrent use by any number of goroutines.
type Transformer struct {
	// flatten
	sep              []byte
	arrays           arraysMode
	maxDepth         int
	dropNulls        bool
	dropEmptyObjects bool

	// keys
	snake       bool
	digitPrefix []byte
	collision   collisionPolicy
	segAliases  map[string][]byte
	aliasGroups []aliasGroup // groups with a condition in config order, then the one without
	keep        keyFilter    // empty: every key is kept
	drop        keyFilter
	rest        []byte // column that collects what keep removed; nil: none
	fixKeys     bool   // some key correction is configured

	// input
	explode []string

	conds      []cond
	derived    []derived
	derivedIdx map[string]int
	columns    []column
	columnIdx  map[string]int // column name -> index of the name
	sections   []section      // never empty: without sections, one for the whole record
	outputs    []output       // never empty: without outputs, one that always matches
	named      bool           // outputs were configured
	expose     []source

	// rules
	actions    map[string]*action // by source path
	merges     []merge
	nslots     int
	defaults   []defaultRule
	defaultIdx map[string]int

	newline bool
	simple  bool // Append is allowed
	extend  bool // a row may start as a copy of the previous row

	pool sync.Pool
}

// Options are the per-call inputs of Each.
type Options struct {
	// Now is the value of "$now". The zero value means time.Now().
	Now time.Time
	// Vars holds the values of "$name" sources that are not derived values.
	// The map is only read, so one map can serve many concurrent calls.
	Vars map[string]string
}

// Record is one output row, or one failed source record. It belongs to the
// Transformer: the Record and all of its slices are valid only until the
// callback returns. Copy what you keep.
type Record struct {
	// Index is the position of the source record in the exploded array.
	Index int
	// Err is set when the source record failed. JSON and Name are then nil,
	// and no rows of that source record are delivered.
	Err error
	// Name is the name of the matching output. It is nil when the config has
	// no outputs.
	Name []byte
	// JSON is the flat JSON object.
	JSON []byte
	// Fields holds the values of the "expose" sources, in config order. An
	// entry is nil when the source has no value.
	Fields [][]byte
	// Dropped counts the keys left out because their output key was taken.
	Dropped int
}

// A condition is evaluated at most once per record.
const (
	condUnknown int8 = iota
	condTrue
	condFalse
)

// What the active columns make of a column name for one record.
const (
	nameFree          int8 = iota
	nameReserved           // other keys of that name are dropped and counted
	nameReservedQuiet      // dropped, not counted
)

// rowSeg locates one finished row in the output buffer.
type rowSeg struct {
	start, bodyEnd, end int // bodyEnd is where the closing brace starts
	output              int
	dropped             int
}

// state is everything one call needs. It is pooled, so that a call allocates
// nothing once its buffers have grown to the size of the traffic.
type state struct {
	t      *Transformer
	parser fastjson.Parser

	out     []byte // where rows are written: the caller's dst in Append, buf in Each
	buf     []byte // kept between Each calls
	base    int    // length of out before the first row of the record
	rowOpen int    // position of the opening brace of the current row

	src  []byte // source path of the current node, "." separated
	dst  []byte // output key of the current node
	save []byte // stack of output keys replaced by a rename
	seg  []byte // normalised key segment
	pfx  []byte // digit-prefixed key
	rest []byte // the keys keep removed from this row, as one JSON object
	raw  []byte // raw JSON of one value
	num  []byte // number text
	idx  []byte // array index text

	depth int  // container level of the nodes being visited; a section root is 1
	quiet bool // the current section is quiet

	rec, env *fastjson.Value // the record, and the document around it if exploded
	opt      Options
	now      time.Time
	nowBuf   []byte // "$now" as text; empty until first needed in a call
	skewBuf  []byte

	derivedVals [][]byte
	condCache   []int8
	reserved    []int8 // per column name
	colWritten  []bool // per column name
	groupOn     []bool // per alias group
	secOn       []bool // per section
	slots       []*fastjson.Value
	seen        []bool // per default: its key was written
	fields      [][]byte
	fieldOff    []int
	fieldBuf    []byte
	nameBuf     []byte

	rows    []rowSeg
	keys    keySet
	dropped int
	err     error
	record  Record

	// Method values are created once here. Creating one per call allocates.
	visitFn    func(key []byte, v *fastjson.Value)
	rawVisitFn func(key []byte, v *fastjson.Value)
}

func newState(t *Transformer) *state {
	s := &state{
		t:           t,
		derivedVals: make([][]byte, len(t.derived)),
		condCache:   make([]int8, len(t.conds)),
		reserved:    make([]int8, len(t.columnIdx)),
		colWritten:  make([]bool, len(t.columnIdx)),
		groupOn:     make([]bool, len(t.aliasGroups)),
		secOn:       make([]bool, len(t.sections)),
		slots:       make([]*fastjson.Value, t.nslots),
		seen:        make([]bool, len(t.defaults)),
		fields:      make([][]byte, len(t.expose)),
		fieldOff:    make([]int, 2*len(t.expose)),
	}
	s.visitFn = s.visit
	s.rawVisitFn = s.rawVisit
	return s
}

// begin gets a pooled state ready for one call.
func (s *state) begin(out []byte, opt Options) {
	s.out, s.base = out, len(out)
	s.opt = opt
	s.env = nil
	s.nowBuf = s.nowBuf[:0]
}

// release drops what points into the parsed document or to the caller and
// returns the state to the pool.
func (t *Transformer) release(s *state, inputLen int) {
	if inputLen > maxPooledInput {
		return
	}
	s.out = nil
	s.rec, s.env = nil, nil
	clear(s.slots)
	s.opt = Options{}
	s.err = nil
	s.record = Record{}
	t.pool.Put(s)
}

func (s *state) parse(src []byte) (*fastjson.Value, error) {
	v, err := s.parser.ParseBytes(src)
	if err != nil {
		return nil, fmt.Errorf("jsonflat: %w", err)
	}
	return v, nil
}

// Append flattens the document src and appends the result to dst. It is for
// configs that produce exactly one row per document: those without
// input.explode and without outputs. For any other config it returns
// ErrNotSimple.
//
// On error, dst is returned unchanged. The result does not alias src.
func (t *Transformer) Append(dst, src []byte) ([]byte, error) {
	if !t.simple {
		return dst, ErrNotSimple
	}
	s := t.pool.Get().(*state)
	s.begin(dst, Options{})
	v, err := s.parse(src)
	if err == nil {
		s.build(v)
		err = s.err
	}
	out := s.out
	t.release(s, len(src))
	if err != nil {
		return dst, err
	}
	return out, nil
}

// Transform is Append with a nil dst.
func (t *Transformer) Transform(src []byte) ([]byte, error) {
	return t.Append(nil, src)
}

// Each turns the document src into rows and calls fn for every row. With
// input.explode, every element of that array is a source record; a source
// record that matches several outputs gives one row for each.
//
// A source record that fails is reported as one Record with Err set, and none
// of its rows are delivered; the remaining records are still processed. Each
// itself returns an error only when src is not valid JSON, for ErrExplode, or
// when fn returns an error, which stops the iteration and is returned as is.
//
// The Record passed to fn, and every slice in it, is valid only until fn
// returns.
func (t *Transformer) Each(src []byte, opt Options, fn func(*Record) error) error {
	s := t.pool.Get().(*state)
	s.begin(s.buf[:0], opt)
	err := s.each(src, fn)
	s.buf = s.out // keep the buffer, which may have grown
	t.release(s, len(src))
	return err
}

func (s *state) each(src []byte, fn func(*Record) error) error {
	v, err := s.parse(src)
	if err != nil {
		return err
	}
	var batch *fastjson.Value
	if s.t.explode != nil {
		batch = v.Get(s.t.explode...)
	}
	if batch == nil {
		// The document itself is the one record, and it has no envelope.
		s.build(v)
		return s.deliver(0, fn)
	}
	if batch.Type() != fastjson.TypeArray {
		return ErrExplode
	}
	s.env = v
	for i, rec := range batch.GetArray() {
		s.build(rec)
		if err := s.deliver(i, fn); err != nil {
			return err
		}
	}
	return nil
}

// deliver hands the rows of the record just built to fn. Rows are delivered
// only after all of them are built, so a record never arrives in part.
func (s *state) deliver(index int, fn func(*Record) error) error {
	r := &s.record
	r.Index = index
	r.Fields = nil
	if len(s.fields) > 0 {
		r.Fields = s.fields
	}
	if s.err != nil {
		r.Err, r.Name, r.JSON, r.Dropped = s.err, nil, nil, 0
		return fn(r)
	}
	r.Err = nil
	for i := range s.rows {
		row := &s.rows[i]
		o := &s.t.outputs[row.output]
		switch {
		case !s.t.named:
			r.Name = nil
		case o.nameFrom >= 0:
			r.Name = s.derivedVals[o.nameFrom]
		default:
			// A copy, so that a callback cannot change the Transformer.
			s.nameBuf = append(s.nameBuf[:0], o.name...)
			r.Name = s.nameBuf
		}
		r.JSON = s.out[row.start:row.end]
		r.Dropped = row.dropped
		if err := fn(r); err != nil {
			return err
		}
	}
	return nil
}

func (s *state) fail(base error, text []byte) {
	if s.err == nil {
		s.err = fmt.Errorf("%w: %q", base, text)
	}
}

// build writes every row of one source record to s.out and lists them in
// s.rows, or sets s.err.
func (s *state) build(v *fastjson.Value) {
	t := s.t
	s.err = nil
	s.rows = s.rows[:0]
	s.out = s.out[:s.base]
	clear(s.fields)
	if tp := v.Type(); tp != fastjson.TypeObject && tp != fastjson.TypeArray {
		s.err = ErrRootNotContainer
		return
	}
	s.rec = v
	clear(s.condCache)

	// Fields come first, so that a record that fails below still carries the
	// IDs needed to dead-letter it.
	s.resolveFields()
	s.computeDerived()
	if s.err != nil {
		return
	}

	for i := range t.aliasGroups {
		s.groupOn[i] = s.condOK(t.aliasGroups[i].when)
	}
	for i := range t.sections {
		s.secOn[i] = !t.sections[i].dead && s.condOK(t.sections[i].when)
	}
	// An active column reserves its name whether or not it gets a value.
	clear(s.reserved)
	for i := range t.columns {
		c := &t.columns[i]
		if s.reserved[c.nameIdx] != nameFree || !s.condOK(c.when) {
			continue
		}
		s.reserved[c.nameIdx] = nameReserved
		if c.quiet {
			s.reserved[c.nameIdx] = nameReservedQuiet
		}
	}

	for i := range t.outputs {
		o := &t.outputs[i]
		if !s.condOK(o.when) {
			continue
		}
		if o.nameFrom >= 0 && len(s.derivedVals[o.nameFrom]) == 0 {
			continue // an output without a name matches nothing
		}
		s.row(i)
		if s.err != nil {
			return
		}
	}
	if len(s.rows) == 0 {
		s.err = ErrNoOutput
	}
}

func (s *state) resolveFields() {
	t := s.t
	if len(t.expose) == 0 {
		return
	}
	// The texts are copied into one buffer, and the slices are cut afterwards
	// because the buffer may move while it grows.
	s.fieldBuf = s.fieldBuf[:0]
	for i := range t.expose {
		b, ok := s.text(&t.expose[i])
		if !ok {
			s.fieldOff[2*i] = -1
			continue
		}
		s.fieldOff[2*i] = len(s.fieldBuf)
		s.fieldBuf = append(s.fieldBuf, b...)
		s.fieldOff[2*i+1] = len(s.fieldBuf)
	}
	for i := range t.expose {
		if from, to := s.fieldOff[2*i], s.fieldOff[2*i+1]; from >= 0 {
			s.fields[i] = s.fieldBuf[from:to:to]
		}
	}
}

func (s *state) computeDerived() {
	t := s.t
	for i := range t.derived {
		d := &t.derived[i]
		s.derivedVals[i] = s.derivedVals[i][:0]
		if !s.condOK(d.when) {
			continue // the value stays empty, and "required" does not apply
		}
		s.derivedVals[i] = s.appendDerived(s.derivedVals[i], d)
		if len(s.derivedVals[i]) == 0 && d.required {
			s.fail(ErrRequired, []byte(d.name))
			return
		}
	}
}

// appendDerived appends the value of d: the first source with text,
// normalised and prefixed as configured.
func (s *state) appendDerived(dst []byte, d *derived) []byte {
	for i := range d.srcs {
		b, ok := s.text(&d.srcs[i])
		if !ok {
			continue
		}
		if d.snake {
			s.seg = appendSnake(s.seg[:0], b)
			b = s.seg
		}
		if _, isReserved := d.reserved[string(b)]; isReserved {
			dst = append(dst, d.reservedPrefix...)
		} else if startsWithDigit(b) {
			dst = append(dst, d.digitPrefix...)
		}
		return append(dst, b...)
	}
	return dst
}

// row builds the row of one output.
func (s *state) row(oi int) {
	t := s.t
	member := t.outputs[oi].member
	start := len(s.out)
	s.rowOpen = start
	first := s.openRow(member)
	for i := first; i < len(t.sections) && s.err == nil; i++ {
		if member[i] && s.secOn[i] {
			s.section(&t.sections[i])
		}
	}
	s.writeMerges()
	s.writeDefaults()
	s.writeRest()
	if s.err != nil {
		return
	}
	bodyEnd := len(s.out)
	s.out = append(s.out, '}')
	if t.newline {
		s.out = append(s.out, '\n')
	}
	s.rows = append(s.rows, rowSeg{start: start, bodyEnd: bodyEnd, end: len(s.out), output: oi, dropped: s.dropped})
}

// openRow starts a row and returns the first section that is still to be
// walked.
//
// When the output holds everything the previous row of this record holds, the
// row starts as a copy of that one and only the new sections are walked. The
// key set and the dropped count carry on from there.
func (s *state) openRow(member []bool) int {
	t := s.t
	if t.extend && len(s.rows) > 0 {
		prev := s.rows[len(s.rows)-1]
		if first, ok := s.extends(t.outputs[prev.output].member, member); ok {
			s.out = append(s.out, s.out[prev.start:prev.bodyEnd]...)
			return first
		}
	}
	s.dropped = 0
	if t.collision != collisionKeep {
		s.keys.reset()
	}
	clear(s.slots)
	clear(s.seen)
	clear(s.colWritten)
	s.rest = s.rest[:0]
	s.out = append(s.out, '{')
	s.writeColumns()
	return 0
}

// extends reports whether the active sections of output a are a prefix of the
// active sections of output b, and if so, the first section that only b has.
func (s *state) extends(a, b []bool) (int, bool) {
	k := 0
	for k < len(a) && (a[k] && s.secOn[k]) == (b[k] && s.secOn[k]) {
		k++
	}
	for j := k; j < len(a); j++ {
		if a[j] && s.secOn[j] {
			return 0, false
		}
	}
	return k, true
}

func (s *state) writeColumns() {
	t := s.t
	for i := range t.columns {
		c := &t.columns[i]
		// The first column of a name that has a value wins.
		if s.colWritten[c.nameIdx] || !s.condOK(c.when) {
			continue
		}
		for j := range c.srcs {
			src := &c.srcs[j]
			if c.asString || src.kind != srcPath {
				b, ok := s.text(src)
				if !ok {
					continue
				}
				s.writeKey(c.name)
				s.out = appendQuoted(s.out, b)
			} else {
				v := s.lookup(&src.path)
				if v == nil {
					continue
				}
				s.writeKey(c.name)
				s.writeValue(v)
			}
			s.colWritten[c.nameIdx] = true
			break
		}
		if s.err != nil {
			return
		}
	}
}

func (s *state) section(sec *section) {
	root := s.rec
	if sec.from != nil {
		if root = s.rec.Get(sec.from...); root == nil {
			return
		}
	}
	s.src = append(s.src[:0], sec.srcBase...)
	s.dst = append(s.dst[:0], sec.dstBase...)
	s.save = s.save[:0]
	s.depth = 1
	s.quiet = sec.quiet
	// The section root is always entered, whatever flatten.arrays says: there
	// is no key yet under which an array could be written as one value.
	switch root.Type() {
	case fastjson.TypeObject:
		root.GetObject().Visit(s.visitFn)
	case fastjson.TypeArray:
		s.elements(root.GetArray())
	}
	s.quiet = false
}

func (s *state) visit(key []byte, v *fastjson.Value) {
	if s.err != nil {
		return
	}
	seg := key
	if s.t.snake {
		s.seg = appendSnake(s.seg[:0], key)
		if len(s.seg) == 0 {
			return // nothing left of the key: skip it and all below it
		}
		seg = s.seg
	}
	if s.t.segAliases != nil {
		if alias, ok := s.t.segAliases[string(seg)]; ok {
			seg = alias
		}
	}
	s.child(key, seg, v)
}

func (s *state) elements(arr []*fastjson.Value) {
	for i, v := range arr {
		if s.err != nil {
			return
		}
		s.idx = strconv.AppendInt(s.idx[:0], int64(i), 10)
		s.child(s.idx, s.idx, v)
	}
}

// child extends the source path by key and the output key by seg, handles the
// node and takes both back. key and seg may be scratch buffers: they are
// copied before anything below the node can reuse them.
func (s *state) child(key, seg []byte, v *fastjson.Value) {
	ls, ld := len(s.src), len(s.dst)
	if s.t.actions != nil {
		s.src = append(s.src, key...)
	}
	s.dst = append(s.dst, seg...)
	s.node(v)
	s.src, s.dst = s.src[:ls], s.dst[:ld]
}

func (s *state) node(v *fastjson.Value) {
	var act *action
	if s.t.actions != nil {
		act = s.t.actions[string(s.src)]
	}
	if act == nil {
		s.handle(v, nil)
		return
	}
	if act.drop {
		return
	}
	if act.renameTo == nil {
		s.handle(v, act)
		return
	}
	// A rename replaces the whole output key, which overwrites the parent's
	// key in the same buffer. Truncating would not bring it back, so keep a
	// copy and restore it.
	mark := len(s.save)
	s.save = append(s.save, s.dst...)
	s.dst = append(s.dst[:0], act.renameTo...)
	s.handle(v, act)
	s.dst = append(s.dst[:0], s.save[mark:]...)
	s.save = s.save[:mark]
}

func (s *state) handle(v *fastjson.Value, act *action) {
	t := s.t
	tp := v.Type()
	// The children of this node would be container level depth+1.
	deeper := t.maxDepth == 0 || s.depth < t.maxDepth
	switch tp {
	case fastjson.TypeObject:
		if o := v.GetObject(); o.Len() > 0 && deeper {
			s.descend()
			o.Visit(s.visitFn)
			s.depth--
			return
		}
	case fastjson.TypeArray:
		if arr := v.GetArray(); len(arr) > 0 && deeper && t.arrays == arraysIndex {
			s.descend()
			s.elements(arr)
			s.depth--
			return
		}
	}

	// The node is written as one value.
	if act != nil && len(act.slots) > 0 && tp != fastjson.TypeObject && tp != fastjson.TypeArray {
		for _, slot := range act.slots {
			s.slots[slot] = v
		}
		if act.consume {
			return
		}
	}
	if tp == fastjson.TypeNull && t.dropNulls {
		return
	}
	if tp == fastjson.TypeObject && t.dropEmptyObjects && v.GetObject().Len() == 0 {
		return
	}
	key := s.dst
	if t.fixKeys {
		var verdict keyVerdict
		switch key, verdict = s.fixKey(key); verdict {
		case keyDropped:
			return
		case keyNotKept:
			// A copy of an active column is a duplicate, not an unmapped key.
			if t.rest != nil && !s.reservedBy(key) {
				s.collect(key, v)
			}
			return
		}
	}
	if s.claim(key) {
		s.out = s.appendValue(s.out, v)
	}
}

// collect adds a key that keep removed to the rest column of the row, under
// its final name.
func (s *state) collect(key []byte, v *fastjson.Value) {
	if len(s.rest) == 0 {
		s.rest = append(s.rest, '{')
	} else {
		s.rest = append(s.rest, ',')
	}
	s.rest = appendQuoted(s.rest, key)
	s.rest = append(s.rest, ':')
	s.rest = s.appendValue(s.rest, v)
}

// descend gets both paths ready for the children of the current node. The
// separator belongs to the container, not to the child, because a key can be
// empty: {"":{"a":1}} gives ".a". The caller's child truncates it away again.
func (s *state) descend() {
	s.depth++
	if s.t.actions != nil {
		s.src = append(s.src, '.')
	}
	s.dst = append(s.dst, s.t.sep...)
}

// What the keep and drop lists make of a key.
type keyVerdict uint8

const (
	keyWritten keyVerdict = iota
	keyDropped            // matched by drop: removed on purpose
	keyNotKept            // not in keep: unmapped, and collected when keys.rest is set
)

// fixKey applies the full-key corrections: aliases, the digit prefix, then the
// drop and keep lists.
func (s *state) fixKey(key []byte) ([]byte, keyVerdict) {
	t := s.t
	for i := range t.aliasGroups {
		if !s.groupOn[i] {
			continue
		}
		if to, ok := t.aliasGroups[i].m[string(key)]; ok {
			key = to
			break
		}
	}
	if len(t.digitPrefix) > 0 && startsWithDigit(key) {
		s.pfx = append(append(s.pfx[:0], t.digitPrefix...), key...)
		key = s.pfx
	}
	if t.drop.matches(key) {
		return key, keyDropped
	}
	if !t.keep.empty() && !t.keep.matches(key) {
		return key, keyNotKept
	}
	return key, keyWritten
}

// keyFilter is a set of output keys: exact names, and the prefixes of entries
// that were written with a trailing *.
type keyFilter struct {
	exact    map[string]struct{}
	prefixes [][]byte
}

func (f *keyFilter) empty() bool { return len(f.exact) == 0 && len(f.prefixes) == 0 }

func (f *keyFilter) matches(key []byte) bool {
	if _, ok := f.exact[string(key)]; ok {
		return true
	}
	for _, p := range f.prefixes {
		if bytes.HasPrefix(key, p) {
			return true
		}
	}
	return false
}

// claim decides whether a flattened key, a merge result or a default may be
// written under name, and if so writes the key.
func (s *state) claim(name []byte) bool {
	t := s.t
	if s.reservedBy(name) {
		// The name belongs to an active column. That is never an error,
		// whatever the collision policy.
		if s.reserved[t.columnIdx[string(name)]] == nameReserved && !s.quiet {
			s.dropped++
		}
		return false
	}
	if t.collision != collisionKeep && !s.keys.add(name) {
		switch {
		case s.quiet:
		case t.collision == collisionError:
			s.fail(ErrCollision, name)
		default:
			s.dropped++
		}
		return false
	}
	s.writeKey(name)
	return true
}

// reservedBy reports whether an active column of this record owns name.
func (s *state) reservedBy(name []byte) bool {
	if s.t.columnIdx == nil {
		return false
	}
	ci, ok := s.t.columnIdx[string(name)]
	return ok && s.reserved[ci] != nameFree
}

// writeKey writes the comma, the key and the colon. Every key of a row goes
// through it, which is how a default knows that its key is already there.
//
// Columns call it directly. They come first in a row and their names are
// reserved, so nothing can have taken a column's key.
func (s *state) writeKey(name []byte) {
	if s.t.defaultIdx != nil {
		if di, ok := s.t.defaultIdx[string(name)]; ok {
			s.seen[di] = true
		}
	}
	if len(s.out) > s.rowOpen+1 {
		s.out = append(s.out, ',')
	}
	s.out = appendQuoted(s.out, name)
	s.out = append(s.out, ':')
}

// writeValue writes v as the value of the key just written.
func (s *state) writeValue(v *fastjson.Value) {
	s.out = s.appendValue(s.out, v)
}

// appendValue appends v the way a flattened leaf is written: scalars as they
// are, containers as raw JSON, arrays as a JSON string in "string" mode. dst
// must not be s.raw, which is scratch space here.
func (s *state) appendValue(dst []byte, v *fastjson.Value) []byte {
	tp := v.Type()
	if tp != fastjson.TypeObject && tp != fastjson.TypeArray {
		return s.appendScalar(dst, v)
	}
	s.raw = s.raw[:0]
	s.appendRaw(v)
	if tp == fastjson.TypeArray && s.t.arrays == arraysString {
		return appendQuoted(dst, s.raw)
	}
	return append(dst, s.raw...)
}

// appendNumber appends the text of the number v as it is, which keeps its
// precision, and reports whether it is a number by RFC 8259. The parser also
// accepts 00, +1, 1.2.3, NaN and inf.
func appendNumber(dst []byte, v *fastjson.Value) ([]byte, bool) {
	n := len(dst)
	dst = v.MarshalTo(dst) // MarshalTo on a number: copies the number text
	return dst, validNumber(dst[n:])
}

// appendScalar appends a string, number, true, false or null as JSON.
//
// fastjson's MarshalTo must not see a string: it escapes the Go way ("\x01",
// "\a"), which is not JSON.
func (s *state) appendScalar(dst []byte, v *fastjson.Value) []byte {
	switch v.Type() {
	case fastjson.TypeString:
		return appendQuoted(dst, v.GetStringBytes())
	case fastjson.TypeNumber:
		n := len(dst)
		dst, ok := appendNumber(dst, v)
		if !ok {
			s.fail(ErrInvalidNumber, dst[n:])
		}
		return dst
	}
	return v.MarshalTo(dst) // MarshalTo on true, false or null
}

// appendRaw serialises v into s.raw.
func (s *state) appendRaw(v *fastjson.Value) {
	if s.err != nil {
		return
	}
	switch v.Type() {
	case fastjson.TypeObject:
		s.raw = append(s.raw, '{')
		v.GetObject().Visit(s.rawVisitFn)
		s.raw = append(s.raw, '}')
	case fastjson.TypeArray:
		s.raw = append(s.raw, '[')
		for i, e := range v.GetArray() {
			if i > 0 {
				s.raw = append(s.raw, ',')
			}
			s.appendRaw(e)
		}
		s.raw = append(s.raw, ']')
	default:
		s.raw = s.appendScalar(s.raw, v)
	}
}

// rawVisit writes one object member. Whether a comma is due shows in the last
// byte written, so no state per nesting level is needed: a value never ends
// in "{".
func (s *state) rawVisit(key []byte, v *fastjson.Value) {
	if s.err != nil {
		return
	}
	if s.raw[len(s.raw)-1] != '{' {
		s.raw = append(s.raw, ',')
	}
	s.raw = appendQuoted(s.raw, key)
	s.raw = append(s.raw, ':')
	s.appendRaw(v)
}

func (s *state) writeMerges() {
	t := s.t
	for i := range t.merges {
		if s.err != nil {
			return
		}
		m := &t.merges[i]
		vals := s.slots[m.first : m.first+m.n]
		s.raw = s.raw[:0]
		switch m.mode {
		case mergeFirst:
			s.mergeFirst(vals)
		case mergeArray:
			s.mergeArray(vals)
		default:
			s.mergeConcat(vals, m.sep)
		}
		// An empty s.raw means that no source was present.
		if s.err == nil && len(s.raw) > 0 && s.claim(m.to) {
			s.out = append(s.out, s.raw...)
		}
	}
}

// mergeFirst takes the first source that is present and not null, unchanged.
func (s *state) mergeFirst(vals []*fastjson.Value) {
	for _, v := range vals {
		if v != nil && v.Type() != fastjson.TypeNull {
			s.appendRaw(v)
			return
		}
	}
}

// mergeArray lists the sources that are present. A present null is kept.
func (s *state) mergeArray(vals []*fastjson.Value) {
	for _, v := range vals {
		if v == nil {
			continue
		}
		if len(s.raw) == 0 {
			s.raw = append(s.raw, '[')
		} else {
			s.raw = append(s.raw, ',')
		}
		s.appendRaw(v)
	}
	if len(s.raw) > 0 {
		s.raw = append(s.raw, ']')
	}
}

// mergeConcat joins the sources into one string. Strings go in by their
// unescaped bytes, escaped once; numbers and booleans by their text.
func (s *state) mergeConcat(vals []*fastjson.Value, sep []byte) {
	for _, v := range vals {
		if v == nil || v.Type() == fastjson.TypeNull {
			continue
		}
		if len(s.raw) == 0 {
			s.raw = append(s.raw, '"')
		} else {
			s.raw = append(s.raw, sep...)
		}
		if v.Type() == fastjson.TypeString {
			s.raw = appendEscaped(s.raw, v.GetStringBytes())
		} else {
			s.raw = s.appendScalar(s.raw, v)
		}
	}
	if len(s.raw) > 0 {
		s.raw = append(s.raw, '"')
	}
}

// writeRest writes the keys that keep removed as one JSON string, at the end
// of the row, when keys.rest is set and something was removed.
func (s *state) writeRest() {
	if len(s.rest) == 0 || s.err != nil {
		return
	}
	s.rest = append(s.rest, '}')
	if s.claim(s.t.rest) {
		s.out = appendQuoted(s.out, s.rest)
	}
}

func (s *state) writeDefaults() {
	t := s.t
	for i := range t.defaults {
		if s.err != nil {
			return
		}
		if !s.seen[i] && s.claim(t.defaults[i].key) {
			s.out = append(s.out, t.defaults[i].value...)
		}
	}
}

// valueAt returns the value at keys, or nil when it is missing or null.
func valueAt(doc *fastjson.Value, keys []string) *fastjson.Value {
	v := doc.Get(keys...)
	if v != nil && v.Type() == fastjson.TypeNull {
		return nil
	}
	return v
}

// lookup returns the value at a source path, or nil when it is missing or
// null. A path listed in input.inherit falls back to the envelope.
func (s *state) lookup(p *pathRef) *fastjson.Value {
	v := valueAt(s.rec, p.keys)
	if v == nil && p.inherit {
		v = valueAt(s.env, p.keys) // a nil envelope has nothing
	}
	return v
}

// text returns the text of a source: string bytes, or number text if it is a
// valid number. The result may live in a scratch buffer and is valid until the
// next call of text.
func (s *state) text(src *source) ([]byte, bool) {
	switch src.kind {
	case srcPath:
		v := s.lookup(&src.path)
		if v == nil {
			return nil, false
		}
		switch v.Type() {
		case fastjson.TypeString:
			return v.GetStringBytes(), true
		case fastjson.TypeNumber:
			var ok bool
			s.num, ok = appendNumber(s.num[:0], v)
			return s.num, ok
		}
		return nil, false
	case srcNow:
		return s.nowText(), true
	case srcDerived:
		b := s.derivedVals[src.idx]
		return b, len(b) > 0
	case srcVar:
		val := s.opt.Vars[src.name]
		if val == "" {
			return nil, false
		}
		// A read-only view; it does not outlive the call.
		return unsafe.Slice(unsafe.StringData(val), len(val)), true
	case srcSkew:
		return s.skew(src)
	}
	return nil, false
}

// present reports whether a source has a value of any type. It must not go
// through text: an object has no text but does exist.
func (s *state) present(src *source) bool {
	if src.kind == srcPath {
		return s.lookup(&src.path) != nil
	}
	_, ok := s.text(src)
	return ok
}

// nowText formats "$now" at most once per call.
func (s *state) nowText() []byte {
	if len(s.nowBuf) == 0 {
		s.now = s.opt.Now
		if s.now.IsZero() {
			s.now = time.Now()
		}
		s.now = s.now.UTC()
		s.nowBuf = s.now.AppendFormat(s.nowBuf, timeLayout)
	}
	return s.nowBuf
}

// skew computes now - (sent - original): the time of the event on the
// server's clock, when the client's clock cannot be trusted.
func (s *state) skew(src *source) ([]byte, bool) {
	sent, ok := s.timeAt(&src.sent)
	if !ok {
		return nil, false
	}
	original, ok := s.timeAt(&src.original)
	if !ok {
		return nil, false
	}
	s.nowText()
	at := s.now.Add(-sent.Sub(original))
	s.skewBuf = at.AppendFormat(s.skewBuf[:0], timeLayout)
	return s.skewBuf, true
}

func (s *state) timeAt(p *pathRef) (time.Time, bool) {
	b := s.lookup(p).GetStringBytes() // nil unless it is a string
	if len(b) == 0 {
		return time.Time{}, false
	}
	// A read-only view for the duration of Parse, which keeps no reference.
	at, err := time.Parse(time.RFC3339, unsafe.String(unsafe.SliceData(b), len(b)))
	return at, err == nil
}

// condOK reports whether condition i passes for the current record. -1 is the
// condition that was not configured, which always passes.
func (s *state) condOK(i int) bool {
	if i < 0 {
		return true
	}
	if s.condCache[i] == condUnknown {
		s.condCache[i] = condFalse
		if s.evalCond(&s.t.conds[i]) {
			s.condCache[i] = condTrue
		}
	}
	return s.condCache[i] == condTrue
}

func (s *state) evalCond(c *cond) bool {
	if c.op == condExists {
		for i := range c.srcs {
			if s.present(&c.srcs[i]) {
				return c.exists
			}
		}
		return !c.exists
	}
	// equals and in test the first source that has text.
	for i := range c.srcs {
		b, ok := s.text(&c.srcs[i])
		if !ok {
			continue
		}
		if c.op == condEquals {
			return string(b) == c.equals
		}
		for _, want := range c.in {
			if string(b) == want {
				return true
			}
		}
		return false
	}
	return false
}
