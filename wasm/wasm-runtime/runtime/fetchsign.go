package runtime

import "github.com/gothicframework/htmx-go/v2/ext/sigv4"

// fetchsign.go holds the PURE decision logic for signing a Fetch body on AWS.
// It carries NO build tag and no syscall/js dependency, so it compiles and is
// unit-tested on the host; the js/wasm caller lives in dom.go.
//
// # Why Fetch needs its own signer
//
// Gothic apps deployed on AWS sit behind CloudFront OAC (SigV4) → Lambda Function
// URL with authorization_type = AWS_IAM. Any request carrying a body must send
// x-amz-content-sha256 = sha256-hex(body) or the edge rejects it with HTTP 403.
//
// htmx-go covers its own requests through an in-path RequestTransformer
// (htmx-go/ext/sigv4). Fetch does NOT go through htmx — it calls the browser's
// fetch() directly — so it was never signed, and every POST/PUT/PATCH a component
// made against its own backend on AWS returned 403. The documented
// Encode[T] → POST → Decode[T] round-trip is exactly that shape.
//
// # Why Fetch is the SAFER of the two signing paths
//
// The htmx signer has to REPRODUCE the body htmx will serialize, byte for byte,
// including JavaScript's encodeURIComponent semantics — a mismatch of one byte is
// a 403. Fetch has no such problem: the bytes being hashed here are the same
// values handed to fetch() a few lines later in prepareFetch, so byte-identity is
// structural rather than reconstructed.

// shouldSignFetch reports whether a Fetch request must carry the AWS body-hash
// header. BOTH conditions are required and neither is a default:
//
//   - providerIsAWS: the server-rendered <meta name="gothic-provider" content="AWS">
//     marker. Off AWS there is no SigV4 check to satisfy, and sending the header
//     anyway would leak an AWS-shaped detail into every request.
//   - sameOrigin: the request targets the app's own origin, i.e. it will traverse
//     the CloudFront distribution that performs the check. A Fetch to a THIRD-PARTY
//     API must never carry x-amz-content-sha256 — that header is meaningless to
//     them at best, and at worst it is our request shape leaking to another vendor.
func shouldSignFetch(providerIsAWS, sameOrigin bool) bool {
	return providerIsAWS && sameOrigin
}

// fetchBodyHash returns the x-amz-content-sha256 value for a Fetch body.
//
// Only one of body/bodyBytes is ever populated (prepareFetch prefers the string).
// An absent body hashes to the empty-body constant rather than being skipped: the
// htmx signer sets the header on bodyless GET/DELETE too, and matching it keeps a
// single observable contract across both paths.
//
// There is no multipart escape hatch here, unlike the htmx signer. That sentinel
// exists because the BROWSER builds a multipart body with a random boundary the
// signer cannot reproduce; a caller who assembles multipart bytes by hand and
// passes them as BodyBytes has already given us the exact payload, so it hashes
// like any other body.
func fetchBodyHash(body string, bodyBytes []byte) string {
	switch {
	case body != "":
		return sigv4.Sha256Hex(body)
	case len(bodyBytes) > 0:
		return sigv4.Sha256Hex(string(bodyBytes))
	default:
		return sigv4.EmptyBodyHash
	}
}

// sameOriginURL reports whether target resolves to pageOrigin.
//
// pageOrigin is location.origin, e.g. "https://app.example.com" — scheme, host and
// (when non-default) port, never a trailing slash.
//
// Relative forms ("/api/x", "api/x", "?q=1", "#frag") always resolve to the page's
// own origin. Absolute and protocol-relative forms are compared by origin prefix,
// with the following character required to be a boundary ("/", "?", "#", or end)
// so that "https://app.example.com.evil.test" is NOT treated as same-origin as
// "https://app.example.com". Anything else (data:, blob:, mailto:) is not signed.
func sameOriginURL(target, pageOrigin string) bool {
	if target == "" {
		return true
	}

	// Protocol-relative: //host/path inherits the page's scheme.
	if hasPrefix(target, "//") {
		if pageOrigin == "" {
			return false
		}
		scheme := pageOrigin
		if i := indexOf(scheme, "://"); i >= 0 {
			scheme = scheme[:i+1] // "https:"
		} else {
			return false
		}
		return originMatches(scheme+target, pageOrigin)
	}

	// Any other scheme-bearing URL must match the origin exactly. Without a known
	// origin there is nothing to compare against, so it is not signed: an absolute
	// URL is the only form that CAN point at a third party.
	if indexOf(target, "://") >= 0 {
		if pageOrigin == "" {
			return false
		}
		return originMatches(target, pageOrigin)
	}
	// A bare scheme with no authority (data:, blob:, mailto:) is never our origin.
	if i := indexOf(target, ":"); i >= 0 && indexOf(target[:i], "/") < 0 {
		return false
	}

	// Relative path, query or fragment: same origin by construction.
	return true
}

// originMatches compares an absolute URL against an origin, requiring the next
// character after the origin to be a path/query/fragment boundary.
func originMatches(absolute, origin string) bool {
	if !hasPrefix(absolute, origin) {
		return false
	}
	rest := absolute[len(origin):]
	if rest == "" {
		return true
	}
	switch rest[0] {
	case '/', '?', '#':
		return true
	default:
		return false
	}
}

// hasPrefix and indexOf avoid importing strings here purely to keep this file's
// dependency surface to the sigv4 hash helper, which is what the WASM binary pays
// for on every page.
func hasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}

func indexOf(s, sub string) int {
	if len(sub) == 0 || len(s) < len(sub) {
		return -1
	}
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
