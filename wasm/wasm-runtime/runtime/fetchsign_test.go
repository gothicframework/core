package runtime

import (
	"testing"

	"github.com/gothicframework/htmx-go/v2/ext/sigv4"
)

func TestShouldSignFetch(t *testing.T) {
	cases := []struct {
		name       string
		aws, same  bool
		want       bool
		wantReason string
	}{
		{"on AWS, own backend", true, true, true, "the CloudFront SigV4 check is in the path"},
		{"on AWS, third party", true, false, false, "our body hash must not leak to another vendor"},
		{"off AWS, own backend", false, true, false, "no SigV4 check exists to satisfy"},
		{"off AWS, third party", false, false, false, "neither condition holds"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := shouldSignFetch(c.aws, c.same); got != c.want {
				t.Errorf("shouldSignFetch(%v,%v) = %v, want %v — %s", c.aws, c.same, got, c.want, c.wantReason)
			}
		})
	}
}

func TestFetchBodyHashMatchesTheBytesSent(t *testing.T) {
	// The whole point of signing at this layer: the hash covers exactly the value
	// handed to fetch(), so it cannot drift from the wire the way a reconstructed
	// urlencoded body can.
	const jsonBody = `{"message":"round trip","count":99}`

	if got, want := fetchBodyHash(jsonBody, nil), sigv4.Sha256Hex(jsonBody); got != want {
		t.Errorf("string body hash = %q, want %q", got, want)
	}
	if got, want := fetchBodyHash("", []byte(jsonBody)), sigv4.Sha256Hex(jsonBody); got != want {
		t.Errorf("byte body hash = %q, want %q", got, want)
	}
	if got := fetchBodyHash("", nil); got != sigv4.EmptyBodyHash {
		t.Errorf("empty body hash = %q, want the empty-body constant", got)
	}
	// A string body and the identical bytes must agree — callers may use either.
	if fetchBodyHash(jsonBody, nil) != fetchBodyHash("", []byte(jsonBody)) {
		t.Error("string and []byte forms of the same body must hash identically")
	}
}

func TestSameOriginURL(t *testing.T) {
	const origin = "https://app.example.com"

	cases := []struct {
		target string
		want   bool
		why    string
	}{
		{"/api/jsonEchoPost", true, "absolute path is our own origin"},
		{"api/x", true, "relative path is our own origin"},
		{"?q=1", true, "bare query resolves against this page"},
		{"#frag", true, "bare fragment resolves against this page"},
		{"", true, "empty target is this page"},
		{"https://app.example.com/api/x", true, "explicit same origin"},
		{"https://app.example.com", true, "origin with no path"},
		{"https://app.example.com?q=1", true, "origin followed by a query"},
		{"//app.example.com/api/x", true, "protocol-relative, same host"},

		{"https://api.stripe.com/v1/charges", false, "third-party API must stay unsigned"},
		{"http://app.example.com/api/x", false, "scheme differs, so the origin differs"},
		{"https://app.example.com.evil.test/x", false, "prefix match without a boundary is NOT our origin"},
		{"https://evil.test/app.example.com", false, "our origin appearing in a path proves nothing"},
		{"//evil.test/x", false, "protocol-relative to another host"},
		{"data:text/plain,hi", false, "data: has no origin to sign for"},
		{"blob:https://app.example.com/abc", false, "blob: is not an http origin"},
		{"mailto:a@b.c", false, "mailto: is not an http origin"},
	}
	for _, c := range cases {
		t.Run(c.target, func(t *testing.T) {
			if got := sameOriginURL(c.target, origin); got != c.want {
				t.Errorf("sameOriginURL(%q, %q) = %v, want %v — %s", c.target, origin, got, c.want, c.why)
			}
		})
	}
}

// TestSameOriginURLWithoutPageOriginFailsClosed pins the failure direction: if
// location.origin cannot be read, nothing counts as same-origin, so nothing is
// signed. Sending a wrong hash is a 403; sending none to a third party is a leak.
// Signing nothing is the only harmless outcome.
func TestSameOriginURLFailsClosedWithoutOrigin(t *testing.T) {
	for _, target := range []string{"https://app.example.com/x", "//app.example.com/x"} {
		if sameOriginURL(target, "") {
			t.Errorf("sameOriginURL(%q, \"\") = true, want false when the page origin is unknown", target)
		}
	}
	// A relative URL still targets this page whatever its origin string is.
	if !sameOriginURL("/api/x", "") {
		t.Error("a relative URL always targets this page")
	}
}
