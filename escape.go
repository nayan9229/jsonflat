package jsonflat

import "github.com/nayan9229/jsonflat/internal/jsonenc"

// Short names for the JSON encoding helpers. They also keep jsonenc out of
// the import block of config.go, which is fixed.

func appendQuoted(dst, s []byte) []byte { return jsonenc.AppendQuoted(dst, s) }

func appendEscaped(dst, s []byte) []byte { return jsonenc.AppendEscaped(dst, s) }

func validNumber(s []byte) bool { return jsonenc.ValidNumber(s) }

func startsWithDigit(s []byte) bool { return len(s) > 0 && jsonenc.IsDigit(s[0]) }
