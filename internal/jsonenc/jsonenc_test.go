package jsonenc

import (
	"encoding/json"
	"testing"
)

func TestAppendEscaped(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{"empty", "", ""},
		{"plain", "hello world", "hello world"},
		{"quote", `a"b`, `a\"b`},
		{"backslash", `a\b`, `a\\b`},
		{"newline, return, tab", "a\nb\rc\td", `a\nb\rc\td`},
		{"nul", "\x00", `\u0000`},
		{"bell is not a Go escape", "\a", `\u0007`},
		{"backspace and form feed", "\b\f", `\u0008\u000c`},
		{"highest control character", "\x1f", `\u001f`},
		{"0x7f passes through", "\x7f", "\x7f"},
		{"multi-byte UTF-8 passes through", "é 😀 日本", "é 😀 日本"},
		{"invalid UTF-8 passes through", "\xff\xfe", "\xff\xfe"},
		{"slash is not escaped", "a/b", "a/b"},
		{"specials at both ends", `"mid"`, `\"mid\"`},
		{"consecutive specials", "\"\\\n", `\"\\\n`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := string(AppendEscaped(nil, []byte(tc.in))); got != tc.want {
				t.Errorf("AppendEscaped(%q) = %q, want %q", tc.in, got, tc.want)
			}
			quoted := AppendQuoted([]byte("x"), []byte(tc.in))
			if want := `x"` + tc.want + `"`; string(quoted) != want {
				t.Errorf("AppendQuoted(%q) = %q, want %q", tc.in, quoted, want)
			}
		})
	}
}

// Every byte value must round-trip through encoding/json when the input is
// valid UTF-8, and must at least produce valid JSON when it is not.
func TestAppendQuotedRoundTrip(t *testing.T) {
	for c := 0; c < 256; c++ {
		in := []byte{'a', byte(c), 'z'}
		out := AppendQuoted(nil, in)
		if !json.Valid(out) {
			t.Fatalf("byte 0x%02x: invalid JSON %q", c, out)
		}
		if c >= 0x80 {
			continue // a lone high byte is not valid UTF-8; json replaces it
		}
		var s string
		if err := json.Unmarshal(out, &s); err != nil {
			t.Fatalf("byte 0x%02x: %v", c, err)
		}
		if s != string(in) {
			t.Fatalf("byte 0x%02x: round trip gave %q", c, s)
		}
	}
}

func TestValidNumber(t *testing.T) {
	valid := []string{
		"0", "-0", "1", "-1", "10", "1234567890", "0.5", "-0.5", "1.25", "0.0",
		"1e5", "1E5", "1e+5", "1e-5", "1E+2", "1.5e10", "-1.5E-10", "0e0",
		"1e400", "12345678901234567890.123456789",
	}
	for _, s := range valid {
		if !ValidNumber([]byte(s)) {
			t.Errorf("ValidNumber(%q) = false, want true", s)
		}
		if !json.Valid([]byte(s)) {
			t.Errorf("test vector %q is not valid JSON", s)
		}
	}
	invalid := []string{
		"", "-", "+", "+1", "00", "01", "-01", "1.", ".5", "-.5", "1e", "1e+",
		"1e-", "1E", "e5", "1.2.3", "1..2", "1.e5", "1e5.5", "1e5e5", "--1",
		"NaN", "nan", "inf", "-inf", "Infinity", "0x10", "1 ", " 1", "1a", "1-",
	}
	for _, s := range invalid {
		if ValidNumber([]byte(s)) {
			t.Errorf("ValidNumber(%q) = true, want false", s)
		}
	}
}

func TestIsDigit(t *testing.T) {
	for c := 0; c < 256; c++ {
		if got, want := IsDigit(byte(c)), c >= '0' && c <= '9'; got != want {
			t.Errorf("IsDigit(%q) = %v, want %v", byte(c), got, want)
		}
	}
}
