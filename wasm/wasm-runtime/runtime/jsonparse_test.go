package runtime

import (
	"math"
	"reflect"
	"testing"
)

// The JSON parser (jsonparse.go) is build-tag-free on purpose so it compiles
// host-side and can be exercised on the normal `go test` toolchain — no browser
// needed. Two invariants matter, mirroring the binary codec's fuzz discipline:
//
//  1. Correctness — a representative table of well-formed documents decodes to
//     the exact Go values D5 mandates (map[string]any / []any / string / float64
//     / bool / nil), including unicode escapes and surrogate pairs.
//
//  2. FuzzJSONParseNeverPanics — feeding *arbitrary* bytes to parseJSON NEVER
//     panics; it either returns a value or a non-nil error. A panic inside a WASM
//     component is a RuntimeError: unreachable that kills the instance, so this is
//     a hard safety property, not a nicety.

func TestParseJSONValues(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want any
	}{
		{"null", `null`, nil},
		{"true", `true`, true},
		{"false", `false`, false},
		{"string", `"hello"`, "hello"},
		{"empty string", `""`, ""},
		{"int", `42`, float64(42)},
		{"negative", `-17`, float64(-17)},
		{"zero", `0`, float64(0)},
		{"negative zero", `-0`, math.Copysign(0, -1)},
		{"float", `3.14`, float64(3.14)},
		{"exponent", `1e3`, float64(1000)},
		{"exponent signed", `2.5E-2`, float64(0.025)},
		{"big exponent", `1.5e10`, float64(1.5e10)},
		{"empty object", `{}`, map[string]any{}},
		{"empty array", `[]`, []any{}},
		{"array of numbers", `[1,2,3]`, []any{float64(1), float64(2), float64(3)}},
		{"mixed array", `[1,"a",true,null]`, []any{float64(1), "a", true, nil}},
		{"simple object", `{"a":1}`, map[string]any{"a": float64(1)}},
		{
			"nested",
			`{"user":{"name":"Bob","tags":["x","y"],"active":true},"n":2}`,
			map[string]any{
				"user": map[string]any{
					"name":   "Bob",
					"tags":   []any{"x", "y"},
					"active": true,
				},
				"n": float64(2),
			},
		},
		{"whitespace", "  \t\n{ \"a\" : [ 1 , 2 ] }\r\n ", map[string]any{"a": []any{float64(1), float64(2)}}},
		// String escapes.
		{"escapes", `"a\"b\\c\/d\b\f\n\r\te"`, "a\"b\\c/d\b\f\n\r\te"},
		{"unicode BMP", `"é"`, "é"},          // é
		{"unicode ascii", `"A"`, "A"},             // A
		{"surrogate pair", `"😀"`, "\U0001F600"}, // 😀
		{"unicode + raw utf8", `"café ☕"`, "café ☕"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseJSON([]byte(tc.in))
			if err != nil {
				t.Fatalf("parseJSON(%q) unexpected error: %v", tc.in, err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("parseJSON(%q)\n want %#v\n  got %#v", tc.in, tc.want, got)
			}
		})
	}
}

// TestParseJSONNumberPrecision documents the D5 int64>2^53 precision caveat: a
// large integer is coerced to float64 and the low bits are lost. This is a
// deliberate, documented limitation — the test pins the behaviour so a future
// change to int-vs-float coercion is a conscious decision.
func TestParseJSONNumberPrecision(t *testing.T) {
	got, err := parseJSON([]byte(`9007199254740993`)) // 2^53 + 1
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	f, ok := got.(float64)
	if !ok {
		t.Fatalf("want float64, got %T", got)
	}
	// 2^53+1 is not representable as float64; it rounds to 2^53.
	if f != 9007199254740992 {
		t.Fatalf("want 9007199254740992 (rounded), got %v", f)
	}
}

func TestParseJSONMalformed(t *testing.T) {
	bad := []struct {
		name string
		in   string
	}{
		{"empty", ``},
		{"whitespace only", `   `},
		{"unterminated string", `"abc`},
		{"unterminated object", `{"a":1`},
		{"unterminated array", `[1,2`},
		{"trailing garbage", `{} x`},
		{"two values", `1 2`},
		{"leading zero", `01`},
		{"leading zero array", `[01]`},
		{"bare word", `foo`},
		{"truncated true", `tru`},
		{"truncated null", `nul`},
		{"lone minus", `-`},
		{"dot no digits", `1.`},
		{"exp no digits", `1e`},
		{"bad \\u short", `"\u12"`},
		{"bad \\u nonhex", `"\uZZZZ"`},
		{"lone high surrogate", `"\uD83D"`},
		{"lone low surrogate", `"\uDE00"`},
		{"high then non-surrogate", `"\uD83Dx"`},
		{"bad escape", `"\x"`},
		{"unescaped control", "\"a\x01b\""},
		{"object no colon", `{"a" 1}`},
		{"object non-string key", `{1:2}`},
		{"object trailing comma", `{"a":1,}`},
		{"array trailing comma", `[1,]`},
		{"just comma", `,`},
		{"unclosed brace only", `{`},
		{"unclosed bracket only", `[`},
		{"plus number", `+1`},
	}
	for _, tc := range bad {
		t.Run(tc.name, func(t *testing.T) {
			v, err := parseJSON([]byte(tc.in))
			if err == nil {
				t.Fatalf("parseJSON(%q) = %#v, want error", tc.in, v)
			}
		})
	}
}

// TestParseJSONDeepNesting confirms the max-depth guard rejects pathological
// nesting with an error instead of overflowing the stack.
func TestParseJSONDeepNesting(t *testing.T) {
	deep := make([]byte, 0, 2*(maxJSONDepth+50))
	for i := 0; i < maxJSONDepth+50; i++ {
		deep = append(deep, '[')
	}
	for i := 0; i < maxJSONDepth+50; i++ {
		deep = append(deep, ']')
	}
	if _, err := parseJSON(deep); err == nil {
		t.Fatalf("expected max-depth error for %d-deep array", maxJSONDepth+50)
	}

	// A document just within the limit must still parse.
	ok := make([]byte, 0, 2*10)
	for i := 0; i < 10; i++ {
		ok = append(ok, '[')
	}
	for i := 0; i < 10; i++ {
		ok = append(ok, ']')
	}
	if _, err := parseJSON(ok); err != nil {
		t.Fatalf("shallow nesting should parse, got %v", err)
	}
}

func FuzzJSONParseNeverPanics(f *testing.F) {
	seeds := []string{
		``,
		`   `,
		`null`, `true`, `false`,
		`0`, `-0`, `42`, `-17`, `3.14`, `1e10`, `2.5E-3`,
		`""`, `"hello"`, `"é"`, `"😀"`,
		`{}`, `[]`,
		`{"a":1,"b":[true,null,"x"]}`,
		`[1,2,3,{"k":"v"}]`,
		// Adversarial seeds: truncated, garbage, bad escapes, lone surrogates.
		`"abc`, `{"a":1`, `[1,2`, `01`, `foo`, `tru`, `-`, `1.`, `1e`,
		`"\uD83D"`, `"\uZZZZ"`, `"\x"`, `{"a" 1}`, `{1:2}`, `{} x`, `,`,
		"\"a\x01b\"",
	}
	for _, s := range seeds {
		f.Add([]byte(s))
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("parseJSON panicked on input % x: %v", data, r)
			}
		}()
		// Contract: either a value with nil error, or nil-ish value with a
		// non-nil error. Never a panic. We do not assert which — arbitrary bytes
		// may legitimately be valid JSON — only that the call returns.
		if v, err := parseJSON(data); err != nil && v != nil {
			t.Fatalf("error path must return nil value, got %#v (err %v)", v, err)
		}
	})
}
