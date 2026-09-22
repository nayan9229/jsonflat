package jsonflat

// strMap is a map of output keys or source paths that also keeps a bitmask
// of the key lengths it holds. The walk looks every key it visits up in
// several such maps, most of which have a handful of entries, and most
// lookups miss; a key whose length no entry has is turned away before it is
// hashed. Profiles of the RudderStack preset showed 60 % of map time going
// to one-entry maps; this cut both hot benchmarks by 16 %.
type strMap[V any] struct {
	lens uint64 // bit n: some key has length n; bit 63: some key is 63 bytes or longer
	m    map[string]V
}

func lenBit(n int) uint64 { return 1 << min(n, 63) }

func (sm *strMap[V]) set(k string, v V) {
	if sm.m == nil {
		sm.m = map[string]V{}
	}
	sm.m[k] = v
	sm.lens |= lenBit(len(k))
}

// get looks k up without allocating: the compiler does not copy a []byte
// that is converted to a string only to index a map.
func (sm *strMap[V]) get(k []byte) (V, bool) {
	if sm.lens&lenBit(len(k)) == 0 {
		var zero V
		return zero, false
	}
	v, ok := sm.m[string(k)]
	return v, ok
}

// lookup is get for the compiler, which holds strings.
func (sm *strMap[V]) lookup(k string) (V, bool) {
	v, ok := sm.m[k]
	return v, ok
}

func (sm *strMap[V]) len() int { return len(sm.m) }
