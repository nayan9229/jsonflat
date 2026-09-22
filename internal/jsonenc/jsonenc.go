// Package jsonenc holds the JSON string escaper and the RFC 8259 number check
// used by jsonflat.
package jsonenc

const hexDigits = "0123456789abcdef"

// AppendQuoted appends s as a JSON string, including the quotes.
func AppendQuoted(dst, s []byte) []byte {
	dst = append(dst, '"')
	dst = AppendEscaped(dst, s)
	return append(dst, '"')
}

// AppendEscaped appends s with JSON string escaping applied, without quotes.
// Only the characters that RFC 8259 requires to be escaped are escaped. Bytes
// of 0x80 and above pass through unchanged, so valid UTF-8 stays valid.
func AppendEscaped(dst, s []byte) []byte {
	start := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 0x20 && c != '"' && c != '\\' {
			continue
		}
		dst = append(dst, s[start:i]...)
		switch c {
		case '"':
			dst = append(dst, '\\', '"')
		case '\\':
			dst = append(dst, '\\', '\\')
		case '\n':
			dst = append(dst, '\\', 'n')
		case '\r':
			dst = append(dst, '\\', 'r')
		case '\t':
			dst = append(dst, '\\', 't')
		default:
			dst = append(dst, '\\', 'u', '0', '0', hexDigits[c>>4], hexDigits[c&0xf])
		}
		start = i + 1
	}
	return append(dst, s[start:]...)
}

// ValidNumber reports whether s is a number as defined by RFC 8259:
//
//	-? (0 | [1-9][0-9]*) (\.[0-9]+)? ([eE][+-]?[0-9]+)?
func ValidNumber(s []byte) bool {
	i := 0
	if i < len(s) && s[i] == '-' {
		i++
	}
	switch {
	case i < len(s) && s[i] == '0':
		i++
	case i < len(s) && s[i] >= '1' && s[i] <= '9':
		for i < len(s) && IsDigit(s[i]) {
			i++
		}
	default:
		return false
	}
	if i < len(s) && s[i] == '.' {
		i++
		if i == len(s) || !IsDigit(s[i]) {
			return false
		}
		for i < len(s) && IsDigit(s[i]) {
			i++
		}
	}
	if i < len(s) && (s[i] == 'e' || s[i] == 'E') {
		i++
		if i < len(s) && (s[i] == '+' || s[i] == '-') {
			i++
		}
		if i == len(s) || !IsDigit(s[i]) {
			return false
		}
		for i < len(s) && IsDigit(s[i]) {
			i++
		}
	}
	return i == len(s)
}

// IsDigit reports whether c is an ASCII digit.
func IsDigit(c byte) bool { return c >= '0' && c <= '9' }
