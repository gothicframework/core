package awssign

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

// sha256Hex is an INDEPENDENT reference digest for the tests — deliberately not
// calling ContentHashHex, so a bug there cannot mask itself.
func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func TestContentHashHex_Empty(t *testing.T) {
	if got := ContentHashHex(nil); got != EmptyBodyHash {
		t.Fatalf("ContentHashHex(nil) = %q, want EmptyBodyHash %q", got, EmptyBodyHash)
	}
	if got := ContentHashHex([][2]string{}); got != EmptyBodyHash {
		t.Fatalf("ContentHashHex(empty) = %q, want EmptyBodyHash %q", got, EmptyBodyHash)
	}
	// Sanity: EmptyBodyHash really is sha256("").
	if want := sha256Hex(""); EmptyBodyHash != want {
		t.Fatalf("EmptyBodyHash = %q, want sha256(\"\") = %q", EmptyBodyHash, want)
	}
}

func TestContentHashHex_SingleAndMultiParam(t *testing.T) {
	tests := []struct {
		name     string
		entries  [][2]string
		wantBody string
		wantHash string // computed independently (see comment / python cross-check)
	}{
		{
			name:     "single",
			entries:  [][2]string{{"a", "b"}},
			wantBody: "a=b",
			wantHash: "42144f3939c3ffbbf0bf8b1f12affb5c23a4c5bd41e0ff672d54a5754f062058",
		},
		{
			name:     "multi",
			entries:  [][2]string{{"a", "b"}, {"c", "d"}},
			wantBody: "a=b&c=d",
			wantHash: "703820ccaccb60bb7a1563d6e6736bcc261c8c061f8d8896103cccf6bfe41e33",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if body := EncodeBody(tc.entries); body != tc.wantBody {
				t.Fatalf("EncodeBody = %q, want %q", body, tc.wantBody)
			}
			// Digest must equal both the hardcoded known-answer AND an
			// independent recompute over the expected body bytes.
			if got := ContentHashHex(tc.entries); got != tc.wantHash {
				t.Fatalf("ContentHashHex = %q, want %q", got, tc.wantHash)
			}
			if got, want := ContentHashHex(tc.entries), sha256Hex(tc.wantBody); got != want {
				t.Fatalf("ContentHashHex = %q, want sha256(%q) = %q", got, tc.wantBody, want)
			}
		})
	}
}

func TestContentHashHex_KnownAnswerVector(t *testing.T) {
	// Realistic set with a space in a value: htmx serializes the space as %20.
	entries := [][2]string{{"key", "value"}, {"key2", "value with space"}}
	const wantBody = "key=value&key2=value%20with%20space"
	const wantHash = "30dc7722f658fbdff277b5794d220871433d92863d77059a58d972c04ad3943b"

	if body := EncodeBody(entries); body != wantBody {
		t.Fatalf("EncodeBody = %q, want %q", body, wantBody)
	}
	if got := ContentHashHex(entries); got != wantHash {
		t.Fatalf("ContentHashHex = %q, want %q", got, wantHash)
	}
	if got := ContentHashHex(entries); got != sha256Hex(wantBody) {
		t.Fatalf("ContentHashHex = %q, want sha256(%q) = %q", got, wantBody, sha256Hex(wantBody))
	}
}

func TestEncodeURIComponent_SpaceIsPercent20(t *testing.T) {
	if got := encodeURIComponent("a b"); got != "a%20b" {
		t.Fatalf("encodeURIComponent(%q) = %q, want %q (space must be %%20, never +)", "a b", got, "a%20b")
	}
	if got := encodeURIComponent(" "); got != "%20" {
		t.Fatalf("encodeURIComponent(space) = %q, want %%20", got)
	}
}

func TestEncodeURIComponent_LeavesUnreservedLiteral(t *testing.T) {
	// The full JS encodeURIComponent unreserved set plus alnum must pass through
	// byte-for-byte.
	unreserved := "ABCabc123-_.!~*'()"
	if got := encodeURIComponent(unreserved); got != unreserved {
		t.Fatalf("encodeURIComponent(%q) = %q, want it unchanged", unreserved, got)
	}
}

func TestEncodeURIComponent_PercentEncodesReserved(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"&", "%26"},
		{"=", "%3D"},
		{"+", "%2B"},
		{"/", "%2F"},
		{"?", "%3F"},
		{"#", "%23"},
		{"%", "%25"},
		{"@", "%40"},
		{" ", "%20"},
		{":", "%3A"},
		{",", "%2C"},
	}
	for _, tc := range tests {
		if got := encodeURIComponent(tc.in); got != tc.want {
			t.Errorf("encodeURIComponent(%q) = %q, want %q (uppercase hex)", tc.in, got, tc.want)
		}
	}
}

func TestEncodeURIComponent_UTF8PerByte(t *testing.T) {
	// 'é' is U+00E9 = UTF-8 bytes 0xC3 0xA9 → %C3%A9 (per-byte, uppercase hex).
	if got := encodeURIComponent("é"); got != "%C3%A9" {
		t.Fatalf("encodeURIComponent(%q) = %q, want %%C3%%A9", "é", got)
	}
	// A 3-byte char: U+20AC '€' = 0xE2 0x82 0xAC → %E2%82%AC.
	if got := encodeURIComponent("€"); got != "%E2%82%AC" {
		t.Fatalf("encodeURIComponent(%q) = %q, want %%E2%%82%%AC", "€", got)
	}
	// Cross-check the digest of a unicode body against an independent recompute.
	entries := [][2]string{{"name", "é"}}
	if got, want := ContentHashHex(entries), sha256Hex("name=%C3%A9"); got != want {
		t.Fatalf("ContentHashHex(unicode) = %q, want %q", got, want)
	}
}

func FuzzContentHashHexNeverPanics(f *testing.F) {
	f.Add("a", "b")
	f.Add("", "")
	f.Add("key with space", "value&=+/?#é€")
	f.Add("%%%", "\x00\x01\xff")
	f.Fuzz(func(t *testing.T, k, v string) {
		entries := [][2]string{{k, v}}
		got := ContentHashHex(entries)
		// A non-empty entry always yields a 64-char lowercase hex digest.
		if len(got) != 64 {
			t.Fatalf("ContentHashHex len = %d, want 64 (%q)", len(got), got)
		}
		// EncodeBody must be reproducible and hash-consistent.
		if want := sha256Hex(EncodeBody(entries)); got != want {
			t.Fatalf("ContentHashHex/EncodeBody inconsistent: %q vs %q", got, want)
		}
	})
}
