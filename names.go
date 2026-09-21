package jsonflat

const (
	clsNone = iota
	clsLower
	clsUpper
	clsDigit
)

// appendSnake appends s to dst in snake_case:
//
//	productId   -> product_id      Product ID  -> product_id
//	userID      -> user_id         HTTPServer  -> http_server
//	v2Beta      -> v2_beta         address1    -> address1
//	$price      -> price           first-name  -> first_name
//
// Any run of characters that are not letters or digits becomes one underscore,
// and leading and trailing underscores are removed. Bytes of 0x80 and above
// are copied unchanged and treated as lower-case letters.
func appendSnake(dst, s []byte) []byte {
	start := len(dst)
	prev := clsNone
	sep := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		cls := clsNone
		switch {
		case c >= 'a' && c <= 'z', c >= 0x80:
			cls = clsLower
		case c >= 'A' && c <= 'Z':
			cls = clsUpper
		case c >= '0' && c <= '9':
			cls = clsDigit
		}
		if cls == clsNone {
			sep = true
			continue
		}
		boundary := sep
		if cls == clsUpper && !boundary {
			switch prev {
			case clsLower, clsDigit:
				boundary = true // productId, v2Beta
			case clsUpper:
				// HTTPServer: break before the last capital of an acronym.
				if i+1 < len(s) && s[i+1] >= 'a' && s[i+1] <= 'z' {
					boundary = true
				}
			}
		}
		if boundary && len(dst) > start {
			dst = append(dst, '_')
		}
		if cls == clsUpper {
			c += 'a' - 'A'
		}
		dst = append(dst, c)
		prev, sep = cls, false
	}
	return dst
}

// keySet records the column names already written for one record. It stores
// 64-bit FNV-1a hashes rather than the names, and uses a generation counter so
// that resetting it between records costs nothing. It allocates only when it
// has to grow.
//
// Two different names with the same 64-bit hash would be treated as one. With
// a few hundred columns per record the chance of that is about 1 in 10^15.
type keySet struct {
	slots []keySlot
	gen   uint32
	n     int
}

type keySlot struct {
	hash uint64
	gen  uint32
}

func (k *keySet) reset() {
	if k.slots == nil {
		k.slots = make([]keySlot, 256)
	}
	k.gen++
	k.n = 0
	if k.gen == 0 { // wrapped: clear stale generations once
		clear(k.slots)
		k.gen = 1
	}
}

// add reports whether name was not in the set yet.
func (k *keySet) add(name []byte) bool {
	if k.n*2 >= len(k.slots) {
		k.grow()
	}
	h := fnv1a(name)
	mask := uint64(len(k.slots) - 1)
	for i := h & mask; ; i = (i + 1) & mask {
		s := &k.slots[i]
		if s.gen != k.gen {
			s.hash, s.gen = h, k.gen
			k.n++
			return true
		}
		if s.hash == h {
			return false
		}
	}
}

func (k *keySet) grow() {
	old := k.slots
	k.slots = make([]keySlot, len(old)*2)
	mask := uint64(len(k.slots) - 1)
	for _, o := range old {
		if o.gen != k.gen {
			continue
		}
		i := o.hash & mask
		for k.slots[i].gen == k.gen {
			i = (i + 1) & mask
		}
		k.slots[i] = o
	}
}

func fnv1a(b []byte) uint64 {
	h := uint64(14695981039346656037)
	for _, c := range b {
		h ^= uint64(c)
		h *= 1099511628211
	}
	return h
}
