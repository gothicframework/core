// Package awssign holds the PURE, host-testable body-hashing logic for the
// Gothic full-Go static core's AWS request signer (CloudFront OAC / SigV4). It
// has NO syscall/js dependency, so it compiles and runs under the standard host
// toolchain and can be unit-tested without a WASM runtime. The core's main
// package (wasm/core-runtime, //go:build js && wasm) is a thin js.Value adapter
// that lifts the htmx configRequest parameter set into [][2]string and calls
// into here to compute the x-amz-content-sha256 header value.
//
// # Why this exists
//
// Gothic apps on AWS run behind CloudFront OAC (SigV4) → Lambda Function URL
// with authorization_type = AWS_IAM. Every htmx request that carries a body must
// send header x-amz-content-sha256 = sha256-hex(request body) or CloudFront's
// SigV4 body check rejects the request with HTTP 403. This package reproduces —
// byte-for-byte — the application/x-www-form-urlencoded body htmx 2.0.3 builds,
// so the hash we sign matches the bytes htmx actually sends.
//
// # Byte-exactness is the whole game
//
// htmx serializes the body with its urlEncode helper: for each FormData entry
// (in insertion order) it emits encodeURIComponent(key) + "=" +
// encodeURIComponent(value), joined by "&". encodeURIComponent here means the
// JAVASCRIPT function, NOT Go's net/url — url.QueryEscape encodes space as "+"
// and escapes ! * ' ( ), both of which would change the bytes → wrong SHA-256 →
// HTTP 403. EncodeBody + encodeURIComponent below match JS encodeURIComponent
// exactly (space → %20, unreserved set A-Z a-z 0-9 - _ . ! ~ * ' ( ) passes
// through literally, every other UTF-8 byte → %XX uppercase).
package awssign

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// EmptyBodyHash is sha256("") in lowercase hex — the value to send when the
// request has no body (a GET/DELETE with its params in the URL, or a POST with
// no parameters). Hardcoded so the common empty-body path never hashes.
const EmptyBodyHash = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

// upperHex is the uppercase hex alphabet used for percent-encoding. JavaScript's
// encodeURIComponent emits UPPERCASE hex (%20, %C3), so we must too — lowercase
// would change the body bytes and break the signature.
const upperHex = "0123456789ABCDEF"

// isUnreserved reports whether a byte is in JavaScript encodeURIComponent's
// unreserved set — the bytes that pass through literally: A-Z a-z 0-9 and the
// punctuation - _ . ! ~ * ' ( ). Every other byte is percent-encoded.
func isUnreserved(b byte) bool {
	switch {
	case b >= 'A' && b <= 'Z':
		return true
	case b >= 'a' && b <= 'z':
		return true
	case b >= '0' && b <= '9':
		return true
	}
	switch b {
	case '-', '_', '.', '!', '~', '*', '\'', '(', ')':
		return true
	}
	return false
}

// encodeURIComponent replicates JavaScript's encodeURIComponent EXACTLY — it is
// deliberately NOT net/url. Every byte of s's UTF-8 encoding either passes
// through literally (the unreserved set) or becomes %XX with UPPERCASE hex.
// Space → %20 (never "+"). Because a Go string is already UTF-8 bytes, iterating
// byte-by-byte is inherently per-UTF-8-byte encoding: e.g. 'é' (0xC3 0xA9) →
// "%C3%A9".
func encodeURIComponent(s string) string {
	var sb strings.Builder
	sb.Grow(len(s))
	for i := 0; i < len(s); i++ {
		b := s[i]
		if isUnreserved(b) {
			sb.WriteByte(b)
			continue
		}
		sb.WriteByte('%')
		sb.WriteByte(upperHex[b>>4])
		sb.WriteByte(upperHex[b&0x0f])
	}
	return sb.String()
}

// EncodeBody builds the application/x-www-form-urlencoded body EXACTLY as htmx
// 2.0.3 does: for each [key, value] pair in the given order,
// encodeURIComponent(key) + "=" + encodeURIComponent(value), joined by "&". The
// pair order MUST match htmx's FormData insertion order (the caller iterates
// e.detail.parameters via forEach to preserve it). Split out from the hash so
// tests can assert the exact body bytes independently of the digest.
func EncodeBody(entries [][2]string) string {
	var sb strings.Builder
	for i := range entries {
		if i > 0 {
			sb.WriteByte('&')
		}
		sb.WriteString(encodeURIComponent(entries[i][0]))
		sb.WriteByte('=')
		sb.WriteString(encodeURIComponent(entries[i][1]))
	}
	return sb.String()
}

// ContentHashHex returns the x-amz-content-sha256 header value for a urlencoded
// body built from entries: sha256 of EncodeBody(entries) as lowercase hex. When
// entries is empty the body is empty, so it returns the precomputed
// EmptyBodyHash without hashing. crypto/sha256 is synchronous, so the WASM
// adapter can call this inline in the htmx:configRequest listener.
func ContentHashHex(entries [][2]string) string {
	if len(entries) == 0 {
		return EmptyBodyHash
	}
	sum := sha256.Sum256([]byte(EncodeBody(entries)))
	return hex.EncodeToString(sum[:])
}
