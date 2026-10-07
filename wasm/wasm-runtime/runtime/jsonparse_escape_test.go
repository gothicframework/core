package runtime

import "testing"

// The \uXXXX decoding path had no success-case coverage: every existing unicode
// case in jsonparse_test.go carries the character as raw UTF-8 bytes, so hex4
// and the surrogate branch of parseUnicodeEscape were only ever reached by the
// malformed-input cases. These tests drive the escape form itself.

func TestParseJSONUnicodeEscapes(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"ascii escape", `"\u0041"`, "A"},
		{"nul", `"\u0000"`, "\x00"},
		{"latin1 BMP", `"\u00e9"`, "é"},
		{"uppercase hex digits", `"\u00E9"`, "é"},
		{"mixed-case hex digits", `"\u00eF"`, "ï"},
		{"max BMP", `"\uffff"`, "\uffff"},
		{"escape then literal", `"\u0041B"`, "AB"},
		{"two escapes in a row", `"\u0041\u0042"`, "AB"},
		{"escape inside text", `"caf\u00e9 bar"`, "café bar"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseJSON([]byte(tc.in))
			if err != nil {
				t.Fatalf("parseJSON(%s) returned error: %v", tc.in, err)
			}
			s, ok := got.(string)
			if !ok {
				t.Fatalf("parseJSON(%s) returned %T, want string", tc.in, got)
			}
			if s != tc.want {
				t.Errorf("parseJSON(%s) = %q, want %q", tc.in, s, tc.want)
			}
		})
	}
}

// TestParseJSONSurrogatePairEscape drives utf16.DecodeRune's success branch: a
// high surrogate followed by a low one must combine into a single astral rune.
// Only the failure branches (lone high, lone low, high-then-non-surrogate) were
// covered before.
func TestParseJSONSurrogatePairEscape(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"emoji", `"\uD83D\uDE00"`, "\U0001F600"},
		{"lowercase surrogate hex", `"\ud83d\ude00"`, "\U0001F600"},
		{"first astral code point", `"\uD800\uDC00"`, "\U00010000"},
		{"last astral code point", `"\uDBFF\uDFFF"`, "\U0010FFFF"},
		{"pair surrounded by text", `"a\uD83D\uDE00b"`, "a\U0001F600b"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseJSON([]byte(tc.in))
			if err != nil {
				t.Fatalf("parseJSON(%s) returned error: %v", tc.in, err)
			}
			if got != tc.want {
				t.Errorf("parseJSON(%s) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestHex4ConsumesExactlyFourDigits pins the digit count. Reading a fifth digit
// would swallow the character after the escape, so an escape followed by a hex
// character is the case that distinguishes the two behaviours.
func TestHex4ConsumesExactlyFourDigits(t *testing.T) {
	// "\u0041" followed by 'a', itself a valid hex digit: a five-digit read
	// would consume the 'a' and produce a different string (or an error).
	got, err := parseJSON([]byte(`"\u0041a"`))
	if err != nil {
		t.Fatalf("parseJSON returned error: %v", err)
	}
	if got != "Aa" {
		t.Errorf("parseJSON(`\"\\u0041a\"`) = %q, want %q — hex4 must consume exactly 4 digits", got, "Aa")
	}

	// The same shape inside an object value, so the position bookkeeping after
	// the escape is exercised with a following token.
	obj, err := parseJSON([]byte(`{"k":"\u0041a","n":1}`))
	if err != nil {
		t.Fatalf("parseJSON(object) returned error: %v", err)
	}
	m, ok := obj.(map[string]any)
	if !ok {
		t.Fatalf("parseJSON(object) returned %T, want map[string]any", obj)
	}
	if m["k"] != "Aa" {
		t.Errorf(`m["k"] = %q, want "Aa"`, m["k"])
	}
	if m["n"] != float64(1) {
		t.Errorf(`m["n"] = %v, want 1 — parsing must resume correctly after the escape`, m["n"])
	}
}
