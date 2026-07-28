package runtime

// # Why the hash is computed by the BROWSER, not by this package
//
// The body hash is produced with window.crypto.subtle.digest, not Go's
// crypto/sha256, and that choice is about payload size.
//
// This file compiles into the runtime that the CLI extracts into EVERY page and
// EVERY stateful component binary — 38 of them in the reference app alone. Linking
// a Go SHA-256 here therefore costs its bytes once per unit, whether or not that
// unit ever calls Fetch. Measured on the reference app, brotli (what travels):
//
//	crypto/sha256 imported directly ....... +37 KB per unit  (~1.4 MB total)
//	via the sigv4 package ................. +70 KB per unit  (~2.7 MB total)
//	window.crypto.subtle .................. 0
//
// The pages that grew included ones with no Fetch call at all, so tree-shaking
// does not rescue it. Paying a megabyte across the app to sign the small subset of
// requests that need it is the wrong trade, especially right after shrinking the
// static core from 797 KB to 246 KB.
//
// Two alternatives were rejected:
//
//   - Bridging to the static core, which already links sha256 for the htmx signer.
//     Free in bytes, but it routes every signed request through the ONE shared core
//     instance: two extra copies of the body across the WASM boundary, and a
//     synchronous re-entry into an instance that may be mid-task — the asyncify
//     re-entrancy hazard this project has already paid for once.
//   - A hand-written SHA-256 in gothic-core.js. Synchronous and small, but it would
//     make the framework own a crypto implementation for no gain over the one every
//     browser already ships.
//
// subtle.digest is async, which is why signing threads through a callback below
// rather than returning a value. That asynchrony is confined to the AWS path: when
// the provider marker is absent — every non-AWS app, and every local dev run —
// signing is skipped and the Fetch path is byte-identical to what it was before.
//
// Availability is not a practical concern: crypto.subtle requires a secure context,
// signing only happens when the provider is AWS (always https), and http://localhost
// is a secure context by specification. Verified returning the correct digest on
// Chromium, Firefox and WebKit.

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

// fetchBodyBytes returns the exact payload that will be handed to fetch(), which
// is what must be hashed. Only one of body/bodyBytes is ever populated
// (prepareFetch prefers the string). Returning nil means "no body".
//
// This is where Fetch is structurally safer than the htmx signer: that one has to
// REPRODUCE the body htmx will serialize, byte for byte, including JavaScript's
// encodeURIComponent semantics, and one wrong byte is a 403. Here the bytes hashed
// are the same values passed to fetch() a few lines later.
func fetchBodyBytes(body string, bodyBytes []byte) []byte {
	switch {
	case body != "":
		return []byte(body)
	case len(bodyBytes) > 0:
		return bodyBytes
	default:
		return nil
	}
}

// These two mirror sigv4.EmptyBodyHash and sigv4.ContentSha256Header and are
// deliberately NOT imported from there.
//
// Importing that package for two constants linked its whole contents into every
// page and component binary: +70 KB brotli each, ~2.7 MB across the reference app,
// even after the hashing itself had moved to the browser. TinyGo links at package
// granularity, so there is no way to reference one constant cheaply. Copying two
// literals is the entire cost of avoiding that.
//
// fetchsign_test.go asserts both stay equal to the sigv4 originals. That import is
// test-only, so it never reaches a shipped binary while still failing the build if
// the values ever diverge.
const (
	emptyBodyHash       = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	contentSha256Header = "x-amz-content-sha256"
)

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
